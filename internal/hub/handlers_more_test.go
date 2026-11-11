package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func newSetupHub(t *testing.T) *apiTestHub {
	t.Helper()
	hub := newAPITestHub(t, Config{HubName: "Test Hub"})
	if status, body := hub.setup(); status != http.StatusCreated {
		t.Fatalf("setup: %d %s", status, body)
	}
	if status, body := hub.do(nil, http.MethodPost, "/api/v1/auth/step-up/password", map[string]string{"password": "correct-horse-battery"}, nil); status != http.StatusOK {
		t.Fatalf("step-up: %d %s", status, body)
	}
	return hub
}

func TestAPISetupStatus(t *testing.T) {
	hub := newAPITestHub(t, Config{HubName: "Test Hub"})
	status, body := hub.do(nil, http.MethodGet, "/api/v1/setup/status", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "setup status")
	if !strings.Contains(string(body), `"setup_required":true`) {
		t.Fatalf("unexpected setup status: %s", body)
	}
	hub.setup()
	status, body = hub.do(nil, http.MethodGet, "/api/v1/setup/status", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "setup status after setup")
	if !strings.Contains(string(body), `"setup_required":false`) {
		t.Fatalf("unexpected setup status: %s", body)
	}
}

func TestAPIThemeFavoritesNotificationsProjects(t *testing.T) {
	hub := newSetupHub(t)
	owner, _ := hub.store.UserByUsername("owner")
	hostID, err := hub.store.CreateHost(owner.ID, "demo", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	sessions := []protocol.Session{{
		Agent: "opencode", NativeID: "ses_demo", Title: "Demo session", Cwd: "/work/demo",
		RepoURL: "https://github.com/example/demo", State: protocol.StateIdle,
		CreatedAt: time.Now(), UpdatedAt: time.Now(), MessageCount: 3,
	}}
	if _, _, err := hub.store.ReplaceHostSessions(hostID, sessions); err != nil {
		t.Fatal(err)
	}
	list, _ := hub.store.ListSessions(owner.ID, hostID, "", "", "", "", 10, 0, true)
	if len(list) != 1 {
		t.Fatalf("sessions: %+v", list)
	}
	sessionID := list[0].ID

	status, body := hub.do(nil, http.MethodGet, "/api/v1/me/settings", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "get settings")
	if !strings.Contains(string(body), `"theme":"system"`) {
		t.Fatalf("default theme: %s", body)
	}
	status, body = hub.do(nil, http.MethodPut, "/api/v1/me/settings", map[string]string{"theme": "dark"}, nil)
	expectStatus(t, status, http.StatusOK, body, "put settings")
	status, body = hub.do(nil, http.MethodPut, "/api/v1/me/settings", map[string]string{"theme": "neon"}, nil)
	expectStatus(t, status, http.StatusBadRequest, body, "invalid theme")

	status, body = hub.do(nil, http.MethodPut, "/api/v1/sessions/"+sessionID+"/favorite", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "favorite")
	status, body = hub.do(nil, http.MethodGet, "/api/v1/favorites", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "favorites")
	if !strings.Contains(string(body), sessionID) {
		t.Fatalf("favorite missing: %s", body)
	}
	status, body = hub.do(nil, http.MethodDelete, "/api/v1/sessions/"+sessionID+"/favorite", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "unfavorite")

	if err := hub.store.AddNotification(owner.ID, "completed", "Run finished", "A run finished.", "", "#/runs"); err != nil {
		t.Fatal(err)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/notifications?limit=10", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "notifications")
	if !strings.Contains(string(body), "Run finished") || !strings.Contains(string(body), `"unread":1`) {
		t.Fatalf("notification payload: %s", body)
	}
	status, body = hub.do(nil, http.MethodPost, "/api/v1/notifications/read", map[string]string{"id": "all"}, nil)
	expectStatus(t, status, http.StatusOK, body, "mark all read")
	status, body = hub.do(nil, http.MethodPost, "/api/v1/notifications/read", map[string]string{"id": "bad-json"}, nil)
	expectStatus(t, status, http.StatusOK, body, "mark unknown read")

	status, body = hub.do(nil, http.MethodGet, "/api/v1/projects?limit=10", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "projects")
	if !strings.Contains(string(body), "demo") {
		t.Fatalf("project payload: %s", body)
	}
}

func TestAPISearchAndExportPermissionGates(t *testing.T) {
	hub := newSetupHub(t)
	owner, _ := hub.store.UserByUsername("owner")
	hostID, err := hub.store.CreateHost(owner.ID, "demo", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := hub.store.ReplaceHostSessions(hostID, []protocol.Session{{
		Agent: "opencode", NativeID: "ses_demo", Title: "Demo", Cwd: "/work/demo",
		State: protocol.StateIdle, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	list, _ := hub.store.ListSessions(owner.ID, hostID, "", "", "", "", 10, 0, true)
	sessionID := list[0].ID

	status, body := hub.do(nil, http.MethodGet, "/api/v1/search?q=x", nil, nil)
	expectStatus(t, status, http.StatusBadRequest, body, "search query too short")
	status, body = hub.do(nil, http.MethodGet, "/api/v1/search?q=demo", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "search offline host")
	if !strings.Contains(string(body), `"results":[]`) {
		t.Fatalf("offline search: %s", body)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions/"+sessionID+"/export?format=markdown", nil, nil)
	expectStatus(t, status, http.StatusServiceUnavailable, body, "export offline host")
	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions/"+sessionID+"/export?format=nope", nil, nil)
	expectStatus(t, status, http.StatusServiceUnavailable, body, "export permission before format")
}

func TestAPIPasskeyAndDevicePairing(t *testing.T) {
	hub := newSetupHub(t)
	status, body := hub.do(nil, http.MethodGet, "/api/v1/auth/step-up", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "step-up status")
	status, body = hub.do(nil, http.MethodPost, "/api/v1/auth/step-up/begin", map[string]any{}, nil)
	expectStatus(t, status, http.StatusOK, body, "step-up begin")
	if !strings.Contains(string(body), `"method":"password"`) {
		t.Fatalf("step-up method: %s", body)
	}

	status, body = hub.do(nil, http.MethodPost, "/api/v1/auth/passkey/register/finish", map[string]any{"id": "bogus"}, nil)
	expectStatus(t, status, http.StatusBadRequest, body, "passkey register finish")

	status, body = hub.do(nil, http.MethodPost, "/api/v1/auth/passkey/finish", map[string]any{"id": "bogus"}, nil)
	expectStatus(t, status, http.StatusUnauthorized, body, "passkey login finish without a login session")

	status, body = hub.do(nil, http.MethodPost, "/api/v1/devices/pairing", map[string]any{}, nil)
	expectStatus(t, status, http.StatusCreated, body, "create device pairing")
	var pairing struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &pairing); err != nil || pairing.Token == "" {
		t.Fatalf("pairing token: %v %s", err, body)
	}
	anon := &http.Client{}
	status, body = hub.do(anon, http.MethodPost, "/api/v1/auth/device-pair", map[string]string{"token": pairing.Token, "name": "phone"}, nil)
	expectStatus(t, status, http.StatusOK, body, "device pair")
	status, body = hub.do(anon, http.MethodPost, "/api/v1/auth/device-pair", map[string]string{"token": pairing.Token, "name": "again"}, nil)
	expectStatus(t, status, http.StatusUnauthorized, body, "device pair single use")
}

func TestAPIDevicesPushAndHubKey(t *testing.T) {
	hub := newSetupHub(t)
	status, body := hub.do(nil, http.MethodGet, "/api/v1/devices", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "devices")
	if !strings.Contains(string(body), "current_device_id") {
		t.Fatalf("devices payload: %s", body)
	}
	var devices struct {
		Devices         []Device `json:"devices"`
		CurrentDeviceID string   `json:"current_device_id"`
	}
	if err := json.Unmarshal(body, &devices); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if devices.CurrentDeviceID == "" {
		t.Fatal("current device missing")
	}
	status, body = hub.do(nil, http.MethodDelete, "/api/v1/devices/"+devices.CurrentDeviceID, nil, nil)
	expectStatus(t, status, http.StatusBadRequest, body, "revoke current device")

	status, body = hub.do(nil, http.MethodGet, "/api/v1/passkeys", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "passkeys")

	status, body = hub.do(nil, http.MethodGet, "/api/v1/push/key", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "push key")
	if !strings.Contains(string(body), "public_key") {
		t.Fatalf("push key payload: %s", body)
	}
	status, body = hub.do(nil, http.MethodPost, "/api/v1/push/subscribe", map[string]any{"endpoint": "http://insecure", "keys": map[string]string{"p256dh": "x", "auth": "y"}}, nil)
	expectStatus(t, status, http.StatusBadRequest, body, "invalid push endpoint")
	status, body = hub.do(nil, http.MethodPost, "/api/v1/push/subscribe", map[string]any{"endpoint": "https://push.example/abc", "keys": map[string]string{"p256dh": "p", "auth": "a"}}, nil)
	expectStatus(t, status, http.StatusCreated, body, "push subscribe")
	status, body = hub.do(nil, http.MethodPost, "/api/v1/push/unsubscribe", map[string]string{"endpoint": "https://push.example/abc"}, nil)
	expectStatus(t, status, http.StatusOK, body, "push unsubscribe")

	status, body = hub.do(nil, http.MethodGet, "/api/v1/hub-key", nil, nil)
	expectStatus(t, status, http.StatusServiceUnavailable, body, "hub key without signer")
	signer, err := OpenHubSigner(filepath.Join(t.TempDir(), "signing"))
	if err != nil {
		t.Fatal(err)
	}
	hub.server.SetSigner(signer)
	status, body = hub.do(nil, http.MethodGet, "/api/v1/hub-key", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "hub key")
	if !strings.Contains(string(body), "public_key") {
		t.Fatalf("hub key payload: %s", body)
	}
}

func TestAPIHostAdminGates(t *testing.T) {
	hub := newSetupHub(t)
	owner, _ := hub.store.UserByUsername("owner")
	hostID, err := hub.store.CreateHost(owner.ID, "demo", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	status, body := hub.do(nil, http.MethodGet, "/api/v1/hosts/"+hostID+"/update", nil, nil)
	expectStatus(t, status, http.StatusServiceUnavailable, body, "update check offline host")
	status, body = hub.do(nil, http.MethodPost, "/api/v1/hosts/"+hostID+"/confirm", map[string]any{}, nil)
	expectStatus(t, status, http.StatusNotFound, body, "confirm without pending SAS")
	if err := hub.store.CreateHostConfirmation(hostID, "123-456"); err != nil {
		t.Fatal(err)
	}
	status, body = hub.do(nil, http.MethodPost, "/api/v1/hosts/"+hostID+"/confirm", map[string]any{}, nil)
	expectStatus(t, status, http.StatusOK, body, "confirm host")
}

func TestPasskeyBeginHandlers(t *testing.T) {
	hub := newAPITestHub(t, Config{HubName: "Test Hub", PublicURL: "https://nexus.example.com"})
	if status, body := hub.setup(); status != http.StatusCreated {
		t.Fatalf("setup: %d %s", status, body)
	}
	owner, _ := hub.store.UserByUsername("owner")

	req := httptest.NewRequest(http.MethodPost, "https://nexus.example.com/api/v1/auth/passkey/register/begin", nil)
	ctx := context.WithValue(req.Context(), ctxUser, owner)
	ctx = context.WithValue(ctx, ctxStepUp, time.Now().Add(time.Minute))
	rec := httptest.NewRecorder()
	hub.server.handlePasskeyRegisterBegin(rec, req.WithContext(ctx))
	if rec.Code != http.StatusOK {
		t.Fatalf("register begin: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "publicKey") {
		t.Fatalf("register options: %s", rec.Body.String())
	}

	body := strings.NewReader(`{"username":"owner"}`)
	req = httptest.NewRequest(http.MethodPost, "https://nexus.example.com/api/v1/auth/passkey/begin", body)
	rec = httptest.NewRecorder()
	hub.server.handlePasskeyLoginBegin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login begin without registered passkeys: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "passkey login unavailable") {
		t.Fatalf("login begin error: %s", rec.Body.String())
	}
}
