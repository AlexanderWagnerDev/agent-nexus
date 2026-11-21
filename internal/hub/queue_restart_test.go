package hub

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// A hub restart closes live runs before hosts can reconnect. The queue worker
// must not race that recovery window and fail every due item deterministically.
func TestDuePromptSurvivesStartupRecoveryGrace(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	box, err := OpenSecretBox(dir)
	if err != nil {
		t.Fatal(err)
	}
	store.SetSecretBox(box)

	owner, err := store.CreateUserWithRole("owner", "hash", "secret", "owner")
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := store.CreateHost(owner.ID, "restart-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	const runID = "run-restart"
	if err := store.CreateRun(runID, "", hostID, protocol.ModeStructured, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	store.UpdateRunState(runID, protocol.StateRunning)
	item, err := store.EnqueuePrompt(runID, owner.ID, "device", "survive restart", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	srv := NewServer(Config{HubName: "restart"}, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// NewServer closed the live run; the queued item must still be pending.
	time.Sleep(100 * time.Millisecond)
	got, err := store.PromptQueueItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "queued" {
		t.Fatalf("queued prompt was resolved during startup grace: %q (%s)", got.State, got.LastError)
	}

	// After the grace period a run that the host never re-adopted is failed,
	// and its item is failed instead of being sent to a dead run.
	srv.queueGraceUntil = time.Now().Add(-time.Second)
	srv.dispatchDuePrompts()
	got, err = store.PromptQueueItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "failed" {
		t.Fatalf("due prompt was not resolved after grace: %q", got.State)
	}
}
