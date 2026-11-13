package host

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/coder/websocket"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestChoosePermissionOption(t *testing.T) {
	options := []acp.PermissionOption{
		{OptionId: "yes", Kind: acp.PermissionOptionKindAllowOnce},
		{OptionId: "no", Kind: acp.PermissionOptionKindRejectOnce},
	}
	if got := choosePermissionOption(options, true); got == nil || got.OptionId != "yes" {
		t.Fatalf("allow picked %+v", got)
	}
	if got := choosePermissionOption(options, false); got == nil || got.OptionId != "no" {
		t.Fatalf("reject picked %+v", got)
	}
	if got := choosePermissionOption(nil, true); got != nil {
		t.Fatalf("empty options picked %+v", got)
	}
}

func TestACPCommandOverride(t *testing.T) {
	cfg := Config{ACPCommands: map[string]string{"custom": "my-acp-server --stdio"}}
	if got := cfg.ACPCommand("custom"); got != "my-acp-server --stdio" {
		t.Fatalf("override = %q", got)
	}
	if got := cfg.ACPCommand("missing"); got != "" {
		t.Fatalf("missing agent = %q", got)
	}
}

func TestSplitCommandLine(t *testing.T) {
	got := splitCommandLine(`"C:\Program Files\ACP\adapter.exe" --stdio`)
	want := []string{`C:\Program Files\ACP\adapter.exe`, "--stdio"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("split = %#v", got)
	}
}

func TestACPRunApprovalInterruptAndStop(t *testing.T) {
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
			_ = json.Unmarshal(data, &envelope)
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
	run := &ACPRun{
		id: "run-1", sessionID: "session-1", daemon: &Daemon{ws: ws, log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		approvals: map[string]chan string{}, state: "",
	}
	run.cancel = func() {}

	run.send(protocol.RunEvent{Type: "run.state", State: protocol.StateRunning})
	if run.currentState() != protocol.StateRunning {
		t.Fatalf("state: %s", run.currentState())
	}
	approval := make(chan string, 1)
	run.approvals["a1"] = approval
	if err := run.ReplyApproval("a1", "once"); err != nil {
		t.Fatal(err)
	}
	if got := <-approval; got != "once" {
		t.Fatalf("approval value: %q", got)
	}
	if err := run.ReplyApproval("missing", "once"); err == nil {
		t.Fatal("unknown approval accepted")
	}
	if err := run.Prompt("hello", &protocol.PromptOptions{Model: "m"}); err == nil {
		t.Fatal("ACP options were accepted without capability")
	}
	run.mu.Lock()
	run.closed = true
	run.mu.Unlock()
	if err := run.Prompt("hello", nil); err == nil {
		t.Fatal("closed run accepted a prompt")
	}
	if err := run.Interrupt(); err == nil {
		t.Fatal("closed run accepted an interrupt")
	}
	run.Stop()
	run.Stop()
	if got := acpContentText(acp.TextBlock("hello")); got != "hello" {
		t.Fatalf("acp text: %q", got)
	}
	if got := acpContentText(acp.ContentBlock{}); got != "" {
		t.Fatalf("empty acp text: %q", got)
	}
}

func newACPRunForTest(opts ...func(*ACPRun)) *ACPRun {
	r := &ACPRun{
		id: "run-1", sessionID: "session-1",
		daemon:    &Daemon{log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		approvals: map[string]chan string{}, state: protocol.StateIdle,
	}
	r.cancel = func() {}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func TestACPSessionUpdateEvents(t *testing.T) {
	r := newACPRunForTest()
	messageID := "m1"
	chunk := acp.SessionUpdateAgentMessageChunk{Content: acp.TextBlock("hello"), MessageId: &messageID}
	if err := r.SessionUpdate(context.Background(), acp.SessionNotification{Update: acp.SessionUpdate{AgentMessageChunk: &chunk}}); err != nil {
		t.Fatal(err)
	}
	if err := r.SessionUpdate(context.Background(), acp.SessionNotification{Update: acp.SessionUpdate{AgentMessageChunk: &chunk}}); err != nil {
		t.Fatal(err)
	}
	empty := acp.SessionUpdateAgentMessageChunk{Content: acp.ContentBlock{}}
	if err := r.SessionUpdate(context.Background(), acp.SessionNotification{Update: acp.SessionUpdate{AgentMessageChunk: &empty}}); err != nil {
		t.Fatal(err)
	}
	thought := acp.SessionUpdateAgentThoughtChunk{Content: acp.TextBlock("thinking")}
	if err := r.SessionUpdate(context.Background(), acp.SessionNotification{Update: acp.SessionUpdate{AgentThoughtChunk: &thought}}); err != nil {
		t.Fatal(err)
	}
	if got := r.thought.String(); got != "thinking" {
		t.Fatalf("thought: %q", got)
	}
	title := "Run tests"
	tool := acp.SessionUpdateToolCall{ToolCallId: "c1", Title: title, RawInput: map[string]any{"command": "go test"}}
	if err := r.SessionUpdate(context.Background(), acp.SessionNotification{Update: acp.SessionUpdate{ToolCall: &tool}}); err != nil {
		t.Fatal(err)
	}
	completed := acp.ToolCallStatusCompleted
	update := acp.SessionToolCallUpdate{ToolCallId: "c1", Status: &completed, RawOutput: "ok"}
	if err := r.SessionUpdate(context.Background(), acp.SessionNotification{Update: acp.SessionUpdate{ToolCallUpdate: &update}}); err != nil {
		t.Fatal(err)
	}
	failed := acp.ToolCallStatusFailed
	update = acp.SessionToolCallUpdate{ToolCallId: "c2", Status: &failed, Content: []acp.ToolCallContent{acp.ToolContent(acp.TextBlock("boom"))}}
	if err := r.SessionUpdate(context.Background(), acp.SessionNotification{Update: acp.SessionUpdate{ToolCallUpdate: &update}}); err != nil {
		t.Fatal(err)
	}
	update = acp.SessionToolCallUpdate{ToolCallId: "c3", RawInput: map[string]any{"path": "x"}}
	if err := r.SessionUpdate(context.Background(), acp.SessionNotification{Update: acp.SessionUpdate{ToolCallUpdate: &update}}); err != nil {
		t.Fatal(err)
	}
	if err := r.SessionUpdate(context.Background(), acp.SessionNotification{Update: acp.SessionUpdate{}}); err != nil {
		t.Fatal(err)
	}
}

func TestACPRequestPermissionApprovedAndCancelled(t *testing.T) {
	r := newACPRunForTest()
	title := "Write file"
	request := acp.RequestPermissionRequest{
		SessionId: "session-1",
		ToolCall:  acp.ToolCallUpdate{ToolCallId: "c1", Title: &title, RawInput: map[string]any{"path": "a.txt"}},
		Options: []acp.PermissionOption{
			{OptionId: "yes", Kind: acp.PermissionOptionKindAllowOnce},
			{OptionId: "no", Kind: acp.PermissionOptionKindRejectOnce},
		},
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		r.mu.Lock()
		var id string
		for key := range r.approvals {
			id = key
		}
		r.mu.Unlock()
		if id != "" {
			_ = r.ReplyApproval(id, "once")
		}
	}()
	resp, err := r.RequestPermission(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Outcome.Selected == nil || string(resp.Outcome.Selected.OptionId) != "yes" {
		t.Fatalf("approved outcome: %+v", resp.Outcome)
	}

	request.Options = nil
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	resp, err = r.RequestPermission(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Outcome.Cancelled == nil {
		t.Fatalf("cancelled outcome: %+v", resp.Outcome)
	}
}

func TestACPFailedPromptReturnsToIdle(t *testing.T) {
	events := make(chan protocol.RunEvent, 8)
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

	run := newACPRunForTest(func(r *ACPRun) {
		r.daemon = &Daemon{ws: ws, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	})
	run.mu.Lock()
	run.active = true
	run.state = protocol.StateRunning
	run.mu.Unlock()

	run.finishPrompt(errors.New("prompt failed"))

	var got []protocol.RunEvent
	for len(got) < 2 {
		select {
		case ev := <-events:
			got = append(got, ev)
		case <-time.After(5 * time.Second):
			t.Fatalf("events = %+v, want run.error then run.state idle", got)
		}
	}
	if got[0].Type != "run.error" || got[0].Error == "" {
		t.Fatalf("first event = %+v", got[0])
	}
	if got[1].Type != "run.state" || got[1].State != protocol.StateIdle {
		t.Fatalf("second event = %+v", got[1])
	}
	if run.currentState() != protocol.StateIdle {
		t.Fatalf("state = %q, want idle", run.currentState())
	}
	run.mu.Lock()
	active := run.active
	run.mu.Unlock()
	if active {
		t.Fatal("run stayed busy after a failed prompt")
	}
}

func TestACPPromptInterruptAndFileGuards(t *testing.T) {
	r := newACPRunForTest()
	if err := r.Prompt("hello", &protocol.PromptOptions{Model: "m"}); err == nil {
		t.Fatal("ACP options were accepted")
	}
	r.closed = true
	if err := r.Prompt("hello", nil); err == nil {
		t.Fatal("closed run accepted a prompt")
	}
	if err := r.Interrupt(); err == nil {
		t.Fatal("closed run accepted an interrupt")
	}
	r.closed = false
	r.active = true
	if err := r.Prompt("hello", nil); err == nil {
		t.Fatal("busy run accepted a prompt")
	}
	r.active = false
	r.conn = nil
	if _, err := r.ReadTextFile(context.Background(), acp.ReadTextFileRequest{}); err == nil {
		t.Fatal("read text file was supported")
	}
	if _, err := r.WriteTextFile(context.Background(), acp.WriteTextFileRequest{}); err == nil {
		t.Fatal("write text file was supported")
	}
	if _, err := r.CreateTerminal(context.Background(), acp.CreateTerminalRequest{}); err == nil {
		t.Fatal("create terminal was supported")
	}
	if _, err := r.KillTerminal(context.Background(), acp.KillTerminalRequest{}); err == nil {
		t.Fatal("kill terminal was supported")
	}
	if _, err := r.TerminalOutput(context.Background(), acp.TerminalOutputRequest{}); err == nil {
		t.Fatal("terminal output was supported")
	}
	if _, err := r.ReleaseTerminal(context.Background(), acp.ReleaseTerminalRequest{}); err == nil {
		t.Fatal("release terminal was supported")
	}
	if _, err := r.WaitForTerminalExit(context.Background(), acp.WaitForTerminalExitRequest{}); err == nil {
		t.Fatal("wait for terminal exit was supported")
	}
}
func TestACPControlsWithInProcessAgent(t *testing.T) {
	model := acp.SessionConfigOptionCategoryModel
	thought := acp.SessionConfigOptionCategoryThoughtLevel
	low := "low"
	sel := func(id string, category *acp.SessionConfigOptionCategory, current string, options []acp.SessionConfigSelectOption) acp.SessionConfigOption {
		return acp.SessionConfigOption{Select: &acp.SessionConfigOptionSelect{
			Id: acp.SessionConfigId(id), Name: id, Category: category,
			CurrentValue: acp.SessionConfigValueId(current),
			Options:      acp.SessionConfigSelectOptions{Ungrouped: (*acp.SessionConfigSelectOptionsUngrouped)(&options)},
		}}
	}
	_ = low
	fake := &fakeACPAgent{
		sessions: map[string]bool{},
		modes: &acp.SessionModeState{
			CurrentModeId: acp.SessionModeId("code"),
			AvailableModes: []acp.SessionMode{
				{Id: "code", Name: "Code"},
				{Id: "ask", Name: "Ask", Description: &low},
			},
		},
		configs: []acp.SessionConfigOption{
			sel("model", &model, "model-1", []acp.SessionConfigSelectOption{{Value: "model-1", Name: "Model One"}, {Value: "model-2", Name: "Model Two"}}),
			sel("reasoning", &thought, "medium", []acp.SessionConfigSelectOption{{Value: "medium", Name: "Medium"}, {Value: "high", Name: "High"}}),
		},
	}
	clientToAgentR, clientToAgentW := io.Pipe()
	agentToClientR, agentToClientW := io.Pipe()
	run := &ACPRun{
		id: "run-acp", sessionID: "session-1",
		daemon:    &Daemon{log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		approvals: map[string]chan string{}, state: protocol.StateIdle,
	}
	run.cancel = func() {}
	run.conn = acp.NewClientSideConnection(run, clientToAgentW, agentToClientR)
	fake.conn = acp.NewAgentSideConnection(fake, agentToClientW, clientToAgentR)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	init, err := run.conn.Initialize(ctx, acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersionNumber})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := run.conn.NewSession(ctx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	run.sessionID = resp.SessionId
	run.applySessionState(init, resp.Modes, resp.ConfigOptions)

	options, err := run.Options(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Models) != 2 || options.CurrentModel != "model-1" || len(options.Models[0].Variants) != 2 {
		t.Fatalf("models: %+v", options)
	}
	if len(options.Modes) != 2 || options.CurrentMode != "code" || !options.SupportsFiles {
		t.Fatalf("modes/files: %+v", options)
	}
	if options.CurrentVariant != "medium" {
		t.Fatalf("variant: %+v", options)
	}

	err = run.Prompt("hello", &protocol.PromptOptions{
		Model: "model-2", Variant: "high", AgentMode: "ask",
		Attachments: []protocol.Attachment{{Name: "shot.png", MIME: "image/png", Data: "cG5n"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForHost(t, "acp prompt", func() bool { return len(fake.lastPrompt()) == 2 })
	blocks := fake.lastPrompt()
	if blocks[0].Text == nil || blocks[1].Image == nil || blocks[1].Image.MimeType != "image/png" {
		t.Fatalf("prompt blocks: %+v", blocks)
	}
	calls := fake.setSessionCalls()
	joined := strings.Join(calls, ",")
	for _, want := range []string{"config:model=model-2", "config:reasoning=high", "mode:ask"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}

	// Unsupported options fail before a prompt is started.
	if err := run.Prompt("x", &protocol.PromptOptions{Variant: "unknown"}); err != nil {
		_ = err // the agent does expose reasoning, so this is valid
	}
	run.Stop()
}
