package hub

import (
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// kickQueue starts the single queue worker lazily and wakes it.
func (s *Server) kickQueue() {
	s.ensureQueueWorker()
	select {
	case s.queueKick <- struct{}{}:
	default:
	}
}

func (s *Server) ensureQueueWorker() {
	s.queueOnce.Do(func() {
		go s.queueLoop()
	})
}

func (s *Server) queueLoop() {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		select {
		case <-s.queueKick:
		case <-timer.C:
		}
		s.dispatchDuePrompts()
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		// Poll only while work is pending; otherwise sleep until kicked.
		if pending, _ := s.store.HasQueuedPrompts(); pending {
			timer.Reset(2 * time.Second)
		} else {
			timer.Reset(time.Hour)
		}
	}
}

func (s *Server) armQueue(runID string) {
	s.queueMu.Lock()
	s.queueArmed[runID] = true
	s.queueMu.Unlock()
}

func (s *Server) disarmQueue(runID string) {
	s.queueMu.Lock()
	s.queueArmed[runID] = false
	s.queueMu.Unlock()
}

// queueIsArmed reports whether a new prompt may be delivered. The in-memory
// flag is set by an idle run event and cleared after a send, so a slow state
// update cannot cause two prompts to be delivered back to back.
func (s *Server) queueIsArmed(runID string, fallback bool) bool {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	armed, ok := s.queueArmed[runID]
	if !ok {
		return fallback
	}
	return armed
}

func terminalQueueState(state string) bool {
	switch state {
	case protocol.StateExited, protocol.StateCompleted, protocol.StateFailed:
		return true
	default:
		return false
	}
}

// dispatchDuePrompts sends at most one queued prompt per run per pass and
// only when the run is idle. The SQL claim makes duplicate delivery
// impossible even if two passes overlap.
func (s *Server) dispatchDuePrompts() {
	items, err := s.store.DuePromptQueue(time.Now())
	if err != nil || len(items) == 0 {
		return
	}
	sentRuns := map[string]bool{}
	for _, queued := range items {
		if sentRuns[queued.RunID] {
			continue
		}
		run, _, err := s.store.RunByID(queued.RunID)
		if err != nil {
			s.store.FinishPromptQueueItem(queued.ID, "failed", "run not found")
			continue
		}
		if run.Mode != protocol.ModeStructured || terminalQueueState(run.State) {
			s.store.FinishPromptQueueItem(queued.ID, "failed", "run is no longer accepting prompts")
			s.broadcast(protocol.ServerEnvelope{Type: "queue.changed", RunID: run.ID})
			continue
		}
		if run.State != protocol.StateIdle {
			continue
		}
		paused, err := s.store.PromptQueuePaused(run.ID)
		if err != nil || paused {
			continue
		}
		if run.LeaseDeviceID == "" || run.LeaseDeviceID != queued.DeviceID {
			s.store.FinishPromptQueueItem(queued.ID, "failed", "controller lease moved to another device; the prompt was not sent")
			s.broadcast(protocol.ServerEnvelope{Type: "queue.changed", RunID: run.ID})
			continue
		}
		if !s.hostOnline(run.HostID) {
			// The host may reconnect; keep the prompt queued instead of
			// losing it. The UI shows the host as offline.
			continue
		}
		if !s.queueIsArmed(run.ID, run.State == protocol.StateIdle) {
			continue
		}
		user, err := s.store.UserByID(queued.UserID)
		if err != nil {
			s.store.FinishPromptQueueItem(queued.ID, "failed", "queue owner no longer exists")
			s.broadcast(protocol.ServerEnvelope{Type: "queue.changed", RunID: run.ID})
			continue
		}
		sess, err := s.store.SessionByID(user.ID, run.SessionID, isAdminRole(user.Role))
		if err != nil {
			s.store.FinishPromptQueueItem(queued.ID, "failed", "session is no longer available")
			s.broadcast(protocol.ServerEnvelope{Type: "queue.changed", RunID: run.ID})
			continue
		}
		if !s.sessionPermission(user, sess, "resume") {
			s.store.FinishPromptQueueItem(queued.ID, "failed", "resume permission was revoked; the prompt was not sent")
			s.broadcast(protocol.ServerEnvelope{Type: "queue.changed", RunID: run.ID})
			s.audit(user.Username, "prompt_queue.denied", map[string]string{"run_id": run.ID, "item_id": queued.ID})
			continue
		}
		item, claimed, err := s.store.ClaimPromptQueueItem(queued.ID)
		if err != nil || !claimed {
			continue
		}
		s.disarmQueue(run.ID)
		if err := s.sendHost(run.HostID, protocol.Envelope{
			Type: "run.prompt", RunID: run.ID, Message: item.Text, Options: item.Options,
		}); err != nil {
			s.store.FinishPromptQueueItem(item.ID, "failed", "host rejected the prompt: "+err.Error())
			s.broadcast(protocol.ServerEnvelope{Type: "queue.changed", RunID: run.ID})
			s.audit(user.Username, "prompt_queue.failed", map[string]string{"run_id": run.ID, "item_id": item.ID})
			continue
		}
		s.store.FinishPromptQueueItem(item.ID, "sent", "")
		sentRuns[run.ID] = true
		s.broadcast(protocol.ServerEnvelope{Type: "run.prompt.sent", RunID: run.ID, Message: item.Text})
		s.broadcast(protocol.ServerEnvelope{Type: "queue.changed", RunID: run.ID})
		s.audit(user.Username, "prompt_queue.sent", map[string]string{"run_id": run.ID, "item_id": item.ID})
		s.log.Info("queued prompt sent", "run_id", run.ID, "item_id", item.ID, "host_id", run.HostID)
	}
}
