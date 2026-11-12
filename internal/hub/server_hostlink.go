package hub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/coder/websocket"
)

func (h *hostConn) close() {
	h.once.Do(func() { close(h.done) })
	h.ws.Close(websocket.StatusNormalClosure, "closing")
}

func (h *hostConn) runWriter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.done:
			return
		case msg := <-h.send:
			if err := h.signer.Sign(&msg); err != nil {
				continue
			}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err = h.ws.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				h.close()
				return
			}
		}
	}
}

func (s *Server) handleHostWS(w http.ResponseWriter, r *http.Request) {
	hostID := r.URL.Query().Get("host_id")
	if hostID == "" {
		writeError(w, http.StatusBadRequest, "host_id required")
		return
	}
	pub, err := s.store.HostPublicKey(hostID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unknown or revoked host")
		return
	}
	hostMeta, err := s.store.HostByID(hostID)
	if err != nil || hostMeta.DisabledAt != nil {
		writeError(w, http.StatusForbidden, "host is disabled")
		return
	}
	if _, confirmed, _, err := s.store.HostConfirmation(hostID); err == nil && !confirmed {
		writeError(w, http.StatusForbidden, "host awaiting local SAS confirmation")
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer ws.Close(websocket.StatusNormalClosure, "closed")
	ws.SetReadLimit(hostReadLimit)

	nonce := base64.RawURLEncoding.EncodeToString([]byte(randomToken(24)))
	if err := writeWS(r.Context(), ws, protocol.Envelope{Type: "hello", Protocol: protocol.Version, Nonce: nonce}); err != nil {
		return
	}
	authCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	_, data, err := ws.Read(authCtx)
	if err != nil {
		return
	}
	var auth protocol.Envelope
	if err := json.Unmarshal(data, &auth); err != nil || auth.Type != "auth" {
		return
	}
	sig, err := base64.StdEncoding.DecodeString(auth.Signature)
	if err != nil || !verifyEd25519(pub, []byte(nonce), sig) {
		_ = writeWS(r.Context(), ws, protocol.Envelope{Type: "auth.error", Error: "invalid signature"})
		return
	}
	if major(auth.Protocol) != major(protocol.Version) {
		_ = writeWS(r.Context(), ws, protocol.Envelope{Type: "auth.error", Error: "protocol version mismatch", Reason: "upgrade the host daemon"})
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	hc := &hostConn{id: hostID, ws: ws, send: make(chan protocol.Envelope, hostSendQueue), done: make(chan struct{}), signer: s.signer}
	s.registerHost(hc)
	defer s.unregisterHost(hostID, hc)
	go hc.runWriter(ctx)

	// The hub-side name is authoritative after pairing so an administrator's
	// rename is not reverted by every reconnect. The daemon still reports its
	// platform and version.
	// The platform is reported on every connection. Persist it so a host that
	// was paired from a different machine or upgraded to another OS does not
	// keep stale inventory metadata.
	if auth.OS != "" || auth.Arch != "" || auth.Version != "" {
		_, _ = s.store.db.Exec(`UPDATE hosts SET os=COALESCE(NULLIF(?, ''), os), arch=COALESCE(NULLIF(?, ''), arch), version=COALESCE(NULLIF(?, ''), version) WHERE id=?`,
			auth.OS, auth.Arch, auth.Version, hostID)
	}
	features := []string{"session-delta-v1"}
	if s.signer != nil {
		features = append(features, "hub-signing-v1")
	}
	authOK := protocol.Envelope{Type: "auth.ok", Protocol: protocol.Version, Features: features}
	if err := s.signer.Sign(&authOK); err != nil {
		s.log.Warn("cannot sign auth.ok", "host_id", hostID, "error", err)
	}
	_ = writeWS(ctx, ws, authOK)
	s.store.UpdateHostSeen(hostID)
	s.broadcastHostUpdate(hostID)
	s.log.Info("host connected", "request_id", requestIDFrom(ctx), "host_id", hostID, "version", auth.Version, "os", auth.OS, "arch", auth.Arch)
	s.audit(auth.HostName, "host.connected", map[string]string{"host_id": hostID, "version": auth.Version})

	// Ping the daemon so dead NAT mappings are noticed even when idle.
	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-hc.done:
				return
			case <-t.C:
				pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
				err := ws.Ping(pctx)
				pcancel()
				if err != nil {
					hc.close()
					return
				}
			}
		}
	}()

	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			s.log.Info("host disconnected", "request_id", requestIDFrom(ctx), "host_id", hostID, "error", err)
			return
		}
		var msg protocol.Envelope
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		s.handleHostMessage(ctx, hc, msg)
	}
}

// adoptHostRuns reconciles runs after a hub restart or host reconnect. The
// host is authoritative: runs it reports stay active, runs it no longer knows
// about are marked failed instead of hanging forever in the UI.
func (s *Server) adoptHostRuns(hostID string, reported []protocol.RunInfo) {
	seen := map[string]bool{}
	for _, info := range reported {
		seen[info.RunID] = true
		run, _, err := s.store.RunByID(info.RunID)
		if err != nil || run.HostID != hostID {
			continue
		}
		state := info.State
		if state == "" {
			state = protocol.StateRunning
		}
		s.setRunState(info.RunID, state)
		s.broadcast(protocol.ServerEnvelope{Type: "run.state", RunID: info.RunID, State: state, Epoch: s.epoch})
	}
	active, _ := s.store.ActiveRuns(hostID)
	for _, run := range active {
		if !seen[run.ID] {
			s.setRunState(run.ID, protocol.StateFailed)
			s.broadcast(protocol.ServerEnvelope{Type: "run.error", RunID: run.ID, Error: "host no longer reports the run"})
		}
	}
}

// hostOwnsRun prevents a paired host from injecting output, events or state
// for a run that belongs to a different machine.
func (s *Server) hostOwnsRun(hostID, runID string) bool {
	if runID == "" {
		return false
	}
	s.mu.RLock()
	cached, ok := s.runHosts[runID]
	s.mu.RUnlock()
	if ok {
		return cached == hostID
	}
	run, _, err := s.store.RunByID(runID)
	if err != nil || run.HostID != hostID {
		return false
	}
	s.mu.Lock()
	s.runHosts[runID] = hostID
	s.mu.Unlock()
	return true
}

func (s *Server) handleHostMessage(ctx context.Context, hc *hostConn, msg protocol.Envelope) {
	if msg.Type == "run.started" || msg.Type == "run.output" || msg.Type == "run.event" ||
		msg.Type == "run.exit" || msg.Type == "run.error" {
		if !s.hostOwnsRun(hc.id, msg.RunID) {
			s.log.Warn("host attempted to update a run it does not own",
				"host_id", hc.id, "run_id", msg.RunID, "type", msg.Type)
			return
		}
	}
	switch msg.Type {
	case "inventory":
		if err := s.store.SetHostInventory(hc.id, msg.Version, msg.Agents, msg.Policy); err != nil {
			s.log.Error("inventory update failed", "host_id", hc.id, "error", err)
		}
		if msg.Index != nil {
			s.setHostIndex(hc.id, msg.Index)
		}
		s.store.UpdateHostSeen(hc.id)
		s.broadcastHostUpdate(hc.id)
	case "sessions":
		var upserted, deleted int
		var err error
		if msg.Full {
			upserted, deleted, err = s.store.ReplaceHostSessions(hc.id, msg.Sessions)
		} else {
			upserted, deleted, err = s.store.ApplyHostSessionDelta(hc.id, msg.Sessions, msg.Removed)
		}
		if err != nil {
			s.log.Error("session snapshot failed", "host_id", hc.id, "error", err)
			return
		}
		if upserted+deleted > 0 {
			s.log.Info("session changes applied", "host_id", hc.id, "upserted", upserted, "deleted", deleted, "full", msg.Full)
			s.broadcast(protocol.ServerEnvelope{Type: "sessions.changed", Message: hc.id})
		}
	case "update.status":
		if msg.Update != nil {
			status := *msg.Update
			if status.CheckedAt.IsZero() {
				status.CheckedAt = time.Now().UTC()
			}
			s.updateMu.Lock()
			s.hostUpdates[hc.id] = status
			notify := status.Available && s.lastUpdateNotified[hc.id] != status.Latest
			if notify {
				s.lastUpdateNotified[hc.id] = status.Latest
			}
			s.updateMu.Unlock()
			s.broadcastHostUpdate(hc.id)
			if notify {
				if host, err := s.store.HostByID(hc.id); err == nil && host.OwnerID != "" {
					_ = s.store.AddNotification(host.OwnerID, "update",
						"Update available on "+host.Name,
						"nexusd "+status.Latest+" is available (installed "+status.Current+")",
						"", "#/hosts")
				}
			}
		}
	case "active_runs":
		s.adoptHostRuns(hc.id, msg.Runs)
	case "result":
		s.mu.RLock()
		p := s.pending[msg.RequestID]
		s.mu.RUnlock()
		if p != nil && p.hostID == hc.id {
			if p.requestID != "" {
				s.log.Debug("host result", "request_id", p.requestID, "host_request_id", msg.RequestID, "host_id", hc.id, "method", p.method, "ok", msg.OK)
			}
			select {
			case p.ch <- msg:
			default:
			}
		}
	case "run.started":
		if msg.NativeID != "" {
			s.adoptNativeSession(msg.RunID, msg.NativeID, msg.Agent, msg.Cwd)
		}
		s.setRunState(msg.RunID, protocol.StateRunning)
		s.broadcast(protocol.ServerEnvelope{Type: "run.state", RunID: msg.RunID, State: protocol.StateRunning, Epoch: s.epoch})
	case "run.output":
		s.appendRunOutput(msg.RunID, msg.Seq, msg.Data)
	case "run.event":
		if msg.Event != nil {
			s.appendRunEvent(msg.RunID, *msg.Event)
			if msg.Event.Type == "approval.requested" {
				go s.notifyRun(msg.RunID, "approval", "Approval needed", "An agent is waiting for your approval.")
			}
		}
	case "run.usage":
		if msg.Usage != nil {
			if err := s.store.UpsertRunUsage(msg.RunID, *msg.Usage); err != nil {
				s.log.Debug("cannot store run usage", "run_id", msg.RunID, "error", err)
				break
			}
			usage := *msg.Usage
			s.broadcast(protocol.ServerEnvelope{Type: "run.usage", RunID: msg.RunID, Usage: &usage})
		}
	case "run.exit":
		state := protocol.StateExited
		if msg.Code != 0 {
			state = protocol.StateFailed
		}
		s.setRunState(msg.RunID, state)
		s.broadcast(protocol.ServerEnvelope{Type: "run.exit", RunID: msg.RunID, State: state, Message: msg.Reason})
		s.log.Info("run exited", "run_id", msg.RunID, "code", msg.Code, "reason", msg.Reason)
		if state == protocol.StateFailed {
			go s.notifyRun(msg.RunID, "failed", "Agent run failed", "A run exited with an error. Open AgentNexus for details.")
		} else {
			go s.notifyRun(msg.RunID, "completed", "Agent run finished", "A run finished. Open AgentNexus for details.")
		}
	case "run.error":
		s.setRunState(msg.RunID, protocol.StateFailed)
		s.broadcast(protocol.ServerEnvelope{Type: "run.error", RunID: msg.RunID, Error: msg.Error})
		s.audit("host:"+hc.id, "run.failed", map[string]string{"run_id": msg.RunID, "error": msg.Error})
		go s.notifyRun(msg.RunID, "failed", "Agent error", "A run failed. Open AgentNexus for details.")
	case "pong", "pong ":
	default:
		s.log.Debug("unknown host message", "type", msg.Type, "host_id", hc.id)
	}
}

// validForkNativeID bounds a host-reported native session id. It accepts the
// printable subset that Codex and ACP session ids use.
func validForkNativeID(id string) bool {
	if id == "" || len(id) > 200 {
		return false
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// adoptNativeSession links a new or forked structured run to the native
// session the host created. The source session row is never modified.
func (s *Server) adoptNativeSession(runID, nativeID, reportedAgent, reportedCwd string) {
	if !validForkNativeID(nativeID) {
		s.log.Warn("host reported an invalid forked session id", "run_id", runID)
		return
	}
	run, _, err := s.store.RunByID(runID)
	if err != nil || run.HostID == "" {
		return
	}
	agent, cwd := run.Agent, run.Cwd
	if agent == "" {
		agent = strings.TrimSpace(reportedAgent)
	}
	if cwd == "" {
		cwd = strings.TrimSpace(reportedCwd)
	}
	if agent == "" {
		return
	}
	title := "New session"
	if run.SessionTitle != "" {
		title = "Fork of " + run.SessionTitle
	}
	sessionID, err := s.store.EnsureForkedSession(run.HostID, agent, nativeID, title, cwd)
	if err != nil {
		s.log.Warn("cannot record forked session", "run_id", runID, "error", err)
		return
	}
	if err := s.store.RebindRunSession(runID, sessionID); err != nil {
		s.log.Warn("cannot bind run to forked session", "run_id", runID, "error", err)
		return
	}
	s.broadcast(protocol.ServerEnvelope{Type: "sessions.changed", Message: run.HostID})
	s.log.Info("run adopted its forked session", "run_id", runID, "session_id", sessionID, "host_id", run.HostID)
}

func (s *Server) registerHost(hc *hostConn) {
	s.mu.Lock()
	old := s.hosts[hc.id]
	s.hosts[hc.id] = hc
	s.mu.Unlock()
	if old != nil {
		s.metrics.hostReconnected()
		old.close()
	}
}

func (s *Server) unregisterHost(id string, hc *hostConn) {
	s.mu.Lock()
	wasCurrent := s.hosts[id] == hc
	if wasCurrent {
		delete(s.hosts, id)
	}
	s.mu.Unlock()
	hc.close()
	if !wasCurrent {
		// A newer connection for the same host already owns the link and its
		// active runs; do not fail them from the stale connection's defer.
		return
	}
	if runs, err := s.store.ActiveRuns(id); err == nil {
		for _, r := range runs {
			s.setRunState(r.ID, protocol.StateFailed)
			s.broadcast(protocol.ServerEnvelope{Type: "run.error", RunID: r.ID, Error: "host disconnected"})
			go s.notifyRun(r.ID, "disconnected", "Agent disconnected", "The host disconnected while the run was active.")
		}
	}
	s.broadcastHostUpdate(id)
}

func (s *Server) hostOnline(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.hosts[id]
	return ok
}

func (s *Server) sendHost(hostID string, msg protocol.Envelope) error {
	s.mu.RLock()
	hc := s.hosts[hostID]
	s.mu.RUnlock()
	if hc == nil {
		return errors.New("host is offline")
	}
	select {
	case hc.send <- msg:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("host send queue is full")
	}
}

func (s *Server) dropHost(id, reason string) {
	s.mu.RLock()
	hc := s.hosts[id]
	s.mu.RUnlock()
	if hc != nil {
		hc.close()
	}
}

func (s *Server) broadcastHostUpdate(id string) {
	host, err := s.store.HostByID(id)
	if err != nil {
		return
	}
	hv := host.HostView
	if host.DisabledAt != nil {
		hv.Status = "disabled"
	} else if s.hostOnline(id) {
		hv.Status = "online"
	}
	s.updateMu.Lock()
	if status, ok := s.hostUpdates[id]; ok {
		copyStatus := status
		hv.Update = &copyStatus
	}
	s.updateMu.Unlock()
	hv.Index = s.hostIndexStatus(id)
	s.broadcast(protocol.ServerEnvelope{Type: "host.updated", Host: &hv})
}

// hostRequest sends one request to a host and waits for the matching result.
func (s *Server) hostRequest(ctx context.Context, hostID, method string, params []byte) ([]byte, error) {
	if !s.hostOnline(hostID) {
		return nil, errors.New("host is offline")
	}
	id := newID()
	requestID := requestIDFrom(ctx)
	p := &pendingRequest{hostID: hostID, requestID: requestID, method: method, ch: make(chan protocol.Envelope, 1)}
	s.mu.Lock()
	s.pending[id] = p
	s.mu.Unlock()
	if requestID != "" {
		s.log.Debug("host request", "request_id", requestID, "host_request_id", id, "host_id", hostID, "method", method)
	}
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()
	if err := s.sendHost(hostID, protocol.Envelope{Type: "request", RequestID: id, TraceID: requestID, Method: method, Params: params}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		s.log.Debug("host request ended without a result", "request_id", requestID, "host_request_id", id, "host_id", hostID, "method", method, "error", ctx.Err())
		return nil, ctx.Err()
	case res := <-p.ch:
		if !res.OK {
			return nil, errors.New(res.Error)
		}
		return res.Result, nil
	}
}

// ---- browser clients ----

type clientConn struct {
	server *Server
	ws     *websocket.Conn
	user   User
	device Device
	send   chan protocol.ServerEnvelope
	done   chan struct{}
	once   sync.Once

	mu   sync.Mutex
	subs map[string]bool
}

func (s *Server) setHostIndex(hostID string, status *protocol.IndexStatus) {
	if status == nil {
		return
	}
	s.indexMu.Lock()
	s.hostIndex[hostID] = *status
	s.indexMu.Unlock()
}

func (s *Server) hostIndexStatus(hostID string) *protocol.IndexStatus {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	status, ok := s.hostIndex[hostID]
	if !ok {
		return nil
	}
	return &status
}
