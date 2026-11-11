package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func geminiFixture(t *testing.T) *GeminiAdapter {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "project-hash", "chats")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := `{
		"sessionId": "gem-1",
		"startTime": "2025-01-02T03:04:05Z",
		"messages": [
			{"role": "user", "content": "Investigate the retry loop in uploads"},
			{"role": "model", "content": [{"text": "The retry cap is missing; add one."}]}
		]
	}`
	if err := os.WriteFile(filepath.Join(dir, "session-1.json"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &GeminiAdapter{root: root}
}

func TestGeminiSearchFindsMessages(t *testing.T) {
	a := geminiFixture(t)
	hits, err := a.Search(context.Background(), "retry cap", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits: %+v", hits)
	}
	if hits[0].NativeID != "gem-1" || hits[0].Agent != "gemini" {
		t.Fatalf("hit identity: %+v", hits[0])
	}
	if hits[0].Snippet == "" || hits[0].Title == "" {
		t.Fatalf("hit snippet/title missing: %+v", hits[0])
	}
	if _, ok := hits[0].At.MarshalJSON(); ok != nil {
		t.Fatalf("timestamp: %v", hits[0].At)
	}
}

func TestGeminiSearchNoMatchAndEmptyQuery(t *testing.T) {
	a := geminiFixture(t)
	hits, err := a.Search(context.Background(), "nothing-like-this", 10)
	if err != nil || len(hits) != 0 {
		t.Fatalf("unexpected hits: %+v %v", hits, err)
	}
	hits, err = a.Search(context.Background(), "   ", 10)
	if err != nil || len(hits) != 0 {
		t.Fatalf("empty query: %+v %v", hits, err)
	}
}

func TestGeminiTranscriptAndSessions(t *testing.T) {
	a := geminiFixture(t)
	sessions, err := a.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].MessageCount != 2 {
		t.Fatalf("sessions: %+v", sessions)
	}
	tr, err := a.Transcript(context.Background(), "gem-1", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Messages) != 2 || tr.Messages[1].Role != "assistant" {
		t.Fatalf("transcript: %+v", tr)
	}
	if _, err := a.Transcript(context.Background(), "missing", 0, 10); err == nil {
		t.Fatal("unknown transcript was accepted")
	}
}

func TestGeminiBuildCommand(t *testing.T) {
	a := &GeminiAdapter{bin: "gemini"}
	cmd, err := a.BuildCommand(Action{Kind: "resume", NativeID: "gem-1", Cwd: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Cwd != "/tmp" || len(cmd.Args) < 2 || cmd.Args[0] != "--resume" {
		t.Fatalf("command: %+v", cmd)
	}
	if _, err := a.BuildCommand(Action{Kind: "resume"}); err == nil {
		t.Fatal("resume without native id accepted")
	}
	if _, err := a.BuildCommand(Action{Kind: "fork", NativeID: "x"}); err == nil {
		t.Fatal("fork accepted")
	}
	if _, err := a.BuildCommand(Action{Kind: "bogus"}); err == nil {
		t.Fatal("unknown action accepted")
	}
}

func TestSearchSnippetBounds(t *testing.T) {
	text := "prefix " + string(make([]byte, 0)) + "the needle appears here and then a long tail follows"
	got := searchSnippet(text, "needle")
	if got == "" || len(got) > len(text) {
		t.Fatalf("snippet: %q", got)
	}
	if got := searchSnippet(text, "absent"); got == "" {
		t.Fatal("no-match snippet should still return a bounded prefix")
	}
}
