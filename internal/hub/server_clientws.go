package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/coder/websocket"
)

func (c *clientConn) close() {
	c.once.Do(func() { close(c.done) })
	c.ws.Close(websocket.StatusNormalClosure, "closing")
}

func (c *clientConn) subscribed(runID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.subs[runID]
}

func (c *clientConn) runWriter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case msg := <-c.send:
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err = c.ws.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				c.close()
				return
			}
		}
	}
}

func (c *clientConn) push(msg protocol.ServerEnvelope) {
	select {
	case c.send <- msg:
	default:
		// A slow viewer is disconnected instead of blocking the hub; it can
		// reconnect and replay from its last sequence number.
		c.close()
	}
}

func (s *Server) handleClientWS(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" {
		if !s.originAllowed(origin, r.Host) {
			writeError(w, http.StatusForbidden, "origin mismatch")
			return
		}
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer ws.Close(websocket.StatusNormalClosure, "closed")
	ws.SetReadLimit(clientReadLimit)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c := &clientConn{
		server: s, ws: ws, user: userFrom(r), device: deviceFrom(r),
		send: make(chan protocol.ServerEnvelope, clientSendQueue), done: make(chan struct{}),
		subs: map[string]bool{}, limiter: newRateLimiter(240, 10*time.Second),
	}
	s.mu.Lock()
	s.clients[c] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
		c.close()
	}()
	go c.runWriter(ctx)
	_ = c.ws.Write(ctx, websocket.MessageText, mustJSON(protocol.ServerEnvelope{Type: "ready", Message: s.cfg.HubName}))

	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.done:
				return
			case <-t.C:
				pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
				err := ws.Ping(pctx)
				pcancel()
				if err != nil {
					c.close()
					return
				}
			}
		}
	}()

	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var msg protocol.ClientEnvelope
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if !c.limiter.allow("ws") {
			c.push(protocol.ServerEnvelope{Type: "error", Error: "too many client messages"})
			c.close()
			return
		}
		s.handleClientMessage(ctx, c, msg)
	}
}

func (s *Server) handleClientMessage(ctx context.Context, c *clientConn, msg protocol.ClientEnvelope) {
	if signedWriteMessage(msg.Type) {
		if err := s.verifyWSDeviceSignature(c.device.ID, msg); err != nil {
			c.push(protocol.ServerEnvelope{Type: "error", Error: err.Error()})
			return
		}
	}
	switch msg.Type {
	case "subscribe":
		if msg.RunID == "" {
			return
		}
		run, startedBy, err := s.store.RunByID(msg.RunID)
		if err != nil || (startedBy != c.user.ID && !isAdminRole(c.user.Role)) {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "run not found"})
			return
		}
		c.mu.Lock()
		c.subs[msg.RunID] = true
		c.mu.Unlock()
		rs := s.runState(msg.RunID)
		state := run.State
		var chunks []protocol.ServerEnvelope
		var events []protocol.ServerEnvelope
		if rs != nil {
			rs.mu.Lock()
			state = rs.state
			for _, ch := range coalesceChunks(rs.chunks, msg.LastSeq, replayBatchBytes) {
				chunks = append(chunks, protocol.ServerEnvelope{Type: "run.output", RunID: msg.RunID, Seq: ch.seq, Data: ch.data})
			}
			for i := range rs.events {
				ev := rs.events[i]
				if ev.Seq > msg.LastSeq {
					events = append(events, protocol.ServerEnvelope{Type: "run.event", RunID: msg.RunID, Seq: ev.Seq, Event: &ev})
				}
			}
			rs.mu.Unlock()
		}
		c.push(protocol.ServerEnvelope{Type: "run.state", RunID: msg.RunID, State: state, Epoch: s.epoch})
		for _, ch := range chunks {
			c.push(ch)
		}
		for _, ev := range events {
			c.push(ev)
		}
	case "unsubscribe":
		c.mu.Lock()
		delete(c.subs, msg.RunID)
		c.mu.Unlock()
	case "input":
		if len(msg.Data) > maxTerminalInputSize {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "terminal input is too large"})
			return
		}
		run, startedBy, err := s.store.RunByID(msg.RunID)
		if err != nil || (startedBy != c.user.ID && !isAdminRole(c.user.Role)) {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "run not found"})
			return
		}
		if run.LeaseDeviceID != c.device.ID {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "you do not hold the controller lease"})
			return
		}
		if err := s.sendHost(run.HostID, protocol.Envelope{Type: "run.input", RunID: msg.RunID, Data: msg.Data}); err != nil {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: err.Error()})
		}
	case "prompt":
		run, startedBy, err := s.store.RunByID(msg.RunID)
		if err != nil || (startedBy != c.user.ID && !isAdminRole(c.user.Role)) {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "run not found"})
			return
		}
		if run.Mode != protocol.ModeStructured {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "run is not in structured mode"})
			return
		}
		if run.LeaseDeviceID != c.device.ID {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "you do not hold the controller lease"})
			return
		}
		if msgErr := validQueuedPrompt(msg.Text, msg.Options); msgErr != "" {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: msgErr})
			return
		}
		if err := s.sendHost(run.HostID, protocol.Envelope{Type: "run.prompt", RunID: msg.RunID, Message: msg.Text, Options: msg.Options}); err != nil {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: err.Error()})
		}
	case "approval":
		run, startedBy, err := s.store.RunByID(msg.RunID)
		if err != nil || (startedBy != c.user.ID && !isAdminRole(c.user.Role)) {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "run not found"})
			return
		}
		if run.LeaseDeviceID != c.device.ID {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "you do not hold the controller lease"})
			return
		}
		if msg.Decision != "once" && msg.Decision != "always" && msg.Decision != "reject" {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "invalid approval decision"})
			return
		}
		if err := s.sendHost(run.HostID, protocol.Envelope{Type: "run.approval", RunID: msg.RunID, ApprovalID: msg.ApprovalID, Decision: msg.Decision}); err != nil {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: err.Error()})
		}
		s.metrics.approvalAnswered(run.ID, msg.ApprovalID)
		s.audit(c.user.Username, "approval.answered", map[string]string{"run_id": msg.RunID, "approval_id": msg.ApprovalID, "decision": msg.Decision})
	case "interrupt":
		run, startedBy, err := s.store.RunByID(msg.RunID)
		if err != nil || (startedBy != c.user.ID && !isAdminRole(c.user.Role)) {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "run not found"})
			return
		}
		if run.LeaseDeviceID != c.device.ID {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "you do not hold the controller lease"})
			return
		}
		if err := s.sendHost(run.HostID, protocol.Envelope{Type: "run.interrupt", RunID: msg.RunID}); err != nil {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: err.Error()})
		}
	case "resize":
		run, startedBy, err := s.store.RunByID(msg.RunID)
		if err != nil || (startedBy != c.user.ID && !isAdminRole(c.user.Role)) {
			return
		}
		if run.LeaseDeviceID != c.device.ID {
			return
		}
		_ = s.sendHost(run.HostID, protocol.Envelope{Type: "run.resize", RunID: msg.RunID, Cols: msg.Cols, Rows: msg.Rows})
	case "ping":
		c.push(protocol.ServerEnvelope{Type: "pong"})
	}
}

func (s *Server) broadcast(msg protocol.ServerEnvelope) {
	if msg.RunID != "" && (msg.Type == "run.output" || msg.Type == "run.event") {
		s.mu.RLock()
		clients := make([]*clientConn, 0, len(s.clients))
		for c := range s.clients {
			clients = append(clients, c)
		}
		s.mu.RUnlock()
		for _, c := range clients {
			if c.subscribed(msg.RunID) {
				c.push(msg)
			}
		}
		return
	}
	s.mu.RLock()
	clients := make([]*clientConn, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.RUnlock()
	for _, c := range clients {
		c.push(msg)
	}
}

// ---- run buffers ----

type runChunk struct {
	seq  uint64
	data []byte
}

type runState struct {
	mu       sync.Mutex
	state    string
	chunks   []runChunk
	bytes    int
	events   []protocol.RunEvent
	eventSeq uint64
}

// replayBatchBytes bounds one replayed terminal message. Coalescing keeps a
// long run from filling the per-client send queue with thousands of small PTY
// reads, which would disconnect the client and repeat on every reconnect.
const replayBatchBytes = 64 << 10

// coalesceChunks merges consecutive replay chunks into bounded messages. The
// merged message carries the newest sequence number; the bytes stay in order.
func coalesceChunks(chunks []runChunk, lastSeq uint64, maxBytes int) []runChunk {
	if maxBytes <= 0 {
		maxBytes = replayBatchBytes
	}
	var out []runChunk
	var current runChunk
	for _, chunk := range chunks {
		if chunk.seq <= lastSeq || len(chunk.data) == 0 {
			continue
		}
		if len(current.data) > 0 && len(current.data)+len(chunk.data) > maxBytes {
			out = append(out, current)
			current = runChunk{}
		}
		current.data = append(current.data, chunk.data...)
		current.seq = chunk.seq
	}
	if len(current.data) > 0 {
		out = append(out, current)
	}
	return out
}

func (s *Server) closeDevice(deviceID string) {
	s.mu.RLock()
	clients := make([]*clientConn, 0, len(s.clients))
	for c := range s.clients {
		if c.device.ID == deviceID {
			clients = append(clients, c)
		}
	}
	s.mu.RUnlock()
	for _, c := range clients {
		c.close()
	}
}
