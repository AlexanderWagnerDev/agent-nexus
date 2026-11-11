package hub

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/coder/websocket"
)

// fakeHost is a scripted nexusd peer. It authenticates like a real host,
// answers hub requests and can push inventory, sessions and run events.
type fakeHost struct {
	t      *testing.T
	hub    *apiTestHub
	hostID string
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	answer func(method string, params json.RawMessage) (any, string)
	got    []protocol.Envelope
	gotCh  chan protocol.Envelope
}

type fakeHostAnswer func(method string, params json.RawMessage) (any, string)

func startFakeHost(t *testing.T, hub *apiTestHub, owner User, name string) *fakeHost {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := hub.store.CreateHost(owner.ID, name, public, "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateHostConfirmation(hostID, "000-000"); err != nil {
		t.Fatal(err)
	}
	if err := hub.store.ConfirmHost(owner.ID, hostID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	conn, _, err := websocket.Dial(ctx, wsURL(hub, "/v1/host/connect?host_id="+hostID), nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	hello := hub.readEnvelope(t, conn, 5*time.Second)
	if hello.Type != "hello" || hello.Nonce == "" {
		cancel()
		t.Fatalf("hello: %+v", hello)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte(hello.Nonce)))
	hub.writeEnvelope(t, conn, protocol.Envelope{
		Type: "auth", Protocol: protocol.Version, Signature: signature,
		HostName: name, OS: "linux", Arch: "amd64", Version: "alpha",
	})
	auth := hub.readEnvelope(t, conn, 5*time.Second)
	if auth.Type != "auth.ok" {
		cancel()
		t.Fatalf("auth: %+v", auth)
	}
	f := &fakeHost{t: t, hub: hub, hostID: hostID, conn: conn, ctx: ctx, cancel: cancel, gotCh: make(chan protocol.Envelope, 64)}
	go f.readLoop()
	t.Cleanup(func() {
		cancel()
		conn.Close(websocket.StatusNormalClosure, "test done")
	})
	return f
}

func (f *fakeHost) setAnswer(answer fakeHostAnswer) {
	f.mu.Lock()
	f.answer = answer
	f.mu.Unlock()
}

func (f *fakeHost) readLoop() {
	for {
		_, data, err := f.conn.Read(f.ctx)
		if err != nil {
			return
		}
		var msg protocol.Envelope
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.Type != "request" {
			f.mu.Lock()
			f.got = append(f.got, msg)
			f.mu.Unlock()
			select {
			case f.gotCh <- msg:
			default:
			}
			continue
		}
		f.mu.Lock()
		answer := f.answer
		f.mu.Unlock()
		var result any
		errText := ""
		if answer == nil {
			errText = "no scripted answer"
		} else {
			result, errText = answer(msg.Method, msg.Params)
		}
		raw, _ := json.Marshal(result)
		resp := protocol.Envelope{Type: "result", RequestID: msg.RequestID, OK: errText == "", Result: raw, Error: errText}
		data, _ = json.Marshal(resp)
		_ = f.conn.Write(f.ctx, websocket.MessageText, data)
	}
}

func (f *fakeHost) send(msg protocol.Envelope) {
	f.t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.conn.Write(f.ctx, websocket.MessageText, data); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeHost) waitCommand(t *testing.T, kind string) protocol.Envelope {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-f.gotCh:
			if msg.Type == kind {
				return msg
			}
		case <-deadline:
			t.Fatalf("host did not receive %q", kind)
		}
	}
}

func writeClientEnvelope(t *testing.T, conn *websocket.Conn, msg protocol.ClientEnvelope) {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func readClientError(t *testing.T, conn *websocket.Conn) protocol.ServerEnvelope {
	t.Helper()
	for i := 0; i < 20; i++ {
		msg := protocol.ServerEnvelope{}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, data, err := conn.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read client error: %v", err)
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.Type == "error" {
			return msg
		}
	}
	t.Fatal("no error envelope received")
	return protocol.ServerEnvelope{}
}

func waitFor(t *testing.T, label string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", label)
}

func sessionViewByNative(t *testing.T, hub *apiTestHub, owner User, hostID, nativeID string) protocol.SessionView {
	t.Helper()
	list, err := hub.store.ListSessions(owner.ID, hostID, "", "", "", "", 100, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.NativeID == nativeID {
			return s
		}
	}
	t.Fatalf("session %s not found", nativeID)
	return protocol.SessionView{}
}

func TestHubFakeHostSessionsTranscriptSearchAndExport(t *testing.T) {
	hub := newSetupHub(t)
	signer, err := OpenHubSigner(filepath.Join(t.TempDir(), "signing"))
	if err != nil {
		t.Fatal(err)
	}
	hub.server.SetSigner(signer)
	owner, _ := hub.store.UserByUsername("owner")
	host := startFakeHost(t, hub, owner, "fake-linux")
	host.setAnswer(func(method string, params json.RawMessage) (any, string) {
		switch method {
		case "transcript":
			return protocol.Transcript{
				Messages: []protocol.Message{{Role: "user", Kind: "text", Text: "hello from the fake host", Timestamp: time.Now().UTC()}},
				Total:    1, Next: 0,
			}, ""
		case "search":
			return []protocol.SearchHit{{NativeID: "native-1", Agent: "opencode", Title: "Fix the bug", Snippet: "the needle is here"}}, ""
		case "git":
			return map[string]any{"branch": "main", "status": " M file.txt", "diff": "+added", "root": "/work/demo"}, ""
		default:
			return nil, "unsupported method " + method
		}
	})

	host.send(protocol.Envelope{Type: "inventory", Version: "alpha", Agents: []protocol.AgentInfo{
		{Name: "opencode", Version: "1.0.0", Supported: true, Capabilities: []string{"terminal", "structured"}},
	}})
	host.send(protocol.Envelope{Type: "sessions", Full: true, Sessions: []protocol.Session{{
		Agent: "opencode", NativeID: "native-1", Title: "Fix the bug", Cwd: "/work/demo",
		State: protocol.StateIdle, MessageCount: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}})

	waitFor(t, "session snapshot", func() bool {
		list, err := hub.store.ListSessions(owner.ID, host.hostID, "", "", "", "", 10, 0, true)
		return err == nil && len(list) == 1
	})
	sess := sessionViewByNative(t, hub, owner, host.hostID, "native-1")

	status, body := hub.do(nil, http.MethodGet, "/api/v1/hosts", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "hosts")
	if !strings.Contains(string(body), `"status":"online"`) {
		t.Fatalf("host not online: %s", body)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions?limit=10", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "sessions")
	if !strings.Contains(string(body), "Fix the bug") {
		t.Fatalf("session missing: %s", body)
	}

	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/transcript?limit=10", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "transcript")
	if !strings.Contains(string(body), "hello from the fake host") {
		t.Fatalf("transcript: %s", body)
	}

	status, body = hub.do(nil, http.MethodGet, "/api/v1/search?q=needle", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "search")
	if !strings.Contains(string(body), "the needle is here") || !strings.Contains(string(body), sess.ID) {
		t.Fatalf("search: %s", body)
	}

	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/git", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "git")
	if !strings.Contains(string(body), "main") || !strings.Contains(string(body), "+added") {
		t.Fatalf("git: %s", body)
	}

	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/export?format=markdown", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "markdown export")
	if !strings.Contains(string(body), "hello from the fake host") || !strings.Contains(string(body), "# Fix the bug") {
		t.Fatalf("markdown export: %s", body)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/export?format=json", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "json export")
	if !strings.Contains(string(body), `"messages"`) {
		t.Fatalf("json export: %s", body)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/export?format=nope", nil, nil)
	expectStatus(t, status, http.StatusBadRequest, body, "invalid export format")
}

func TestHubFakeHostRunsAndRunRequests(t *testing.T) {
	hub := newSetupHub(t)
	signer, err := OpenHubSigner(filepath.Join(t.TempDir(), "signing"))
	if err != nil {
		t.Fatal(err)
	}
	hub.server.SetSigner(signer)
	owner, _ := hub.store.UserByUsername("owner")
	host := startFakeHost(t, hub, owner, "fake-runs")
	host.setAnswer(func(method string, params json.RawMessage) (any, string) {
		switch method {
		case "run.options":
			return protocol.RunOptions{
				Models:       []protocol.ModelOption{{ID: "model-1", Name: "Model One", Provider: "prov"}},
				Modes:        []protocol.AgentModeOption{{ID: "build", Name: "Build"}},
				CurrentModel: "model-1",
			}, ""
		case "update.check":
			return protocol.UpdateStatus{Current: "alpha", Latest: "alpha", Available: false}, ""
		default:
			return nil, "unsupported method " + method
		}
	})
	host.send(protocol.Envelope{Type: "sessions", Full: true, Sessions: []protocol.Session{{
		Agent: "opencode", NativeID: "native-run", Title: "Run me", Cwd: "/work/demo",
		State: protocol.StateIdle, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}})
	waitFor(t, "run session", func() bool {
		list, err := hub.store.ListSessions(owner.ID, host.hostID, "", "", "", "", 10, 0, true)
		return err == nil && len(list) == 1
	})
	sess := sessionViewByNative(t, hub, owner, host.hostID, "native-run")

	status, body := hub.do(nil, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs", map[string]any{"mode": "structured"}, nil)
	expectStatus(t, status, http.StatusAccepted, body, "create structured run")
	var run protocol.RunView
	if err := json.Unmarshal(body, &run); err != nil || run.ID == "" {
		t.Fatalf("run response: %v %s", err, body)
	}
	start := host.waitCommand(t, "run.start")
	if start.Run == nil || start.Run.RunID != run.ID || start.Run.NativeID != "native-run" || start.Run.Mode != protocol.ModeStructured {
		t.Fatalf("run.start: %+v", start.Run)
	}

	status, body = hub.do(nil, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs", map[string]any{"mode": "structured"}, nil)
	expectStatus(t, status, http.StatusConflict, body, "duplicate run")
	if !strings.Contains(string(body), run.ID) {
		t.Fatalf("conflict should carry the active run: %s", body)
	}

	status, body = hub.do(nil, http.MethodGet, "/api/v1/runs/"+run.ID+"/options", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "run options")
	if !strings.Contains(string(body), "model-1") {
		t.Fatalf("options: %s", body)
	}

	// Host-side events flow into the client subscription and the run store.
	host.send(protocol.Envelope{Type: "run.started", RunID: run.ID})
	host.send(protocol.Envelope{Type: "run.event", RunID: run.ID, Event: &protocol.RunEvent{Type: "message", Seq: 1, Role: "assistant", Text: "hello"}})
	host.send(protocol.Envelope{Type: "run.output", RunID: run.ID, Seq: 1, Data: []byte("chunk")})
	waitFor(t, "run state", func() bool {
		got, _, err := hub.store.RunByID(run.ID)
		return err == nil && got.State == protocol.StateRunning
	})
	status, body = hub.do(nil, http.MethodGet, "/api/v1/runs", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "active runs")
	if !strings.Contains(string(body), run.ID) {
		t.Fatalf("active runs: %s", body)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/runs/"+run.ID, nil, nil)
	expectStatus(t, status, http.StatusOK, body, "run detail")
	if !strings.Contains(string(body), `"last_event_seq":1`) {
		t.Fatalf("run detail: %s", body)
	}

	status, body = hub.do(nil, http.MethodPost, "/api/v1/runs/"+run.ID+"/lease", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "lease")
	status, body = hub.do(nil, http.MethodPost, "/api/v1/runs/"+run.ID+"/stop", nil, nil)
	expectStatus(t, status, http.StatusAccepted, body, "stop")
	host.waitCommand(t, "run.stop")

	host.send(protocol.Envelope{Type: "run.exit", RunID: run.ID, Code: 0, Reason: "done"})
	waitFor(t, "run exit", func() bool {
		got, _, err := hub.store.RunByID(run.ID)
		return err == nil && got.State == protocol.StateExited
	})

	status, body = hub.do(nil, http.MethodGet, "/api/v1/hosts/"+host.hostID+"/update", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "host update check")
	if !strings.Contains(string(body), "alpha") {
		t.Fatalf("update status: %s", body)
	}

	status, body = hub.do(nil, http.MethodGet, "/api/v1/audit", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "audit")
	if !strings.Contains(string(body), "run.started") {
		t.Fatalf("audit: %s", body)
	}
}

func TestHubClientWebSocketDrivesRun(t *testing.T) {
	hub := newSetupHub(t)
	signer, err := OpenHubSigner(filepath.Join(t.TempDir(), "signing"))
	if err != nil {
		t.Fatal(err)
	}
	hub.server.SetSigner(signer)
	owner, _ := hub.store.UserByUsername("owner")
	host := startFakeHost(t, hub, owner, "fake-ws")
	host.setAnswer(func(method string, params json.RawMessage) (any, string) {
		return map[string]any{}, ""
	})
	host.send(protocol.Envelope{Type: "sessions", Full: true, Sessions: []protocol.Session{{
		Agent: "opencode", NativeID: "native-ws", Title: "WS run", Cwd: "/work/demo",
		State: protocol.StateIdle, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}})
	waitFor(t, "ws session", func() bool {
		list, err := hub.store.ListSessions(owner.ID, host.hostID, "", "", "", "", 10, 0, true)
		return err == nil && len(list) == 1
	})
	sess := sessionViewByNative(t, hub, owner, host.hostID, "native-ws")
	status, body := hub.do(nil, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs", map[string]any{"mode": "structured"}, nil)
	expectStatus(t, status, http.StatusAccepted, body, "create run")
	var run protocol.RunView
	_ = json.Unmarshal(body, &run)
	host.waitCommand(t, "run.start")

	base, _ := url.Parse(hub.http.URL)
	var cookie string
	for _, c := range hub.client.Jar.Cookies(base) {
		if c.Name == cookieName {
			cookie = c.Name + "=" + c.Value
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(hub, "/api/v1/ws"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {cookie}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	if ready := hub.readEnvelope(t, conn, 5*time.Second); ready.Type != "ready" {
		t.Fatalf("ready: %+v", ready)
	}

	writeClientEnvelope(t, conn, protocol.ClientEnvelope{Type: "subscribe", RunID: run.ID})
	if state := hub.readEnvelope(t, conn, 5*time.Second); state.Type != "run.state" {
		t.Fatalf("subscribe: %+v", state)
	}
	status, body = hub.do(nil, http.MethodPost, "/api/v1/runs/"+run.ID+"/lease", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "lease")

	writeClientEnvelope(t, conn, protocol.ClientEnvelope{Type: "input", RunID: run.ID, Data: []byte("ls\n")})
	host.waitCommand(t, "run.input")
	writeClientEnvelope(t, conn, protocol.ClientEnvelope{Type: "prompt", RunID: run.ID, Text: "do the thing"})
	host.waitCommand(t, "run.prompt")
	writeClientEnvelope(t, conn, protocol.ClientEnvelope{Type: "approval", RunID: run.ID, ApprovalID: "a1", Decision: "once"})
	host.waitCommand(t, "run.approval")
	writeClientEnvelope(t, conn, protocol.ClientEnvelope{Type: "interrupt", RunID: run.ID})
	host.waitCommand(t, "run.interrupt")
	writeClientEnvelope(t, conn, protocol.ClientEnvelope{Type: "unsubscribe", RunID: run.ID})

	// Unknown runs and invalid decisions are rejected without reaching the host.
	writeClientEnvelope(t, conn, protocol.ClientEnvelope{Type: "subscribe", RunID: "missing"})
	if reply := readClientError(t, conn); !strings.Contains(reply.Error, "run not found") {
		t.Fatalf("unknown subscribe: %+v", reply)
	}
	writeClientEnvelope(t, conn, protocol.ClientEnvelope{Type: "approval", RunID: run.ID, ApprovalID: "a2", Decision: "bogus"})
	if reply := readClientError(t, conn); !strings.Contains(reply.Error, "invalid approval") {
		t.Fatalf("invalid approval: %+v", reply)
	}
	writeClientEnvelope(t, conn, protocol.ClientEnvelope{Type: "input", RunID: run.ID, Data: make([]byte, maxTerminalInputSize+1)})
	if reply := readClientError(t, conn); !strings.Contains(reply.Error, "too large") {
		t.Fatalf("oversized input: %+v", reply)
	}
	writeClientEnvelope(t, conn, protocol.ClientEnvelope{Type: "prompt", RunID: run.ID, Text: strings.Repeat("x", maxPromptBytes+1)})
	if reply := readClientError(t, conn); !strings.Contains(reply.Error, "too large") {
		t.Fatalf("oversized prompt: %+v", reply)
	}
}

func TestHubHostDisconnectFailsActiveRuns(t *testing.T) {
	hub := newSetupHub(t)
	signer, err := OpenHubSigner(filepath.Join(t.TempDir(), "signing"))
	if err != nil {
		t.Fatal(err)
	}
	hub.server.SetSigner(signer)
	owner, _ := hub.store.UserByUsername("owner")
	host := startFakeHost(t, hub, owner, "fake-drop")
	host.setAnswer(func(method string, params json.RawMessage) (any, string) { return map[string]any{}, "" })
	host.send(protocol.Envelope{Type: "sessions", Full: true, Sessions: []protocol.Session{{
		Agent: "opencode", NativeID: "native-drop", Title: "Drop", Cwd: "/work/demo",
		State: protocol.StateIdle, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}})
	waitFor(t, "drop session", func() bool {
		list, err := hub.store.ListSessions(owner.ID, host.hostID, "", "", "", "", 10, 0, true)
		return err == nil && len(list) == 1
	})
	sess := sessionViewByNative(t, hub, owner, host.hostID, "native-drop")
	status, body := hub.do(nil, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs", map[string]any{"mode": "terminal"}, nil)
	expectStatus(t, status, http.StatusAccepted, body, "create run")
	var run protocol.RunView
	_ = json.Unmarshal(body, &run)
	host.waitCommand(t, "run.start")
	host.send(protocol.Envelope{Type: "run.started", RunID: run.ID})
	waitFor(t, "running", func() bool {
		got, _, err := hub.store.RunByID(run.ID)
		return err == nil && got.State == protocol.StateRunning
	})
	host.cancel()
	waitFor(t, "failed after disconnect", func() bool {
		got, _, err := hub.store.RunByID(run.ID)
		return err == nil && got.State == protocol.StateFailed
	})
}

func TestHubProjectsHostIndexStatus(t *testing.T) {
	hub := newSetupHub(t)
	signer, err := OpenHubSigner(filepath.Join(t.TempDir(), "signing"))
	if err != nil {
		t.Fatal(err)
	}
	hub.server.SetSigner(signer)
	owner, _ := hub.store.UserByUsername("owner")
	host := startFakeHost(t, hub, owner, "index-host")
	host.setAnswer(func(method string, params json.RawMessage) (any, string) {
		if method == "search" {
			return []protocol.SearchHit{{NativeID: "n1", Agent: "opencode", Title: "Hit", Snippet: "text"}}, ""
		}
		return nil, "unsupported"
	})
	host.send(protocol.Envelope{Type: "inventory", Version: "alpha", Index: &protocol.IndexStatus{
		State: "building", Indexed: 42, Total: 100, Bytes: 4096,
	}})
	waitFor(t, "index status", func() bool {
		status := hub.server.hostIndexStatus(host.hostID)
		return status != nil && status.State == "building" && status.Indexed == 42
	})
	status, body := hub.do(nil, http.MethodGet, "/api/v1/hosts", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "hosts with index")
	if !strings.Contains(string(body), `"state":"building"`) || !strings.Contains(string(body), `"indexed":42`) {
		t.Fatalf("host index: %s", body)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/search?q=needle", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "search with index status")
	if !strings.Contains(string(body), `"index_status"`) || !strings.Contains(string(body), `"index-host"`) {
		t.Fatalf("search index status: %s", body)
	}
}
