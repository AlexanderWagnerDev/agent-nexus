package host

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeAndCodexSearch(t *testing.T) {
	claudeRoot := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeRoot)
	dir := filepath.Join(claudeRoot, "projects", "-tmp-project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","sessionId":"abc-123","cwd":"/tmp/project","timestamp":"2026-01-02T03:04:05Z","message":{"role":"user","content":"Needle in the Claude transcript"}}`
	if err := os.WriteFile(filepath.Join(dir, "abc-123.jsonl"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	claude := NewClaudeAdapter("claude")
	hits, err := claude.Search(context.Background(), "needle", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].NativeID != "abc-123" || !strings.Contains(hits[0].Snippet, "Needle") {
		t.Fatalf("claude search: %+v", hits)
	}
	if claude.Name() != "claude" {
		t.Fatalf("name: %q", claude.Name())
	}
	_, _ = (&ClaudeAdapter{bin: "definitely-not-installed-agent", root: t.TempDir()}).Detect(context.Background())

	codexRoot := t.TempDir()
	t.Setenv("CODEX_HOME", codexRoot)
	codexDir := filepath.Join(codexRoot, "sessions", "2026", "01", "02")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	codexLines := strings.Join([]string{
		`{"timestamp":"2026-01-02T03:04:05Z","type":"session_meta","payload":{"id":"11111111-2222-3333-4444-555555555555","session_id":"11111111-2222-3333-4444-555555555555","cwd":"/tmp/project"}}`,
		`{"timestamp":"2026-01-02T03:04:06Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Find the needle in Codex"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(codexDir, "rollout-2026-01-02T03-04-05-11111111-2222-3333-4444-555555555555.jsonl"), []byte(codexLines), 0o600); err != nil {
		t.Fatal(err)
	}
	codex := NewCodexAdapter("codex")
	hits, err = codex.Search(context.Background(), "needle", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].NativeID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("codex search: %+v", hits)
	}
	if codex.Name() != "codex" {
		t.Fatalf("name: %q", codex.Name())
	}
	_, _ = (&CodexAdapter{bin: "definitely-not-installed-agent", root: t.TempDir()}).Detect(context.Background())
}

func TestSmallHelpers(t *testing.T) {
	if PlatformName() == "" || !strings.Contains(PlatformName(), "/") {
		t.Fatalf("platform: %q", PlatformName())
	}
	if ReadHostname() == "" {
		t.Fatal("empty hostname")
	}
	for i := 0; i < 20; i++ {
		if got := jitter(time.Second); got < 0 || got > time.Second {
			t.Fatalf("jitter out of range: %v", got)
		}
	}
	closed := false
	z := &zstdReadCloser{Reader: strings.NewReader("x"), closeFn: func() { closed = true }}
	if _, err := io.ReadAll(z); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil || !closed {
		t.Fatalf("close: %v %v", err, closed)
	}
}
