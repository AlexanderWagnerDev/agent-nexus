package host

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// DoctorCheck is a sanitized diagnostic result suitable for the hub UI. It
// intentionally omits host ids, paths, keys and configuration values.
type DoctorCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"` // ok, warning, error
	Detail string `json:"detail"`
}

// DoctorReport is the safe subset of `nexusd doctor` exposed to the hub.
type DoctorReport struct {
	Version string        `json:"version"`
	Issues  int           `json:"issues"`
	Checks  []DoctorCheck `json:"checks"`
}

func (d *Daemon) safeDoctor() DoctorReport {
	report := DoctorReport{Version: Version}
	add := func(id, status, detail string) {
		report.Checks = append(report.Checks, DoctorCheck{ID: id, Status: status, Detail: detail})
		if status == "error" {
			report.Issues++
		}
	}
	if runtime.GOOS != "windows" {
		if path, err := ConfigPath(); err == nil {
			if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o077 != 0 {
				add("config_permissions", "warning", "The configuration file is readable by other users.")
			}
		}
	}
	if strings.TrimSpace(d.cfg.HostID) == "" {
		add("pairing", "error", "The daemon is not paired with a hub.")
	} else {
		add("pairing", "ok", "A host identity is configured.")
	}
	if _, err := d.cfg.SigningKey(); err != nil {
		add("identity", "error", "The signing identity is missing or invalid.")
	} else {
		add("identity", "ok", "The host signing identity is readable.")
	}
	if d.cfg.HubPublicKey == "" {
		if d.cfg.HubSigningRequired {
			add("hub_signing", "error", "Hub signing is required but no hub key is pinned.")
		} else {
			add("hub_signing", "ok", "No hub key is pinned yet; it is pinned on the next connection to a signing hub.")
		}
	} else {
		add("hub_signing", "ok", "A hub signing key is pinned.")
	}
	if strings.TrimSpace(d.cfg.HubURL) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(d.cfg.HubURL, "/")+"/healthz", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			add("hub_health", "warning", "The hub health endpoint is currently unreachable.")
		} else {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				add("hub_health", "ok", "The hub health endpoint answered with HTTP 200.")
			} else {
				add("hub_health", "warning", "The hub health endpoint answered with a non-200 status.")
			}
		}
	}
	missing := 0
	for _, root := range d.policy.AllowedRoots {
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			missing++
		}
	}
	if missing > 0 {
		add("policy_roots", "error", "One or more allowed roots do not exist or are not directories.")
	} else {
		add("policy_roots", "ok", "All configured allowed roots exist.")
	}
	if len(d.policy.AllowedAgents) == 1 && d.policy.AllowedAgents[0] == "" {
		add("policy_agents", "error", "The allowed-agent policy contains an empty entry.")
	} else {
		add("policy_agents", "ok", "The allowed-agent policy is readable.")
	}
	agents := d.inventoryAgents()
	if len(agents) == 0 {
		add("agents", "error", "No coding agent was detected.")
	} else {
		var names []string
		unsupported := 0
		for _, agent := range agents {
			names = append(names, agent.Name+" "+agent.Version)
			if !agent.Supported {
				unsupported++
			}
		}
		if unsupported > 0 {
			add("agents", "warning", "Detected agents: "+strings.Join(names, ", ")+". One or more stores are unavailable.")
		} else {
			add("agents", "ok", "Detected agents: "+strings.Join(names, ", ")+".")
		}
	}
	if index := d.searchIndexStatus(); index != nil {
		switch index.State {
		case "error":
			add("search_index", "warning", "The host-local transcript index reported an error.")
		case "building":
			add("search_index", "ok", "The host-local transcript index is building.")
		case "disabled":
			add("search_index", "ok", "The host-local transcript index is disabled.")
		default:
			add("search_index", "ok", "The host-local transcript index is ready.")
		}
	}
	return report
}
