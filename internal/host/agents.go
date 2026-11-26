package host

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

type agentSpec struct {
	name    string
	binary  string
	aliases []string
}

var agentSpecs = []agentSpec{
	{name: "claude", binary: "claude"},
	{name: "codex", binary: "codex"},
	{name: "opencode", binary: "opencode"},
	{name: "commandcode", binary: "command-code", aliases: []string{"cmdc", "commandcode"}},
	{name: "gemini", binary: "gemini"},
	{name: "cursor", binary: "cursor-agent", aliases: []string{"agent"}},
	{name: "devin", binary: "devin"},
}

// DetectAdapters returns one adapter per installed agent. Unknown agents are
// ignored instead of failing the daemon.
func DetectAdapters(logger *slog.Logger) []Adapter {
	var out []Adapter
	detected := map[string]bool{}
	dirs := agentSearchDirs()
	for _, spec := range agentSpecs {
		path, ok := findAgentBinary(spec, dirs)
		if !ok {
			continue
		}
		switch spec.name {
		case "claude":
			out = append(out, NewClaudeAdapter(path))
		case "codex":
			out = append(out, NewCodexAdapter(path))
		case "opencode":
			out = append(out, NewOpenCodeAdapter(path))
		case "commandcode":
			out = append(out, NewCommandCodeAdapter(path))
		case "gemini":
			out = append(out, NewGeminiAdapter(path))
		case "cursor":
			out = append(out, NewCursorAdapter(path))
		case "devin":
			out = append(out, NewDevinAdapter(path))
		}
		detected[spec.name] = true
		logger.Info("agent detected", "agent", spec.name, "path", path)
	}
	// Cursor and Devin Desktop keep their transcript/session stores even when
	// the CLI is not on PATH. Discovery and transcripts still work; the agents
	// stay out of the new-session picker until their CLI is installed.
	if !detected["cursor"] {
		if adapter := NewCursorAdapter(""); adapter.storeExists() {
			out = append(out, adapter)
			logger.Info("agent store detected", "agent", "cursor", "path", adapter.root)
		}
	}
	if !detected["devin"] {
		if adapter := NewDevinAdapter(""); adapter.storeExists() {
			out = append(out, adapter)
			logger.Info("agent store detected", "agent", "devin", "path", adapter.db)
		}
	}
	return out
}

// findAgentBinary resolves a CLI on PATH first and then in the directories
// that installers add to a login shell or to the registry after this process
// started. A logon task keeps its environment from start time, so LookPath
// alone misses agents that were installed later.
func findAgentBinary(spec agentSpec, dirs []string) (string, bool) {
	names := append([]string{spec.binary}, spec.aliases...)
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, true
		}
	}
	for _, name := range names {
		for _, ext := range executableExtensions() {
			for _, dir := range dirs {
				path := filepath.Join(dir, name+ext)
				if info, err := os.Stat(path); err == nil && !info.IsDir() {
					return path, true
				}
			}
		}
	}
	return "", false
}

// agentSearchDirs adds the per-user directories that agent and package-manager
// installers use but that a service environment often lacks.
func agentSearchDirs() []string {
	var dirs []string
	add := func(dir string) {
		dir = strings.Trim(strings.TrimSpace(dir), `"`)
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		add(dir)
	}
	for _, dir := range userPathDirs() {
		add(dir)
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "bin"),
		filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".opencode", "bin"),
		filepath.Join(home, ".bun", "bin"),
		filepath.Join(home, ".volta", "bin"),
		"/usr/local/bin",
		"/opt/homebrew/bin",
	} {
		add(dir)
	}
	if runtime.GOOS == "windows" {
		for _, dir := range []string{
			filepath.Join(os.Getenv("APPDATA"), "npm"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "npm"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links"),
			filepath.Join(home, "scoop", "shims"),
			filepath.Join(home, ".local", "bin"),
		} {
			add(dir)
		}
	}
	return dirs
}

func executableExtensions() []string {
	if runtime.GOOS != "windows" {
		return []string{""}
	}
	return []string{".exe", ".cmd", ".bat", ".com", ""}
}

func detectInfo(name, path string) (protocol.AgentInfo, error) {
	return protocol.AgentInfo{
		Name:         name,
		Version:      versionOf(path, 5*time.Second),
		Path:         path,
		Supported:    true,
		Capabilities: []string{"terminal", "discovery", "transcript"},
	}, nil
}
