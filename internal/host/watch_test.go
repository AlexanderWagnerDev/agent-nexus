package host

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionFingerprintTracksAgentStores(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "sessions")
	if err := os.MkdirAll(filepath.Join(sessions, "2026", "01"), 0o700); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(sessions, "2026", "01", "rollout.jsonl")
	if err := os.WriteFile(first, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(root, "opencode.db")
	if err := os.WriteFile(db, []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := []string{sessions, db}
	before := sessionFingerprint(paths)
	if again := sessionFingerprint(paths); again != before {
		t.Fatal("fingerprint is not stable between identical scans")
	}

	// A new session file, an extended file and a WAL write must all change the
	// fingerprint so the daemon refreshes within the five-second budget.
	second := filepath.Join(sessions, "2026", "01", "rollout-2.jsonl")
	if err := os.WriteFile(second, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	afterNew := sessionFingerprint(paths)
	if afterNew == before {
		t.Fatal("new session file did not change the fingerprint")
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(first, []byte("{}\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	afterExtend := sessionFingerprint(paths)
	if afterExtend == afterNew {
		t.Fatal("extended session file did not change the fingerprint")
	}
	wal := db + "-wal"
	if err := os.WriteFile(wal, []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if sessionFingerprint(paths) == afterExtend {
		t.Fatal("SQLite WAL write did not change the fingerprint")
	}
}

func TestWatchPathsComeFromAdapters(t *testing.T) {
	d := &Daemon{adapters: []Adapter{
		NewClaudeAdapter("claude"),
		NewCodexAdapter("codex"),
		NewOpenCodeAdapter("opencode"),
		NewGeminiAdapter("gemini"),
	}}
	for _, path := range d.watchPaths() {
		if path == "" {
			t.Fatal("adapter returned an empty watch path")
		}
	}
	if len(d.watchPaths()) != 4 {
		t.Fatalf("expected four watch paths, got %v", d.watchPaths())
	}
}
