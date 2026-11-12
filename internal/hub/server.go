package hub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/coder/websocket"
)

const (
	ed25519PublicKeySize = 32
	hostReadLimit        = 32 << 20
	clientReadLimit      = 4 << 20 // terminal input is capped at 2 MiB base64
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

	indexMu   sync.Mutex
	hostIndex map[string]protocol.IndexStatus

	passkeySessions passkeySessionStore
	metrics         *metrics

	queueOnce  sync.Once
	queueKick  chan struct{}
	queueMu    sync.Mutex
	queueArmed map[string]bool
}

type pendingRequest struct {
	hostID    string
	requestID string
	method    string
	ch        chan protocol.Envelope
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
		hostIndex:          map[string]protocol.IndexStatus{},
		epoch:              newID(),
		metrics:            newMetrics(),
		queueKick:          make(chan struct{}, 1),
		queueArmed:         map[string]bool{},
	}
	s.ensureVAPID()
	store.RecoverPromptQueue()
	if queued, _ := store.HasQueuedPrompts(); queued {
		s.kickQueue()
	}
	return s
}

func (s *Server) SetAssets(h http.Handler) { s.assets = h }

func (s *Server) SetSigner(signer *HubSigner) { s.signer = signer }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	if s.cfg.MetricsEnabled {
		mux.HandleFunc("GET /metrics", s.handleMetrics)
	}
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
	mux.HandleFunc("PUT /api/v1/me/settings/notifications/hosts/{hostID}", s.requireAuth(s.handlePutHostNotificationPrefs))
	mux.HandleFunc("DELETE /api/v1/me/settings/notifications/hosts/{hostID}", s.requireAuth(s.handleDeleteHostNotificationPrefs))
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
	mux.HandleFunc("PATCH /api/v1/hosts/{id}", s.requireAuth(s.requireStepUp(s.handleHostUpdateSettings)))
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
	mux.HandleFunc("GET /api/v1/sessions/tags", s.requireAuth(s.handleSessionTags))
	mux.HandleFunc("GET /api/v1/sessions/{id}", s.requireAuth(s.handleSession))
	mux.HandleFunc("PATCH /api/v1/sessions/{id}", s.requireAuth(s.handleSessionMetadata))
	mux.HandleFunc("POST /api/v1/sessions/{id}/tags", s.requireAuth(s.handleAddSessionTag))
	mux.HandleFunc("DELETE /api/v1/sessions/{id}/tags/{tag}", s.requireAuth(s.handleDeleteSessionTag))
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
	mux.HandleFunc("GET /api/v1/runs/{id}/queue", s.requireAuth(s.handleGetPromptQueue))
	mux.HandleFunc("POST /api/v1/runs/{id}/queue", s.requireAuth(s.handleAddPromptQueue))
	mux.HandleFunc("PATCH /api/v1/runs/{id}/queue/{itemID}", s.requireAuth(s.handleEditPromptQueue))
	mux.HandleFunc("DELETE /api/v1/runs/{id}/queue/{itemID}", s.requireAuth(s.handleDeletePromptQueue))
	mux.HandleFunc("PUT /api/v1/runs/{id}/queue/order", s.requireAuth(s.handleReorderPromptQueue))
	mux.HandleFunc("POST /api/v1/runs/{id}/queue/pause", s.requireAuth(s.handlePausePromptQueue))

	mux.HandleFunc("GET /api/v1/ws", s.requireAuth(s.handleClientWS))
	mux.HandleFunc("GET /v1/host/connect", s.handleHostWS)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	if s.assets != nil {
		mux.Handle("/", s.assets)
	}
	return s.observeHTTP(s.securityHeaders(bodyLimit(s.csrfGuard(mux))))
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
