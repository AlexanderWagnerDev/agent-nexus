package host

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func writeServiceFile(t *testing.T, dir, raw string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "service.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadOpenCodeServiceAcceptsLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "opencode" || pass != "secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	writeServiceFile(t, dir, `{"url":"`+server.URL+`","password":"secret","pid":1}`)
	svc, ok := readOpenCodeService(dir)
	if !ok || svc.URL != server.URL || svc.Password != "secret" {
		t.Fatalf("service = %+v ok=%v", svc, ok)
	}
}

func TestReadOpenCodeServiceRejectsNonLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	raw, _ := json.Marshal(openCodeService{URL: "http://example.com" + ":" + parsed.Port(), Password: "secret"})
	dir := t.TempDir()
	writeServiceFile(t, dir, string(raw))
	if _, ok := readOpenCodeService(dir); ok {
		t.Fatal("a non-loopback service URL was accepted")
	}
}

func TestReadOpenCodeServiceFallsBackWhenUnreachable(t *testing.T) {
	dir := t.TempDir()
	writeServiceFile(t, dir, `{"url":"http://127.0.0.1:1","password":"secret"}`)
	if _, ok := readOpenCodeService(dir); ok {
		t.Fatal("an unreachable service was accepted")
	}
	writeServiceFile(t, dir, `{"url":"http://127.0.0.1:1","password":""}`)
	if _, ok := readOpenCodeService(dir); ok {
		t.Fatal("an empty password was accepted")
	}
}

// A shared service must outlive the run: stopping the AgentNexus run only
// removes its registration and event stream.
func TestSharedStructuredRunStopsWithoutKillingService(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/event" {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	d := NewDaemon(Config{Policy: Policy{AllowedRoots: []string{t.TempDir()}}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	spec := protocol.RunSpec{RunID: "run-shared", Agent: "opencode", NativeID: "ses_shared", Action: protocol.ActionResume, Cwd: t.TempDir()}
	runCtx, cancel := context.WithCancel(context.Background())
	sr := newStructuredRun(d, spec, spec.Cwd, server.URL, "secret")
	sr.cancel = cancel
	if err := d.activateStructuredRun(spec, sr, runCtx, nil); err != nil {
		t.Fatal(err)
	}
	d.runMu.Lock()
	_, registered := d.structured[spec.RunID]
	d.runMu.Unlock()
	if !registered {
		t.Fatal("shared run was not registered")
	}
	sr.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for {
		d.runMu.Lock()
		_, still := d.structured[spec.RunID]
		d.runMu.Unlock()
		if !still {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shared run stayed registered after stop")
		}
		time.Sleep(20 * time.Millisecond)
	}
	resp, err := http.Get(server.URL + "/api/session")
	if err != nil {
		t.Fatalf("shared service was not left running: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("shared service returned HTTP %d", resp.StatusCode)
	}
}
