package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
