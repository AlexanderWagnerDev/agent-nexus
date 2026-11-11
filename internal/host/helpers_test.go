package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "binary" {
		t.Fatalf("copy: %q %v", got, err)
	}
	if info, err := os.Stat(dst); err == nil && info.Mode().Perm()&0o200 == 0 {
		t.Fatalf("copied file is not writable: %v", info.Mode())
	}
	if err := copyFile(filepath.Join(dir, "missing"), dst, 0o600); err == nil {
		t.Fatal("missing source was accepted")
	}
}

func TestUpdateInstallGuards(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	staged := filepath.Join(t.TempDir(), "nexusd")
	if err := os.WriteFile(staged, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := InstallStagedUpdate(staged, true); err == nil {
		t.Fatal("update without an installed service was accepted")
	}
	if err := RollbackUpdate(true); err == nil {
		t.Fatal("rollback without a previous binary was accepted")
	}
}

func TestTextHelpers(t *testing.T) {
	if got := truncate("abcdef", 4); !strings.HasPrefix(got, "abcd") || !strings.Contains(got, "truncated") {
		t.Fatalf("truncate: %q", got)
	}
	if got := truncate("abc", 4); got != "abc" {
		t.Fatalf("truncate short: %q", got)
	}
	if got := cleanTitle("  hello\nworld  "); got != "hello world" {
		t.Fatalf("cleanTitle: %q", got)
	}
	if got := usableTitle("  Real title  "); got != "Real title" {
		t.Fatalf("usableTitle: %q", got)
	}
	if got := parseTime("2025-01-02T03:04:05Z"); got.IsZero() || got.Year() != 2025 {
		t.Fatalf("parseTime: %v", got)
	}
	if got := parseTime("not a time"); !got.IsZero() {
		t.Fatalf("parseTime invalid: %v", got)
	}
}
