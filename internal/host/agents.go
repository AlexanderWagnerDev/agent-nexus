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
	{name: "gemini", binary: "gemini", capabilities: []string{"terminal", "discovery"}},
}

// DetectAdapters returns one adapter per installed agent. Unknown agents are
// ignored instead of failing the daemon.
func DetectAdapters(logger *slog.Logger) []Adapter {
	var out []Adapter
	for _, spec := range agentSpecs {
		path, err := exec.LookPath(spec.binary)
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
		case "gemini":
			out = append(out, NewGeminiAdapter(path))
		}
		logger.Info("agent detected", "agent", spec.name, "path", path)
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
