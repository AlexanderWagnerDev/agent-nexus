package hub

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestPromptQueueRejectsEndedRun(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "queue-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	const runID = "run-ended-queue"
	if err := hub.store.CreateRun(runID, "", hostID, protocol.ModeStructured, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	hub.server.setRunState(runID, protocol.StateExited)

	status, body := hub.do(nil, http.MethodPost, "/api/v1/runs/"+runID+"/queue",
		map[string]string{"text": "too late"}, nil)
	expectStatus(t, status, http.StatusConflict, body, "queue item on an ended run")
}

func TestPromptQueueDeleteAllowedAfterRunEnds(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "queue-cleanup-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := hub.store.ReplaceHostSessions(hostID, []protocol.Session{{
		Agent: "opencode", NativeID: "native-queue-cleanup", Title: "cleanup", Cwd: "/work",
	}}); err != nil {
		t.Fatal(err)
	}
	sessionID, err := hub.store.SessionIDForNative(hostID, "opencode", "native-queue-cleanup")
	if err != nil {
		t.Fatal(err)
	}
	const runID = "run-queue-cleanup"
	if err := hub.store.CreateRun(runID, sessionID, hostID, protocol.ModeStructured, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	item, err := hub.store.EnqueuePrompt(runID, owner.ID, "device", "remove me", nil, nil, maxQueuedRunBytes)
	if err != nil {
		t.Fatal(err)
	}
	hub.server.setRunState(runID, protocol.StateExited)
	status, body := hub.do(nil, http.MethodDelete, "/api/v1/runs/"+runID+"/queue/"+item.ID, nil, nil)
	expectStatus(t, status, http.StatusOK, body, "delete prompt after run end")
}

func TestPromptQueueByteBudget(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "queue-bytes-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := hub.store.ReplaceHostSessions(hostID, []protocol.Session{{
		Agent: "opencode", NativeID: "native-queue-bytes", Title: "queue", Cwd: "/work",
	}}); err != nil {
		t.Fatal(err)
	}
	sessionID, err := hub.store.SessionIDForNative(hostID, "opencode", "native-queue-bytes")
	if err != nil {
		t.Fatal(err)
	}
	_, body := hub.do(nil, http.MethodGet, "/api/v1/me", nil, nil)
	var me struct {
		Device Device `json:"device"`
	}
	if err := json.Unmarshal(body, &me); err != nil {
		t.Fatal(err)
	}
	const runID = "run-queue-bytes"
	if err := hub.store.CreateRun(runID, sessionID, hostID, protocol.ModeStructured, owner.ID, me.Device.ID); err != nil {
		t.Fatal(err)
	}
	previous := maxQueuedRunBytes
	maxQueuedRunBytes = 8
	defer func() { maxQueuedRunBytes = previous }()
	status, body := hub.do(nil, http.MethodPost, "/api/v1/runs/"+runID+"/queue",
		map[string]string{"text": "this exceeds the byte budget"}, nil)
	expectStatus(t, status, http.StatusTooManyRequests, body, "queue byte budget")
	if !json.Valid(body) {
		t.Fatalf("invalid response: %s", body)
	}
	// SQLite length(text) counts characters; the budget must count UTF-8 bytes.
	maxQueuedRunBytes = 3
	status, body = hub.do(nil, http.MethodPost, "/api/v1/runs/"+runID+"/queue",
		map[string]string{"text": "éé"}, nil)
	expectStatus(t, status, http.StatusTooManyRequests, body, "queue byte budget counts UTF-8 bytes")
}

func TestPromptQueueByteBudgetIsAtomic(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "queue-bytes-atomic-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := hub.store.ReplaceHostSessions(hostID, []protocol.Session{{
		Agent: "opencode", NativeID: "native-queue-bytes-atomic", Title: "queue", Cwd: "/work",
	}}); err != nil {
		t.Fatal(err)
	}
	sessionID, err := hub.store.SessionIDForNative(hostID, "opencode", "native-queue-bytes-atomic")
	if err != nil {
		t.Fatal(err)
	}
	const runID = "run-queue-bytes-atomic"
	if err := hub.store.CreateRun(runID, sessionID, hostID, protocol.ModeStructured, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	const limit = 4
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = hub.store.EnqueuePrompt(runID, owner.ID, "device", "abc", nil, nil, limit)
		}()
	}
	wg.Wait()
	if got := hub.store.PromptQueueBytes(runID); got > limit {
		t.Fatalf("concurrent enqueues exceeded the byte budget: %d > %d", got, limit)
	}
}

func TestTaskDeleteRejectsDelegatedRunBeforeSessionBinding(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "task-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	const runID = "run-task-unbound"
	if err := hub.store.CreateRun(runID, "", hostID, protocol.ModeStructured, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	task, err := hub.store.CreateTask(protocol.Task{
		UserID: owner.ID, Title: "delegated", HostID: hostID, Agent: "codex",
		Project: "/work", Mode: protocol.ModeStructured, Priority: "normal", Status: protocol.TaskDraft,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.LinkTaskRun(task.ID, runID, ""); err != nil {
		t.Fatal(err)
	}

	status, body := hub.do(nil, http.MethodDelete, "/api/v1/tasks/"+task.ID, nil, nil)
	expectStatus(t, status, http.StatusConflict, body, "delete task with a live delegated run")
}
