package hub

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func createTask(t *testing.T, hub *apiTestHub, body map[string]any) protocol.Task {
	t.Helper()
	status, raw := hub.do(nil, http.MethodPost, "/api/v1/tasks", body, nil)
	expectStatus(t, status, http.StatusCreated, raw, "create task")
	var task protocol.Task
	if err := json.Unmarshal(raw, &task); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestTaskDependenciesAndCycleRejection(t *testing.T) {
	hub := newSetupHub(t)
	a := createTask(t, hub, map[string]any{"title": "Analyze", "status": "ready"})
	b := createTask(t, hub, map[string]any{"title": "Implement", "status": "ready"})
	c := createTask(t, hub, map[string]any{"title": "Review", "status": "ready"})

	for _, edge := range [][2]string{{b.ID, a.ID}, {c.ID, b.ID}} {
		status, body := hub.do(nil, http.MethodPost, "/api/v1/tasks/"+edge[0]+"/dependencies", map[string]string{"depends_on": edge[1]}, nil)
		expectStatus(t, status, http.StatusOK, body, "add dependency")
	}
	status, body := hub.do(nil, http.MethodPost, "/api/v1/tasks/"+a.ID+"/dependencies", map[string]string{"depends_on": c.ID}, nil)
	expectStatus(t, status, http.StatusConflict, body, "cycle")
	if !jsonContains(body, "cycle") {
		t.Fatalf("cycle error not returned: %s", body)
	}
	blocked, err := hub.store.TaskByID(b.ID)
	if err != nil || blocked.Status != protocol.TaskBlocked {
		t.Fatalf("dependent task should be blocked: %+v %v", blocked, err)
	}
	status, body = hub.do(nil, http.MethodPatch, "/api/v1/tasks/"+a.ID, map[string]any{"status": "completed"}, nil)
	expectStatus(t, status, http.StatusOK, body, "complete dependency")
	ready, err := hub.store.TaskByID(b.ID)
	if err != nil || ready.Status != protocol.TaskReady {
		t.Fatalf("task should become ready: %+v %v", ready, err)
	}
	stillBlocked, _ := hub.store.TaskByID(c.ID)
	if stillBlocked.Status != protocol.TaskBlocked {
		t.Fatalf("transitive dependency should stay blocked: %+v", stillBlocked)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/tasks/"+b.ID, nil, nil)
	expectStatus(t, status, http.StatusOK, body, "task detail")
	for _, want := range []string{`"dependencies"`, `"dependents"`, a.ID} {
		if !jsonContains(body, want) {
			t.Fatalf("detail missing %s: %s", want, body)
		}
	}
	if status, body := hub.do(nil, http.MethodDelete, "/api/v1/tasks/"+b.ID+"/dependencies/"+a.ID, nil, nil); status != http.StatusOK {
		t.Fatalf("remove dependency: %d %s", status, body)
	}
}

func jsonContains(body []byte, want string) bool {
	return strings.Contains(string(body), want)
}

func TestDelegateTaskStartsRunAndSyncsLifecycle(t *testing.T) {
	hub := newSetupHub(t)
	owner, _ := hub.store.UserByUsername("owner")
	host := startFakeHost(t, hub, owner, "task-host")
	host.setAnswer(func(method string, params json.RawMessage) (any, string) { return map[string]any{}, "" })

	task := createTask(t, hub, map[string]any{
		"title": "Implement the parser", "description": "Add a parser and tests.",
		"host_id": host.hostID, "project": "/work/repo", "agent": "opencode",
		"status": "ready",
	})
	status, body := hub.do(nil, http.MethodPost, "/api/v1/tasks/"+task.ID+"/delegate", map[string]any{
		"mode": "new", "run_mode": "structured",
	}, nil)
	expectStatus(t, status, http.StatusAccepted, body, "delegate task")
	start := host.waitCommand(t, "run.start")
	if start.Run == nil || start.Run.Action != protocol.ActionNew || start.Run.Mode != protocol.ModeStructured {
		t.Fatalf("run start: %+v", start.Run)
	}
	host.send(protocol.Envelope{Type: "run.started", RunID: start.Run.RunID, NativeID: "ses_task", Agent: "opencode", Cwd: "/work/repo", Action: protocol.ActionNew})
	host.send(protocol.Envelope{Type: "run.event", RunID: start.Run.RunID, Event: &protocol.RunEvent{Type: "run.state", State: protocol.StateIdle}})
	prompt := host.waitCommand(t, "run.prompt")
	if prompt.Message != "Add a parser and tests." {
		t.Fatalf("delegated prompt: %q", prompt.Message)
	}
	waitFor(t, "task running", func() bool {
		got, err := hub.store.TaskByID(task.ID)
		return err == nil && got.RunID == start.Run.RunID && got.Status == protocol.TaskRunning
	})
	host.send(protocol.Envelope{Type: "run.exit", RunID: start.Run.RunID, Code: 0, Reason: "done"})
	waitFor(t, "task completed", func() bool {
		got, err := hub.store.TaskByID(task.ID)
		return err == nil && got.Status == protocol.TaskCompleted
	})
	got, _ := hub.store.TaskByID(task.ID)
	sess, err := hub.store.SessionByID(owner.ID, got.SessionID, true)
	if err != nil || sess.NativeID != "ses_task" || got.Result != "run completed" {
		t.Fatalf("completed task: %+v session=%+v err=%v", got, sess, err)
	}
}

func TestReconcileTasksFailsOrphanedRuns(t *testing.T) {
	hub := newSetupHub(t)
	owner, _ := hub.store.UserByUsername("owner")
	task, err := hub.store.CreateTask(protocol.Task{
		UserID: owner.ID, Title: "orphan", Status: protocol.TaskRunning, Mode: protocol.ModeStructured,
		Priority: "normal",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := hub.store.ReconcileTasks(); n != 1 {
		t.Fatalf("reconciled %d tasks", n)
	}
	got, _ := hub.store.TaskByID(task.ID)
	if got.Status != protocol.TaskFailed || got.CompletedAt == nil {
		t.Fatalf("orphan task: %+v", got)
	}
}

func TestTaskOwnershipIsolation(t *testing.T) {
	hub := newSetupHub(t)
	task := createTask(t, hub, map[string]any{"title": "private task", "status": "ready"})
	_, memberToken := hub.sessionFor("member", "task-member")
	memberClient := hub.clientWithToken(memberToken)
	status, body := hub.do(memberClient, http.MethodGet, "/api/v1/tasks/"+task.ID, nil, nil)
	expectStatus(t, status, http.StatusNotFound, body, "other user's task")
	status, body = hub.do(memberClient, http.MethodGet, "/api/v1/tasks", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "member task list")
	if jsonContains(body, task.ID) {
		t.Fatalf("member saw owner task: %s", body)
	}
}
