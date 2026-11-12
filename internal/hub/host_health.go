package hub

import (
	"fmt"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// buildHostHealth derives an explainable health state from inventory that the
// host actually reported. It deliberately does not probe a host: callers pass
// the current connection state.
func buildHostHealth(h protocol.HostView, online bool) protocol.HostHealth {
	out := protocol.HostHealth{State: "healthy", CheckedAt: time.Now().UTC()}
	add := func(code, severity, message string) {
		out.Reasons = append(out.Reasons, protocol.HostHealthReason{Code: code, Severity: severity, Message: message})
		if severity == "error" || (severity == "warning" && out.State == "healthy") {
			out.State = "degraded"
		}
	}
	if h.Disabled {
		out.State = "disabled"
		add("host_disabled", "warning", "The host is disabled and accepts no new runs.")
		return out
	}
	if h.Pending {
		out.State = "pending"
		add("host_unconfirmed", "warning", "The pairing SAS has not been confirmed.")
		return out
	}
	if !online {
		out.State = "offline"
		if !h.LastSeen.IsZero() {
			add("host_offline", "warning", fmt.Sprintf("No active host link; last contact %s.", h.LastSeen.UTC().Format(time.RFC3339)))
		} else {
			add("host_offline", "warning", "No active host link.")
		}
		return out
	}
	if len(h.Agents) == 0 {
		add("no_agents", "warning", "No coding agent was detected on this host.")
	}
	for _, agent := range h.Agents {
		if !agent.Supported {
			add("agent_unsupported", "error", fmt.Sprintf("%s is present but not supported by this host build.", agent.Name))
			continue
		}
		if agent.Untested {
			add("agent_untested_version", "warning", fmt.Sprintf("%s %s is outside the tested version range.", agent.Name, agent.Version))
		} else if agent.VersionWarning != "" {
			add("agent_version_warning", "warning", fmt.Sprintf("%s: %s", agent.Name, agent.VersionWarning))
		}
	}
	if h.Index != nil {
		switch h.Index.State {
		case "error":
			add("index_error", "warning", "The host-local transcript index reported an error.")
		case "building":
			add("index_building", "info", "The host-local transcript index is still building.")
		}
	}
	if h.Update != nil && h.Update.Available {
		add("update_available", "info", fmt.Sprintf("nexusd %s is available on the update channel.", h.Update.Latest))
	}
	if h.Policy == nil {
		add("policy_missing", "warning", "The host did not report a policy summary.")
	} else if len(h.Policy.AllowedRoots) == 0 {
		add("policy_no_roots", "warning", "The host policy allows no project roots, so no session can be resumed.")
	}
	return out
}
