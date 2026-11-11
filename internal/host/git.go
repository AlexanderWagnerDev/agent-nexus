package host

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type gitRepoCache struct {
	branch string
	root   string
	url    string
	at     time.Time
}

// gitRepo returns the branch, repository root and origin URL for a directory.
// Results are cached for ten minutes so a discovery snapshot does not spawn
// one git process per session. Worktrees report their own root and branch.
func (d *Daemon) gitRepo(ctx context.Context, cwd string) gitRepoCache {
	if !d.policy.WithinRoots(cwd) {
		return gitRepoCache{}
	}
	d.gitMu.Lock()
	cached, ok := d.gitBranches[cwd]
	d.gitMu.Unlock()
	if ok && time.Since(cached.at) < 10*time.Minute {
		return cached
	}
	var repo gitRepoCache
	if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		cmd := exec.CommandContext(cctx, "git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD", "--show-toplevel")
		cmd.Dir = cwd
		hideProcessWindow(cmd)
		out, err := cmd.CombinedOutput()
		cancel()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > 0 {
				repo.branch = strings.TrimSpace(lines[0])
			}
			if len(lines) > 1 {
				repo.root = filepath.ToSlash(strings.TrimSpace(lines[len(lines)-1]))
			}
		}
		cctx, cancel = context.WithTimeout(ctx, 3*time.Second)
		remote := exec.CommandContext(cctx, "git", "-C", cwd, "remote", "get-url", "origin")
		remote.Dir = cwd
		hideProcessWindow(remote)
		out, err = remote.CombinedOutput()
		cancel()
		if err == nil {
			repo.url = normalizeRepoURL(string(out))
		}
	}
	repo.at = time.Now()
	d.gitMu.Lock()
	d.gitBranches[cwd] = repo
	d.gitMu.Unlock()
	return repo
}

// gitBranch keeps the branch-only helper used by adapters.
func (d *Daemon) gitBranch(ctx context.Context, cwd string) string {
	return d.gitRepo(ctx, cwd).branch
}

func normalizeRepoURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "git@") {
		if i := strings.Index(value, ":"); i > 4 {
			value = "https://" + value[4:i] + "/" + value[i+1:]
		}
	} else if strings.HasPrefix(value, "ssh://") {
		value = strings.TrimPrefix(value, "ssh://")
		if i := strings.Index(value, "@"); i >= 0 {
			value = value[i+1:]
		}
		value = "https://" + value
	}
	value = strings.TrimSuffix(strings.TrimRight(value, "/"), ".git")
	return value
}

type gitView struct {
	Branch string `json:"branch,omitempty"`
	Root   string `json:"root,omitempty"`
	Status string `json:"status"`
	Stat   string `json:"stat"`
	Diff   string `json:"diff"`
	Error  string `json:"error,omitempty"`
}

func (d *Daemon) gitView(ctx context.Context, cwd string) (gitView, error) {
	if !d.policy.WithinRoots(cwd) {
		return gitView{}, errors.New("working directory is outside the allowed roots")
	}
	if _, err := os.Stat(filepath.Join(cwd, ".git")); err != nil {
		return gitView{Status: "not a git repository"}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	run := func(args ...string) string {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
		cmd.Dir = cwd
		hideProcessWindow(cmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return strings.TrimSpace(string(out)) + "\n" + err.Error()
		}
		return strings.TrimSpace(string(out))
	}
	return gitView{
		Branch: run("rev-parse", "--abbrev-ref", "HEAD"),
		Root:   run("rev-parse", "--show-toplevel"),
		Status: run("status", "--porcelain=v1"),
		Stat:   run("diff", "--stat", "--no-color"),
		Diff:   truncate(run("diff", "--no-color"), 200_000),
	}, nil
}
