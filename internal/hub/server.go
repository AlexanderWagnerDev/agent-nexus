package hub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/coder/websocket"
)

const (
	ed25519PublicKeySize = 32
	hostReadLimit        = 32 << 20
	clientReadLimit      = 32 << 20
	runBufferBytes       = 4 << 20
	hostSendQueue        = 256
	clientSendQueue      = 512
	maxPromptBytes       = 256 << 10
	maxTerminalInputSize = 2 << 20 // base64 payload for a terminal paste
)

// Version is overridden at build time for release images and binaries.
var Version = "alpha"

// Server is the AgentNexus hub: REST API, browser WebSocket gateway, host
// link gateway, relay and metadata index.
type Server struct {
	cfg    Config
	store  *Store
	log    *slog.Logger
	assets http.Handler

	loginLimiter *rateLimiter
	pairLimiter  *rateLimiter

	mu       sync.RWMutex
	hosts    map[string]*hostConn
	clients  map[*clientConn]struct{}
	runs     map[string]*runState
	runHosts map[string]string
	pending  map[string]*pendingRequest

	vapidPublic  string
	vapidPrivate string
	epoch        string
	signer       *HubSigner

	updateMu           sync.Mutex
	hostUpdates        map[string]protocol.UpdateStatus
	lastUpdateNotified map[string]string

	passkeySessions passkeySessionStore
}

type pendingRequest struct {
	hostID string
	ch     chan protocol.Envelope
}

func NewServer(cfg Config, store *Store, logger *slog.Logger) *Server {
	if cfg.HubName == "" {
		cfg.HubName = "AgentNexus"
	}
	s := &Server{
		cfg:                cfg,
		store:              store,
		log:                logger,
		loginLimiter:       newRateLimiter(10, 5*time.Minute),
		pairLimiter:        newRateLimiter(10, 10*time.Minute),
		hosts:              map[string]*hostConn{},
		clients:            map[*clientConn]struct{}{},
		runs:               map[string]*runState{},
		runHosts:           map[string]string{},
		pending:            map[string]*pendingRequest{},
		passkeySessions:    passkeySessionStore{data: map[string]passkeySession{}},
		hostUpdates:        map[string]protocol.UpdateStatus{},
		lastUpdateNotified: map[string]string{},
		epoch:              newID(),
	}
	s.ensureVAPID()
	return s
}

func (s *Server) SetAssets(h http.Handler) { s.assets = h }

func (s *Server) SetSigner(signer *HubSigner) { s.signer = signer }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /api/v1/setup/status", s.handleSetupStatus)
	mux.HandleFunc("POST /api/v1/setup", s.handleSetup)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/passkey/begin", s.handlePasskeyLoginBegin)
	mux.HandleFunc("POST /api/v1/auth/passkey/finish", s.handlePasskeyLoginFinish)
	mux.HandleFunc("POST /api/v1/auth/device-pair", s.handleDevicePair)
	mux.HandleFunc("GET /api/v1/hub-key", s.handleHubKey)
	mux.HandleFunc("POST /api/v1/hosts/pair", s.handlePairHost)

	mux.HandleFunc("POST /api/v1/auth/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("GET /api/v1/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("GET /api/v1/me/settings", s.requireAuth(s.handleGetSettings))
	mux.HandleFunc("PUT /api/v1/me/settings", s.requireAuth(s.handlePutSettings))
	mux.HandleFunc("GET /api/v1/auth/step-up", s.requireAuth(s.handleStepUpStatus))
	mux.HandleFunc("POST /api/v1/auth/step-up/begin", s.requireAuth(s.handleStepUpBegin))
	mux.HandleFunc("POST /api/v1/auth/step-up/passkey", s.requireAuth(s.handleStepUpPasskeyFinish))
	mux.HandleFunc("POST /api/v1/auth/step-up/password", s.requireAuth(s.handleStepUpPassword))
	mux.HandleFunc("GET /api/v1/passkeys", s.requireAuth(s.handlePasskeys))
	mux.HandleFunc("POST /api/v1/auth/passkey/register/begin", s.requireAuth(s.requireStepUp(s.handlePasskeyRegisterBegin)))
	mux.HandleFunc("POST /api/v1/auth/passkey/register/finish", s.requireAuth(s.requireStepUp(s.handlePasskeyRegisterFinish)))
	mux.HandleFunc("POST /api/v1/auth/totp/begin", s.requireAuth(s.requireStepUp(s.handleTOTPBegin)))
	mux.HandleFunc("POST /api/v1/auth/totp/enable", s.requireAuth(s.requireStepUp(s.handleTOTPEnable)))
	mux.HandleFunc("GET /api/v1/devices", s.requireAuth(s.handleDevices))
	mux.HandleFunc("DELETE /api/v1/devices/{id}", s.requireAuth(s.requireStepUp(s.handleRevokeDevice)))
	mux.HandleFunc("POST /api/v1/devices/pairing", s.requireAuth(s.requireStepUp(s.handleCreateDevicePairing)))
	mux.HandleFunc("GET /api/v1/push/key", s.requireAuth(s.handlePushKey))
	mux.HandleFunc("POST /api/v1/push/subscribe", s.requireAuth(s.handlePushSubscribe))
	mux.HandleFunc("POST /api/v1/push/unsubscribe", s.requireAuth(s.handlePushUnsubscribe))
	mux.HandleFunc("GET /api/v1/users", s.requireAuth(s.handleUsers))
	mux.HandleFunc("POST /api/v1/users", s.requireAuth(s.requireStepUp(s.handleCreateUser)))
	mux.HandleFunc("PATCH /api/v1/users/{id}", s.requireAuth(s.requireStepUp(s.handleUpdateUser)))
	mux.HandleFunc("GET /api/v1/hosts/{id}/grants", s.requireAuth(s.handleHostGrants))
	mux.HandleFunc("PUT /api/v1/hosts/{id}/grants/{userID}", s.requireAuth(s.requireStepUp(s.handleSetHostGrant)))
	mux.HandleFunc("DELETE /api/v1/hosts/{id}/grants/{userID}", s.requireAuth(s.requireStepUp(s.handleDeleteHostGrant)))

	mux.HandleFunc("GET /api/v1/hosts", s.requireAuth(s.handleHosts))
	mux.HandleFunc("POST /api/v1/hosts/pairing-codes", s.requireAuth(s.requireStepUp(s.handlePairingCode)))
	mux.HandleFunc("DELETE /api/v1/hosts/{id}", s.requireAuth(s.requireStepUp(s.handleRevokeHost)))
	mux.HandleFunc("POST /api/v1/hosts/{id}/confirm", s.requireAuth(s.handleConfirmHost))
	mux.HandleFunc("GET /api/v1/hosts/{id}/update", s.requireAuth(s.handleHostUpdate))
	mux.HandleFunc("POST /api/v1/hosts/{id}/runs", s.requireAuth(s.handleNewRun))

	mux.HandleFunc("GET /api/v1/projects", s.requireAuth(s.handleProjects))
	mux.HandleFunc("GET /api/v1/favorites", s.requireAuth(s.handleFavorites))
	mux.HandleFunc("GET /api/v1/notifications", s.requireAuth(s.handleNotifications))
	mux.HandleFunc("POST /api/v1/notifications/read", s.requireAuth(s.handleNotificationsRead))
	mux.HandleFunc("GET /api/v1/sessions", s.requireAuth(s.handleSessions))
	mux.HandleFunc("GET /api/v1/sessions/{id}", s.requireAuth(s.handleSession))
	mux.HandleFunc("GET /api/v1/sessions/{id}/transcript", s.requireAuth(s.handleTranscript))
	mux.HandleFunc("GET /api/v1/sessions/{id}/export", s.requireAuth(s.handleExport))
	mux.HandleFunc("GET /api/v1/sessions/{id}/git", s.requireAuth(s.handleSessionGit))
	mux.HandleFunc("POST /api/v1/sessions/{id}/runs", s.requireAuth(s.handleCreateRun))
	mux.HandleFunc("PUT /api/v1/sessions/{id}/favorite", s.requireAuth(func(w http.ResponseWriter, r *http.Request) { s.handleSetFavorite(w, r, true) }))
	mux.HandleFunc("DELETE /api/v1/sessions/{id}/favorite", s.requireAuth(func(w http.ResponseWriter, r *http.Request) { s.handleSetFavorite(w, r, false) }))
	mux.HandleFunc("GET /api/v1/search", s.requireAuth(s.handleSearch))
	mux.HandleFunc("GET /api/v1/audit", s.requireAuth(s.handleAudit))

	mux.HandleFunc("GET /api/v1/runs", s.requireAuth(s.handleActiveRuns))
	mux.HandleFunc("GET /api/v1/runs/{id}", s.requireAuth(s.handleRun))
	mux.HandleFunc("GET /api/v1/runs/{id}/options", s.requireAuth(s.handleRunOptions))
	mux.HandleFunc("POST /api/v1/runs/{id}/stop", s.requireAuth(s.handleStopRun))
	mux.HandleFunc("POST /api/v1/runs/{id}/lease", s.requireAuth(s.handleLease))

	mux.HandleFunc("GET /api/v1/ws", s.requireAuth(s.handleClientWS))
	mux.HandleFunc("GET /v1/host/connect", s.handleHostWS)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	if s.assets != nil {
		mux.Handle("/", s.assets)
	}
	return s.securityHeaders(s.csrfGuard(mux))
}

// ---- host connections ----

type hostConn struct {
	id     string
	ws     *websocket.Conn
	send   chan protocol.Envelope
	done   chan struct{}
	once   sync.Once
	signer *HubSigner
}

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

	if auth.HostName != "" {
		_, _ = s.store.db.Exec(`UPDATE hosts SET name=? WHERE id=?`, auth.HostName, hostID)
	}
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
	s.log.Info("host connected", "host_id", hostID, "version", auth.Version, "os", auth.OS, "arch", auth.Arch)
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
			s.log.Info("host disconnected", "host_id", hostID, "error", err)
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
			select {
			case p.ch <- msg:
			default:
			}
		}
	case "run.started":
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

func (s *Server) registerHost(hc *hostConn) {
	s.mu.Lock()
	old := s.hosts[hc.id]
	s.hosts[hc.id] = hc
	s.mu.Unlock()
	if old != nil {
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

func (s *Server) broadcastHostUpdate(id string) {
	host, err := s.store.HostByID(id)
	if err != nil {
		return
	}
	hv := host.HostView
	if s.hostOnline(id) {
		hv.Status = "online"
	}
	s.updateMu.Lock()
	if status, ok := s.hostUpdates[id]; ok {
		copyStatus := status
		hv.Update = &copyStatus
	}
	s.updateMu.Unlock()
	s.broadcast(protocol.ServerEnvelope{Type: "host.updated", Host: &hv})
}

// hostRequest sends one request to a host and waits for the matching result.
func (s *Server) hostRequest(ctx context.Context, hostID, method string, params []byte) ([]byte, error) {
	if !s.hostOnline(hostID) {
		return nil, errors.New("host is offline")
	}
	id := newID()
	p := &pendingRequest{hostID: hostID, ch: make(chan protocol.Envelope, 1)}
	s.mu.Lock()
	s.pending[id] = p
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()
	if err := s.sendHost(hostID, protocol.Envelope{Type: "request", RequestID: id, Method: method, Params: params}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
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
		subs: map[string]bool{},
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
		s.handleClientMessage(ctx, c, msg)
	}
}

func (s *Server) handleClientMessage(ctx context.Context, c *clientConn, msg protocol.ClientEnvelope) {
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
			for _, ch := range rs.chunks {
				if ch.seq > msg.LastSeq {
					chunks = append(chunks, protocol.ServerEnvelope{Type: "run.output", RunID: msg.RunID, Seq: ch.seq, Data: ch.data})
				}
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
		if strings.TrimSpace(msg.Text) == "" {
			return
		}
		if len(msg.Text) > maxPromptBytes {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "prompt is too large"})
			return
		}
		if msg.Options != nil && len(msg.Options.Attachments) > 6 {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "at most 6 attachments are allowed"})
			return
		}
		if msg.Options != nil {
			total := 0
			for _, attachment := range msg.Options.Attachments {
				total += len(attachment.Data)
			}
			if total > 24<<20 {
				c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "attachments are too large"})
				return
			}
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
		if msg.Decision != "once" && msg.Decision != "reject" {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: "invalid approval decision"})
			return
		}
		if err := s.sendHost(run.HostID, protocol.Envelope{Type: "run.approval", RunID: msg.RunID, ApprovalID: msg.ApprovalID, Decision: msg.Decision}); err != nil {
			c.push(protocol.ServerEnvelope{Type: "error", RunID: msg.RunID, Error: err.Error()})
		}
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

func (c *clientConn) push(msg protocol.ServerEnvelope) {
	select {
	case c.send <- msg:
	default:
		// A slow viewer is disconnected instead of blocking the hub; it can
		// reconnect and replay from its last sequence number.
		c.close()
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

func writeWS(ctx context.Context, ws *websocket.Conn, v protocol.Envelope) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return ws.Write(ctx, websocket.MessageText, data)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func trimScheme(origin string) string {
	if i := len("http://"); len(origin) > i && origin[:i] == "http://" {
		return origin[i:]
	}
	if i := len("https://"); len(origin) > i && origin[:i] == "https://" {
		return origin[i:]
	}
	return origin
}

func major(v string) string {
	for i, c := range v {
		if c == '.' {
			return v[:i]
		}
	}
	return v
}

func base64Decode(v string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(v)
}

func verifyEd25519(pub, message, sig []byte) bool {
	return ed25519Verify(pub, message, sig)
}
