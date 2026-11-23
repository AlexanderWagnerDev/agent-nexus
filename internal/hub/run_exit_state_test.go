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

// A stop that arrives before the daemon registers the process must survive the
// later run.started message; otherwise the exit of the process it kills looks
// like a crash.
func TestStopBeforeRunStartedStaysStopping(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hostID := newRunForExitTest(t, hub, "run-early-stop")
	hub.server.setRunState("run-early-stop", protocol.StateStopping)

	hub.server.handleHostMessage(context.Background(), &hostConn{id: hostID}, protocol.Envelope{
		Type: "run.started", RunID: "run-early-stop", Action: protocol.ActionNew,
	})
	if run, _, err := hub.store.RunByID("run-early-stop"); err != nil || run.State != protocol.StateStopping {
		t.Fatalf("state after run.started = %q (%v), want %q", run.State, err, protocol.StateStopping)
	}
	hub.server.handleHostMessage(context.Background(), &hostConn{id: hostID}, protocol.Envelope{
		Type: "run.exit", RunID: "run-early-stop", Code: -1, Reason: "run stopped by user",
	})
	if run, _, err := hub.store.RunByID("run-early-stop"); err != nil || run.State != protocol.StateExited {
		t.Fatalf("state after run.exit = %q (%v), want %q", run.State, err, protocol.StateExited)
	}
}

// Stopping a delegated run must not complete the task or unblock its
// dependents.
func TestStoppedRunCancelsDelegatedTask(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hostID := newRunForExitTest(t, hub, "run-task-stop")
	owner, err := hub.store.UserByUsername("owner")
	if err != nil {
		t.Fatal(err)
	}
	task, err := hub.store.CreateTask(protocol.Task{
		UserID: owner.ID, Title: "generate report", Status: protocol.TaskRunning,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.LinkTaskRun(task.ID, "run-task-stop", ""); err != nil {
		t.Fatal(err)
	}
	dependent, err := hub.store.CreateTask(protocol.Task{
		UserID: owner.ID, Title: "review report", Status: protocol.TaskReady,
	}, []string{task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if dependent.Status != protocol.TaskBlocked {
		t.Fatalf("dependent status = %q, want %q", dependent.Status, protocol.TaskBlocked)
	}

	hub.server.setRunState("run-task-stop", protocol.StateStopping)
	hub.server.handleHostMessage(context.Background(), &hostConn{id: hostID}, protocol.Envelope{
		Type: "run.exit", RunID: "run-task-stop", Code: -1, Reason: "run stopped by user",
	})

	updated, err := hub.store.TaskByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != protocol.TaskCancelled {
		t.Fatalf("stopped task status = %q, want %q", updated.Status, protocol.TaskCancelled)
	}
	after, err := hub.store.TaskByID(dependent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != protocol.TaskBlocked {
		t.Fatalf("dependent status after stop = %q, want %q", after.Status, protocol.TaskBlocked)
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
