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

type gitView struct {
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
		Status: run("status", "--porcelain=v1"),
		Stat:   run("diff", "--stat", "--no-color"),
		Diff:   truncate(run("diff", "--no-color"), 200_000),
	}, nil
}
