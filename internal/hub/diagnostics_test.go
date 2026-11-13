package hub

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestAdminDiagnosticsAndRedactedExport(t *testing.T) {
	hub := newSetupHub(t)
	owner, _ := hub.store.UserByUsername("owner")
	hostID, err := hub.store.CreateHost(owner.ID, "secret-host-name", make([]byte, 32), "windows", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.SetHostInventory(hostID, "alpha", []protocol.AgentInfo{{Name: "opencode", Version: "2.2.7", Supported: true}}, &protocol.PolicySummary{AllowedRoots: []string{"C:/work"}}); err != nil {
		t.Fatal(err)
	}
	hub.server.updateMu.Lock()
	hub.server.hostUpdates[hostID] = protocol.UpdateStatus{Current: "alpha", Latest: "alpha-2", Available: true, CheckedAt: time.Now().UTC()}
	hub.server.updateMu.Unlock()
	status, body := hub.do(nil, http.MethodGet, "/api/v1/admin/diagnostics", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "diagnostics")
	text := string(body)
	for _, want := range []string{`"counts"`, `"database"`, `"hosts"`, `"secret-host-name"`, `"uptime_seconds"`, `"errors"`, `"latest":"alpha-2"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("diagnostics missing %s: %s", want, text)
		}
	}
	if strings.Contains(text, "password") || strings.Contains(text, "token_hash") {
		t.Fatalf("diagnostics leaked secret material: %s", text)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/admin/diagnostics/export", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "diagnostics export")
	exported := string(body)
	if strings.Contains(exported, "secret-host-name") {
		t.Fatalf("export leaked host identity: %s", exported)
	}
	if !strings.Contains(exported, "alpha-2") {
		t.Fatalf("export dropped the update metadata: %s", exported)
	}
	if !strings.Contains(exported, `"redaction"`) {
		t.Fatalf("export missing redaction marker: %s", exported)
	}
}

func TestDiagnosticsRequireAdmin(t *testing.T) {
	hub := newSetupHub(t)
	_, memberToken := hub.sessionFor("member", "diag-member")
	client := hub.clientWithToken(memberToken)
	status, body := hub.do(client, http.MethodGet, "/api/v1/admin/diagnostics", nil, nil)
	expectStatus(t, status, http.StatusForbidden, body, "member diagnostics")
	status, body = hub.do(client, http.MethodGet, "/api/v1/admin/diagnostics/export", nil, nil)
	expectStatus(t, status, http.StatusForbidden, body, "member export")
}
