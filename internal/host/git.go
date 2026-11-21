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

// gitEnv removes repository-location and config-injection variables from the
// daemon environment, then adds non-interactive defaults. AgentNexus only ever
// reviews the explicitly selected working tree.
func gitEnv() []string {
	out := make([]string, 0, len(os.Environ())+5)
	for _, entry := range os.Environ() {
		key := entry
		if i := strings.IndexByte(entry, '='); i >= 0 {
			key = entry[:i]
		}
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			continue
		}
		out = append(out, entry)
	}
	return append(out,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
	)
}

// safeFilterName keeps the name unambiguous inside a -c key=value argument.
// Names with '=' or control characters could otherwise rewrite another key.
func safeFilterName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' || r == '/' || r == '+' {
			continue
		}
		return false
	}
	return true
}

type filterConfig struct {
	withCommands  map[string]bool
	required      map[string]bool
	commandOrder  []string
	requiredOrder []string
}

func newFilterConfig() *filterConfig {
	return &filterConfig{withCommands: map[string]bool{}, required: map[string]bool{}}
}

func (c *filterConfig) merge(other *filterConfig) {
	for _, name := range other.commandOrder {
		if !c.withCommands[name] {
			c.withCommands[name] = true
			c.commandOrder = append(c.commandOrder, name)
		}
	}
	for _, name := range other.requiredOrder {
		if !c.required[name] {
			c.required[name] = true
			c.requiredOrder = append(c.requiredOrder, name)
		}
	}
}

func (c *filterConfig) add(key, name string) error {
	// smudge cannot run during status/diff, so an unusual name there must not
	// block the review.
	if key != "clean" && key != "process" && key != "required" {
		return nil
	}
	if name == "" || !safeFilterName(name) {
		return errors.New("git filter configuration uses an unsupported filter name")
	}
	switch key {
	case "clean", "process":
		if !c.withCommands[name] {
			c.withCommands[name] = true
			c.commandOrder = append(c.commandOrder, name)
		}
	case "required":
		if !c.required[name] {
			c.required[name] = true
			c.requiredOrder = append(c.requiredOrder, name)
		}
	}
	return nil
}

func (c *filterConfig) overrides() []string {
	var overrides []string
	for _, name := range c.commandOrder {
		overrides = append(overrides,
			"-c", "filter."+name+".clean=cat",
			"-c", "filter."+name+".process=",
			"-c", "filter."+name+".required=false",
		)
	}
	// A required driver without clean/process would make Git abort the clean
	// conversion. Clearing only the flag keeps the patch readable and does not
	// invent a clean command.
	for _, name := range c.requiredOrder {
		if !c.withCommands[name] {
			overrides = append(overrides, "-c", "filter."+name+".required=false")
		}
	}
	return overrides
}

func submodulePaths(ctx context.Context, cwd string) ([]string, error) {
	path := filepath.Join(cwd, ".gitmodules")
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "-C", cwd, "config", "--file", ".gitmodules", "--get-regexp", `^submodule\..*\.path$`)
	cmd.Dir = cwd
	cmd.Env = gitEnv()
	hideProcessWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New("could not read .gitmodules")
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if i := strings.IndexAny(line, " \t"); i >= 0 {
			if p := strings.TrimSpace(line[i+1:]); p != "" {
				paths = append(paths, p)
			}
		}
	}
	return paths, nil
}

func collectFilterConfig(ctx context.Context, cwd string, depth int, seen map[string]bool) (*filterConfig, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	cfg := newFilterConfig()
	if depth < 0 || seen[abs] {
		return cfg, nil
	}
	seen[abs] = true
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	cmd := exec.CommandContext(cctx, "git", "-C", cwd, "config", "--list", "--name-only", "--show-origin", "--includes")
	cmd.Dir = cwd
	cmd.Env = gitEnv()
	hideProcessWindow(cmd)
	out, err := cmd.Output()
	cancel()
	if err != nil {
		return nil, errors.New("could not read the git filter configuration")
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		// --show-origin prefixes each key with "<origin>\t".
		if i := strings.LastIndexByte(line, '\t'); i >= 0 {
			line = strings.TrimSpace(line[i+1:])
		}
		parts := strings.Split(line, ".")
		if len(parts) < 3 || parts[0] != "filter" {
			continue
		}
		name := strings.Join(parts[1:len(parts)-1], ".")
		if err := cfg.add(parts[len(parts)-1], name); err != nil {
			return nil, err
		}
	}
	if depth == 0 {
		return cfg, nil
	}
	paths, err := submodulePaths(ctx, cwd)
	if err != nil {
		return nil, err
	}
	for _, rel := range paths {
		if filepath.IsAbs(rel) {
			continue
		}
		sub := filepath.Join(cwd, filepath.FromSlash(rel))
		if within, err := filepath.Rel(cwd, sub); err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
			continue
		}
		if _, err := os.Stat(filepath.Join(sub, ".git")); err != nil {
			continue
		}
		child, err := collectFilterConfig(ctx, sub, depth-1, seen)
		if err != nil {
			return nil, err
		}
		cfg.merge(child)
	}
	return cfg, nil
}

// gitFilterOverrides disables repository-configured clean/process filters.
// A diff converts working-tree content through the clean filter, so a hostile
// .gitattributes/.git/config pair can execute a command even with external
// diff and textconv disabled. Reading the config does not run any filter.
// Initialized submodules are included because parent status/diff can recurse
// into them.
func gitFilterOverrides(ctx context.Context, cwd string) ([]string, error) {
	cfg, err := collectFilterConfig(ctx, cwd, 4, map[string]bool{})
	if err != nil {
		return nil, err
	}
	return cfg.overrides(), nil
}

// gitCommand builds a read-only Git invocation. The repository can configure
// helpers such as core.fsmonitor, diff.external or clean filters, which Git
// would otherwise execute as the host user; a read-only transcript grant must
// not become code execution. --no-ext-diff/--no-textconv cover external diff
// and textconv, core.fsmonitor=false covers status, and filters neutralise
// clean/process commands. Callers that run several commands pass filters once
// instead of probing the config for every invocation.
func gitCommand(ctx context.Context, cwd string, filters []string, args ...string) *exec.Cmd {
	// diff.submodule=short keeps submodule output to a gitlink line. Filter
	// overrides include initialized submodules and reach their child Git.
	base := []string{"-c", "core.fsmonitor=false", "-c", "diff.submodule=short"}
	base = append(base, filters...)
	base = append(base, "-C", cwd)
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Dir = cwd
	cmd.Env = gitEnv()
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
		// rev-parse does not read worktree content, so it needs no filters.
		cmd := gitCommand(cctx, cwd, nil, "rev-parse", "--abbrev-ref", "HEAD", "--show-toplevel")
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
		remote := gitCommand(cctx, cwd, nil, "remote", "get-url", "origin")
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
	var filterErr error
	run := func(filtered bool, limit int, args ...string) (string, bool) {
		if filterErr != nil {
			return "", false
		}
		if len(args) > 0 && args[0] == "diff" {
			args = append([]string{"diff", "--no-ext-diff", "--no-textconv", "--ignore-submodules=dirty"}, args[1:]...)
		}
		var filters []string
		if filtered {
			// Refresh immediately before each command that can run a filter:
			// a config change during the review must not outlive discovery.
			var err error
			filters, err = gitFilterOverrides(ctx, cwd)
			if err != nil {
				filterErr = err
				return "", false
			}
		}
		cmd := gitCommand(ctx, cwd, filters, args...)
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
	branch, _ := run(false, 4096, "rev-parse", "--abbrev-ref", "HEAD")
	root, _ := run(false, 4096, "rev-parse", "--show-toplevel")
	// Dirty submodule worktrees are ignored rather than scanned; commit-pointer
	// changes stay visible. Submodule filters are included in the overrides.
	status, statusTruncated := run(true, 100_000, "status", "--porcelain=v1", "--ignore-submodules=dirty")
	stat, statTruncated := run(true, 100_000, "diff", "HEAD", "--stat", "--no-color")
	diff, diffTruncated := "", false
	if !statOnly {
		diff, diffTruncated = run(true, 200_000, "diff", "HEAD", "--no-color")
	}
	if strings.Contains(stat, "fatal: ambiguous argument 'HEAD'") || strings.Contains(diff, "fatal: ambiguous argument 'HEAD'") {
		// A repository without a first commit has no HEAD.
		stat, statTruncated = run(true, 100_000, "diff", "--stat", "--no-color")
		if !statOnly {
			diff, diffTruncated = run(true, 200_000, "diff", "--no-color")
		}
	}
	if filterErr != nil {
		return gitView{}, filterErr
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
