package hub

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func (h *apiTestHub) testOwner() User {
	h.t.Helper()
	users, err := h.store.ListUsers()
	if err != nil || len(users) == 0 {
		h.t.Fatalf("owner user missing: %v", err)
	}
	return users[0]
}

func (h *apiTestHub) setupOK() {
	h.t.Helper()
	status, body := h.setup()
	if status != http.StatusCreated {
		h.t.Fatalf("setup failed: %d %s", status, body)
	}
}

func sessionTokenFromJar(t *testing.T, hub *apiTestHub) string {
	t.Helper()
	base, _ := url.Parse(hub.http.URL)
	for _, cookie := range hub.client.Jar.Cookies(base) {
		if cookie.Name == cookieName {
			return cookie.Value
		}
	}
	t.Fatal("session cookie missing")
	return ""
}

func TestHostRenameAndDisable(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "old-name", make([]byte, 32), "windows", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateHostConfirmation(hostID, "111-222"); err != nil {
		t.Fatal(err)
	}
	// The route is step-up protected. setup() does not grant step-up, so the
	// first attempt proves the guard exists; the owner session is then marked
	// as stepped up just like a password/passkey confirmation would.
	status, body := hub.do(nil, http.MethodPatch, "/api/v1/hosts/"+hostID, map[string]any{"name": "renamed"}, nil)
	expectStatus(t, status, http.StatusForbidden, body, "rename without step-up")
	if !strings.Contains(string(body), "step_up_required") {
		t.Fatalf("expected step_up_required, got %s", body)
	}

	token := sessionTokenFromJar(t, hub)
	if err := hub.store.MarkStepUp(token, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	status, body = hub.do(nil, http.MethodPatch, "/api/v1/hosts/"+hostID, map[string]any{"name": "renamed"}, nil)
	expectStatus(t, status, http.StatusOK, body, "rename")
	host, err := hub.store.HostByID(hostID)
	if err != nil || host.Name != "renamed" {
		t.Fatalf("rename not applied: %+v %v", host, err)
	}
	if host.DisabledAt != nil {
		t.Fatal("host must still be enabled")
	}

	status, body = hub.do(nil, http.MethodPatch, "/api/v1/hosts/"+hostID, map[string]any{"disabled": true}, nil)
	expectStatus(t, status, http.StatusOK, body, "disable")
	host, err = hub.store.HostByID(hostID)
	if err != nil || host.DisabledAt == nil {
		t.Fatalf("disable not applied: %+v %v", host, err)
	}
	if !host.Disabled {
		t.Fatal("host view must report disabled")
	}
	status, body = hub.do(nil, http.MethodPost, "/api/v1/hosts/"+hostID+"/runs", map[string]any{"agent": "opencode", "cwd": "/tmp"}, nil)
	expectStatus(t, status, http.StatusConflict, body, "new run on disabled host")
	if !strings.Contains(string(body), "disabled") {
		t.Fatalf("expected a disabled-host error, got %s", body)
	}
	status, body = hub.do(nil, http.MethodPatch, "/api/v1/hosts/"+hostID, map[string]any{"disabled": false}, nil)
	expectStatus(t, status, http.StatusOK, body, "enable")
	host, err = hub.store.HostByID(hostID)
	if err != nil || host.DisabledAt != nil {
		t.Fatalf("enable not applied: %+v %v", host, err)
	}
}

func TestRunConflictReasonsAndForcedResume(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "conflict-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := hub.store.ReplaceHostSessions(hostID, []protocol.Session{{
		NativeID: "ses-native-1", Agent: "opencode", Title: "Existing", Cwd: "/tmp/project",
		CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now(), ActiveElsewhere: true,
	}}); err != nil {
		t.Fatal(err)
	}
	sessions, err := hub.store.ListSessions(owner.ID, hostID, "", "", "", "", 10, 0, true)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("session missing: %v %v", sessions, err)
	}
	sess := sessions[0]

	// Mark the host online without a real WebSocket writer. sendHost only
	// enqueues, which is enough to prove the conflict handling before a run is
	// accepted.
	hc := &hostConn{id: hostID, send: make(chan protocol.Envelope, 8), done: make(chan struct{})}
	hub.server.mu.Lock()
	hub.server.hosts[hostID] = hc
	hub.server.mu.Unlock()
	t.Cleanup(func() {
		hub.server.mu.Lock()
		delete(hub.server.hosts, hostID)
		hub.server.mu.Unlock()
	})

	status, body := hub.do(nil, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs", map[string]any{"mode": "terminal"}, nil)
	expectStatus(t, status, http.StatusConflict, body, "active elsewhere")
	var conflict map[string]any
	if err := json.Unmarshal(body, &conflict); err != nil {
		t.Fatal(err)
	}
	if conflict["reason"] != "active_elsewhere" {
		t.Fatalf("unexpected conflict reason: %s", body)
	}

	// Forcing needs step-up. Without it the request is rejected before a run
	// row is created.
	status, body = hub.do(nil, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs", map[string]any{"mode": "terminal", "force": true}, nil)
	expectStatus(t, status, http.StatusForbidden, body, "force without step-up")

	token := sessionTokenFromJar(t, hub)
	if err := hub.store.MarkStepUp(token, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	status, body = hub.do(nil, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs", map[string]any{"mode": "terminal", "force": true}, nil)
	expectStatus(t, status, http.StatusAccepted, body, "forced resume")
	if !strings.Contains(string(body), `"id"`) {
		t.Fatalf("run response missing: %s", body)
	}

	// A second writer on the same native session is refused with active_run
	// and carries the existing run id for the Attach action.
	status, body = hub.do(nil, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs", map[string]any{"mode": "terminal"}, nil)
	expectStatus(t, status, http.StatusConflict, body, "active run")
	conflict = map[string]any{}
	if err := json.Unmarshal(body, &conflict); err != nil {
		t.Fatal(err)
	}
	if conflict["reason"] != "active_run" || conflict["run_id"] == "" {
		t.Fatalf("unexpected active_run conflict: %s", body)
	}
}
