package host

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitCommand builds a read-only Git invocation. The repository can configure
// helpers such as core.fsmonitor or diff.external, which Git would otherwise
// execute as the host user; a read-only transcript grant must not become code
// execution. --no-ext-diff/--no-textconv cover diff, and core.fsmonitor=false
// covers status. The environment is scrubbed of interactive and system config.
func gitCommand(ctx context.Context, cwd string, args ...string) *exec.Cmd {
	base := []string{"-c", "core.fsmonitor=false", "-C", cwd}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
	)
	hideProcessWindow(cmd)
	return cmd
}

// cappedBuffer keeps at most limit bytes and remembers that it dropped data.
// It always reports a full write so the child is not killed by a write error.
type cappedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if remaining := c.limit - c.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			_, _ = c.buf.Write(p[:remaining])
			c.overflow = true
		} else {
			_, _ = c.buf.Write(p)
		}
	} else if len(p) > 0 {
		c.overflow = true
	}
	return len(p), nil
}

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
		cmd := gitCommand(cctx, cwd, "rev-parse", "--abbrev-ref", "HEAD", "--show-toplevel")
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
		remote := gitCommand(cctx, cwd, "remote", "get-url", "origin")
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
	// A remote may embed a username or access token. The grouped project key
	// only needs host and path; never send credentials to the hub or store them
	// in its database.
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		value = parsed.String()
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
	// Truncation flags tell the browser that the host bounded a field instead
	// of silently showing an incomplete diff.
	StatusTruncated bool   `json:"status_truncated,omitempty"`
	StatTruncated   bool   `json:"stat_truncated,omitempty"`
	DiffTruncated   bool   `json:"diff_truncated,omitempty"`
	FileCount       int    `json:"file_count"`
	Error           string `json:"error,omitempty"`
}

func (d *Daemon) gitView(ctx context.Context, cwd string) (gitView, error) {
	return d.gitViewOptions(ctx, cwd, false)
}

func (d *Daemon) gitViewOptions(ctx context.Context, cwd string, statOnly bool) (gitView, error) {
	if !d.policy.WithinRoots(cwd) {
		return gitView{}, errors.New("working directory is outside the allowed roots")
	}
	if _, err := os.Stat(filepath.Join(cwd, ".git")); err != nil {
		return gitView{Status: "not a git repository"}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	run := func(limit int, args ...string) (string, bool) {
		if len(args) > 0 && args[0] == "diff" {
			args = append([]string{"diff", "--no-ext-diff", "--no-textconv"}, args[1:]...)
		}
		cmd := gitCommand(ctx, cwd, args...)
		// Bound the allocation while git runs. A multi-hundred-megabyte diff
		// used to be fully buffered before the truncation check.
		buf := &cappedBuffer{limit: limit}
		cmd.Stdout, cmd.Stderr = buf, buf
		err := cmd.Run()
		text := strings.TrimSpace(buf.buf.String())
		if err != nil {
			return truncate(text+"\n"+err.Error(), limit), buf.overflow
		}
		if buf.overflow || len(text) > limit {
			return truncate(text, limit), true
		}
		return text, false
	}
	branch, _ := run(4096, "rev-parse", "--abbrev-ref", "HEAD")
	root, _ := run(4096, "rev-parse", "--show-toplevel")
	status, statusTruncated := run(100_000, "status", "--porcelain=v1")
	stat, statTruncated := run(100_000, "diff", "HEAD", "--stat", "--no-color")
	diff, diffTruncated := "", false
	if !statOnly {
		diff, diffTruncated = run(200_000, "diff", "HEAD", "--no-color")
	}
	if strings.Contains(stat, "fatal: ambiguous argument 'HEAD'") || strings.Contains(diff, "fatal: ambiguous argument 'HEAD'") {
		// A repository without a first commit has no HEAD.
		stat, statTruncated = run(100_000, "diff", "--stat", "--no-color")
		if !statOnly {
			diff, diffTruncated = run(200_000, "diff", "--no-color")
		}
	}
	fileCount := 0
	for _, line := range strings.Split(status, "\n") {
		if strings.TrimSpace(line) != "" {
			fileCount++
		}
	}
	return gitView{
		Branch: branch, Root: root, Status: status, Stat: stat, Diff: diff,
		StatusTruncated: statusTruncated, StatTruncated: statTruncated, DiffTruncated: diffTruncated,
		FileCount: fileCount,
	}, nil
}
