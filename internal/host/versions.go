package host

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// AgentVersionRange is the tested version band for one agent. Structured mode
// is disabled outside the band unless the host explicitly allows untested
// versions; terminal mode remains available.
type AgentVersionRange struct {
	Agent      string
	Min        string
	Max        string
	Structured bool
}

var agentVersionMatrix = []AgentVersionRange{
	{Agent: "claude", Min: "2.0.0", Max: "3.0.0", Structured: true},
	{Agent: "codex", Min: "0.50.0", Max: "1.0.0", Structured: true},
	{Agent: "opencode", Min: "1.0.0", Max: "2.0.0", Structured: true},
	{Agent: "gemini", Min: "0.1.0", Max: "1.0.0", Structured: false},
}

// AgentMatrix returns a copy of the tested version matrix.
func AgentMatrix() []AgentVersionRange {
	out := make([]AgentVersionRange, len(agentVersionMatrix))
	copy(out, agentVersionMatrix)
	return out
}

// TestedRange renders the band for display and documentation.
func (r AgentVersionRange) TestedRange() string {
	return fmt.Sprintf(">= %s < %s", r.Min, r.Max)
}

var versionPattern = regexp.MustCompile(`\d+(?:\.\d+){0,2}`)

func parseAgentVersion(value string) ([]int, bool) {
	match := versionPattern.FindString(value)
	if match == "" {
		return nil, false
	}
	parts := strings.Split(match, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	for len(out) < 3 {
		out = append(out, 0)
	}
	return out, true
}

func compareVersion(a, b []int) int {
	for i := 0; i < 3; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// CheckAgentVersion reports whether a detected version is inside the tested
// band. recognized is false when the matrix has no entry or the version could
// not be parsed.
func CheckAgentVersion(agent, version string) (tested, recognized bool) {
	var band *AgentVersionRange
	for i := range agentVersionMatrix {
		if agentVersionMatrix[i].Agent == agent {
			band = &agentVersionMatrix[i]
			break
		}
	}
	if band == nil {
		return true, false
	}
	got, ok := parseAgentVersion(version)
	if !ok {
		return true, false
	}
	low, _ := parseAgentVersion(band.Min)
	high, _ := parseAgentVersion(band.Max)
	return compareVersion(got, low) >= 0 && compareVersion(got, high) < 0, true
}

// applyVersionPolicy annotates inventory and removes structured mode when the
// version is outside the tested band and the override is off.
func (d *Daemon) applyVersionPolicy(info *protocol.AgentInfo) {
	if info == nil {
		return
	}
	tested, recognized := CheckAgentVersion(info.Name, info.Version)
	info.Untested = recognized && !tested
	if !recognized {
		if strings.TrimSpace(info.Version) != "" && info.Version != "unknown" {
			info.VersionWarning = "The detected version could not be compared with the tested range."
		}
		return
	}
	if tested {
		return
	}
	band, _ := agentVersionRange(info.Name)
	info.VersionWarning = fmt.Sprintf("Version %s is outside the tested range %s.", info.Version, band.TestedRange())
	if !d.cfg.AllowUntestedAgentVersions {
		info.VersionWarning += " Structured mode is disabled; terminal mode still works."
		filtered := info.Capabilities[:0]
		for _, capability := range info.Capabilities {
			if capability != "structured" {
				filtered = append(filtered, capability)
			}
		}
		info.Capabilities = filtered
	} else {
		info.VersionWarning += " Structured mode is allowed by allow_untested_agent_versions."
	}
}

func agentVersionRange(agent string) (AgentVersionRange, bool) {
	for _, band := range agentVersionMatrix {
		if band.Agent == agent {
			return band, true
		}
	}
	return AgentVersionRange{}, false
}

// auditVersionOverrides records once per daemon start when the override is on
// and a detected agent is outside its tested range.
func (d *Daemon) auditVersionOverrides() {
	if !d.cfg.AllowUntestedAgentVersions {
		return
	}
	for _, adapter := range d.adapters {
		info, err := adapter.Detect(context.Background())
		if err != nil {
			continue
		}
		if tested, recognized := CheckAgentVersion(info.Name, info.Version); recognized && !tested {
			hostAudit("agent.version_override", map[string]any{
				"agent": info.Name, "version": info.Version,
			})
		}
	}
}
