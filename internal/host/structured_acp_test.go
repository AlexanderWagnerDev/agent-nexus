package host

import (
	"context"
	"encoding/json"
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
