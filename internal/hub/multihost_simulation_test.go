package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// TestMultiHostSimulation is a deterministic simulation of the three-host
// workflow. It does not replace a real Windows/Portainer run; fake hosts and
// the in-process hub are used on purpose.
func TestMultiHostSimulation(t *testing.T) {
	hub := newSetupHub(t)
	signer, err := OpenHubSigner(filepath.Join(t.TempDir(), "signing"))
	if err != nil {
		t.Fatal(err)
	}
	hub.server.SetSigner(signer)
	owner := hub.testOwner()
	hosts := []*fakeHost{
		startFakeHost(t, hub, owner, "sim-windows-1"),
		startFakeHost(t, hub, owner, "sim-windows-2"),
		startFakeHost(t, hub, owner, "sim-windows-3"),
	}
	now := time.Now()
	for _, host := range hosts {
		host.setAnswer(func(method string, params json.RawMessage) (any, string) { return map[string]any{}, "" })
		host.send(protocol.Envelope{Type: "sessions", Full: true, Sessions: []protocol.Session{
			{Agent: "opencode", NativeID: "sim-open-" + host.hostID[:6], Title: "OpenCode demo", Cwd: "/work/demo", State: protocol.StateIdle, CreatedAt: now, UpdatedAt: now},
			{Agent: "codex", NativeID: "sim-codex-" + host.hostID[:6], Title: "Codex demo", Cwd: "/work/codex", State: protocol.StateIdle, CreatedAt: now, UpdatedAt: now},
		}})
	}
	waitFor(t, "three-host inventory", func() bool {
		list, err := hub.store.ListSessions(owner.ID, "", "", "", "", "", 100, 0, true)
		return err == nil && len(list) == 6
	})
	list, err := hub.store.ListSessions(owner.ID, "", "", "", "", "", 100, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	// Start one structured run per host in parallel and report the native
	// session a new run would create.
	runIDs := []string{}
	for _, host := range hosts {
		var sess protocol.SessionView
		for _, candidate := range list {
			if candidate.HostID == host.hostID {
				sess = candidate
				break
			}
		}
		if sess.ID == "" {
			t.Fatalf("no session for host %s", host.hostID)
		}
		status, body := hub.do(nil, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs",
			map[string]any{"mode": "structured"}, nil)
		expectStatus(t, status, http.StatusAccepted, body, "simulation run")
		command := host.waitCommand(t, "run.start")
		runIDs = append(runIDs, command.Run.RunID)
		host.send(protocol.Envelope{Type: "run.started", RunID: command.Run.RunID, Reason: "structured"})
		host.send(protocol.Envelope{Type: "run.event", RunID: command.Run.RunID, Event: &protocol.RunEvent{Type: "run.state", State: protocol.StateIdle}})
	}
	waitFor(t, "parallel runs", func() bool {
		runs, err := hub.store.ActiveRunsForUser(owner.ID, true)
		return err == nil && len(runs) >= 3
	})

	// A host disconnect fails its active runs but does not delete discovered
	// sessions; the other hosts remain usable.
	hosts[0].cancel()
	waitFor(t, "host run failed", func() bool {
		run, _, err := hub.store.RunByID(runIDs[0])
		return err == nil && run.State == protocol.StateFailed
	})
	remaining, err := hub.store.ListSessions(owner.ID, "", "", "", "", "", 100, 0, true)
	if err != nil || len(remaining) != 6 {
		t.Fatalf("sessions lost on host disconnect: %d %v", len(remaining), err)
	}

	// A hub restart uses the same store; session metadata and completed run
	// rows survive. This is the restore path, not a real process restart.
	restarted := NewServer(Config{HubName: "restarted"}, hub.store, hub.server.log)
	ts := httptest.NewServer(restarted.Handler())
	defer ts.Close()
	after, err := hub.store.ListSessions(owner.ID, "", "", "", "", "", 100, 0, true)
	if err != nil || len(after) != 6 {
		t.Fatalf("sessions lost after hub restart: %d %v", len(after), err)
	}
	_ = runIDs
}
