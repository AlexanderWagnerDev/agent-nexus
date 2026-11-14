package hub

import (
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func (s *Server) runState(id string) *runState {
	s.mu.Lock()
	defer s.mu.Unlock()
	rs := s.runs[id]
	if rs == nil {
		rs = &runState{}
		s.runs[id] = rs
	}
	return rs
}

// runStateRetention bounds how long a finished run keeps its in-memory output
// and events for reconnects. Without it every historical run would retain up
// to its replay buffer for the lifetime of the hub process.
const runStateRetention = 15 * time.Minute

// scheduleRunCleanup releases the in-memory buffers of a terminal run. A
// non-terminal run keeps them; the timer simply expires.
func (s *Server) scheduleRunCleanup(runID string) {
	time.AfterFunc(runStateRetention, func() { s.cleanupRunState(runID) })
}

// cleanupRunState drops the retained events and output of one finished run.
// Live runs are never touched, so a delayed timer cannot affect them.
func (s *Server) cleanupRunState(runID string) {
	s.mu.Lock()
	rs, ok := s.runs[runID]
	s.mu.Unlock()
	if !ok {
		return
	}
	rs.mu.Lock()
	finished := terminalRunState(rs.state)
	rs.mu.Unlock()
	if !finished {
		return
	}
	s.mu.Lock()
	if current, exists := s.runs[runID]; exists && current == rs {
		delete(s.runs, runID)
		delete(s.runHosts, runID)
	}
	s.mu.Unlock()
	s.queueMu.Lock()
	delete(s.queueArmed, runID)
	s.queueMu.Unlock()
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
		s.scheduleRunCleanup(id)
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

// runStartTimeout bounds how long a run may stay in "starting" before the hub
// assumes the host never launched it. Without it, a host that accepts the
// command but never starts the process would block its session forever.
var runStartTimeout = 2 * time.Minute

// failStaleStartingRun closes a run that never left the starting state.
func (s *Server) failStaleStartingRun(runID string) {
	run, _, err := s.store.RunByID(runID)
	if err != nil || run.State != protocol.StateStarting {
		return
	}
	// A host may report output before its state event; treat any received
	// event or output as proof that the run is alive.
	if s.runHasActivity(runID) {
		return
	}
	s.store.SetRunError(runID, ErrCodeTimeout, "host did not start the run in time")
	s.setRunState(runID, protocol.StateFailed)
	s.broadcast(protocol.ServerEnvelope{Type: "run.error", RunID: runID, Error: "host did not start the run in time"})
	go s.notifyRun(runID, "failed", "Agent run failed", "A run did not start in time. Open AgentNexus for details.")
	s.log.Warn("run start timed out", "run_id", runID, "host_id", run.HostID)
}

func (s *Server) runEventCursor(id string) uint64 {
	rs := s.runState(id)
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.eventSeq
}

// runHasActivity reports whether any event or terminal output arrived for the
// run. It is used by the start timeout so a slow-but-alive host is not failed.
func (s *Server) runHasActivity(id string) bool {
	rs := s.runState(id)
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.eventSeq > 0 || len(rs.chunks) > 0
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
	if ev.Type == "run.state" && ev.State != "" {
		if terminalRunState(previousState) && !terminalRunState(ev.State) {
			// A late event must not reopen a finished, failed or stopped run.
			rs.mu.Unlock()
			return
		}
		rs.state = ev.State
	}
	rs.eventSeq++
	ev.Seq = rs.eventSeq
	ev.RunID = runID
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
			s.scheduleRunCleanup(runID)
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
