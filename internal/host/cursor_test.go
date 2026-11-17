package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func cursorFixture(t *testing.T) *CursorAdapter {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "c-Users-demo-Documents-GitHub-agent-nexus")
	terminals := filepath.Join(project, "terminals")
	if err := os.MkdirAll(terminals, 0o755); err != nil {
		t.Fatal(err)
	}
	terminal := `--- pid: 42 cwd: "C:\\Users\\demo\\Documents\\GitHub\\agent-nexus" command: "git status" title: "Status" ---
`
	if err := os.WriteFile(filepath.Join(terminals, "42.txt"), []byte(terminal), 0o600); err != nil {
		t.Fatal(err)
	}
	id := "01a68b37-1b86-4aac-b44a-dda034a4f479"
	dir := filepath.Join(project, "agent-transcripts", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"role":"user","message":{"content":[{"type":"text","text":"<timestamp>Monday, Oct 12, 2026, 8:11 PM (UTC+2)</timestamp>\n<user_query>\nFix the checkout retry\n</user_query>"}]}}`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"I will inspect it."},{"type":"tool_use","name":"Read","input":{"path":"checkout.ts"}}]}}`,
		`{"type":"turn_ended","status":"success"}`,
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Subagent transcripts are one directory deeper and are not sessions.
	subagents := filepath.Join(dir, "subagents")
	if err := os.MkdirAll(subagents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagents, "sub-1.jsonl"), []byte(lines[0]+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A second workspace without terminal metadata exercises the decoded
	// fallback and its warning.
	decodedProject := filepath.Join(root, "c-Users-demo-project")
	decodedID := "057e4b94-8692-48fb-85b8-120e64a7ec77"
	decodedDir := filepath.Join(decodedProject, "agent-transcripts", decodedID)
	if err := os.MkdirAll(decodedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decodedDir, decodedID+".jsonl"), []byte(lines[0]+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &CursorAdapter{bin: "cursor-agent", root: root}
}

func TestCursorStoreOnlyDetect(t *testing.T) {
	root := t.TempDir()
	info, err := (&CursorAdapter{root: root}).Detect(context.Background())
	if err != nil || info.Supported || !sliceContains(info.Capabilities, "discovery") {
		t.Fatalf("store-only detect: %+v %v", info, err)
	}
	if _, err := (&CursorAdapter{root: filepath.Join(root, "missing")}).Detect(context.Background()); err == nil {
		t.Fatal("missing cursor store was accepted")
	}
}

func TestCursorAdapter(t *testing.T) {
	adapter := cursorFixture(t)
	sessions, err := adapter.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions: %+v", sessions)
	}
	byID := map[string]protocol.Session{}
	for _, session := range sessions {
		byID[session.NativeID] = session
	}
	session := byID["01a68b37-1b86-4aac-b44a-dda034a4f479"]
	if session.Agent != "cursor" || session.Title != "Fix the checkout retry" {
		t.Fatalf("session: %+v", session)
	}
	if session.Cwd != `C:\Users\demo\Documents\GitHub\agent-nexus` {
		t.Fatalf("cwd: %q", session.Cwd)
	}
	if session.MessageCount != 2 || session.Warning != "" {
		t.Fatalf("metadata: %+v", session)
	}
	decoded := byID["057e4b94-8692-48fb-85b8-120e64a7ec77"]
	if decoded.Warning == "" || !strings.Contains(decoded.Warning, "decoded") {
		t.Fatalf("decoded warning: %+v", decoded)
	}

	transcript, err := adapter.Transcript(context.Background(), "01a68b37-1b86-4aac-b44a-dda034a4f479", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if transcript.Total != 3 {
		t.Fatalf("transcript: %+v", transcript)
	}
	if transcript.Messages[0].Role != "user" || transcript.Messages[0].Text != "Fix the checkout retry" {
		t.Fatalf("user message: %+v", transcript.Messages[0])
	}
	var sawTool bool
	for _, message := range transcript.Messages {
		if message.Kind == "tool_call" && message.ToolName == "Read" && strings.Contains(message.ToolInput, "checkout.ts") {
			sawTool = true
		}
	}
	if !sawTool {
		t.Fatalf("tool call missing: %+v", transcript.Messages)
	}

	hits, err := adapter.Search(context.Background(), "checkout", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Agent != "cursor" || hits[0].Title != "Fix the checkout retry" {
		t.Fatalf("search: %+v", hits)
	}

	resume, err := adapter.BuildCommand(Action{Kind: protocol.ActionResume, NativeID: "01a68b37-1b86-4aac-b44a-dda034a4f479", Cwd: "C:\\work"})
	if err != nil || strings.Join(resume.Args, " ") != "--resume 01a68b37-1b86-4aac-b44a-dda034a4f479" {
		t.Fatalf("resume: %+v %v", resume, err)
	}
	if _, err := adapter.BuildCommand(Action{Kind: protocol.ActionFork, NativeID: "x"}); err == nil {
		t.Fatal("cursor fork was accepted")
	}
}
