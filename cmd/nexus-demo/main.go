// Command nexus-demo is a synthetic host used for screenshots, documentation
// and UI development. It speaks the normal hub protocol but serves fixture
// sessions, transcripts, git data and a scripted structured run instead of
// touching real agent stores.
//
// It is deliberately a separate binary: the production daemon never loads demo
// data, and the demo never reads a real user's files.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/host"
	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/coder/websocket"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "pair":
		pairCmd(os.Args[2:])
	case "serve":
		serveCmd(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println(`nexus-demo pair  --hub <url> --code <code> --name <name> --config <file>
nexus-demo serve --config <file> --scenario windows|linux|mac`)
}

func pairCmd(args []string) {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	hubURL := fs.String("hub", "http://127.0.0.1:8790", "hub URL")
	code := fs.String("code", "", "one-time pairing code")
	name := fs.String("name", "demo-host", "host display name")
	config := fs.String("config", "", "config file for this demo host")
	_ = fs.Parse(args)
	if *code == "" || *config == "" {
		log.Fatal("pair needs --code and --config")
	}
	_ = os.Setenv("NEXUSD_CONFIG", *config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, sas, err := host.Pair(ctx, *hubURL, *code, *name)
	if err != nil {
		log.Fatalf("pair: %v", err)
	}
	fmt.Printf("HOST=%s\nSAS=%s\n", cfg.HostID, sas)
}

func serveCmd(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	config := fs.String("config", "", "config file for this demo host")
	scenario := fs.String("scenario", "windows", "windows, linux or mac")
	_ = fs.Parse(args)
	if *config == "" {
		log.Fatal("serve needs --config")
	}
	_ = os.Setenv("NEXUSD_CONFIG", *config)
	cfg, err := host.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	key, err := cfg.SigningKey()
	if err != nil {
		log.Fatal(err)
	}
	d := &demoHost{cfg: cfg, key: key, fixtures: fixturesFor(*scenario)}
	for ctx.Err() == nil {
		if err := d.connect(ctx); err != nil && ctx.Err() == nil {
			log.Printf("link ended: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

type demoHost struct {
	cfg      host.Config
	key      ed25519.PrivateKey
	fixtures fixtures
	writeMu  sync.Mutex
}

func (d *demoHost) send(ws *websocket.Conn, msg protocol.Envelope) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return ws.Write(ctx, websocket.MessageText, data)
}

func (d *demoHost) connect(ctx context.Context) error {
	base, err := url.Parse(d.cfg.HubURL)
	if err != nil {
		return err
	}
	switch base.Scheme {
	case "https":
		base.Scheme = "wss"
	default:
		base.Scheme = "ws"
	}
	base.Path = "/v1/host/connect"
	q := base.Query()
	q.Set("host_id", d.cfg.HostID)
	base.RawQuery = q.Encode()

	ws, _, err := websocket.Dial(ctx, base.String(), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"X-Nexus-Request": {"1"}},
	})
	if err != nil {
		return err
	}
	defer ws.Close(websocket.StatusNormalClosure, "closing")
	ws.SetReadLimit(32 << 20)

	readCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, data, err := ws.Read(readCtx)
	if err != nil {
		return err
	}
	var hello protocol.Envelope
	if err := json.Unmarshal(data, &hello); err != nil || hello.Type != "hello" {
		return errors.New("no protocol hello")
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(d.key, []byte(hello.Nonce)))
	if err := d.send(ws, protocol.Envelope{
		Type: "auth", Protocol: protocol.Version, Signature: signature,
		HostName: d.cfg.HostName, OS: d.fixtures.os, Arch: d.fixtures.arch, Version: "alpha",
	}); err != nil {
		return err
	}
	_, data, err = ws.Read(readCtx)
	if err != nil {
		return err
	}
	var auth protocol.Envelope
	if err := json.Unmarshal(data, &auth); err != nil || auth.Type != "auth.ok" {
		return fmt.Errorf("auth rejected: %s", auth.Error)
	}

	_ = d.send(ws, protocol.Envelope{
		Type:    "inventory",
		Version: "alpha",
		Agents: []protocol.AgentInfo{
			{Name: "opencode", Version: "1.4.2", Path: d.fixtures.agentPath, Supported: true, Capabilities: []string{"structured", "attachments", "fork"}},
			{Name: "codex", Version: "0.58.0", Path: d.fixtures.agentPath, Supported: true, Capabilities: []string{"structured", "attachments"}},
			{Name: "claude", Version: "2.1.4", Path: d.fixtures.agentPath, Supported: true, Capabilities: []string{"structured", "attachments"}},
		},
		Policy: &protocol.PolicySummary{
			AllowedRoots:  []string{d.fixtures.root},
			AllowedAgents: []string{"opencode", "codex", "claude"},
			AllowNew:      true,
			AllowFork:     true,
		},
		Index: &protocol.IndexStatus{
			State: "ready", Indexed: len(d.fixtures.sessions), Total: len(d.fixtures.sessions), Bytes: 64 * 1024,
		},
	})
	_ = d.send(ws, protocol.Envelope{
		Type:   "update.status",
		Update: &protocol.UpdateStatus{Current: "alpha", CheckedAt: time.Now().UTC(), Error: "no published release"},
	})
	_ = d.send(ws, protocol.Envelope{Type: "sessions", Full: true, Sessions: d.fixtures.sessions})

	log.Printf("connected to %s as %s", d.cfg.HubURL, d.cfg.HostName)
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return err
		}
		var msg protocol.Envelope
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "request":
			result, err := d.answer(msg.Method, msg.Params)
			if err != nil {
				_ = d.send(ws, protocol.Envelope{Type: "result", RequestID: msg.RequestID, OK: false, Error: err.Error()})
				continue
			}
			_ = d.send(ws, protocol.Envelope{Type: "result", RequestID: msg.RequestID, OK: true, Result: result})
		case "run.start":
			if msg.Run != nil {
				go d.run(ws, *msg.Run)
			}
		case "run.stop":
			_ = d.send(ws, protocol.Envelope{Type: "run.exit", RunID: msg.RunID, Code: 0, Reason: "stopped"})
		}
	}
}

func (d *demoHost) answer(method string, params []byte) ([]byte, error) {
	switch method {
	case "transcript":
		var req struct {
			NativeID string `json:"native_id"`
			Offset   int    `json:"offset"`
			Limit    int    `json:"limit"`
		}
		_ = json.Unmarshal(params, &req)
		if req.Limit <= 0 || req.Limit > 500 {
			req.Limit = 200
		}
		messages := d.fixtures.transcripts[req.NativeID]
		if messages == nil {
			messages = d.fixtures.defaultTranscript
		}
		start := req.Offset
		if start > len(messages) {
			start = len(messages)
		}
		end := start + req.Limit
		if end > len(messages) {
			end = len(messages)
		}
		out := protocol.Transcript{Messages: messages[start:end], Total: len(messages)}
		if end < len(messages) {
			out.Next = end
		}
		return json.Marshal(out)
	case "git":
		return json.Marshal(d.fixtures.git)
	case "run.options":
		return json.Marshal(protocol.RunOptions{
			Models: []protocol.ModelOption{
				{ID: "claude-sonnet-4-5", Provider: "anthropic", Name: "Claude Sonnet 4.5", Variants: []string{"low", "medium", "high"}, DefaultVariant: "medium", Inputs: []string{"text", "image"}},
				{ID: "gpt-5.3-codex", Provider: "openai", Name: "GPT-5.3 Codex", Variants: []string{"low", "medium", "high"}, DefaultVariant: "high", Inputs: []string{"text", "image"}},
				{ID: "grok-code-fast", Provider: "xai", Name: "Grok Code Fast", Inputs: []string{"text"}},
			},
			Modes: []protocol.AgentModeOption{
				{ID: "build", Name: "Build", Description: "Edit files and run commands"},
				{ID: "plan", Name: "Plan", Description: "Read-only analysis"},
				{ID: "review", Name: "Review", Description: "Review the working tree"},
			},
			CurrentModel:    "claude-sonnet-4-5",
			CurrentProvider: "anthropic",
			CurrentVariant:  "medium",
			CurrentMode:     "build",
			SupportsFiles:   true,
		})
	case "search":
		return json.Marshal([]protocol.SearchHit{
			{SessionID: d.fixtures.sessions[0].NativeID, HostID: d.cfg.HostID, HostName: d.cfg.HostName, NativeID: d.fixtures.sessions[0].NativeID, Agent: "opencode", Title: d.fixtures.sessions[0].Title, Snippet: "retry backoff is capped at 30s", At: time.Now().Add(-2 * time.Hour)},
		})
	case "update.check":
		return json.Marshal(protocol.UpdateStatus{Current: "alpha", CheckedAt: time.Now().UTC(), Error: "no published release"})
	}
	return nil, fmt.Errorf("unsupported method %q", method)
}

func (d *demoHost) run(ws *websocket.Conn, spec protocol.RunSpec) {
	send := func(ev protocol.RunEvent) {
		_ = d.send(ws, protocol.Envelope{Type: "run.event", RunID: spec.RunID, Event: &ev})
		time.Sleep(120 * time.Millisecond)
	}
	_ = d.send(ws, protocol.Envelope{Type: "run.started", RunID: spec.RunID, Reason: "structured"})
	send(protocol.RunEvent{Type: "run.state", State: protocol.StateRunning})
	send(protocol.RunEvent{Type: "reasoning", MessageID: "m1", Text: "Reading the retry helper and the failing test, then applying the smallest fix."})
	send(protocol.RunEvent{Type: "message.started", MessageID: "m1"})
	for _, chunk := range []string{
		"I found the retry loop in ", "`internal/http/retry.go`. ", "The backoff keeps doubling without a ceiling, ",
		"so a long outage turns into a multi-minute wait. ", "I'll cap it and add a regression test.",
	} {
		send(protocol.RunEvent{Type: "message.delta", MessageID: "m1", Text: chunk})
	}
	send(protocol.RunEvent{Type: "message.completed", MessageID: "m1", Text: "I found the retry loop in `internal/http/retry.go`. The backoff keeps doubling without a ceiling, so a long outage turns into a multi-minute wait. I'll cap it and add a regression test."})
	send(protocol.RunEvent{Type: "tool.started", CallID: "c1", ToolName: "shell", ToolInput: `{"command":"go test ./internal/http/ -run Retry"}`})
	send(protocol.RunEvent{Type: "tool.completed", CallID: "c1", ToolOutput: "ok  \tgithub.com/example/sample-api/internal/http\t0.42s"})
	send(protocol.RunEvent{Type: "tool.started", CallID: "c2", ToolName: "edit", ToolInput: `{"filePath":"internal/http/retry.go"}`})
	send(protocol.RunEvent{Type: "tool.completed", CallID: "c2", ToolOutput: "--- a/internal/http/retry.go\n+++ b/internal/http/retry.go\n@@ -18,7 +18,7 @@ func backoff(attempt int) time.Duration {\n-\td := base << attempt\n+\td := min(base<<attempt, 30*time.Second)\n"})
	send(protocol.RunEvent{Type: "message.started", MessageID: "m2"})
	if spec.NativeID == "ses_demo_flaky_test" {
		send(protocol.RunEvent{Type: "message.completed", MessageID: "m2", Text: "The checkout test still fails intermittently: the fixture clock is shared between parallel cases. Suggested next step: isolate the clock per test."})
		send(protocol.RunEvent{Type: "tool.started", CallID: "c3", ToolName: "shell", ToolInput: `{"command":"go test ./checkout -run TestCheckout -count=20"}`})
		send(protocol.RunEvent{Type: "tool.completed", CallID: "c3", Error: "exit status 1: 2 of 20 runs failed"})
		_ = d.send(ws, protocol.Envelope{Type: "run.exit", RunID: spec.RunID, Code: 1, Reason: "test failure"})
		return
	}
	send(protocol.RunEvent{Type: "message.completed", MessageID: "m2", Text: "Retry delays are now capped at 30 seconds. The new test covers attempts 0 through 6 and the suite is green."})
	send(protocol.RunEvent{Type: "run.state", State: protocol.StateIdle})
}

const diffFixture = `diff --git a/internal/http/retry.go b/internal/http/retry.go
index 41c2a91..8e7b204 100644
--- a/internal/http/retry.go
+++ b/internal/http/retry.go
@@ -14,10 +14,12 @@ const (
 	maxAttempts = 6
 )
 
-// backoff doubles the delay on every attempt without a ceiling.
+// backoff doubles the delay per attempt and caps it so a long outage does not
+// turn into a multi-minute wait.
 func backoff(attempt int) time.Duration {
-	d := base << attempt
+	d := min(base<<attempt, 30*time.Second)
 	return d
 }
 
@@ -31,6 +33,9 @@ func Do(ctx context.Context, fn func() error) error {
 		if err == nil {
 			return nil
 		}
+		if ctx.Err() != nil {
+			return ctx.Err()
+		}
 		time.Sleep(backoff(attempt))
 	}
 	return err
diff --git a/internal/http/retry_test.go b/internal/http/retry_test.go
index 8a41e77..f2b7c19 100644
--- a/internal/http/retry_test.go
+++ b/internal/http/retry_test.go
@@ -8,3 +8,11 @@ func TestRetrySucceeds(t *testing.T) {
 		return nil
 	}); err != nil {
 		t.Fatal(err)
 	}
 }
+
+func TestBackoffIsCapped(t *testing.T) {
+	for attempt := 0; attempt <= 6; attempt++ {
+		if got := backoff(attempt); got > 30*time.Second {
+			t.Fatalf("attempt %d: backoff %s exceeds cap", attempt, got)
+		}
+	}
+}
`

// fixtureNow allows screenshot and visual-regression runs to pin every
// synthetic timestamp instead of drifting with the wall clock.
func fixtureNow() time.Time {
	if value := strings.TrimSpace(os.Getenv("NEXUS_DEMO_NOW")); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Now().UTC()
}

type fixtures struct {
	os                string
	arch              string
	root              string
	agentPath         string
	sessions          []protocol.Session
	transcripts       map[string][]protocol.Message
	defaultTranscript []protocol.Message
	git               map[string]any
}

func fixturesFor(scenario string) fixtures {
	now := fixtureNow()
	root := `C:\Users\demo\Projects`
	agentPath := `C:\Users\demo\AppData\Roaming\npm\opencode.cmd`
	osName, arch := "windows", "amd64"
	switch scenario {
	case "linux":
		root = "/home/demo/projects"
		agentPath = "/home/demo/.local/bin/opencode"
		osName, arch = "linux", "amd64"
	case "mac":
		root = "/Users/demo/projects"
		agentPath = "/Users/demo/.local/bin/opencode"
		osName, arch = "darwin", "arm64"
	}
	join := func(name string) string {
		if strings.HasPrefix(root, "/") {
			return root + "/" + name
		}
		return root + `\` + name
	}
	baseSessions := []protocol.Session{
		{
			NativeID: "ses_demo_upload_retry", Agent: "opencode",
			Title: "Refactor upload retries in sample-api", Cwd: join("sample-api"),
			GitBranch: "feat/upload-retry", RepoRoot: join("sample-api"), RepoURL: "https://github.com/example/sample-api",
			CreatedAt: now.Add(-26 * time.Hour), UpdatedAt: now.Add(-40 * time.Minute),
			MessageCount: 18, State: protocol.StateIdle,
		},
		{
			NativeID: "ses_demo_pagination", Agent: "codex",
			Title: "Add pagination to the customers endpoint", Cwd: join("example-service"),
			GitBranch: "main", RepoRoot: join("example-service"), RepoURL: "https://github.com/example/example-service",
			CreatedAt: now.Add(-9 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
			MessageCount: 12, State: protocol.StateIdle,
		},
		{
			NativeID: "ses_demo_flaky_test", Agent: "opencode",
			Title: "Investigate the flaky checkout test", Cwd: join("web-checkout"),
			GitBranch: "fix/flaky-checkout", RepoRoot: join("web-checkout"), RepoURL: "https://github.com/example/web-checkout",
			CreatedAt: now.Add(-3 * 24 * time.Hour), UpdatedAt: now.Add(-5 * time.Hour),
			MessageCount: 9, State: protocol.StateCompleted,
		},
	}
	if scenario == "linux" {
		baseSessions = []protocol.Session{
			{
				NativeID: "ses_demo_dockerfile", Agent: "claude",
				Title: "Harden the Dockerfile for example-service", Cwd: join("example-service"),
				GitBranch: "chore/container-hardening", RepoRoot: join("example-service"), RepoURL: "https://github.com/example/example-service",
				CreatedAt: now.Add(-30 * time.Hour), UpdatedAt: now.Add(-15 * time.Minute),
				MessageCount: 14, State: protocol.StateIdle,
			},
			{
				NativeID: "ses_demo_pg_client", Agent: "codex",
				Title: "Upgrade the PostgreSQL client", Cwd: join("billing-worker"),
				GitBranch: "main", RepoRoot: join("billing-worker"), RepoURL: "https://github.com/example/billing-worker",
				CreatedAt: now.Add(-4 * 24 * time.Hour), UpdatedAt: now.Add(-8 * time.Hour),
				MessageCount: 7, State: protocol.StateIdle,
			},
		}
	}
	if scenario == "mac" {
		baseSessions = []protocol.Session{
			{
				NativeID: "ses_demo_a11y", Agent: "claude",
				Title: "Review accessibility of the settings page", Cwd: join("sample-web"),
				GitBranch: "feat/settings-a11y", RepoRoot: join("sample-web"), RepoURL: "https://github.com/example/sample-web",
				CreatedAt: now.Add(-7 * time.Hour), UpdatedAt: now.Add(-25 * time.Minute),
				MessageCount: 11, State: protocol.StateIdle,
			},
			{
				NativeID: "ses_demo_docs", Agent: "opencode",
				Title: "Document the release checklist", Cwd: join("sample-api"),
				GitBranch: "docs/release-checklist", RepoRoot: join("sample-api"), RepoURL: "https://github.com/example/sample-api",
				CreatedAt: now.Add(-2 * 24 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour),
				MessageCount: 6, State: protocol.StateIdle,
			},
		}
	}

	user := protocol.Message{Role: "user", Kind: "text", Text: "The upload retries can wait for minutes during an outage. Can you find why and add a test?", Timestamp: now.Add(-70 * time.Minute)}
	assistant := protocol.Message{Role: "assistant", Kind: "text", Text: "The backoff doubles on every attempt with no ceiling. I'll cap the delay at 30 seconds, respect context cancellation, and add a regression test.", Timestamp: now.Add(-68 * time.Minute)}
	tool := protocol.Message{
		Role: "assistant", Kind: "tool_call", ToolName: "shell",
		ToolInput:  `{"command":"rg -n \"func backoff\" internal/http"}`,
		ToolOutput: "internal/http/retry.go:18:func backoff(attempt int) time.Duration {",
		Timestamp:  now.Add(-67 * time.Minute),
	}
	editInput, _ := json.Marshal(map[string]string{"filePath": "internal/http/retry.go", "diff": diffFixture})
	edit := protocol.Message{
		Role: "assistant", Kind: "tool_call", ToolName: "edit",
		ToolInput:  string(editInput),
		ToolOutput: "updated internal/http/retry.go",
		Timestamp:  now.Add(-64 * time.Minute),
	}
	result := protocol.Message{Role: "assistant", Kind: "text", Text: "Retry delays are capped at 30 seconds and cancellation now stops the loop. `go test ./internal/http/` passes.", Timestamp: now.Add(-62 * time.Minute)}

	transcripts := map[string][]protocol.Message{
		"ses_demo_upload_retry": {user, assistant, tool, edit, result},
	}
	return fixtures{
		os: osName, arch: arch, root: root, agentPath: agentPath,
		sessions:    baseSessions,
		transcripts: transcripts,
		defaultTranscript: []protocol.Message{
			{Role: "user", Kind: "text", Text: "Summarize the current state of this task.", Timestamp: now.Add(-20 * time.Minute)},
			{Role: "assistant", Kind: "text", Text: "The change is implemented and the focused test suite passes. Nothing is committed yet.", Timestamp: now.Add(-19 * time.Minute)},
		},
		git: map[string]any{
			"branch": "feat/upload-retry",
			"root":   join("sample-api"),
			"status": " M internal/http/retry.go\n M internal/http/retry_test.go",
			"stat":   " internal/http/retry.go      | 7 ++++---\n internal/http/retry_test.go | 8 ++++++++\n 2 files changed, 12 insertions(+), 3 deletions(-)",
			"diff":   diffFixture,
		},
	}
}
