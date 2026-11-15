package hub

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// A delegated task carries its run id from the moment of delegation, while the
// native session id is only known after the host reports the new session. The
// status guard must therefore look at the delegated run, not only at the
// session, or a task can be reset while its agent is still working.
func TestTaskStatusChangeRejectedWhileDelegatedRunIsActive(t *testing.T) {
	hub := newSetupHub(t)
	owner, _ := hub.store.UserByUsername("owner")
	host := startFakeHost(t, hub, owner, "task-guard-host")
	host.setAnswer(func(method string, params json.RawMessage) (any, string) { return map[string]any{}, "" })

	task := createTask(t, hub, map[string]any{
		"title": "Guarded task", "description": "Do the work.",
		"host_id": host.hostID, "project": "/work/repo", "agent": "opencode",
		"status": "ready",
	})
	status, body := hub.do(nil, http.MethodPost, "/api/v1/tasks/"+task.ID+"/delegate", map[string]any{
		"mode": "new", "run_mode": "structured",
	}, nil)
	expectStatus(t, status, http.StatusAccepted, body, "delegate task")
	host.waitCommand(t, "run.start")

	// No run.started with a native id yet: task.SessionID is still empty while
	// the delegated run row is active.
	status, body = hub.do(nil, http.MethodPatch, "/api/v1/tasks/"+task.ID, map[string]any{"status": "draft"}, nil)
	expectStatus(t, status, http.StatusConflict, body, "status change during an active delegated run")

	got, err := hub.store.TaskByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == protocol.TaskDraft {
		t.Fatalf("task status was changed to %q while its run was active", got.Status)
	}
}
