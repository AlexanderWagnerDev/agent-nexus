package hub

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestFanoutCreatesIndependentRunsAndDeliversPrompt(t *testing.T) {
	hub := newSetupHub(t)
	signer, err := OpenHubSigner(filepath.Join(t.TempDir(), "signing"))
	if err != nil {
		t.Fatal(err)
	}
	hub.server.SetSigner(signer)
	owner := hub.testOwner()
	hostA := startFakeHost(t, hub, owner, "fanout-a")
	hostB := startFakeHost(t, hub, owner, "fanout-b")
	for _, host := range []*fakeHost{hostA, hostB} {
		host.setAnswer(func(method string, params json.RawMessage) (any, string) {
			return map[string]any{}, ""
		})
	}

	status, body := hub.do(nil, http.MethodPost, "/api/v1/fanouts", map[string]any{
		"prompt": "review the upload path",
		"targets": []map[string]string{
			{"host_id": hostA.hostID, "agent": "codex", "cwd": "/work/repo"},
			{"host_id": hostB.hostID, "agent": "codex", "cwd": "/work/repo"},
		},
	}, nil)
	expectStatus(t, status, http.StatusCreated, body, "create fanout")
	var response struct {
		Fanout FanoutView `json:"fanout"`
		Errors []string   `json:"errors"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Errors) != 0 {
		t.Fatalf("fanout errors: %v", response.Errors)
	}
	if len(response.Fanout.Runs) != 2 {
		t.Fatalf("fanout runs: %+v", response.Fanout)
	}
	startA := hostA.waitCommand(t, "run.start")
	startB := hostB.waitCommand(t, "run.start")
	if startA.Run == nil || startB.Run == nil || startA.Run.Action != protocol.ActionNew || startB.Run.Action != protocol.ActionNew {
		t.Fatalf("start actions: %+v %+v", startA.Run, startB.Run)
	}
	hostA.send(protocol.Envelope{Type: "run.started", RunID: startA.Run.RunID, NativeID: "new-a", Agent: "codex", Cwd: "/work/repo"})
	hostB.send(protocol.Envelope{Type: "run.started", RunID: startB.Run.RunID, NativeID: "new-b", Agent: "codex", Cwd: "/work/repo"})
	waitFor(t, "fanout sessions adopted", func() bool {
		view, err := hub.store.Fanout(response.Fanout.ID, owner.ID, true)
		if err != nil || len(view.Runs) != 2 {
			return false
		}
		for _, run := range view.Runs {
			if run.SessionID == "" || run.Title != "New session" {
				return false
			}
		}
		return true
	})
	hostA.send(protocol.Envelope{Type: "run.event", RunID: startA.Run.RunID, Event: &protocol.RunEvent{Type: "run.state", State: protocol.StateIdle}})
	hostB.send(protocol.Envelope{Type: "run.event", RunID: startB.Run.RunID, Event: &protocol.RunEvent{Type: "run.state", State: protocol.StateIdle}})
	if prompt := hostA.waitCommand(t, "run.prompt"); prompt.Message != "review the upload path" {
		t.Fatalf("host A prompt: %q", prompt.Message)
	}
	if prompt := hostB.waitCommand(t, "run.prompt"); prompt.Message != "review the upload path" {
		t.Fatalf("host B prompt: %q", prompt.Message)
	}

	status, body = hub.do(nil, http.MethodGet, "/api/v1/fanouts/"+response.Fanout.ID, nil, nil)
	expectStatus(t, status, http.StatusOK, body, "get fanout")
	status, body = hub.do(nil, http.MethodGet, "/api/v1/fanouts", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "list fanouts")

	// The same host and worktree must not be launched twice concurrently.
	status, body = hub.do(nil, http.MethodPost, "/api/v1/fanouts", map[string]any{
		"prompt": "duplicate",
		"targets": []map[string]string{
			{"host_id": hostA.hostID, "agent": "codex", "cwd": "/work/same"},
			{"host_id": hostA.hostID, "agent": "opencode", "cwd": "/work/same"},
		},
	}, nil)
	expectStatus(t, status, http.StatusBadRequest, body, "duplicate worktree")

	// All runs remain independently visible.
	time.Sleep(50 * time.Millisecond)
	view, err := hub.store.Fanout(response.Fanout.ID, owner.ID, true)
	if err != nil || len(view.Runs) != 2 {
		t.Fatalf("final fanout: %+v %v", view, err)
	}
}
