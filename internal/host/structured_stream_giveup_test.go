package host

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// A private OpenCode server whose event stream keeps failing must not leave a
// run registered forever: without events the run can never change state, so
// the driver has to stop it and release the registration when the process is
// reaped.
func TestStructuredRunReleasesRegistrationAfterEventStreamGiveUp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/event" {
			http.Error(w, "stream unavailable", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	d := NewDaemon(Config{Policy: Policy{AllowedRoots: []string{t.TempDir()}}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	spec := protocol.RunSpec{RunID: "run-stream-giveup", Agent: "opencode", NativeID: "ses_giveup", Action: protocol.ActionResume, Cwd: t.TempDir()}
	sr := newStructuredRun(d, spec, spec.Cwd, server.URL, "secret")
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sr.cancel = cancel

	// The process exit channel is normally fed by the server supervisor. The
	// test supplies it so it can observe the release after the stop.
	exited := make(chan error, 1)
	if err := d.activateStructuredRun(spec, sr, runCtx, exited); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		sr.mu.Lock()
		closed := sr.closed
		sr.mu.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("run was not stopped after the event stream gave up")
		}
		time.Sleep(50 * time.Millisecond)
	}

	exited <- nil
	deadline = time.Now().Add(2 * time.Second)
	for {
		d.runMu.Lock()
		_, registered := d.structured[spec.RunID]
		d.runMu.Unlock()
		if !registered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("run stayed registered after the process was reaped")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
