package hub

import (
	"net/http"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

const maxQueuedAttachments = 24 << 20

type queueAddRequest struct {
	Text        string                  `json:"text"`
	Options     *protocol.PromptOptions `json:"options,omitempty"`
	ScheduledAt string                  `json:"scheduled_at,omitempty"`
}

type queueEditRequest struct {
	Text        string `json:"text"`
	ScheduledAt string `json:"scheduled_at,omitempty"`
}

type queueOrderRequest struct {
	IDs []string `json:"ids"`
}

type queuePauseRequest struct {
	Paused bool `json:"paused"`
}

func validQueuedPrompt(text string, options *protocol.PromptOptions) string {
	if strings.TrimSpace(text) == "" {
		return "prompt text is required"
	}
	if len(text) > maxPromptBytes {
		return "prompt is too large"
	}
	if options != nil {
		if len(options.Attachments) > 6 {
			return "at most 6 attachments are allowed"
		}
		total := 0
		for _, attachment := range options.Attachments {
			total += len(attachment.Data)
		}
		if total > maxQueuedAttachments {
			return "attachments are too large"
		}
	}
	return ""
}

func parseQueueSchedule(value string) (*time.Time, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, ""
	}
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, "scheduled_at must be an RFC 3339 timestamp"
	}
	now := time.Now()
	if at.After(now.Add(365 * 24 * time.Hour)) {
		return nil, "scheduled_at must be within one year"
	}
	return &at, ""
}

// queueAccess loads the run, session and user and enforces ownership,
// structured mode, resume permission and (for mutations) the controller lease.
func (s *Server) queueAccess(w http.ResponseWriter, r *http.Request, needLease bool) (User, protocol.RunView, protocol.SessionView, bool) {
	u := userFrom(r)
	run, startedBy, err := s.store.RunByID(r.PathValue("id"))
	if err != nil || (startedBy != u.ID && !isAdminRole(u.Role)) {
		writeError(w, http.StatusNotFound, "run not found")
		return u, run, protocol.SessionView{}, false
	}
	if run.Mode != protocol.ModeStructured {
		writeError(w, http.StatusBadRequest, "prompt queues are only available in structured mode")
		return u, run, protocol.SessionView{}, false
	}
	sess, err := s.store.SessionByID(u.ID, run.SessionID, isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return u, run, sess, false
	}
	if !s.sessionPermission(u, sess, "resume") {
		writeError(w, http.StatusForbidden, "resume permission required")
		return u, run, sess, false
	}
	if needLease {
		if !s.hostPermission(u, sess.HostID, "terminal_input") {
			writeError(w, http.StatusForbidden, "terminal_input permission required")
			return u, run, sess, false
		}
		device := deviceFrom(r)
		if run.LeaseDeviceID == "" || run.LeaseDeviceID != device.ID {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "you do not hold the controller lease", "reason": "lease_required",
			})
			return u, run, sess, false
		}
	}
	return u, run, sess, true
}

func (s *Server) handleGetPromptQueue(w http.ResponseWriter, r *http.Request) {
	_, run, _, ok := s.queueAccess(w, r, false)
	if !ok {
		return
	}
	items, err := s.store.ListPromptQueue(run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	paused, _ := s.store.PromptQueuePaused(run.ID)
	writeJSON(w, http.StatusOK, protocol.PromptQueue{Items: items, Paused: paused})
}

func (s *Server) handleAddPromptQueue(w http.ResponseWriter, r *http.Request) {
	u, run, _, ok := s.queueAccess(w, r, true)
	if !ok {
		return
	}
	var req queueAddRequest
	if !s.decodeSignedJSON(w, r, &req) {
		return
	}
	if msg := validQueuedPrompt(req.Text, req.Options); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if s.store.CountQueuedPrompts(run.ID) >= 100 {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "prompt queue is full", "reason": "queue_limit"})
		return
	}
	scheduled, msg := parseQueueSchedule(req.ScheduledAt)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	item, err := s.store.EnqueuePrompt(run.ID, u.ID, deviceFrom(r).ID, strings.TrimSpace(req.Text), req.Options, scheduled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "prompt_queue.enqueued", map[string]string{"run_id": run.ID, "item_id": item.ID})
	s.broadcast(protocol.ServerEnvelope{Type: "queue.changed", RunID: run.ID})
	s.armQueue(run.ID)
	s.kickQueue()
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleEditPromptQueue(w http.ResponseWriter, r *http.Request) {
	u, run, _, ok := s.queueAccess(w, r, true)
	if !ok {
		return
	}
	var req queueEditRequest
	if !s.decodeSignedJSON(w, r, &req) {
		return
	}
	if msg := validQueuedPrompt(req.Text, nil); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	scheduled, msg := parseQueueSchedule(req.ScheduledAt)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	itemID := r.PathValue("itemID")
	if err := s.store.UpdatePromptQueueItem(run.ID, itemID, strings.TrimSpace(req.Text), scheduled); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(u.Username, "prompt_queue.edited", map[string]string{"run_id": run.ID, "item_id": itemID})
	s.broadcast(protocol.ServerEnvelope{Type: "queue.changed", RunID: run.ID})
	s.armQueue(run.ID)
	s.kickQueue()
	item, _ := s.store.PromptQueueItem(itemID)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleDeletePromptQueue(w http.ResponseWriter, r *http.Request) {
	u, run, _, ok := s.queueAccess(w, r, true)
	if !ok {
		return
	}
	itemID := r.PathValue("itemID")
	if err := s.store.DeletePromptQueueItem(run.ID, itemID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(u.Username, "prompt_queue.removed", map[string]string{"run_id": run.ID, "item_id": itemID})
	s.broadcast(protocol.ServerEnvelope{Type: "queue.changed", RunID: run.ID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) handleReorderPromptQueue(w http.ResponseWriter, r *http.Request) {
	u, run, _, ok := s.queueAccess(w, r, true)
	if !ok {
		return
	}
	var req queueOrderRequest
	if !s.decodeSignedJSON(w, r, &req) {
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids are required")
		return
	}
	if err := s.store.ReorderPromptQueue(run.ID, req.IDs); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(u.Username, "prompt_queue.reordered", map[string]string{"run_id": run.ID})
	s.broadcast(protocol.ServerEnvelope{Type: "queue.changed", RunID: run.ID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "reordered"})
}

func (s *Server) handlePausePromptQueue(w http.ResponseWriter, r *http.Request) {
	u, run, _, ok := s.queueAccess(w, r, true)
	if !ok {
		return
	}
	var req queuePauseRequest
	if !s.decodeSignedJSON(w, r, &req) {
		return
	}
	if err := s.store.SetPromptQueuePaused(run.ID, req.Paused); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	action := "prompt_queue.resumed"
	if req.Paused {
		action = "prompt_queue.paused"
	}
	s.audit(u.Username, action, map[string]string{"run_id": run.ID})
	s.broadcast(protocol.ServerEnvelope{Type: "queue.changed", RunID: run.ID})
	if !req.Paused {
		s.armQueue(run.ID)
		s.kickQueue()
	}
	writeJSON(w, http.StatusOK, map[string]any{"paused": req.Paused})
}
