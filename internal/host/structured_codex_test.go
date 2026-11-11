package host

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// TestCodexRunJSONRPC drives the Codex app-server client against a scripted
// JSON-RPC peer. It covers request correlation, options, prompt, interrupt and
// approval replies without starting the real Codex CLI.
func TestCodexRunJSONRPC(t *testing.T) {
	requestReader, requestWriter := io.Pipe()
	responseReader, responseWriter := io.Pipe()
	defer requestWriter.Close()
	defer responseWriter.Close()

	run := &CodexRun{
		id: "run-1", threadID: "thread-1", agent: "codex", cwd: t.TempDir(),
		stdin: requestWriter, pending: map[int64]chan json.RawMessage{},
		approvals: map[int64]string{}, daemon: &Daemon{log: slog.New(slog.NewTextHandler(io.Discard, nil))},
	}
	run.cancel = func() {}
	go run.readLoop(responseReader)

	seen := map[string]map[string]any{}
	go func() {
		scanner := bufio.NewScanner(requestReader)
		for scanner.Scan() {
			var request map[string]any
			if json.Unmarshal(scanner.Bytes(), &request) != nil {
				continue
			}
			method, _ := request["method"].(string)
			if method == "" {
				continue
			}
			seen[method] = request
			id, _ := request["id"].(float64)
			result := "{}"
			switch method {
			case "thread/start":
				result = `{"thread":{"id":"thread-1"}}`
			case "turn/start":
				result = `{"turn":{"id":"turn-1"}}`
			case "model/list":
				result = `{"data":[{"id":"m1","model":"gpt-test","displayName":"Test Model","isDefault":true,"defaultReasoningEffort":"medium","inputModalities":["text","image"],"supportedReasoningEfforts":[{"reasoningEffort":"low"},{"reasoningEffort":"high"}]},{"id":"hidden","model":"hidden-model","hidden":true}]}`
			}
			line := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`+"\n", int64(id), result)
			if _, err := responseWriter.Write([]byte(line)); err != nil {
				return
			}
		}
	}()

	if err := run.initialize(); err != nil {
		t.Fatal(err)
	}
	thread, err := run.startThread("/tmp")
	if err != nil || thread != "thread-1" {
		t.Fatalf("startThread: %q %v", thread, err)
	}
	options, err := run.Options(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Models) != 1 || options.Models[0].ID != "gpt-test" {
		t.Fatalf("options: %+v", options)
	}
	if len(options.Models[0].Variants) != 2 || !options.SupportsFiles {
		t.Fatalf("variants: %+v", options.Models[0])
	}
	if options.CurrentModel != "gpt-test" || options.CurrentVariant != "medium" {
		t.Fatalf("current model: %+v", options)
	}
	if err := run.Prompt("hello", &protocol.PromptOptions{Model: "gpt-test", Variant: "high"}); err != nil {
		t.Fatal(err)
	}
	if err := run.Interrupt(); err != nil {
		t.Fatal(err)
	}
	run.approvals[7] = "session/requestApproval"
	if err := run.ReplyApproval("7", "once"); err != nil {
		t.Fatal(err)
	}
	if err := run.ReplyApproval("not-a-number", "once"); err == nil {
		t.Fatal("invalid approval id accepted")
	}
	run.Stop()
	run.Stop()
	if !run.closed {
		t.Fatal("stop did not close the run")
	}
	if _, ok := seen["turn/start"]; !ok {
		t.Fatal("turn/start was not sent")
	}
	if _, ok := seen["model/list"]; !ok {
		t.Fatal("model/list was not sent")
	}
}
