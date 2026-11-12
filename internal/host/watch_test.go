package host

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitForWatcherChange polls like the daemon loop does. It absorbs the
// directory-timestamp lag Windows shows right after a write.
func waitForWatcherChange(w *storeWatcher, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if w.FastChanged(time.Now()) {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return w.FastChanged(time.Now())
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

func TestStoreWatcherDetectsNewAndChangedFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sessions", "2026", "01")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(dir, "rollout-1.jsonl")
	if err := os.WriteFile(first, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	watcher := newStoreWatcher([]string{filepath.Join(root, "sessions")})
	watcher.FullScan()
	if watcher.FastChanged(time.Now()) {
		t.Fatal("an unchanged store was reported as changed")
	}

	second := filepath.Join(dir, "rollout-2.jsonl")
	if err := os.WriteFile(second, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Windows can update the parent directory timestamp slightly after the
	// write. The daemon polls every two seconds; the test polls until the
	// five-second requirement is satisfied instead of assuming the immediate
	// next call sees the file.
	if !waitForWatcherChange(watcher, 5*time.Second) {
		t.Fatal("a new session file was not detected within five seconds")
	}
	if watcher.FastChanged(time.Now()) {
		t.Fatal("the new file was detected twice without another change")
	}

	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(second, []byte("{}\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !watcher.FastChanged(time.Now()) {
		t.Fatal("a changed hot session file was not detected")
	}
}

func TestStoreWatcherFullScanCatchesColdFiles(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)
	for i := 0; i < 260; i++ {
		path := filepath.Join(root, fmt.Sprintf("session-%03d.jsonl", i))
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := old.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	cold := filepath.Join(root, "session-000.jsonl")
	watcher := newStoreWatcher([]string{root})
	watcher.FullScan()
	if len(watcher.hot) != discoveryHotFileLimit {
		t.Fatalf("hot list size: %d", len(watcher.hot))
	}
	for _, path := range watcher.hot {
		if path == cold {
			t.Fatal("the oldest file must not be in the hot list")
		}
	}
	// Modifying a cold file does not touch its directory mtime. It is only
	// found by the 30-second full scan.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(cold, []byte("{}\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if watcher.FastChanged(time.Now()) {
		t.Fatal("the cold file was unexpectedly detected by the hot poll")
	}
	if !watcher.FullScan() {
		t.Fatal("the full scan did not detect the changed cold file")
	}
}

func TestStoreWatcherDatabaseAndFallback(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "opencode.db")
	if err := os.WriteFile(db, []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	watcher := newStoreWatcher([]string{db})
	watcher.FullScan()
	if watcher.FastChanged(time.Now()) {
		t.Fatal("an unchanged database was reported as changed")
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(db, []byte("db2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !watcher.FastChanged(time.Now()) {
		t.Fatal("a database write was not detected")
	}
	if err := os.WriteFile(db+"-wal", []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !watcher.FastChanged(time.Now()) {
		t.Fatal("a WAL creation was not detected")
	}

	dirWatcher := newStoreWatcher([]string{root})
	dirWatcher.FullScan()
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	// A vanished directory must fall back to a full scan rather than loop on
	// stat errors or report success forever.
	dirWatcher.FastChanged(time.Now())
	if len(dirWatcher.files) != 0 {
		t.Fatalf("deleted files remained in the index: %v", dirWatcher.files)
	}
}

func makeSessionTree(tb testing.TB, count int) string {
	tb.Helper()
	root := tb.TempDir()
	for i := 0; i < count; i++ {
		dir := filepath.Join(root, fmt.Sprintf("bucket-%02d", i%100))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			tb.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("session-%05d.jsonl", i))
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			tb.Fatal(err)
		}
		stamp := time.Now().Add(-time.Duration(i) * time.Second)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			tb.Fatal(err)
		}
	}
	// Windows can update directory timestamps slightly after the file writes
	// that caused them. Let the fixture settle before taking a baseline.
	time.Sleep(300 * time.Millisecond)
	return root
}

func TestStoreWatcher10kReaction(t *testing.T) {
	if testing.Short() {
		t.Skip("10k fixture is skipped in short mode")
	}
	root := makeSessionTree(t, 10000)
	watcher := newStoreWatcher([]string{root})
	start := time.Now()
	watcher.FullScan()
	full := time.Since(start)

	// Windows can flush directory timestamp updates after the bulk create.
	// One warm-up poll may legitimately fall back to a full scan; steady state
	// is what the two-second poll actually costs.
	time.Sleep(100 * time.Millisecond)
	watcher.FastChanged(time.Now())
	start = time.Now()
	watcher.FastChanged(time.Now())
	idle := time.Since(start)

	newFile := filepath.Join(root, "bucket-00", "new-session.jsonl")
	start = time.Now()
	if err := os.WriteFile(newFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !waitForWatcherChange(watcher, 5*time.Second) {
		t.Fatal("the new file was not detected within five seconds")
	}
	newReaction := time.Since(start)

	hot := watcher.hot[0]
	time.Sleep(10 * time.Millisecond)
	start = time.Now()
	if err := os.WriteFile(hot, []byte("{}\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !watcher.FastChanged(time.Now()) {
		t.Fatal("the hot file change was not detected")
	}
	hotReaction := time.Since(start)
	t.Logf("10k watcher: full scan %s, idle poll %s, new-file reaction %s, hot-file reaction %s",
		full, idle, newReaction, hotReaction)
	if newReaction > 5*time.Second || hotReaction > 5*time.Second {
		t.Fatalf("reaction exceeded five seconds: new=%s hot=%s", newReaction, hotReaction)
	}
	if idle > time.Second {
		t.Fatalf("idle fast poll took %s", idle)
	}
}

func legacyWalkFingerprint(root string) uint64 {
	var count uint64
	var sum int64
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			count++
			sum += info.Size() + info.ModTime().UnixNano()
		}
		return nil
	})
	return count ^ uint64(sum)
}

func BenchmarkStoreWatcherFastPoll10k(b *testing.B) {
	root := makeSessionTree(b, 10000)
	watcher := newStoreWatcher([]string{root})
	watcher.FullScan()
	now := time.Now()
	// Warm up the timestamp-lag path once; the benchmark measures steady state.
	watcher.FastChanged(now)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		watcher.FastChanged(now)
	}
}

func BenchmarkStoreWatcherFullScan10k(b *testing.B) {
	root := makeSessionTree(b, 10000)
	watcher := newStoreWatcher([]string{root})
	watcher.FullScan()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		watcher.FullScan()
	}
}

func BenchmarkLegacyWalk10k(b *testing.B) {
	root := makeSessionTree(b, 10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		legacyWalkFingerprint(root)
	}
}
