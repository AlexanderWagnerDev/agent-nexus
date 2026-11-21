package hub

import (
	"encoding/json"
	"net/http"
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
