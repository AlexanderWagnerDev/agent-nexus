package host

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeRepoURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:owner/repo.git":         "https://github.com/owner/repo",
		"ssh://git@github.com/owner/repo.git":   "https://github.com/owner/repo",
		"https://github.com/owner/repo.git":     "https://github.com/owner/repo",
		"https://gitlab.com/group/sub/repo/":    "https://gitlab.com/group/sub/repo",
		"  https://example.com/team/repo.git  ": "https://example.com/team/repo",
		"":                                      "",
	}
	for input, want := range cases {
		if got := normalizeRepoURL(input); got != want {
			t.Errorf("normalizeRepoURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestGitViewInsideAllowedRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init")
	git("symbolic-ref", "HEAD", "refs/heads/main")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "file.txt")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "add file")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	d := &Daemon{policy: Policy{AllowedRoots: []string{dir}}}
	view, err := d.gitView(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if view.Branch != "main" {
		t.Fatalf("branch: %q", view.Branch)
	}
	if !strings.Contains(view.Status, "file.txt") || !strings.Contains(view.Diff, "+two") {
		t.Fatalf("status/diff: %+v", view)
	}
	if view.Root == "" {
		t.Fatal("root missing")
	}
	if _, err := d.gitView(context.Background(), t.TempDir()); err == nil {
		t.Fatal("git view outside allowed roots was accepted")
	}
	if view, err := d.gitView(context.Background(), t.TempDir()); err == nil {
		t.Fatalf("unexpected view: %+v", view)
	}
}
