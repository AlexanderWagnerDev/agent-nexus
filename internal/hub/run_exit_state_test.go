package hub

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// newRunForExitTest creates a host and one active run started by its owner.
func newRunForExitTest(t *testing.T, hub *apiTestHub, runID string) (hostID string) {
	t.Helper()
	owner, err := hub.store.CreateUserWithRole("owner", "hash", NewTOTPSecret(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostID, err = hub.store.CreateHost(owner.ID, "host", public, "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateRunAt(runID, "", hostID, "/work", protocol.ModeStructured, owner.ID, "device-1"); err != nil {
		t.Fatal(err)
	}
	return hostID
}

// A stop requested by the user ends in "exited" (docs/ARCHITECTURE.md §7.4),
// even though the killed process reports a non-zero exit code.
func TestUserStoppedRunEndsExited(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hostID := newRunForExitTest(t, hub, "run-stop")
	hub.server.setRunState("run-stop", protocol.StateStopping)

	hub.server.handleHostMessage(context.Background(), &hostConn{id: hostID}, protocol.Envelope{
		Type: "run.exit", RunID: "run-stop", Code: -1, Reason: "run stopped by user",
	})

	run, _, err := hub.store.RunByID("run-stop")
	if err != nil {
		t.Fatal(err)
	}
	if run.State != protocol.StateExited {
		t.Fatalf("stopped run state = %q, want %q", run.State, protocol.StateExited)
	}
	if run.ErrorCode != "" || run.ErrorMessage != "" {
		t.Fatalf("stopped run recorded an error: %q / %q", run.ErrorCode, run.ErrorMessage)
	}
	if run.EndedAt == nil {
		t.Fatal("stopped run has no end time")
	}
}

// An unprompted non-zero exit is still a failure.
func TestUnexpectedRunExitFails(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hostID := newRunForExitTest(t, hub, "run-crash")
	hub.server.setRunState("run-crash", protocol.StateRunning)

	hub.server.handleHostMessage(context.Background(), &hostConn{id: hostID}, protocol.Envelope{
		Type: "run.exit", RunID: "run-crash", Code: 2, Reason: "agent crashed",
	})

	run, _, err := hub.store.RunByID("run-crash")
	if err != nil {
		t.Fatal(err)
	}
	if run.State != protocol.StateFailed {
		t.Fatalf("crashed run state = %q, want %q", run.State, protocol.StateFailed)
	}
	if run.ErrorCode == "" {
		t.Fatal("crashed run did not record an error code")
	}
}
