package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/coder/websocket"
)

// TestPersistentApprovalRequiresStepUp pins the documented rule from
// docs/SECURITY.md section 11: the agent's persistent permission option is
// only offered after step-up. A session without step-up must not be able to
// turn one click into a lasting grant.
func TestPersistentApprovalRequiresStepUp(t *testing.T) {
	hub := newAPITestHub(t, Config{HubName: "Test Hub"})
	if status, body := hub.setup(); status != http.StatusCreated {
		t.Fatalf("setup: %d %s", status, body)
	}
	owner, _ := hub.store.UserByUsername("owner")
	token, _, err := hub.store.CreateSession(owner.ID, "no-stepup")
	if err != nil {
		t.Fatal(err)
	}
	client := hub.clientWithToken(token)

	host := startFakeHost(t, hub, owner, "approval-stepup")
	host.setAnswer(func(method string, params json.RawMessage) (any, string) {
		return map[string]any{}, ""
	})
	host.send(protocol.Envelope{Type: "sessions", Full: true, Sessions: []protocol.Session{{
		Agent: "opencode", NativeID: "native-approval", Title: "Approval", Cwd: "/work/demo",
		State: protocol.StateIdle, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}})
	waitFor(t, "session", func() bool {
		list, err := hub.store.ListSessions(owner.ID, host.hostID, "", "", "", "", 10, 0, true)
		return err == nil && len(list) == 1
	})
	sess := sessionViewByNative(t, hub, owner, host.hostID, "native-approval")
	status, body := hub.do(client, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs", map[string]any{"mode": "structured"}, nil)
	expectStatus(t, status, http.StatusAccepted, body, "create run")
	var run protocol.RunView
	_ = json.Unmarshal(body, &run)
	host.waitCommand(t, "run.start")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(hub, "/api/v1/ws"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {cookieName + "=" + token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	writeClientEnvelope(t, conn, protocol.ClientEnvelope{Type: "approval", RunID: run.ID, ApprovalID: "a1", Decision: "always"})
	reply := readClientError(t, conn)
	if !strings.Contains(reply.Error, "step-up") {
		t.Fatalf("persistent approval without step-up: %+v", reply)
	}
	time.Sleep(200 * time.Millisecond)
	host.mu.Lock()
	for _, msg := range host.got {
		if msg.Type == "run.approval" {
			host.mu.Unlock()
			t.Fatalf("a rejected persistent approval still reached the host: %+v", msg)
		}
	}
	host.mu.Unlock()

	// A one-time approval needs no step-up and must still work.
	writeClientEnvelope(t, conn, protocol.ClientEnvelope{Type: "approval", RunID: run.ID, ApprovalID: "a1", Decision: "once"})
	if msg := host.waitCommand(t, "run.approval"); msg.Decision != "once" {
		t.Fatalf("one-time approval decision: %+v", msg)
	}

	// Step-up acquired after the socket connected must be honoured without a
	// reconnect; the check reads the session rather than a connect snapshot.
	if err := hub.store.MarkStepUp(token, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	writeClientEnvelope(t, conn, protocol.ClientEnvelope{Type: "approval", RunID: run.ID, ApprovalID: "a2", Decision: "always"})
	if msg := host.waitCommand(t, "run.approval"); msg.Decision != "always" {
		t.Fatalf("persistent approval after step-up: %+v", msg)
	}
}
