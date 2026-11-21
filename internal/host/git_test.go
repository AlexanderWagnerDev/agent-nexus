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
		"https://user:token@github.com/o/r.git": "https://github.com/o/r",
		"  https://example.com/team/repo.git  ": "https://example.com/team/repo",
		"":                                      "",
	}
	for input, want := range cases {
		if got := normalizeRepoURL(input); got != want {
			t.Errorf("normalizeRepoURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestGitViewDoesNotRunRepositoryExternalDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is not available for the helper script")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "helper-ran")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=audit@example.invalid", "-c", "user.name=Audit"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "a.txt")
	git("commit", "-qm", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "echo ran > " + strings.ReplaceAll(marker, `\`, "/")
	git("config", "diff.external", "sh -c '"+script+"'")

	d := &Daemon{policy: Policy{AllowedRoots: []string{dir}}}
	if _, err := d.gitView(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("repository-configured diff.external ran during read-only Git review")
	}
}

func TestGitViewDoesNotRunRepositoryCleanFilter(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is not available for the helper script")
	}
	dir := t.TempDir()
	marker := filepath.Clean(filepath.Join(dir, "clean-filter-ran"))
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=audit@example.invalid", "-c", "user.name=Audit"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.txt filter=evil\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "a.txt", ".gitattributes")
	git("commit", "-qm", "initial")
	script := "echo ran > " + strings.ReplaceAll(marker, `\`, "/") + "; cat"
	git("config", "filter.evil.clean", "sh -c '"+script+"'")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	d := &Daemon{policy: Policy{AllowedRoots: []string{dir}}}
	view, err := d.gitView(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("repository-configured clean filter ran during read-only Git review")
	}
	if !strings.Contains(view.Diff, "+two") {
		t.Fatalf("ordinary diff output changed:\n%s", view.Diff)
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
	if err := os.WriteFile(filepath.Join(dir, "gone.txt"), []byte("gone\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "file.txt", "gone.txt")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "add files")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "file.txt")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o600); err != nil {
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
	if !strings.Contains(view.Status, "file.txt") || !strings.Contains(view.Status, "gone.txt") || !strings.Contains(view.Status, "new.txt") {
		t.Fatalf("status misses changed, deleted or new files: %+v", view)
	}
	if !strings.Contains(view.Diff, "+three") || !strings.Contains(view.Diff, "-gone") {
		t.Fatalf("diff misses staged/unstaged or deleted content: %+v", view)
	}
	if view.FileCount != 3 {
		t.Fatalf("file count: %d (%+v)", view.FileCount, view)
	}
	if view.Root == "" {
		t.Fatal("root missing")
	}
	statOnly, err := d.gitViewOptions(context.Background(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if statOnly.Diff != "" || statOnly.FileCount != 3 || !strings.Contains(statOnly.Status, "file.txt") {
		t.Fatalf("stat-only view: %+v", statOnly)
	}
	if _, err := d.gitView(context.Background(), t.TempDir()); err == nil {
		t.Fatal("git view outside allowed roots was accepted")
	}
	if view, err := d.gitView(context.Background(), t.TempDir()); err == nil {
		t.Fatalf("unexpected view: %+v", view)
	}
}
