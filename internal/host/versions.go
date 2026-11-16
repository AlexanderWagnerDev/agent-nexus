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
	// Note explains a supported band that intentionally has no structured
	// driver, for example OpenCode 1.x.
	Note string
}

var agentVersionMatrix = []AgentVersionRange{
	{Agent: "claude", Min: "2.0.0", Max: "3.0.0", Structured: true},
	{Agent: "codex", Min: "0.50.0", Max: "1.0.0", Structured: true},
	// OpenCode 1.x sessions are discovered, read and resumed in terminal mode.
	// The structured HTTP driver was verified against 2.2+ and is disabled for
	// the older server API unless the operator explicitly allows it.
	{Agent: "opencode", Min: "1.0.0", Max: "2.2.0", Structured: false,
		Note: "OpenCode 1.x supports terminal, discovery and transcript; structured mode needs 2.2 or newer."},
	{Agent: "opencode", Min: "2.2.0", Max: "3.0.0", Structured: true},
	{Agent: "commandcode", Min: "1.0.0", Max: "2.0.0", Structured: true},
	{Agent: "gemini", Min: "0.1.0", Max: "1.0.0", Structured: true},
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

// CheckAgentVersion reports whether a detected version is inside any tested
// band. recognized is false when the matrix has no entry or the version could
// not be parsed.
func CheckAgentVersion(agent, version string) (tested, recognized bool) {
	hasBand := false
	got, ok := parseAgentVersion(version)
	for _, band := range agentVersionMatrix {
		if band.Agent != agent {
			continue
		}
		hasBand = true
		if !ok {
			continue
		}
		low, _ := parseAgentVersion(band.Min)
		high, _ := parseAgentVersion(band.Max)
		if compareVersion(got, low) >= 0 && compareVersion(got, high) < 0 {
			return true, true
		}
	}
	if !hasBand || !ok {
		return true, false
	}
	return false, true
}

// agentVersionBand returns the tested band that contains a version.
func agentVersionBand(agent, version string) (AgentVersionRange, bool) {
	got, ok := parseAgentVersion(version)
	if !ok {
		return AgentVersionRange{}, false
	}
	for _, band := range agentVersionMatrix {
		if band.Agent != agent {
			continue
		}
		low, _ := parseAgentVersion(band.Min)
		high, _ := parseAgentVersion(band.Max)
		if compareVersion(got, low) >= 0 && compareVersion(got, high) < 0 {
			return band, true
		}
	}
	return AgentVersionRange{}, false
}

// agentSupportedRange renders the union of all bands for an agent.
func agentSupportedRange(agent string) (string, bool) {
	min, max := "", ""
	for _, band := range agentVersionMatrix {
		if band.Agent != agent {
			continue
		}
		if min == "" {
			min = band.Min
		} else if got, _ := parseAgentVersion(band.Min); got != nil {
			low, _ := parseAgentVersion(min)
			if compareVersion(got, low) < 0 {
				min = band.Min
			}
		}
		if max == "" {
			max = band.Max
		} else if got, _ := parseAgentVersion(band.Max); got != nil {
			high, _ := parseAgentVersion(max)
			if compareVersion(got, high) > 0 {
				max = band.Max
			}
		}
	}
	if min == "" {
		return "", false
	}
	return fmt.Sprintf(">= %s < %s", min, max), true
}

// applyVersionPolicy annotates inventory. A band with Structured=false keeps
// terminal/discovery/transcript and removes only structured mode.
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
	if band, matched := agentVersionBand(info.Name, info.Version); matched && !band.Structured {
		info.VersionWarning = band.Note
		if !d.cfg.AllowUntestedAgentVersions {
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
		return
	}
	if tested {
		return
	}
	rangeText, _ := agentSupportedRange(info.Name)
	info.VersionWarning = fmt.Sprintf("Version %s is outside the tested range %s.", info.Version, rangeText)
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

// rememberAgentVersion caches the version seen during inventory so a run start
// does not spawn another `--version` process.
func (d *Daemon) rememberAgentVersion(agent, version string) {
	if agent == "" || version == "" {
		return
	}
	d.versionMu.Lock()
	if d.versions == nil {
		d.versions = map[string]string{}
	}
	d.versions[agent] = version
	d.versionMu.Unlock()
}

func (d *Daemon) cachedAgentVersion(agent string) (string, bool) {
	d.versionMu.Lock()
	defer d.versionMu.Unlock()
	version, ok := d.versions[agent]
	return version, ok
}

// checkStructuredVersion refuses structured mode for a detected version that
// is outside the tested band, unless the operator allowed untested versions.
// The inventory hides the capability, but the hub (or a stale UI) could still
// send run.start; this keeps the contract enforced on the host.
func (d *Daemon) checkStructuredVersion(ctx context.Context, agent string) error {
	if d.cfg.AllowUntestedAgentVersions {
		return nil
	}
	version, ok := d.cachedAgentVersion(agent)
	if !ok {
		a := d.adapter(agent)
		if a == nil {
			// A configured ACP command without a local adapter is not covered
			// by the version matrix; the operator opted in explicitly.
			return nil
		}
		info, err := a.Detect(ctx)
		if err != nil {
			return nil
		}
		version = info.Version
		d.rememberAgentVersion(agent, version)
	}
	if band, matched := agentVersionBand(agent, version); matched && !band.Structured {
		return fmt.Errorf("agent version %s supports terminal mode but not structured mode: %s", version, band.Note)
	}
	tested, recognized := CheckAgentVersion(agent, version)
	if !recognized || tested {
		return nil
	}
	rangeText, _ := agentSupportedRange(agent)
	return fmt.Errorf("agent version %s is outside the tested structured range %s; terminal mode still works", version, rangeText)
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
