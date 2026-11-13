package hub

import "github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"

func (s *Server) runState(id string) *runState {
	s.mu.Lock()
	defer s.mu.Unlock()
	rs := s.runs[id]
	if rs == nil {
		rs = &runState{state: protocol.StateStarting}
		s.runs[id] = rs
	}
	return rs
}

func (s *Server) setRunState(id, state string) {
	rs := s.runState(id)
	rs.mu.Lock()
	rs.state = state
	rs.mu.Unlock()
	s.store.UpdateRunState(id, state)
	s.store.SyncTaskRunState(id, state)
	if sess, _, err := s.store.RunByID(id); err == nil && sess.SessionID != "" {
		if state == protocol.StateRunning {
			s.store.SetSessionState(sess.SessionID, protocol.StateRunning)
		} else if state == protocol.StateExited || state == protocol.StateCompleted || state == protocol.StateFailed {
			s.store.SetSessionState(sess.SessionID, protocol.StateIdle)
		}
	}
	if terminalRunState(state) {
		// A finished, failed or stopped run can no longer answer approvals.
		// Drop their metric entries even if the host never sent a resolution.
		s.metrics.approvalsEnded(id)
		s.store.FailPendingPromptQueue(id, "run ended before the queued prompt was sent")
		s.disarmQueue(id)
	}
}

func terminalRunState(state string) bool {
	switch state {
	case protocol.StateExited, protocol.StateCompleted, protocol.StateFailed:
		return true
	default:
		return false
	}
}

func (s *Server) runEventCursor(id string) uint64 {
	rs := s.runState(id)
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.eventSeq
}

// pendingApproval returns the newest approval.requested event that has no
// matching approval.resolved event in the retained run window.
func (s *Server) pendingApproval(id string) *protocol.RunEvent {
	rs := s.runState(id)
	rs.mu.Lock()
	defer rs.mu.Unlock()
	pending := map[string]protocol.RunEvent{}
	for _, ev := range rs.events {
		switch ev.Type {
		case "approval.requested":
			if ev.ApprovalID != "" {
				pending[ev.ApprovalID] = ev
			}
		case "approval.resolved":
			delete(pending, ev.ApprovalID)
		}
	}
	var newest *protocol.RunEvent
	for _, ev := range pending {
		candidate := ev
		if newest == nil || candidate.Seq > newest.Seq {
			newest = &candidate
		}
	}
	return newest
}

func (s *Server) appendRunEvent(runID string, ev protocol.RunEvent) {
	rs := s.runState(runID)
	rs.mu.Lock()
	previousState := rs.state
	rs.eventSeq++
	ev.Seq = rs.eventSeq
	ev.RunID = runID
	if ev.Type == "run.state" && ev.State != "" {
		rs.state = ev.State
	}
	if ev.Type == "approval.requested" && ev.ApprovalID != "" {
		s.metrics.approvalRequested(runID, ev.ApprovalID)
		s.store.SyncTaskRunState(runID, protocol.StateWaiting)
	}
	if ev.Type == "approval.resolved" && ev.ApprovalID != "" {
		s.metrics.approvalAnswered(runID, ev.ApprovalID)
		s.store.SyncTaskRunState(runID, protocol.StateRunning)
	}
	rs.events = append(rs.events, ev)
	if len(rs.events) > 500 {
		rs.events = rs.events[len(rs.events)-500:]
	}
	rs.mu.Unlock()

	if ev.Type == "run.state" && ev.State != "" {
		if terminalRunState(ev.State) {
			s.metrics.approvalsEnded(runID)
		}
		s.store.UpdateRunState(runID, ev.State)
		s.store.SyncTaskRunState(runID, ev.State)
		if run, _, err := s.store.RunByID(runID); err == nil && run.SessionID != "" {
			switch ev.State {
			case protocol.StateRunning, protocol.StateWaiting:
				s.store.SetSessionState(run.SessionID, protocol.StateRunning)
			case protocol.StateIdle, protocol.StateCompleted, protocol.StateExited, protocol.StateFailed:
				s.store.SetSessionState(run.SessionID, protocol.StateIdle)
			}
		}
		switch {
		case terminalRunState(ev.State):
			s.store.FailPendingPromptQueue(runID, "run ended before the queued prompt was sent")
			s.disarmQueue(runID)
		case ev.State == protocol.StateIdle:
			if previousState != protocol.StateIdle && s.store.HasQueuedPromptsForRun(runID) {
				s.armQueue(runID)
				s.kickQueue()
			}
		default:
			s.disarmQueue(runID)
		}
	}
	s.broadcast(protocol.ServerEnvelope{Type: "run.event", RunID: runID, Seq: ev.Seq, Event: &ev})
}

func (s *Server) appendRunOutput(runID string, seq uint64, data []byte) {
	if len(data) == 0 {
		return
	}
	rs := s.runState(runID)
	rs.mu.Lock()
	if n := len(rs.chunks); n > 0 && seq <= rs.chunks[n-1].seq {
		rs.mu.Unlock()
		return
	}
	cp := append([]byte(nil), data...)
	rs.chunks = append(rs.chunks, runChunk{seq: seq, data: cp})
	rs.bytes += len(cp)
	for rs.bytes > runBufferBytes && len(rs.chunks) > 1 {
		rs.bytes -= len(rs.chunks[0].data)
		rs.chunks = rs.chunks[1:]
	}
	rs.mu.Unlock()
	s.broadcast(protocol.ServerEnvelope{Type: "run.output", RunID: runID, Seq: seq, Data: cp})
}

// ---- helpers ----
