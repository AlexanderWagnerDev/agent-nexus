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
	if sess, _, err := s.store.RunByID(id); err == nil && sess.SessionID != "" {
		if state == protocol.StateRunning {
			s.store.SetSessionState(sess.SessionID, protocol.StateRunning)
		} else if state == protocol.StateExited || state == protocol.StateCompleted || state == protocol.StateFailed {
			s.store.SetSessionState(sess.SessionID, protocol.StateIdle)
		}
	}
}

func (s *Server) runEventCursor(id string) uint64 {
	rs := s.runState(id)
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.eventSeq
}

func (s *Server) appendRunEvent(runID string, ev protocol.RunEvent) {
	rs := s.runState(runID)
	rs.mu.Lock()
	rs.eventSeq++
	ev.Seq = rs.eventSeq
	ev.RunID = runID
	if ev.Type == "run.state" && ev.State != "" {
		rs.state = ev.State
	}
	if ev.Type == "approval.requested" && ev.ApprovalID != "" {
		s.metrics.approvalRequested(ev.ApprovalID)
	}
	rs.events = append(rs.events, ev)
	if len(rs.events) > 500 {
		rs.events = rs.events[len(rs.events)-500:]
	}
	rs.mu.Unlock()

	if ev.Type == "run.state" && ev.State != "" {
		s.store.UpdateRunState(runID, ev.State)
		if run, _, err := s.store.RunByID(runID); err == nil && run.SessionID != "" {
			switch ev.State {
			case protocol.StateRunning, protocol.StateWaiting:
				s.store.SetSessionState(run.SessionID, protocol.StateRunning)
			case protocol.StateIdle, protocol.StateCompleted, protocol.StateExited, protocol.StateFailed:
				s.store.SetSessionState(run.SessionID, protocol.StateIdle)
			}
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
