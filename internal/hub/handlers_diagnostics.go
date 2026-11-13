package hub

import (
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

type diagnosticsAgent struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Supported    bool     `json:"supported"`
	Capabilities []string `json:"capabilities,omitempty"`
	Warning      string   `json:"warning,omitempty"`
}

type diagnosticsHost struct {
	Name          string                 `json:"name,omitempty"`
	Status        string                 `json:"status"`
	Health        string                 `json:"health"`
	HealthReasons []string               `json:"health_reasons,omitempty"`
	OS            string                 `json:"os"`
	Arch          string                 `json:"arch"`
	Daemon        string                 `json:"daemon"`
	Agents        []diagnosticsAgent     `json:"agents,omitempty"`
	Update        *protocol.UpdateStatus `json:"update,omitempty"`
	Index         *protocol.IndexStatus  `json:"index,omitempty"`
}

type diagnosticsError struct {
	At      time.Time `json:"at"`
	Action  string    `json:"action"`
	Actor   string    `json:"actor,omitempty"`
	Details string    `json:"details,omitempty"`
}

func (s *Server) diagnosticsPayload(u User, includeIdentity bool) map[string]any {
	counts, countErr := s.store.DiagnosticsCounts()
	dbOK := s.store.Ping() == nil
	online := 0
	s.mu.RLock()
	online = len(s.hosts)
	s.mu.RUnlock()

	hosts, _ := s.store.ListHosts(u.ID, true)
	// Update availability lives in memory until a host reports it; merge it
	// here so diagnostics reflects the same state as the host list.
	s.updateMu.Lock()
	for i := range hosts {
		if status, ok := s.hostUpdates[hosts[i].ID]; ok {
			copyStatus := status
			hosts[i].Update = &copyStatus
		}
	}
	s.updateMu.Unlock()
	var hostRows []diagnosticsHost
	var updateRows []map[string]any
	for _, h := range hosts {
		status := "offline"
		if h.Disabled {
			status = "disabled"
		} else if s.hostOnline(h.ID) {
			status = "online"
		}
		health := buildHostHealth(h, s.hostOnline(h.ID))
		row := diagnosticsHost{
			Status: status, Health: health.State, OS: h.OS, Arch: h.Arch, Daemon: h.Version,
			Update: h.Update, Index: h.Index,
		}
		for _, reason := range health.Reasons {
			row.HealthReasons = append(row.HealthReasons, reason.Code)
		}
		for _, a := range h.Agents {
			row.Agents = append(row.Agents, diagnosticsAgent{
				Name: a.Name, Version: a.Version, Supported: a.Supported,
				Capabilities: a.Capabilities, Warning: a.VersionWarning,
			})
		}
		if includeIdentity {
			row.Name = h.Name
		}
		hostRows = append(hostRows, row)
		if h.Update != nil && h.Update.Available {
			update := map[string]any{
				"current": h.Update.Current, "latest": h.Update.Latest,
				"available": h.Update.Available, "checked_at": h.Update.CheckedAt,
			}
			if includeIdentity {
				update["host"] = h.Name
			}
			updateRows = append(updateRows, update)
		}
	}

	var recentErrors []diagnosticsError
	if events, err := s.store.ListAudit(500); err == nil {
		for _, event := range events {
			lower := strings.ToLower(event.Action)
			if !strings.Contains(lower, "fail") && !strings.Contains(lower, "error") && !strings.Contains(lower, "denied") {
				continue
			}
			row := diagnosticsError{At: event.At, Action: event.Action}
			if includeIdentity {
				row.Actor = event.Actor
				row.Details = event.Details
			}
			recentErrors = append(recentErrors, row)
			if len(recentErrors) >= 20 {
				break
			}
		}
	}

	payload := map[string]any{
		"generated_at": time.Now().UTC(),
		"hub": map[string]any{
			"name":           s.cfg.HubName,
			"version":        Version,
			"go_version":     runtime.Version(),
			"os":             runtime.GOOS,
			"arch":           runtime.GOARCH,
			"started_at":     s.startedAt,
			"uptime_seconds": int64(time.Since(s.startedAt).Seconds()),
			"epoch":          s.epoch,
		},
		"config": map[string]any{
			"public_url_configured": s.cfg.PublicURL != "",
			"tls_enabled":           s.cfg.TLSCert != "" && s.cfg.TLSKey != "",
			"metrics_enabled":       s.cfg.MetricsEnabled,
		},
		"database": map[string]any{
			"ok":    dbOK,
			"error": errorString(countErr),
		},
		"counts": counts,
		"hosts": map[string]any{
			"total": len(hosts), "online": online, "items": hostRows,
		},
		"updates": updateRows,
		"errors":  recentErrors,
	}
	return payload
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s *Server) handleAdminDiagnostics(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !isAdminRole(u.Role) {
		writeError(w, http.StatusForbidden, "admin role required")
		return
	}
	writeJSON(w, http.StatusOK, s.diagnosticsPayload(u, true))
}

func (s *Server) handleDiagnosticsExport(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !isAdminRole(u.Role) {
		writeError(w, http.StatusForbidden, "admin role required")
		return
	}
	if !stepUpValid(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "step-up required", "step_up_required": true})
		return
	}
	payload := s.diagnosticsPayload(u, false)
	payload["exported_at"] = time.Now().UTC()
	payload["redaction"] = "host names, host ids, session ids, actors and audit details are omitted"
	w.Header().Set("Content-Disposition", `attachment; filename="agent-nexus-diagnostics.json"`)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, payload)
	s.audit(u.Username, "diagnostics.exported", nil)
}
