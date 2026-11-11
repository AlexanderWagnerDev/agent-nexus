package hub

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

type apiTestHub struct {
	t      *testing.T
	server *Server
	store  *Store
	http   *httptest.Server
	client *http.Client
}

func newAPITestHub(t *testing.T, cfg Config) *apiTestHub {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	box, err := OpenSecretBox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.SetSecretBox(box)
	srv := NewServer(cfg, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(srv.Handler())
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ts.Close()
		store.Close()
	})
	return &apiTestHub{t: t, server: srv, store: store, http: ts, client: &http.Client{Jar: jar}}
}

func (h *apiTestHub) do(client *http.Client, method, path string, body any, mutate func(*http.Request)) (int, []byte) {
	h.t.Helper()
	if client == nil {
		client = h.client
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, h.http.URL+path, reader)
	if err != nil {
		h.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		req.Header.Set("X-Nexus-Request", "1")
	}
	if mutate != nil {
		mutate(req)
	}
	res, err := client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, data
}

func (h *apiTestHub) setup() (int, []byte) {
	return h.do(nil, http.MethodPost, "/api/v1/setup", map[string]string{
		"username": "owner", "password": "correct-horse-battery",
	}, nil)
}

func (h *apiTestHub) clientWithToken(token string) *http.Client {
	jar, _ := cookiejar.New(nil)
	base, _ := url.Parse(h.http.URL)
	jar.SetCookies(base, []*http.Cookie{{Name: cookieName, Value: token}})
	return &http.Client{Jar: jar}
}

func (h *apiTestHub) sessionFor(role, username string) (User, string) {
	h.t.Helper()
	hash, err := HashPassword("correct-horse-battery")
	if err != nil {
		h.t.Fatal(err)
	}
	user, err := h.store.CreateUserWithRole(username, hash, NewTOTPSecret(), role)
	if err != nil {
		h.t.Fatal(err)
	}
	token, _, err := h.store.CreateSession(user.ID, "test device")
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.store.MarkStepUp(token, 5*time.Minute); err != nil {
		h.t.Fatal(err)
	}
	return user, token
}

func expectStatus(t *testing.T, got, want int, body []byte, label string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: status %d, want %d; body=%s", label, got, want, string(body))
	}
}

func TestAPISetupLoginAndCookies(t *testing.T) {
	hub := newAPITestHub(t, Config{HubName: "Test Hub"})
	status, body := hub.setup()
	expectStatus(t, status, http.StatusCreated, body, "setup")

	status, body = hub.do(nil, http.MethodPost, "/api/v1/setup", map[string]string{
		"username": "other", "password": "correct-horse-battery",
	}, nil)
	expectStatus(t, status, http.StatusConflict, body, "second setup")

	status, body = hub.do(nil, http.MethodGet, "/api/v1/me", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "me")
	if !strings.Contains(string(body), `"role":"owner"`) {
		t.Fatalf("unexpected me body: %s", body)
	}

	status, body = hub.do(nil, http.MethodGet, "/healthz", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "health")
	if !strings.Contains(string(body), `"version":"alpha"`) {
		t.Fatalf("health version: %s", body)
	}

	status, body = hub.do(nil, http.MethodPost, "/api/v1/auth/logout", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "logout")
	status, body = hub.do(nil, http.MethodGet, "/api/v1/me", nil, nil)
	expectStatus(t, status, http.StatusUnauthorized, body, "me after logout")

	status, body = hub.do(nil, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "owner", "password": "wrong-password",
	}, nil)
	expectStatus(t, status, http.StatusUnauthorized, body, "wrong login")

	status, body = hub.do(nil, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "owner", "password": "correct-horse-battery",
	}, nil)
	expectStatus(t, status, http.StatusOK, body, "login")
}

func TestAPICookieFlags(t *testing.T) {
	hub := newAPITestHub(t, Config{HubName: "Test Hub"})
	raw, err := json.Marshal(map[string]string{"username": "owner", "password": "correct-horse-battery"})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, hub.http.URL+"/api/v1/setup", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Nexus-Request", "1")
	res, err := hub.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d", res.StatusCode)
	}
	cookies := res.Cookies()
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == cookieName {
			session = c
		}
	}
	if session == nil {
		t.Fatal("session cookie missing")
	}
	if !session.HttpOnly {
		t.Fatal("session cookie is not HttpOnly")
	}
	if session.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie SameSite=%v", session.SameSite)
	}
	if session.Secure {
		t.Fatal("plain HTTP development cookie must not be Secure")
	}
	if session.MaxAge <= 0 {
		t.Fatalf("session cookie MaxAge=%d", session.MaxAge)
	}
}

func TestAPISecureCookieAndHSTS(t *testing.T) {
	hub := newAPITestHub(t, Config{HubName: "Test Hub", PublicURL: "https://nexus.example.com"})
	raw, _ := json.Marshal(map[string]string{"username": "owner", "password": "correct-horse-battery"})
	req, _ := http.NewRequest(http.MethodPost, hub.http.URL+"/api/v1/setup", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Nexus-Request", "1")
	res, err := hub.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d", res.StatusCode)
	}
	if got := res.Header.Get("Strict-Transport-Security"); got == "" {
		t.Fatal("HSTS header missing behind TLS")
	}
	var secure bool
	for _, c := range res.Cookies() {
		if c.Name == cookieName {
			secure = c.Secure
		}
	}
	if !secure {
		t.Fatal("session cookie must be Secure behind TLS")
	}
	status, body := hub.do(nil, http.MethodPost, "/api/v1/auth/logout", nil, func(req *http.Request) {
		req.Header.Set("Origin", "https://nexus.example.com")
	})
	expectStatus(t, status, http.StatusOK, body, "logout with public origin")
}

func TestAPICSRFGuard(t *testing.T) {
	hub := newAPITestHub(t, Config{HubName: "Test Hub"})
	if status, body := hub.setup(); status != http.StatusCreated {
		t.Fatalf("setup: %d %s", status, body)
	}

	req, _ := http.NewRequest(http.MethodPost, hub.http.URL+"/api/v1/auth/logout", nil)
	res, err := hub.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("missing marker: %d", res.StatusCode)
	}

	status, body := hub.do(nil, http.MethodPost, "/api/v1/auth/logout", nil, func(req *http.Request) {
		req.Header.Set("Origin", "https://evil.example")
	})
	expectStatus(t, status, http.StatusForbidden, body, "foreign origin")

	status, body = hub.do(nil, http.MethodPost, "/api/v1/auth/logout", nil, func(req *http.Request) {
		req.Header.Set("Origin", hub.http.URL)
	})
	expectStatus(t, status, http.StatusOK, body, "same origin")
}

func TestAPILoginRateLimit(t *testing.T) {
	hub := newAPITestHub(t, Config{HubName: "Test Hub"})
	if status, body := hub.setup(); status != http.StatusCreated {
		t.Fatalf("setup: %d %s", status, body)
	}
	hub.do(nil, http.MethodPost, "/api/v1/auth/logout", nil, nil)
	ip := func(req *http.Request) { req.Header.Set("X-Forwarded-For", "203.0.113.9") }
	wrong := map[string]string{"username": "owner", "password": "not-the-password"}
	for i := 0; i < 10; i++ {
		status, body := hub.do(nil, http.MethodPost, "/api/v1/auth/login", wrong, ip)
		if status != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d %s", i+1, status, body)
		}
	}
	status, body := hub.do(nil, http.MethodPost, "/api/v1/auth/login", wrong, ip)
	expectStatus(t, status, http.StatusTooManyRequests, body, "rate limited")
	status, body = hub.do(nil, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "owner", "password": "correct-horse-battery",
	}, ip)
	expectStatus(t, status, http.StatusTooManyRequests, body, "correct login while limited")
}

func TestAPIStepUpRequired(t *testing.T) {
	hub := newAPITestHub(t, Config{HubName: "Test Hub"})
	if status, body := hub.setup(); status != http.StatusCreated {
		t.Fatalf("setup: %d %s", status, body)
	}
	status, body := hub.do(nil, http.MethodPost, "/api/v1/hosts/pairing-codes", map[string]any{}, nil)
	expectStatus(t, status, http.StatusForbidden, body, "pairing code without step-up")
	if !strings.Contains(string(body), "step_up_required") {
		t.Fatalf("step-up marker missing: %s", body)
	}
	status, body = hub.do(nil, http.MethodPost, "/api/v1/auth/step-up/password", map[string]string{"password": "wrong"}, nil)
	expectStatus(t, status, http.StatusUnauthorized, body, "wrong step-up password")
	status, body = hub.do(nil, http.MethodPost, "/api/v1/auth/step-up/password", map[string]string{"password": "correct-horse-battery"}, nil)
	expectStatus(t, status, http.StatusOK, body, "step-up")
	status, body = hub.do(nil, http.MethodPost, "/api/v1/hosts/pairing-codes", map[string]any{}, nil)
	expectStatus(t, status, http.StatusCreated, body, "pairing code after step-up")
}

func TestAPIRBACAndPerHostGrants(t *testing.T) {
	hub := newAPITestHub(t, Config{HubName: "Test Hub"})
	if status, body := hub.setup(); status != http.StatusCreated {
		t.Fatalf("setup: %d %s", status, body)
	}
	owner, _ := hub.store.UserByUsername("owner")
	_, adminToken := hub.sessionFor("admin", "admin-user")
	_, memberToken := hub.sessionFor("member", "member-user")
	_, viewOnlyToken := hub.sessionFor("member", "view-only-user")
	_, viewerToken := hub.sessionFor("viewer", "viewer-user")
	if status, body := hub.do(nil, http.MethodPost, "/api/v1/auth/step-up/password", map[string]string{"password": "correct-horse-battery"}, nil); status != http.StatusOK {
		t.Fatalf("owner step-up: %d %s", status, body)
	}
	adminClient := hub.clientWithToken(adminToken)
	memberClient := hub.clientWithToken(memberToken)
	viewOnlyClient := hub.clientWithToken(viewOnlyToken)
	viewerClient := hub.clientWithToken(viewerToken)

	hostID, err := hub.store.CreateHost(owner.ID, "test-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	sessions := []protocol.Session{{
		Agent: "opencode", NativeID: "ses_test", Title: "Test session",
		Cwd: "/work/demo", State: protocol.StateIdle, MessageCount: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}
	if _, _, err := hub.store.ReplaceHostSessions(hostID, sessions); err != nil {
		t.Fatal(err)
	}
	list, err := hub.store.ListSessions(owner.ID, hostID, "", "", "", "", 10, 0, true)
	if err != nil || len(list) != 1 {
		t.Fatalf("sessions: %v %v", list, err)
	}
	sessionID := list[0].ID

	member, _ := hub.store.UserByUsername("member-user")
	viewOnly, _ := hub.store.UserByUsername("view-only-user")
	if err := hub.store.SetHostGrant(hostID, member.ID, "view,read_transcript,resume,create,manage"); err != nil {
		t.Fatal(err)
	}
	if err := hub.store.SetHostGrant(hostID, viewOnly.ID, "view"); err != nil {
		t.Fatal(err)
	}

	// Directory is visible to any logged-in user, but roles are hidden below admin.
	status, body := hub.do(viewerClient, http.MethodGet, "/api/v1/users", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "viewer user directory")
	if strings.Contains(string(body), `"role":"owner"`) {
		t.Fatalf("viewer saw owner roles: %s", body)
	}

	// Only the owner may create users.
	status, body = hub.do(nil, http.MethodPost, "/api/v1/users", map[string]string{"username": "new-user", "password": "correct-horse-battery", "role": "member"}, nil)
	expectStatus(t, status, http.StatusCreated, body, "owner creates user")
	status, body = hub.do(adminClient, http.MethodPost, "/api/v1/users", map[string]string{"username": "new-user-2", "password": "correct-horse-battery", "role": "member"}, nil)
	expectStatus(t, status, http.StatusForbidden, body, "admin cannot create user")

	// Audit is owner/admin only.
	for name, client := range map[string]*http.Client{"owner": nil, "admin": adminClient} {
		status, body = hub.do(client, http.MethodGet, "/api/v1/audit", nil, nil)
		expectStatus(t, status, http.StatusOK, body, "audit "+name)
	}
	for name, client := range map[string]*http.Client{"member": memberClient, "viewer": viewerClient} {
		status, body = hub.do(client, http.MethodGet, "/api/v1/audit", nil, nil)
		expectStatus(t, status, http.StatusForbidden, body, "audit denied "+name)
	}

	// Pairing codes: member allowed, viewer forbidden.
	status, body = hub.do(memberClient, http.MethodPost, "/api/v1/hosts/pairing-codes", map[string]any{}, nil)
	expectStatus(t, status, http.StatusCreated, body, "member pairing code")
	status, body = hub.do(viewerClient, http.MethodPost, "/api/v1/hosts/pairing-codes", map[string]any{}, nil)
	expectStatus(t, status, http.StatusForbidden, body, "viewer pairing code")

	// Grants listing requires manage.
	grantsPath := "/api/v1/hosts/" + hostID + "/grants"
	for name, client := range map[string]*http.Client{"owner": nil, "admin": adminClient, "member-manage": memberClient} {
		status, body = hub.do(client, http.MethodGet, grantsPath, nil, nil)
		expectStatus(t, status, http.StatusOK, body, "grants "+name)
	}
	for name, client := range map[string]*http.Client{"view-only": viewOnlyClient, "viewer": viewerClient} {
		status, body = hub.do(client, http.MethodGet, grantsPath, nil, nil)
		expectStatus(t, status, http.StatusForbidden, body, "grants denied "+name)
	}

	// Session visibility and the transcript permission gate.
	sessionPath := "/api/v1/sessions/" + sessionID
	for name, client := range map[string]*http.Client{"owner": nil, "admin": adminClient, "member": memberClient, "view-only": viewOnlyClient} {
		status, body = hub.do(client, http.MethodGet, sessionPath, nil, nil)
		expectStatus(t, status, http.StatusOK, body, "session "+name)
	}
	status, body = hub.do(viewerClient, http.MethodGet, sessionPath, nil, nil)
	expectStatus(t, status, http.StatusNotFound, body, "session viewer (hidden)")

	status, body = hub.do(memberClient, http.MethodGet, sessionPath+"/transcript?limit=10", nil, nil)
	expectStatus(t, status, http.StatusServiceUnavailable, body, "transcript member (host offline)")
	status, body = hub.do(viewOnlyClient, http.MethodGet, sessionPath+"/transcript?limit=10", nil, nil)
	expectStatus(t, status, http.StatusForbidden, body, "transcript view-only")

	// Run creation requires resume/create and then reaches the offline host.
	status, body = hub.do(memberClient, http.MethodPost, sessionPath+"/runs", map[string]string{"mode": "terminal"}, nil)
	expectStatus(t, status, http.StatusServiceUnavailable, body, "member resume (host offline)")
	status, body = hub.do(viewOnlyClient, http.MethodPost, sessionPath+"/runs", map[string]string{"mode": "terminal"}, nil)
	expectStatus(t, status, http.StatusForbidden, body, "view-only resume")
	status, body = hub.do(memberClient, http.MethodPost, "/api/v1/hosts/"+hostID+"/runs", map[string]string{"agent": "opencode", "cwd": "/work/demo"}, nil)
	expectStatus(t, status, http.StatusServiceUnavailable, body, "member create (host offline)")
	status, body = hub.do(viewOnlyClient, http.MethodPost, "/api/v1/hosts/"+hostID+"/runs", map[string]string{"agent": "opencode", "cwd": "/work/demo"}, nil)
	expectStatus(t, status, http.StatusForbidden, body, "view-only create")

	// Revoking a host requires manage.
	status, body = hub.do(viewOnlyClient, http.MethodDelete, "/api/v1/hosts/"+hostID, nil, nil)
	expectStatus(t, status, http.StatusForbidden, body, "view-only revoke")
	status, body = hub.do(memberClient, http.MethodDelete, "/api/v1/hosts/"+hostID, nil, nil)
	expectStatus(t, status, http.StatusOK, body, "member revoke with manage")
}

func TestHostPermissionMatrix(t *testing.T) {
	hub := newAPITestHub(t, Config{HubName: "Test Hub"})
	if status, body := hub.setup(); status != http.StatusCreated {
		t.Fatalf("setup: %d %s", status, body)
	}
	owner, _ := hub.store.UserByUsername("owner")
	hostID, err := hub.store.CreateHost(owner.ID, "matrix-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	user, _ := hub.sessionFor("member", "matrix-user")
	perms := []string{"view", "read_transcript", "resume", "create", "fork", "terminal_input", "approve", "manage"}
	for _, need := range perms {
		if hub.server.hostPermission(user, hostID, need) {
			t.Fatalf("%s allowed without a grant", need)
		}
		if err := hub.store.SetHostGrant(hostID, user.ID, need); err != nil {
			t.Fatal(err)
		}
		if !hub.server.hostPermission(user, hostID, need) {
			t.Fatalf("%s denied with an explicit grant", need)
		}
	}
	if err := hub.store.SetHostGrant(hostID, user.ID, "manage"); err != nil {
		t.Fatal(err)
	}
	for _, need := range perms {
		if !hub.server.hostPermission(user, hostID, need) {
			t.Fatalf("%s denied by the manage implication", need)
		}
	}
}
