package hub

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// TestRunApprovalLifecycleAndAdminQueueControl covers two hub-side rules:
// an administrator may pause another user's queue, and a reload must not show
// an approval card for a run that can no longer answer it.
func TestRunApprovalLifecycleAndAdminQueueControl(t *testing.T) {
	hub := newSetupHub(t)
	signer, err := OpenHubSigner(filepath.Join(t.TempDir(), "signing"))
	if err != nil {
		t.Fatal(err)
	}
	hub.server.SetSigner(signer)
	owner := hub.testOwner()
	host := startFakeHost(t, hub, owner, "approval-host")
	host.setAnswer(func(method string, _ json.RawMessage) (any, string) {
		return map[string]any{}, ""
	})
	now := time.Now()
	host.send(protocol.Envelope{Type: "sessions", Full: true, Sessions: []protocol.Session{{
		Agent: "opencode", NativeID: "approval-native", Title: "Approval", Cwd: "/work/approval",
		State: protocol.StateIdle, CreatedAt: now, UpdatedAt: now,
	}}})
	waitFor(t, "approval session", func() bool {
		list, err := hub.store.ListSessions(owner.ID, host.hostID, "", "", "", "", 10, 0, true)
		return err == nil && len(list) == 1
	})
	sess := sessionViewByNative(t, hub, owner, host.hostID, "approval-native")
	status, body := hub.do(nil, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs", map[string]any{"mode": "structured"}, nil)
	expectStatus(t, status, http.StatusAccepted, body, "approval run")
	var run protocol.RunView
	if err := json.Unmarshal(body, &run); err != nil {
		t.Fatal(err)
	}
	host.waitCommand(t, "run.start")

	// An administrator can take control of a run they did not start and pause
	// its queue.
	_, adminToken := hub.sessionFor("admin", "boss")
	admin := hub.clientWithToken(adminToken)
	status, body = hub.do(admin, http.MethodPost, "/api/v1/runs/"+run.ID+"/lease", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "admin lease")
	status, body = hub.do(admin, http.MethodPost, "/api/v1/runs/"+run.ID+"/queue/pause", map[string]any{"paused": true}, nil)
	expectStatus(t, status, http.StatusOK, body, "admin queue pause")
	if paused, _ := hub.store.PromptQueuePaused(run.ID); !paused {
		t.Fatal("admin pause was not persisted")
	}

	host.send(protocol.Envelope{Type: "run.started", RunID: run.ID, Reason: "structured"})
	host.send(protocol.Envelope{Type: "run.event", RunID: run.ID, Event: &protocol.RunEvent{
		Type: "approval.requested", ApprovalID: "a1", Action: "write", Text: "confirm",
	}})
	waitFor(t, "pending approval", func() bool {
		return hub.server.pendingApproval(run.ID) != nil
	})
	status, body = hub.do(nil, http.MethodGet, "/api/v1/runs/"+run.ID, nil, nil)
	expectStatus(t, status, http.StatusOK, body, "run with approval")
	if !strings.Contains(string(body), `"pending_approval"`) {
		t.Fatalf("pending approval missing: %s", body)
	}

	host.send(protocol.Envelope{Type: "run.exit", RunID: run.ID, Code: 1, Reason: "agent failed"})
	waitFor(t, "failed run", func() bool {
		got, _, err := hub.store.RunByID(run.ID)
		return err == nil && got.State == protocol.StateFailed
	})
	status, body = hub.do(nil, http.MethodGet, "/api/v1/runs/"+run.ID, nil, nil)
	expectStatus(t, status, http.StatusOK, body, "failed run")
	if strings.Contains(string(body), `"pending_approval"`) {
		t.Fatalf("failed run still exposes an approval: %s", body)
	}
}
