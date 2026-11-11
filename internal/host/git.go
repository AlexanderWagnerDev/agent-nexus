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

type gitBranchCache struct {
	branch string
	at     time.Time
}

// gitBranch returns the current branch for a repository directory. It trusts
// the host policy and caches results for ten minutes so a discovery snapshot
// does not spawn one git process per session.
func (d *Daemon) gitBranch(ctx context.Context, cwd string) string {
	if !d.policy.WithinRoots(cwd) {
		return ""
	}
	d.gitMu.Lock()
	cached, ok := d.gitBranches[cwd]
	d.gitMu.Unlock()
	if ok && time.Since(cached.at) < 10*time.Minute {
		return cached.branch
	}
	branch := ""
	if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		cmd := exec.CommandContext(cctx, "git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD")
		cmd.Dir = cwd
		hideProcessWindow(cmd)
		out, err := cmd.CombinedOutput()
		cancel()
		if err == nil {
			branch = strings.TrimSpace(string(out))
		}
	}
	d.gitMu.Lock()
	d.gitBranches[cwd] = gitBranchCache{branch: branch, at: time.Now()}
	d.gitMu.Unlock()
	return branch
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
