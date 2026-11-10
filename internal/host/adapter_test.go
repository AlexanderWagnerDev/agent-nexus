package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestClaudeAdapter(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	dir := filepath.Join(root, "projects", "-tmp-project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := strings.Join([]string{
		`{"type":"user","sessionId":"abc-123","cwd":"/tmp/project","gitBranch":"main","timestamp":"2026-01-02T03:04:05Z","message":{"role":"user","content":"Add a rate limiter"}}`,
		`{"type":"assistant","sessionId":"abc-123","cwd":"/tmp/project","timestamp":"2026-01-02T03:04:06Z","message":{"role":"assistant","content":[{"type":"text","text":"Done."},{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "abc-123.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	a := NewClaudeAdapter("claude")
	sessions, err := a.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].NativeID != "abc-123" || sessions[0].Title != "Add a rate limiter" {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}
	if sessions[0].GitBranch != "main" || sessions[0].MessageCount != 2 {
		t.Fatalf("metadata not parsed: %+v", sessions[0])
	}
	tr, err := a.Transcript(context.Background(), "abc-123", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Total != 3 {
		t.Fatalf("expected 3 transcript entries, got %d: %+v", tr.Total, tr.Messages)
	}
	cmd, err := a.BuildCommand(Action{Kind: protocol.ActionResume, NativeID: "abc-123", Cwd: "/tmp/project"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "--resume abc-123") {
		t.Fatalf("unexpected command: %+v", cmd)
	}
}

func TestCodexAdapter(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	dir := filepath.Join(root, "sessions", "2026", "01", "02")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := strings.Join([]string{
		`{"timestamp":"2026-01-02T03:04:05Z","type":"session_meta","payload":{"id":"codex-1","session_id":"codex-1","cwd":"/tmp/project","timestamp":"2026-01-02T03:04:05Z"}}`,
		`{"timestamp":"2026-01-02T03:04:06Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Fix the failing test"}]}}`,
		`{"timestamp":"2026-01-02T03:04:07Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Fixed."}]}}`,
		`{"timestamp":"2026-01-02T03:04:08Z","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"cmd\":\"go test\"}"}}`,
	}, "\n") + "\n"
	path := filepath.Join(dir, "rollout-2026-01-02T03-04-05-codex-1.jsonl")
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	a := NewCodexAdapter("codex")
	sessions, err := a.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].NativeID != "codex-1" || sessions[0].Title != "Fix the failing test" {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}
	tr, err := a.Transcript(context.Background(), "codex-1", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Total != 3 || tr.Messages[0].Text != "Fix the failing test" {
		t.Fatalf("unexpected transcript: %+v", tr)
	}
	cmd, err := a.BuildCommand(Action{Kind: protocol.ActionResume, NativeID: "codex-1", Cwd: "/tmp/project"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "resume codex-1") {
		t.Fatalf("unexpected command: %+v", cmd)
	}
}

func TestPolicy(t *testing.T) {
	root := t.TempDir()
	p := Policy{
		AllowedRoots:  []string{root},
		ForbiddenArgs: DefaultForbiddenArgs(),
	}
	if !p.WithinRoots(filepath.Join(root, "project")) {
		t.Fatal("path inside root was rejected")
	}
	if p.WithinRoots(filepath.Dir(root)) {
		t.Fatal("path outside root was accepted")
	}
	if err := p.ArgsAllowed([]string{"--resume", "abc"}); err != nil {
		t.Fatal(err)
	}
	if err := p.ArgsAllowed([]string{"--dangerously-skip-permissions"}); err == nil {
		t.Fatal("forbidden flag was accepted")
	}
}

func TestNativeIDValidation(t *testing.T) {
	for _, id := range []string{"abc-123", "ses_f01a2a721ffeYo2Tbq8zIUekXD", "01a1b2f4-599b-7c90-830b-e3adcd69fc80"} {
		if !validNativeID(id) {
			t.Fatalf("valid id %q rejected", id)
		}
	}
	for _, id := range []string{"", "--dangerously-skip-permissions", "-rf", "a b", "a/b", "a\nb"} {
		if validNativeID(id) {
			t.Fatalf("invalid id %q accepted", id)
		}
	}
}

func TestMatchActiveSessions(t *testing.T) {
	sessions := []protocol.Session{
		{Agent: "claude", NativeID: "abc-123"},
		{Agent: "codex", NativeID: "01a1b2f4-599b-7c90-830b-e3adcd69fc80"},
		{Agent: "opencode", NativeID: "ses_f01a2a721ffeYo2Tbq8zIUekXD"},
	}
	lines := []string{
		"/usr/bin/claude --resume abc-123",
		"C:\\tools\\opencode.cmd --session ses_f01a2a721ffeYo2Tbq8zIUekXD",
		"grep -R 01a1b2f4-599b-7c90-830b-e3adcd69fc80 .",
	}
	active := matchActiveSessions(lines, sessions)
	if !active["claude:abc-123"] || !active["opencode:ses_f01a2a721ffeYo2Tbq8zIUekXD"] {
		t.Fatalf("expected two active sessions, got %+v", active)
	}
	if active["codex:01a1b2f4-599b-7c90-830b-e3adcd69fc80"] {
		t.Fatal("a grep command line was misclassified as an active codex session")
	}
}
