package hub

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestPromptQueueStoreLifecycle(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "queue-store", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, _, err := hub.store.ReplaceHostSessions(hostID, []protocol.Session{{
		Agent: "codex", NativeID: "queue-session", Title: "Queue", Cwd: "/work/queue",
		State: protocol.StateIdle, CreatedAt: now, UpdatedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}
	sess := sessionViewByNative(t, hub, owner, hostID, "queue-session")
	runID := newID()
	if err := hub.store.CreateRun(runID, sess.ID, hostID, protocol.ModeStructured, owner.ID, "device-1"); err != nil {
		t.Fatal(err)
	}
	first, err := hub.store.EnqueuePrompt(runID, owner.ID, "device-1", "first", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hub.store.EnqueuePrompt(runID, owner.ID, "device-1", "second", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Position >= second.Position {
		t.Fatalf("positions: %d, %d", first.Position, second.Position)
	}
	if err := hub.store.ReorderPromptQueue(runID, []string{second.ID, first.ID}); err != nil {
		t.Fatal(err)
	}
	items, err := hub.store.ListPromptQueue(runID)
	if err != nil || len(items) != 2 || items[0].ID != second.ID {
		t.Fatalf("reordered queue: %+v %v", items, err)
	}
	if err := hub.store.UpdatePromptQueueItem(runID, second.ID, "second edited", nil); err != nil {
		t.Fatal(err)
	}
	if err := hub.store.SetPromptQueuePaused(runID, true); err != nil {
		t.Fatal(err)
	}
	if paused, _ := hub.store.PromptQueuePaused(runID); !paused {
		t.Fatal("queue pause was not persisted")
	}
	if err := hub.store.SetPromptQueuePaused(runID, false); err != nil {
		t.Fatal(err)
	}

	claimed, ok, err := hub.store.ClaimPromptQueueItem(second.ID)
	if err != nil || !ok || claimed.State != "sending" || claimed.Attempts != 1 {
		t.Fatalf("claim: %+v %v %v", claimed, ok, err)
	}
	if _, ok, _ := hub.store.ClaimPromptQueueItem(second.ID); ok {
		t.Fatal("the same item was claimed twice")
	}
	hub.store.FinishPromptQueueItem(second.ID, "sent", "")
	hub.store.RecoverPromptQueue()
	remaining, err := hub.store.ListPromptQueue(runID)
	if err != nil || len(remaining) != 1 || remaining[0].ID != first.ID {
		t.Fatalf("remaining queue: %+v %v", remaining, err)
	}

	// A crash leaves sending items failed, never silently retried.
	if _, ok, _ := hub.store.ClaimPromptQueueItem(first.ID); !ok {
		t.Fatal("second claim failed")
	}
	hub.store.RecoverPromptQueue()
	recovered, _ := hub.store.PromptQueueItem(first.ID)
	if recovered.State != "failed" || recovered.LastError == "" {
		t.Fatalf("recovered item: %+v", recovered)
	}
}

func TestPromptQueueDispatchWaitsForIdleTransition(t *testing.T) {
	hub := newSetupHub(t)
	signer, err := OpenHubSigner(filepath.Join(t.TempDir(), "signing"))
	if err != nil {
		t.Fatal(err)
	}
	hub.server.SetSigner(signer)
	owner := hub.testOwner()
	host := startFakeHost(t, hub, owner, "queue-host")
	host.setAnswer(func(method string, params json.RawMessage) (any, string) {
		return map[string]any{}, ""
	})
	now := time.Now()
	host.send(protocol.Envelope{Type: "sessions", Full: true, Sessions: []protocol.Session{{
		Agent: "codex", NativeID: "queue-native", Title: "Queue target", Cwd: "/work/queue",
		State: protocol.StateIdle, CreatedAt: now, UpdatedAt: now,
	}}})
	waitFor(t, "queue session", func() bool {
		list, err := hub.store.ListSessions(owner.ID, host.hostID, "", "", "", "", 10, 0, true)
		return err == nil && len(list) == 1
	})
	sess := sessionViewByNative(t, hub, owner, host.hostID, "queue-native")
	status, body := hub.do(nil, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs", map[string]any{"mode": "structured"}, nil)
	expectStatus(t, status, http.StatusAccepted, body, "queue run")
	var run protocol.RunView
	if err := json.Unmarshal(body, &run); err != nil {
		t.Fatal(err)
	}
	host.waitCommand(t, "run.start")

	status, body = hub.do(nil, http.MethodPost, "/api/v1/runs/"+run.ID+"/queue", map[string]any{"text": "first queued"}, nil)
	expectStatus(t, status, http.StatusCreated, body, "enqueue first")
	status, body = hub.do(nil, http.MethodPost, "/api/v1/runs/"+run.ID+"/queue", map[string]any{"text": "second queued"}, nil)
	expectStatus(t, status, http.StatusCreated, body, "enqueue second")

	host.send(protocol.Envelope{Type: "run.started", RunID: run.ID, Reason: "structured"})
	host.send(protocol.Envelope{Type: "run.event", RunID: run.ID, Event: &protocol.RunEvent{Type: "run.state", State: protocol.StateIdle}})
	firstPrompt := host.waitCommand(t, "run.prompt")
	if firstPrompt.Message != "first queued" {
		t.Fatalf("first delivered prompt: %q", firstPrompt.Message)
	}
	// A duplicate idle event must not deliver the second prompt before the
	// host reports a new busy transition.
	host.send(protocol.Envelope{Type: "run.event", RunID: run.ID, Event: &protocol.RunEvent{Type: "run.state", State: protocol.StateIdle}})
	select {
	case msg := <-host.gotCh:
		if msg.Type == "run.prompt" {
			t.Fatalf("second prompt was delivered without a busy transition: %q", msg.Message)
		}
	case <-time.After(300 * time.Millisecond):
	}

	host.send(protocol.Envelope{Type: "run.event", RunID: run.ID, Event: &protocol.RunEvent{Type: "run.state", State: protocol.StateRunning}})
	host.send(protocol.Envelope{Type: "run.event", RunID: run.ID, Event: &protocol.RunEvent{Type: "run.state", State: protocol.StateIdle}})
	secondPrompt := host.waitCommand(t, "run.prompt")
	if secondPrompt.Message != "second queued" {
		t.Fatalf("second delivered prompt: %q", secondPrompt.Message)
	}
	waitFor(t, "queue drained", func() bool {
		items, err := hub.store.ListPromptQueue(run.ID)
		return err == nil && len(items) == 0
	})
}

func TestPromptQueueRejectsLeaseMismatch(t *testing.T) {
	hub := newSetupHub(t)
	signer, err := OpenHubSigner(filepath.Join(t.TempDir(), "signing"))
	if err != nil {
		t.Fatal(err)
	}
	hub.server.SetSigner(signer)
	owner := hub.testOwner()
	host := startFakeHost(t, hub, owner, "queue-lease-host")
	host.setAnswer(func(method string, params json.RawMessage) (any, string) {
		return map[string]any{}, ""
	})
	now := time.Now()
	host.send(protocol.Envelope{Type: "sessions", Full: true, Sessions: []protocol.Session{{
		Agent: "codex", NativeID: "lease-native", Title: "Lease", Cwd: "/work/lease",
		State: protocol.StateIdle, CreatedAt: now, UpdatedAt: now,
	}}})
	waitFor(t, "lease session", func() bool {
		list, err := hub.store.ListSessions(owner.ID, host.hostID, "", "", "", "", 10, 0, true)
		return err == nil && len(list) == 1
	})
	sess := sessionViewByNative(t, hub, owner, host.hostID, "lease-native")
	status, body := hub.do(nil, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs", map[string]any{"mode": "structured"}, nil)
	expectStatus(t, status, http.StatusAccepted, body, "lease run")
	var run protocol.RunView
	if err := json.Unmarshal(body, &run); err != nil {
		t.Fatal(err)
	}
	host.waitCommand(t, "run.start")
	status, body = hub.do(nil, http.MethodPost, "/api/v1/runs/"+run.ID+"/queue", map[string]any{"text": "will fail"}, nil)
	expectStatus(t, status, http.StatusCreated, body, "enqueue lease item")
	if _, err := hub.store.db.Exec(`UPDATE runs SET lease_device_id='another-device' WHERE id=?`, run.ID); err != nil {
		t.Fatal(err)
	}
	host.send(protocol.Envelope{Type: "run.started", RunID: run.ID})
	host.send(protocol.Envelope{Type: "run.event", RunID: run.ID, Event: &protocol.RunEvent{Type: "run.state", State: protocol.StateIdle}})
	waitFor(t, "lease mismatch failure", func() bool {
		items, err := hub.store.ListPromptQueue(run.ID)
		return err == nil && len(items) == 1 && items[0].State == "failed" && items[0].LastError != ""
	})
}
