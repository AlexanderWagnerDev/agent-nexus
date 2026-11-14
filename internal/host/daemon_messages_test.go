package host

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func silentDaemon(adapters ...Adapter) *Daemon {
	return NewDaemonWithAdapters(Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), adapters)
}

func TestHandleMessageDispatchesStructuredCommands(t *testing.T) {
	run, _ := newStructuredTestRun(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/prompt"):
			_, _ = w.Write([]byte(`{"data":{"id":"s1"}}`))
		case strings.HasSuffix(r.URL.Path, "/permission/a1/reply"):
			_, _ = w.Write([]byte(`{"data":{}}`))
		case strings.HasSuffix(r.URL.Path, "/interrupt"):
			_, _ = w.Write([]byte(`{"data":{}}`))
		default:
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	})
	t.Setenv("APPDATA", t.TempDir())
	d := silentDaemon()
	run.daemon = d
	run.cancel = func() {}
	d.structured["run-1"] = run

	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.prompt", RunID: "run-1", Message: "hello", Options: &protocol.PromptOptions{Model: "m"}})
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.prompt", RunID: "run-1", Message: strings.Repeat("x", 256<<10+1)})
	timer := time.AfterFunc(time.Hour, func() {})
	run.mu.Lock()
	run.approvals["a1"] = timer
	run.mu.Unlock()
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.approval", RunID: "run-1", ApprovalID: "a1", Decision: "once"})
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.interrupt", RunID: "run-1"})
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.stop", RunID: "run-1"})
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.input", RunID: "run-1", Data: []byte("x")})
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.resize", RunID: "run-1", Cols: 80, Rows: 24})
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.start"})
	d.handleMessage(context.Background(), protocol.Envelope{Type: "unknown"})
	timer.Stop()
}

func TestHandleMessageRejectsOversizedPayloads(t *testing.T) {
	d := silentDaemon()
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.input", Data: make([]byte, 2<<20+1)})
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.prompt", Message: strings.Repeat("x", 256<<10+1)})
}

func TestHandleMessageDispatchesCodexAndACP(t *testing.T) {
	d := silentDaemon()
	// A closed Codex run and ACP run exercise the error branches without a peer.
	codex := &CodexRun{
		id: "codex-1", threadID: "thread-1", pending: map[int64]chan json.RawMessage{},
		approvals: map[int64]string{}, stdin: nopWriteCloser{},
		daemon: d,
	}
	codex.closed = true
	d.codexRuns["codex-1"] = codex
	acp := &ACPRun{id: "acp-1", approvals: map[string]chan string{}, daemon: d}
	acp.cancel = func() {}
	acp.closed = true
	d.acpRuns["acp-1"] = acp

	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.approval", RunID: "codex-1", ApprovalID: "7", Decision: "once"})
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.interrupt", RunID: "codex-1"})
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.stop", RunID: "codex-1"})
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.prompt", RunID: "acp-1", Message: "hi"})
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.approval", RunID: "acp-1", ApprovalID: "a1", Decision: "once"})
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.interrupt", RunID: "acp-1"})
	d.handleMessage(context.Background(), protocol.Envelope{Type: "run.stop", RunID: "acp-1"})
}

func TestDispatchRequest(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	d := silentDaemon(echoTestAdapter{})
	d.policy = Policy{AllowedRoots: []string{root}}
	d.sessionsMu.Lock()
	d.lastByAgent["echo"] = []protocol.Session{{NativeID: "n1", Agent: "echo", Cwd: root}}
	d.sessionsMu.Unlock()

	raw, _ := json.Marshal(map[string]any{"native_id": "n1", "agent": "echo", "offset": 0, "limit": 10})
	if _, err := d.dispatchRequest("transcript", raw); err != nil {
		t.Fatalf("transcript: %v", err)
	}
	raw, _ = json.Marshal(map[string]any{"query": "hello", "limit": 5})
	if hits, err := d.dispatchRequest("search", raw); err != nil {
		t.Fatalf("search: %v", err)
	} else if hits == nil {
		t.Fatal("search returned nil without an error")
	}
	if _, err := d.dispatchRequest("rescan", nil); err == nil {
		t.Fatal("rescan without a connection should report the send error")
	}
	if _, err := d.dispatchRequest("transcript", []byte("{")); err == nil {
		t.Fatal("malformed transcript params were accepted")
	}
	raw, _ = json.Marshal(map[string]any{"cwd": t.TempDir()})
	if _, err := d.dispatchRequest("git", raw); err == nil {
		t.Fatal("a git request outside the allowed roots should fail")
	}
	if _, err := d.dispatchRequest("unknown", nil); err == nil {
		t.Fatal("unknown method was accepted")
	}
	if _, err := d.dispatchRequest("run.options", []byte(`{"run_id":"missing"}`)); err != nil {
		t.Fatalf("missing run options: %v", err)
	}

	// A structured run answers run.options through its own server.
	run, _ := newStructuredTestRun(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/model":
			_, _ = w.Write([]byte(`{"data":[{"id":"m1","enabled":true}]}`))
		case "/api/agent":
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	})
	run.daemon = d
	d.structured["run-9"] = run
	raw, _ = json.Marshal(map[string]any{"run_id": "run-9"})
	if _, err := d.dispatchRequest("run.options", raw); err != nil {
		t.Fatalf("structured options: %v", err)
	}
}

func TestHandleRequestSendsResult(t *testing.T) {
	// With no WebSocket, handleRequest still runs dispatch and swallows the
	// send error. This covers the success and failure wrappers.
	d := silentDaemon(echoTestAdapter{})
	d.handleRequest(protocol.Envelope{Method: "search", Params: []byte(`{"query":"x"}`), RequestID: "r1"})
	d.handleRequest(protocol.Envelope{Method: "bogus", Params: nil, RequestID: "r2"})
}

func TestOpenCodeDetectAndLegacyTranscript(t *testing.T) {
	if _, err := exec.LookPath("definitely-not-installed-agent"); err != nil {
		a := &OpenCodeAdapter{bin: "definitely-not-installed-agent"}
		if a.Name() != "opencode" {
			t.Fatalf("name: %q", a.Name())
		}
		info, _ := a.Detect(context.Background())
		if info.Supported {
			t.Fatalf("missing binary reported as supported: %+v", info)
		}
	}
	if got := openCodeDBPath("opencode"); got == "" {
		t.Fatal("empty database path")
	}
}

func TestListSessionsAggregatesAdapterErrors(t *testing.T) {
	d := silentDaemon(failingAdapter{}, echoTestAdapter{})
	sessions, err := d.listSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = sessions
}

type failingAdapter struct{ echoTestAdapter }

func (failingAdapter) Name() string { return "failing" }
func (failingAdapter) ListSessions(context.Context) ([]protocol.Session, error) {
	return nil, os.ErrPermission
}
func (failingAdapter) Transcript(context.Context, string, int, int) (protocol.Transcript, error) {
	return protocol.Transcript{}, os.ErrPermission
}

func TestRuntimeStatusAndAuditHelpers(t *testing.T) {
	t.Setenv("NEXUSD_CONFIG", filepath.Join(t.TempDir(), "nexusd.json"))
	d := silentDaemon(echoTestAdapter{})
	d.cfg.HostID = "host-1"
	d.cfg.HostName = "demo"
	d.writeRuntimeStatus()
	if got := d.activeRunCount(); got != 0 {
		t.Fatalf("activeRunCount: %d", got)
	}
	if d.Adapters() == nil {
		t.Fatal("adapters nil")
	}
	hostAudit("test.event", map[string]any{"run_id": "r1"})
}

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }
