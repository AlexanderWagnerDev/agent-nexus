package host

import (
	"log/slog"
	"os/exec"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

type agentSpec struct {
	name         string
	binary       string
	capabilities []string
}

var agentSpecs = []agentSpec{
	{name: "claude", binary: "claude", capabilities: []string{"terminal", "discovery", "transcript"}},
	{name: "codex", binary: "codex", capabilities: []string{"terminal", "discovery", "transcript"}},
	{name: "opencode", binary: "opencode", capabilities: []string{"terminal", "discovery", "transcript"}},
	{name: "commandcode", binary: "command-code", capabilities: []string{"terminal", "discovery", "transcript"}},
	{name: "gemini", binary: "gemini", capabilities: []string{"terminal", "discovery"}},
	{name: "cursor", binary: "cursor-agent", capabilities: []string{"terminal", "discovery", "transcript"}},
	{name: "devin", binary: "devin", capabilities: []string{"terminal", "discovery", "transcript"}},
}

// DetectAdapters returns one adapter per installed agent. Unknown agents are
// ignored instead of failing the daemon.
func DetectAdapters(logger *slog.Logger) []Adapter {
	var out []Adapter
	detected := map[string]bool{}
	for _, spec := range agentSpecs {
		path, err := exec.LookPath(spec.binary)
		if err != nil && spec.name == "commandcode" {
			for _, alias := range []string{"cmdc", "commandcode"} {
				if found, findErr := exec.LookPath(alias); findErr == nil {
					path, err = found, nil
					break
				}
			}
		}
		if err != nil {
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

func detectInfo(name, path string) (protocol.AgentInfo, error) {
	return protocol.AgentInfo{
		Name:         name,
		Version:      versionOf(path, 5*time.Second),
		Path:         path,
		Supported:    true,
		Capabilities: []string{"terminal", "discovery", "transcript"},
	}, nil
}
