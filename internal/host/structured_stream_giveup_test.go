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
// the driver has to release it (Stop terminates a private server).
func TestStructuredRunReleasesAfterEventStreamGiveUp(t *testing.T) {
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sr.cancel = cancel

	done := make(chan struct{})
	go func() {
		sr.streamEvents(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("event stream loop did not give up")
	}
	sr.mu.Lock()
	closed := sr.closed
	sr.mu.Unlock()
	if !closed {
		t.Fatal("run stayed registered after the event stream gave up")
	}
}
