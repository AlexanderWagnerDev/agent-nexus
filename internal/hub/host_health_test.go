package hub

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestBuildHostHealth(t *testing.T) {
	base := protocol.HostView{Status: "online", LastSeen: time.Now()}
	if got := buildHostHealth(base, true); got.State != "degraded" {
		t.Fatalf("host without policy or agents should be degraded: %+v", got)
	}
	base.Policy = &protocol.PolicySummary{AllowedRoots: []string{"/work"}}
	base.Agents = []protocol.AgentInfo{{Name: "opencode", Version: "2.2.7", Supported: true}}
	if got := buildHostHealth(base, true); got.State != "healthy" {
		t.Fatalf("healthy host: %+v", got)
	}
	base.Agents = append(base.Agents, protocol.AgentInfo{Name: "claude", Version: "9.0.0", Supported: true, Untested: true})
	if got := buildHostHealth(base, true); got.State != "degraded" || got.Reasons[0].Code != "agent_untested_version" {
		t.Fatalf("untested agent: %+v", got)
	}
	base.Agents = base.Agents[:1]
	base.Update = &protocol.UpdateStatus{State: protocol.UpdateStateBlocked, Message: "signature failed"}
	if got := buildHostHealth(base, true); got.State != "degraded" {
		t.Fatalf("blocked update should degrade health: %+v", got)
	}
	base.Update = nil
	base.Disabled = true
	if got := buildHostHealth(base, true); got.State != "disabled" {
		t.Fatalf("disabled host: %+v", got)
	}
	if got := buildHostHealth(base, false); got.State != "disabled" {
		t.Fatalf("disabled host is disabled regardless of link: %+v", got)
	}
}

func TestAPIHostDiagnostics(t *testing.T) {
	hub := newSetupHub(t)
	owner, _ := hub.store.UserByUsername("owner")
	host := startFakeHost(t, hub, owner, "diagnostics-host")
	host.setAnswer(func(method string, params json.RawMessage) (any, string) {
		if method != "doctor" {
			return nil, "unexpected method"
		}
		return map[string]any{"version": "test", "issues": 0, "checks": []map[string]string{{"id": "pairing", "status": "ok", "detail": "paired"}}}, ""
	})
	status, body := hub.do(nil, http.MethodGet, "/api/v1/hosts/"+host.hostID+"/diagnostics", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "host diagnostics")
	if !jsonContains(body, `"pairing"`) || !jsonContains(body, `"ok"`) {
		t.Fatalf("diagnostics payload: %s", body)
	}
}

func TestAPIHostDetail(t *testing.T) {
	hub := newAPITestHub(t, Config{HubName: "Test Hub"})
	if status, body := hub.setup(); status != http.StatusCreated {
		t.Fatalf("setup: %d %s", status, body)
	}
	owner, _ := hub.store.UserByUsername("owner")
	hostID, err := hub.store.CreateHost(owner.ID, "detail-host", make([]byte, 32), "windows", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if status, body := hub.do(nil, http.MethodPut, "/api/v1/hosts/"+hostID+"/favorite", nil, nil); status != http.StatusOK {
		t.Fatalf("favorite host: %d %s", status, body)
	}
	if favorites, _ := hub.store.FavoriteHostIDs(owner.ID); !favorites[hostID] {
		t.Fatal("host favorite not stored")
	}
	agents := []protocol.AgentInfo{{Name: "opencode", Version: "2.2.7", Path: "opencode", Supported: true, Capabilities: []string{"terminal", "structured"}}}
	policy := &protocol.PolicySummary{AllowedRoots: []string{"C:/work"}, AllowedAgents: []string{"opencode"}, AllowNew: true}
	if err := hub.store.SetHostInventory(hostID, "alpha", agents, policy); err != nil {
		t.Fatal(err)
	}
	sessions := []protocol.Session{{
		Agent: "opencode", NativeID: "ses_detail", Title: "Detail session",
		Cwd: "C:/work/demo", State: protocol.StateIdle, MessageCount: 2,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}
	if _, _, err := hub.store.ReplaceHostSessions(hostID, sessions); err != nil {
		t.Fatal(err)
	}

	status, body := hub.do(nil, http.MethodGet, "/api/v1/hosts/"+hostID, nil, nil)
	expectStatus(t, status, http.StatusOK, body, "host detail")
	text := string(body)
	for _, want := range []string{`"host"`, `"health"`, `"reasons"`, `"agents"`, `"recent_sessions"`, `"events"`, `"can_manage":true`} {
		if !strings.Contains(text, want) {
			t.Fatalf("detail missing %s: %s", want, text)
		}
	}
	if strings.Contains(text, "password") || strings.Contains(text, "secret") {
		t.Fatalf("detail leaked secret-like fields: %s", text)
	}
}
