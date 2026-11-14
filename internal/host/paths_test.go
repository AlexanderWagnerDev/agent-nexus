package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestCodexFindFileRequiresExactSessionSuffix(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sessions", "2025", "01", "01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	wrong := filepath.Join(dir, "rollout-abc-unrelated.jsonl")
	right := filepath.Join(dir, "rollout-2025-01-01T00-00-00-abc.jsonl")
	for _, path := range []string{wrong, right} {
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := &CodexAdapter{root: root}
	found, err := a.findFile("abc")
	if err != nil {
		t.Fatal(err)
	}
	if found != right {
		t.Fatalf("findFile matched %q, want %q", filepath.Base(found), filepath.Base(right))
	}
}

func TestFileURIIsAbsoluteOnEveryPlatform(t *testing.T) {
	cases := map[string]string{
		`C:\Users\me\AppData\Local\Temp\a.png`: "file:///C:/Users/me/AppData/Local/Temp/a.png",
		`C:\Temp\a b#c.png`:                    "file:///C:/Temp/a%20b%23c.png",
		"/tmp/agentnexus/a b.png":              "file:///tmp/agentnexus/a%20b.png",
	}
	for input, want := range cases {
		if got := fileURI(input); got != want {
			t.Errorf("fileURI(%q) = %q, want %q", input, got, want)
		}
	}
	if strings.HasPrefix(fileURI("C:\\tmp\\a.png"), "file://C:") {
		t.Fatal("windows drive letter was parsed as a URL host")
	}
}

func TestMetadataCacheHonoursFileFingerprint(t *testing.T) {
	var cache metadataCache
	mod := time.Unix(100, 0)
	cache.put("a", mod, 10, protocol.Session{NativeID: "one"})
	if got, ok := cache.get("a", mod, 10); !ok || got.NativeID != "one" {
		t.Fatalf("cache miss for an unchanged file: %+v %v", got, ok)
	}
	if _, ok := cache.get("a", mod.Add(time.Second), 10); ok {
		t.Fatal("a newer mtime did not invalidate the entry")
	}
	if _, ok := cache.get("a", mod, 11); ok {
		t.Fatal("a size change did not invalidate the entry")
	}
	if _, ok := cache.get("b", mod, 10); ok {
		t.Fatal("an unknown path was a cache hit")
	}
}
