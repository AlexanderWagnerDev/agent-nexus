package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	q := r.URL.Query()
	limit := atoiOr(q.Get("limit"), 100)
	if limit < 1 {
		limit = 100
	}
	if limit > 2000 {
		limit = 2000
	}
	offset := atoiOr(q.Get("offset"), 0)
	filter := SessionFilter{
		HostID: q.Get("host"), Agent: q.Get("agent"), Project: q.Get("project"), RepoURL: q.Get("repo"),
		State: q.Get("state"), Query: q.Get("q"), Tag: normalizeTag(q.Get("tag")),
		Sort: q.Get("sort"), Order: q.Get("order"), Limit: limit + 1, Offset: offset,
	}
	if raw := strings.TrimSpace(q.Get("since")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "since must be an RFC 3339 timestamp")
			return
		}
		filter.Since = t
	}
	if raw := strings.TrimSpace(q.Get("until")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "until must be an RFC 3339 timestamp")
			return
		}
		filter.Until = t
	}
	switch filter.Sort {
	case "", "updated", "created", "title", "messages":
	default:
		writeError(w, http.StatusBadRequest, "sort must be updated, created, title or messages")
		return
	}
	switch filter.Order {
	case "", "asc", "desc":
	default:
		writeError(w, http.StatusBadRequest, "order must be asc or desc")
		return
	}
	sessions, err := s.store.ListSessionsFiltered(u.ID, filter, isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	total, err := s.store.CountSessionsFiltered(u.ID, filter, isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hasMore := len(sessions) > limit
	if hasMore {
		sessions = sessions[:limit]
	}
	if sessions == nil {
		sessions = []protocol.SessionView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": sessions, "has_more": hasMore, "offset": offset, "total": total,
	})
}

// validTag keeps tags short, searchable and safe in a URL query.
func normalizeTag(raw string) string {
	tag := strings.ToLower(strings.TrimSpace(raw))
	if tag == "" {
		return ""
	}
	if len(tag) > 32 {
		tag = tag[:32]
	}
	for _, r := range tag {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	return tag
}

func (s *Server) handleSessionTags(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	tags, err := s.store.SessionTags(u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

func (s *Server) handleSessionMetadata(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "view") {
		writeError(w, http.StatusForbidden, "view permission required")
		return
	}
	var req struct {
		LocalTitle *string `json:"local_title"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.LocalTitle == nil {
		writeError(w, http.StatusBadRequest, "local_title is required")
		return
	}
	title := strings.TrimSpace(*req.LocalTitle)
	if len(title) > 200 {
		writeError(w, http.StatusBadRequest, "local title must be at most 200 characters")
		return
	}
	if err := s.store.SetSessionLocalTitle(u.ID, sess.ID, title); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "session.local_title", map[string]string{"session_id": sess.ID})
	local, tags := s.store.SessionMetadata(u.ID, sess.ID)
	writeJSON(w, http.StatusOK, map[string]any{"local_title": local, "tags": tags})
}

func (s *Server) handleAddSessionTag(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "view") {
		writeError(w, http.StatusForbidden, "view permission required")
		return
	}
	var req struct {
		Tag string `json:"tag"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	tag := normalizeTag(req.Tag)
	if tag == "" {
		writeError(w, http.StatusBadRequest, "tag must be 1-32 lowercase letters, digits, - or _")
		return
	}
	_, currentTags := s.store.SessionMetadata(u.ID, sess.ID)
	if len(currentTags) >= 10 && !contains(currentTags, tag) {
		writeError(w, http.StatusBadRequest, "at most 10 tags per session")
		return
	}
	if err := s.store.AddSessionTag(u.ID, sess.ID, tag); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "session.tag_added", map[string]string{"session_id": sess.ID, "tag": tag})
	_, tags := s.store.SessionMetadata(u.ID, sess.ID)
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

func (s *Server) handleDeleteSessionTag(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "view") {
		writeError(w, http.StatusForbidden, "view permission required")
		return
	}
	tag := normalizeTag(r.PathValue("tag"))
	if tag == "" {
		writeError(w, http.StatusBadRequest, "invalid tag")
		return
	}
	if err := s.store.DeleteSessionTag(u.ID, sess.ID, tag); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "session.tag_removed", map[string]string{"session_id": sess.ID, "tag": tag})
	_, tags := s.store.SessionMetadata(u.ID, sess.ID)
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "view") {
		writeError(w, http.StatusForbidden, "view permission required")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleSessionRelated(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "view") {
		writeError(w, http.StatusForbidden, "view permission required")
		return
	}
	all := isAdminRole(u.Role)
	var parent *protocol.SessionView
	if sess.ForkedFromNativeID != "" || sess.ParentNativeID != "" {
		native := sess.ForkedFromNativeID
		if native == "" {
			native = sess.ParentNativeID
		}
		if p, err := s.store.SessionByNativeID(u.ID, sess.HostID, sess.Agent, native, all); err == nil {
			parent = &p
		}
	}
	forks, _ := s.store.ForkedSessions(u.ID, sess.HostID, sess.Agent, sess.NativeID, all)
	children, _ := s.store.ChildSessions(u.ID, sess.HostID, sess.Agent, sess.NativeID, all)
	writeJSON(w, http.StatusOK, map[string]any{
		"parent": parent, "forks": forks, "children": children,
	})
}

func (s *Server) handleSessionUsage(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "view") {
		writeError(w, http.StatusForbidden, "view permission required")
		return
	}
	usage, err := s.store.SessionUsage(sess.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

func (s *Server) handleTranscript(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "read_transcript") {
		writeError(w, http.StatusForbidden, "read_transcript permission required")
		return
	}
	if !s.hostOnline(sess.HostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	offset := atoiOr(r.URL.Query().Get("offset"), 0)
	limit := atoiOr(r.URL.Query().Get("limit"), 100)
	if limit > 500 {
		limit = 500
	}
	params, _ := json.Marshal(map[string]any{"native_id": sess.NativeID, "agent": sess.Agent, "offset": offset, "limit": limit})
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := s.hostRequest(ctx, sess.HostID, "transcript", params)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var tr protocol.Transcript
	if err := json.Unmarshal(result, &tr); err != nil {
		writeError(w, http.StatusBadGateway, "invalid host response")
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (s *Server) handleSessionGit(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "read_transcript") {
		writeError(w, http.StatusForbidden, "read_transcript permission required")
		return
	}
	if !s.hostOnline(sess.HostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	params, _ := json.Marshal(map[string]any{"cwd": sess.Cwd, "stat_only": r.URL.Query().Get("stat") == "1"})
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	raw, err := s.hostRequest(ctx, sess.HostID, "git", params)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var view map[string]any
	if err := json.Unmarshal(raw, &view); err != nil {
		writeError(w, http.StatusBadGateway, "invalid host response")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) < 2 || len(query) > 200 {
		writeError(w, http.StatusBadRequest, "query must be 2-200 characters")
		return
	}
	hosts, err := s.store.ListHosts(u.ID, isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type result struct {
		hits   []protocol.SearchHit
		host   string
		hostID string
		failed bool
	}
	ch := make(chan result, len(hosts))
	indexStatus := map[string]protocol.IndexStatus{}
	// A host is "not searched" when it could not be queried: offline, not
	// readable, or the request failed. A successful query with zero matches is
	// a valid empty result and must not be reported as skipped.
	notSearched := map[string]bool{}
	online := 0
	for _, h := range hosts {
		if status := s.hostIndexStatus(h.ID); status != nil {
			indexStatus[h.Name] = *status
		}
		if !s.hostPermission(u, h.ID, "read_transcript") {
			notSearched[h.Name] = true
			continue
		}
		if !s.hostOnline(h.ID) {
			notSearched[h.Name] = true
			continue
		}
		online++
		go func(hostID, hostName string) {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			params, _ := json.Marshal(map[string]any{"query": query, "limit": 50})
			raw, err := s.hostRequest(ctx, hostID, "search", params)
			if err != nil {
				ch <- result{host: hostName, hostID: hostID, failed: true}
				return
			}
			var hits []protocol.SearchHit
			if err := json.Unmarshal(raw, &hits); err != nil {
				ch <- result{host: hostName, hostID: hostID, failed: true}
				return
			}
			ch <- result{hits: hits, host: hostName, hostID: hostID}
		}(h.ID, h.Name)
	}
	all := []protocol.SearchHit{}
	seen := map[string]bool{}
	for i := 0; i < online; i++ {
		res := <-ch
		if res.failed {
			notSearched[res.host] = true
			continue
		}
		for _, hit := range res.hits {
			key := res.hostID + "|" + hit.Agent + "|" + hit.NativeID
			if seen[key] {
				continue
			}
			seen[key] = true
			hit.HostID = res.hostID
			hit.HostName = res.host
			hit.Title = displayTitle(hit.Title)
			hit.Snippet = strings.TrimSpace(hit.Snippet)
			if id, err := s.store.SessionIDForNative(res.hostID, hit.Agent, hit.NativeID); err == nil {
				hit.SessionID = id
			}
			all = append(all, hit)
		}
	}
	if len(all) > 100 {
		all = all[:100]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results": all, "not_searched": notSearched, "hosts_total": len(hosts), "index_status": indexStatus,
	})
}

// ---- runs ----

type runRequest struct {
	HostID string `json:"host_id"`
	Agent  string `json:"agent"`
	Cwd    string `json:"cwd"`
	Mode   string `json:"mode"`
	Fork   bool   `json:"fork"`
	Force  bool   `json:"force"`
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
}

func stepUpValid(r *http.Request) bool {
	until, _ := r.Context().Value(ctxStepUp).(time.Time)
	return time.Now().Before(until)
}

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "resume") {
		writeError(w, http.StatusForbidden, "resume permission required")
		return
	}
	if host, err := s.store.HostByID(sess.HostID); err != nil || host.DisabledAt != nil {
		writeError(w, http.StatusConflict, "host is disabled")
		return
	}
	if !s.hostOnline(sess.HostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	var req runRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if runID, active := s.store.ActiveRunForSession(sess.ID); active {
		// The UI offers "Attach" for this reason; resuming would create a
		// second writer on the same native session.
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":  "session is already active",
			"reason": "active_run",
			"run_id": runID,
		})
		return
	}
	if sess.ActiveElsewhere && !req.Fork && !req.Force {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":  "session is open in a native terminal",
			"reason": "active_elsewhere",
		})
		return
	}
	if req.Force && !req.Fork {
		if !stepUpValid(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error": "step-up required", "step_up_required": true,
			})
			return
		}
	}
	if req.Fork && !s.sessionPermission(u, sess, "fork") {
		writeError(w, http.StatusForbidden, "fork permission required")
		return
	}
	if req.Mode == "" {
		req.Mode = protocol.ModeTerminal
	}
	if req.Mode != protocol.ModeTerminal && req.Mode != protocol.ModeStructured {
		writeError(w, http.StatusBadRequest, "unknown run mode")
		return
	}
	if req.Cols <= 0 {
		req.Cols, req.Rows = 120, 30
	}
	action := protocol.ActionResume
	if req.Fork {
		action = protocol.ActionFork
	}
	run, err := s.startRun(u, deviceFrom(r), sess, action, req.Cwd, req.Mode, req.Cols, req.Rows, req.Force)
	if err != nil {
		writeRunStartError(w, err)
		return
	}
	if req.Force {
		s.audit(u.Username, "run.force_resume", map[string]string{
			"run_id": run.ID, "host_id": sess.HostID, "session_id": sess.ID,
			"reason": "session reported active in a native terminal",
		})
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleNewRun(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	hostID := r.PathValue("id")
	host, err := s.store.HostByID(hostID)
	if err != nil || host.RevokedAt != nil {
		writeError(w, http.StatusNotFound, "host not found")
		return
	}
	if host.DisabledAt != nil {
		writeError(w, http.StatusConflict, "host is disabled")
		return
	}
	if !s.hostPermission(u, hostID, "create") {
		writeError(w, http.StatusForbidden, "create permission required")
		return
	}
	if !s.hostOnline(hostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	var req runRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Agent == "" || req.Cwd == "" {
		writeError(w, http.StatusBadRequest, "agent and cwd are required")
		return
	}
	if req.Mode == "" {
		req.Mode = protocol.ModeTerminal
	}
	if req.Mode != protocol.ModeTerminal && req.Mode != protocol.ModeStructured {
		writeError(w, http.StatusBadRequest, "unknown run mode")
		return
	}
	if req.Cols <= 0 {
		req.Cols, req.Rows = 120, 30
	}
	run, err := s.startRun(u, deviceFrom(r), protocol.SessionView{HostID: hostID, Agent: req.Agent}, protocol.ActionNew, req.Cwd, req.Mode, req.Cols, req.Rows, false)
	if err != nil {
		writeRunStartError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) startRun(u User, d Device, sess protocol.SessionView, action, cwd, mode string, cols, rows int, force bool) (protocol.RunView, error) {
	id := newID()
	if cwd == "" {
		cwd = sess.Cwd
	}
	if cwd == "" && action != protocol.ActionNew {
		return protocol.RunView{}, errors.New("session has no working directory")
	}
	if err := s.checkRunLimits(u.ID, sess.HostID, cwd); err != nil {
		return protocol.RunView{}, err
	}
	if err := s.store.CreateRunAt(id, sess.ID, sess.HostID, cwd, mode, u.ID, d.ID); err != nil {
		return protocol.RunView{}, err
	}
	spec := protocol.RunSpec{
		RunID: id, SessionID: sess.ID, Agent: sess.Agent, NativeID: sess.NativeID,
		Cwd: cwd, Action: action, Mode: mode, Cols: cols, Rows: rows, Force: force,
	}
	s.mu.Lock()
	s.runHosts[id] = sess.HostID
	s.mu.Unlock()
	if err := s.sendHost(sess.HostID, protocol.Envelope{Type: "run.start", Run: &spec}); err != nil {
		s.store.UpdateRunState(id, protocol.StateFailed)
		return protocol.RunView{}, err
	}
	s.setRunState(id, protocol.StateStarting)
	s.audit(u.Username, "run.started", map[string]string{"run_id": id, "host_id": sess.HostID, "agent": sess.Agent, "action": action, "mode": mode})
	run, _, _ := s.store.RunByID(id)
	return run, nil
}

func (s *Server) handleActiveRuns(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	runs, err := s.store.ActiveRunsForUser(u.ID, isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []protocol.RunView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	run, startedBy, err := s.store.RunByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if startedBy != u.ID && !isAdminRole(u.Role) {
		writeError(w, http.StatusForbidden, "not your run")
		return
	}
	run.LastEventSeq = s.runEventCursor(run.ID)
	// A finished or stopping run cannot answer an approval anymore. Do not
	// resurrect a stale approval card after a reload.
	if !terminalRunState(run.State) && run.State != protocol.StateStopping {
		run.PendingApproval = s.pendingApproval(run.ID)
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleRunOptions(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	run, startedBy, err := s.store.RunByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if startedBy != u.ID && !isAdminRole(u.Role) {
		writeError(w, http.StatusForbidden, "not your run")
		return
	}
	if !s.hostOnline(run.HostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	params, _ := json.Marshal(map[string]string{"run_id": run.ID})
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	raw, err := s.hostRequest(ctx, run.HostID, "run.options", params)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var options protocol.RunOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		writeError(w, http.StatusBadGateway, "invalid host response")
		return
	}
	writeJSON(w, http.StatusOK, options)
}

func (s *Server) handleStopRun(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	run, startedBy, err := s.store.RunByID(r.PathValue("id"))
	if err != nil || (startedBy != u.ID && !isAdminRole(u.Role)) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if !s.verifyHTTPDeviceSignature(w, r, deviceFrom(r).ID, nil) {
		return
	}
	if err := s.sendHost(run.HostID, protocol.Envelope{Type: "run.stop", RunID: run.ID}); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.setRunState(run.ID, protocol.StateStopping)
	// Stop invalidates any approval that is still waiting.
	s.metrics.approvalsEnded(run.ID)
	s.store.FailPendingPromptQueue(run.ID, "run was stopped before the queued prompt was sent")
	s.disarmQueue(run.ID)
	s.audit(u.Username, "run.stopped", map[string]string{"run_id": run.ID})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "stopping"})
}

func (s *Server) handleLease(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	run, startedBy, err := s.store.RunByID(r.PathValue("id"))
	if err != nil || (startedBy != u.ID && !isAdminRole(u.Role)) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	d := deviceFrom(r)
	run.LeaseDeviceID = d.ID
	_, err = s.store.db.Exec(`UPDATE runs SET lease_device_id=? WHERE id=?`, d.ID, run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.broadcast(protocol.ServerEnvelope{Type: "run.lease", RunID: run.ID, Message: d.Name})
	s.audit(u.Username, "run.lease_acquired", map[string]string{"run_id": run.ID, "device_id": d.ID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "lease_acquired"})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.Role != "owner" && u.Role != "admin" {
		writeError(w, http.StatusForbidden, "owner role required")
		return
	}
	events, err := s.store.ListAudit(atoiOr(r.URL.Query().Get("limit"), 200))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	broken, _ := s.store.VerifyAudit()
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "broken_seq": broken})
}

func displayTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "Untitled session"
	}
	if len(title) > 200 {
		return title[:200] + "..."
	}
	return title
}
