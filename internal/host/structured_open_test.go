package host

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/coder/websocket"
)

func newStructuredTestRun(t *testing.T, handler http.HandlerFunc) (*StructuredRun, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	run := &StructuredRun{
		id: "run-1", sessionID: "s1", agent: "opencode", cwd: t.TempDir(),
		baseURL: server.URL, password: "secret", client: server.Client(),
		daemon: &Daemon{log: logger},
		calls:  map[string]string{}, approvals: map[string]*time.Timer{},
		reasoningStreamed: map[string]bool{},
	}
	return run, server
}

func TestOpenCodeStructuredOptions(t *testing.T) {
	run, _ := newStructuredTestRun(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("missing basic auth")
		}
		switch r.URL.Path {
		case "/api/model":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"model-1","providerID":"prov","name":"Model One","enabled":true,"status":"active",
				 "variants":[{"id":"low"},{"id":"high"}],"capabilities":{"input":["text","image"]}},
				{"id":"hidden","enabled":false},
				{"id":"old","enabled":true,"status":"deprecated"}
			]}`))
		case "/api/agent":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"build","name":"Build","description":"Edit"},
				{"id":"sub","name":"Sub","mode":"subagent"},
				{"id":"secret","name":"Hidden","hidden":true}
			]}`))
		case "/api/session/s1":
			_, _ = w.Write([]byte(`{"data":{"agent":"build","model":{"id":"model-1","providerID":"prov","variant":"high"}}}`))
		default:
			http.NotFound(w, r)
		}
	})
	options, err := run.Options(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Models) != 1 || options.Models[0].ID != "model-1" {
		t.Fatalf("models: %+v", options.Models)
	}
	if len(options.Models[0].Variants) != 2 || len(options.Models[0].Inputs) != 2 {
		t.Fatalf("model capabilities: %+v", options.Models[0])
	}
	if len(options.Modes) != 1 || options.Modes[0].ID != "build" {
		t.Fatalf("modes: %+v", options.Modes)
	}
	if options.CurrentModel != "model-1" || options.CurrentVariant != "high" || !options.SupportsFiles {
		t.Fatalf("current options: %+v", options)
	}
}

func TestStructuredRunPromptAndInterrupt(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	var mu sync.Mutex
	var paths []string
	run, _ := newStructuredTestRun(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/prompt") {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["text"] != "do the thing" {
				t.Errorf("prompt body: %+v", body)
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"s1"}}`))
	})
	err := run.Prompt("do the thing", &protocol.PromptOptions{
		Model: "model-1", Provider: "prov", Variant: "high", AgentMode: "build",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Interrupt(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(paths, "\n")
	for _, want := range []string{
		"POST /api/session/s1/model",
		"POST /api/session/s1/agent",
		"POST /api/session/s1/prompt",
		"POST /api/session/s1/interrupt",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in requests:\n%s", want, joined)
		}
	}
}

func TestStructuredRunRequestErrorAndCreateSession(t *testing.T) {
	run, _ := newStructuredTestRun(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/session" {
			_, _ = w.Write([]byte(`{"data":{"id":"new-session"}}`))
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	if id, err := run.createSession("/work"); err != nil || id != "new-session" {
		t.Fatalf("createSession: %q %v", id, err)
	}
	if _, _, err := run.request(context.Background(), http.MethodGet, "/fail", nil); err == nil {
		t.Fatal("HTTP error was not returned")
	}
	empty, _ := newStructuredTestRun(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":""}}`))
	})
	if _, err := empty.createSession("/work"); err == nil {
		t.Fatal("empty session id accepted")
	}
}

func TestStructuredRunForkSession(t *testing.T) {
	var path, method string
	run, _ := newStructuredTestRun(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.EscapedPath(), r.Method
		_, _ = w.Write([]byte(`{"data":{"id":"forked-1"}}`))
	})
	id, err := run.forkSession("source/one")
	if err != nil || id != "forked-1" {
		t.Fatalf("forkSession: %q %v", id, err)
	}
	if method != http.MethodPost || path != "/api/session/source%2Fone/fork" {
		t.Fatalf("fork request: %s %s", method, path)
	}
	if _, err := run.forkSession("  "); err == nil {
		t.Fatal("empty source id accepted")
	}
	empty, _ := newStructuredTestRun(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":""}}`))
	})
	if _, err := empty.forkSession("source"); err == nil {
		t.Fatal("empty fork id accepted")
	}
}

func TestServerPasswordAndEnvReplacement(t *testing.T) {
	first, second := serverPassword(), serverPassword()
	if len(first) < 20 || first == second {
		t.Fatalf("password not random: %q %q", first, second)
	}
	env := withEnv([]string{"A=1", "OPENCODE_SERVER_PASSWORD=old"}, "OPENCODE_SERVER_PASSWORD", "new")
	seen := 0
	for _, item := range env {
		if item == "OPENCODE_SERVER_PASSWORD=new" {
			seen++
		}
		if item == "OPENCODE_SERVER_PASSWORD=old" {
			t.Fatal("old password survived")
		}
	}
	if seen != 1 {
		t.Fatalf("expected one replacement, got %d: %v", seen, env)
	}
}

func TestStructuredRunStopAndApproval(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	var replied string
	run, _ := newStructuredTestRun(t, func(w http.ResponseWriter, r *http.Request) {
		replied = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	cancelled := false
	run.cancel = func() { cancelled = true }
	timer := time.AfterFunc(time.Hour, func() {})
	defer timer.Stop()
	run.approvals["a1"] = timer
	if err := run.ReplyApproval("a1", "reject"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(replied, "/permission/a1/reply") {
		t.Fatalf("approval path: %s", replied)
	}
	run.Stop()
	run.Stop()
	if !cancelled || !run.closed {
		t.Fatalf("stop did not run: cancelled=%v closed=%v", cancelled, run.closed)
	}
}

func TestStructuredHelpers(t *testing.T) {
	if str("x") != "x" || str(1) != "" {
		t.Fatal("str")
	}
	if jsonText(map[string]string{"a": "b"}) != `{"a":"b"}` {
		t.Fatal("jsonText")
	}
	if contentText([]any{map[string]any{"text": "a"}, map[string]any{"text": "b"}}) != "a\nb" {
		t.Fatal("contentText")
	}
	if errorText(map[string]any{"message": "boom"}) != "boom" || errorText("plain") != "plain" {
		t.Fatal("errorText")
	}
	if got := stringSlice([]any{"a", "", "b", 1}); len(got) != 2 {
		t.Fatalf("stringSlice: %+v", got)
	}
}

func TestStructuredHandleEvent(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	events := make(chan protocol.RunEvent, 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var envelope protocol.Envelope
			if json.Unmarshal(data, &envelope) == nil && envelope.Event != nil {
				events <- *envelope.Event
			}
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "done")
	daemon := &Daemon{ws: ws, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	run := &StructuredRun{
		id: "run-1", daemon: daemon, calls: map[string]string{},
		approvals: map[string]*time.Timer{}, readyOnce: &sync.Once{},
		reasoningStreamed: map[string]bool{},
	}
	run.cancel = func() {}

	run.handleEvent("session.text.started", map[string]any{"assistantMessageID": "m1"})
	run.handleEvent("session.text.delta", map[string]any{"assistantMessageID": "m1", "delta": "hello"})
	run.handleEvent("session.text.ended", map[string]any{"assistantMessageID": "m1", "text": "hello"})
	run.handleEvent("session.reasoning.delta", map[string]any{"assistantMessageID": "m1", "delta": "think"})
	run.handleEvent("session.reasoning.ended", map[string]any{"assistantMessageID": "m1", "text": "thinking"})
	run.handleEvent("session.tool.input.started", map[string]any{"id": "c1", "name": "shell"})
	run.handleEvent("session.tool.input.ended", map[string]any{"id": "c1", "text": `{"command":"echo hi"}`})
	run.handleEvent("session.tool.called", map[string]any{"id": "c1", "input": map[string]any{"a": "b"}})
	run.handleEvent("session.tool.success", map[string]any{"id": "c1", "content": []any{map[string]any{"text": "ok"}}})
	run.handleEvent("session.tool.failed", map[string]any{"id": "c2", "content": []any{map[string]any{"text": "bad"}}, "error": map[string]any{"message": "boom"}})
	run.handleEvent("permission.asked", map[string]any{"id": "a1", "action": "run", "message": "allow?", "resources": []any{"file"}})
	run.handleEvent("permission.replied", map[string]any{"requestID": "a1", "reply": "once"})
	run.handleEvent("session.execution.started", nil)
	run.handleEvent("session.execution.succeeded", nil)
	run.handleEvent("session.execution.failed", map[string]any{"error": map[string]any{"message": "failed"}})
	run.handleEvent("session.status", map[string]any{"status": map[string]any{"type": "busy"}})
	run.handleEvent("session.status", map[string]any{"status": map[string]any{"type": "idle"}})
	run.handleEvent("unknown.event", nil)

	if run.currentState() != "idle" {
		t.Fatalf("state: %s", run.currentState())
	}
	var got []string
	timeout := time.After(2 * time.Second)
	for len(got) < 16 {
		select {
		case event := <-events:
			got = append(got, event.Type)
		case <-timeout:
			t.Fatalf("only received %d events: %v", len(got), got)
		}
	}
	run.Stop()
}

func TestStructuredStreamEvents(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "opencode" || pass != "secret" {
			t.Errorf("basic auth: %q %q %v", user, pass, ok)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message\n")
		_, _ = io.WriteString(w, "data: not-json\n\n")
		_, _ = io.WriteString(w, `data: {"type":"session.text.ended","data":{"sessionID":"s1","assistantMessageID":"m1","text":"hello"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"session.status","data":{"sessionID":"s1","status":{"type":"busy"}}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"ignored","data":{"sessionID":"other","text":"nope"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"bad","data":"not-an-object"}`+"\n\n")
	}))
	defer server.Close()
	run, _ := newStructuredTestRun(t, func(w http.ResponseWriter, r *http.Request) {})
	run.baseURL = server.URL
	run.password = "secret"
	run.id = "run-stream"
	run.sessionID = "s1"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run.streamEvents(ctx)
	if got := run.currentState(); got != protocol.StateRunning {
		t.Fatalf("state after stream: %q", got)
	}

	broken, _ := newStructuredTestRun(t, func(w http.ResponseWriter, r *http.Request) {})
	broken.baseURL = "http://127.0.0.1:1"
	broken.password = "secret"
	broken.streamEvents(ctx)
	if broken.currentState() != protocol.StateIdle {
		t.Fatalf("broken stream changed state: %q", broken.currentState())
	}
}
