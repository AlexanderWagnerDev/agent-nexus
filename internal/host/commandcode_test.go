package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestCommandCodeAdapter(t *testing.T) {
	root := t.TempDir()
	t.Setenv("COMMANDCODE_HOME", root)
	project := filepath.Join(root, "projects", "c-work-demo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "de35c943-0c91-4b78-8029-25e3d760f698"
	lines := []string{
		`{"type":"session","version":3,"id":"` + id + `","timestamp":"2026-10-08T20:50:30.089Z","cwd":"/work/demo"}`,
		`{"type":"message","id":"1","parentId":null,"timestamp":"2026-10-08T20:50:44.075Z","message":{"role":"user","content":[{"type":"text","text":"Fix the checkout retry"}]}}`,
		`{"type":"message","id":"2","parentId":"1","timestamp":"2026-10-08T20:50:45.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Need to inspect the test."},{"type":"text","text":"I will inspect it."},{"type":"tool_use","id":"call_1","name":"read_file","input":{"file_path":"/work/demo/checkout.ts"}}]}}`,
		`{"type":"message","id":"3","parentId":"2","timestamp":"2026-10-08T20:50:46.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"export function retry() {}"}]}]}}`,
	}
	sessionPath := filepath.Join(project, id+".jsonl")
	if err := os.WriteFile(sessionPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta := `{"title":"Fix checkout retry","parentSessionId":"parent-1"}`
	if err := os.WriteFile(filepath.Join(project, id+".meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	// Sidecars must never appear as sessions of their own.
	if err := os.WriteFile(filepath.Join(project, id+".checkpoints.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	adapter := NewCommandCodeAdapter("command-code")
	sessions, err := adapter.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions: %+v", sessions)
	}
	session := sessions[0]
	if session.NativeID != id || session.Agent != "commandcode" || session.Title != "Fix checkout retry" || session.Cwd != "/work/demo" {
		t.Fatalf("session: %+v", session)
	}
	if session.MessageCount != 3 {
		t.Fatalf("message count: %d", session.MessageCount)
	}
	if session.ForkedFromNativeID != "parent-1" {
		t.Fatalf("fork source: %q", session.ForkedFromNativeID)
	}

	transcript, err := adapter.Transcript(context.Background(), id, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if transcript.Total != 5 {
		t.Fatalf("transcript total: %d (%+v)", transcript.Total, transcript.Messages)
	}
	var sawToolResult bool
	for _, message := range transcript.Messages {
		if message.Kind == "tool_result" {
			sawToolResult = message.ToolName == "read_file" && strings.Contains(message.ToolOutput, "retry")
		}
	}
	if !sawToolResult {
		t.Fatalf("tool result was not paired with its tool call: %+v", transcript.Messages)
	}

	hits, err := adapter.Search(context.Background(), "retry", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Agent != "commandcode" || hits[0].Title != "Fix checkout retry" {
		t.Fatalf("search: %+v", hits)
	}

	resume, err := adapter.BuildCommand(Action{Kind: protocol.ActionResume, NativeID: id, Cwd: "/work/demo"})
	if err != nil || strings.Join(resume.Args, " ") != "--resume "+id {
		t.Fatalf("resume command: %+v %v", resume, err)
	}
	fork, err := adapter.BuildCommand(Action{Kind: protocol.ActionFork, NativeID: id, Cwd: "/work/demo"})
	if err != nil || !strings.Contains(strings.Join(fork.Args, " "), "--fork-session") {
		t.Fatalf("fork command: %+v %v", fork, err)
	}
}
