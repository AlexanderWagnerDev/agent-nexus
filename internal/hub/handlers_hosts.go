package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	hosts, err := s.store.ListHosts(u.ID, isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range hosts {
		if hosts[i].Disabled {
			hosts[i].Status = "disabled"
		} else if s.hostOnline(hosts[i].ID) {
			hosts[i].Status = "online"
		}
		s.updateMu.Lock()
		if status, ok := s.hostUpdates[hosts[i].ID]; ok {
			copyStatus := status
			hosts[i].Update = &copyStatus
		}
		s.updateMu.Unlock()
		hosts[i].Index = s.hostIndexStatus(hosts[i].ID)
		health := buildHostHealth(hosts[i], s.hostOnline(hosts[i].ID))
		hosts[i].Health = &health
	}
	writeJSON(w, http.StatusOK, map[string]any{"hosts": hosts})
}

// handleHostDetail returns one host with explainable health, recent sessions,
// active runs and (for managers) recent host audit events. It never includes
// transcript content or secrets.
func (s *Server) handleHostDetail(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	if !s.hostPermission(u, id, "view") {
		writeError(w, http.StatusNotFound, "host not found")
		return
	}
	host, err := s.store.HostByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "host not found")
		return
	}
	hv := host.HostView
	online := s.hostOnline(id)
	if hv.Disabled {
		hv.Status = "disabled"
	} else if online {
		hv.Status = "online"
	}
	s.updateMu.Lock()
	if status, ok := s.hostUpdates[id]; ok {
		copyStatus := status
		hv.Update = &copyStatus
	}
	s.updateMu.Unlock()
	hv.Index = s.hostIndexStatus(id)
	hv.Agents, _ = s.store.HostAgents(id)
	health := buildHostHealth(hv, online)
	hv.Health = &health

	sessions, projects, activeCount, _ := s.store.HostStats(id)
	recent, _ := s.store.ListSessionsFiltered(u.ID, SessionFilter{HostID: id, Limit: 20}, isAdminRole(u.Role))
	runs, _ := s.store.ActiveRuns(id)
	payload := map[string]any{
		"host": hv, "sessions": sessions, "projects": projects,
		"active_runs": activeCount, "runs": runs, "recent_sessions": recent,
		"can_manage": s.hostPermission(u, id, "manage"),
	}
	if s.hostPermission(u, id, "manage") {
		events, _ := s.store.AuditForHost(id, 30)
		payload["events"] = events
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handlePairingCode(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.Role == "viewer" {
		writeError(w, http.StatusForbidden, "viewer role cannot add hosts")
		return
	}
	code := newPairingCode()
	if err := s.store.CreatePairingCode(u.ID, code, 10*time.Minute); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "host.pairing_code_created", nil)
	writeJSON(w, http.StatusCreated, map[string]any{
		"code":       code,
		"expires_at": time.Now().Add(10 * time.Minute).UTC(),
	})
}

type pairRequest struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Version   string `json:"version"`
}

func (s *Server) handlePairHost(w http.ResponseWriter, r *http.Request) {
	if !s.pairLimiter.allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many pairing attempts")
		return
	}
	var req pairRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !ValidatePairingCode(strings.ToUpper(req.Code)) {
		writeError(w, http.StatusBadRequest, "invalid pairing code")
		return
	}
	key, err := base64Decode(req.PublicKey)
	if err != nil || len(key) != ed25519PublicKeySize {
		writeError(w, http.StatusBadRequest, "invalid public key")
		return
	}
	ownerID, err := s.store.ConsumePairingCode(strings.ToUpper(req.Code))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "pairing code is invalid or expired")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "unnamed host"
	}
	id, err := s.store.CreateHost(ownerID, name, key, req.OS, req.Arch, req.Version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sas := protocol.HostSAS(id, key)
	if err := s.store.CreateHostConfirmation(id, sas); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(name, "host.paired", map[string]string{"host_id": id, "os": req.OS, "arch": req.Arch})
	writeJSON(w, http.StatusCreated, map[string]string{"host_id": id, "sas": sas})
}

func (s *Server) handleConfirmHost(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	host, err := s.store.HostByID(id)
	if err != nil || (host.OwnerID != u.ID && !isAdminRole(u.Role)) {
		writeError(w, http.StatusNotFound, "host not found")
		return
	}
	if err := s.store.ConfirmHost(host.OwnerID, id); err != nil {
		writeError(w, http.StatusNotFound, "host confirmation not found")
		return
	}
	s.broadcastHostUpdate(id)
	s.audit(u.Username, "host.confirmed", map[string]string{"host_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "confirmed"})
}

// handleHostUpdateSettings renames or disables a host. Both actions are
// security-relevant (a disabled host must not receive new runs), so the route
// requires step-up confirmation and the manage grant.
func (s *Server) handleHostUpdateSettings(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	if !s.hostPermission(u, id, "manage") {
		writeError(w, http.StatusForbidden, "host manage permission required")
		return
	}
	var req struct {
		Name     *string `json:"name"`
		Disabled *bool   `json:"disabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Name == nil && req.Disabled == nil {
		writeError(w, http.StatusBadRequest, "name or disabled is required")
		return
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if len(name) < 1 || len(name) > 80 {
			writeError(w, http.StatusBadRequest, "name must be 1-80 characters")
			return
		}
		if err := s.store.RenameHost(id, name); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.audit(u.Username, "host.rename", map[string]string{"host_id": id, "name": name})
	}
	if req.Disabled != nil {
		disabled := *req.Disabled
		if _, err := s.store.HostByID(id); err != nil {
			writeError(w, http.StatusNotFound, "host not found")
			return
		}
		if err := s.store.SetHostDisabled(id, disabled); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		action := "host.enable"
		if disabled {
			action = "host.disable"
		}
		s.audit(u.Username, action, map[string]string{"host_id": id})
		if disabled {
			// Ask the daemon to stop every run before dropping the link. The
			// host must not keep an orphaned agent process running while the
			// hub refuses its reconnect.
			if runs, err := s.store.ActiveRuns(id); err == nil {
				for _, run := range runs {
					_ = s.sendHost(id, protocol.Envelope{Type: "run.stop", RunID: run.ID})
					s.setRunState(run.ID, protocol.StateStopping)
				}
			}
			go func() {
				time.Sleep(2 * time.Second)
				s.dropHost(id, "host disabled")
				s.broadcastHostUpdate(id)
			}()
		} else {
			s.broadcastHostUpdate(id)
		}
	}
	host, err := s.store.HostByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "host not found")
		return
	}
	hv := host.HostView
	if host.DisabledAt != nil {
		hv.Status = "disabled"
	} else if s.hostOnline(id) {
		hv.Status = "online"
	}
	hv.Agents, _ = s.store.HostAgents(id)
	health := buildHostHealth(hv, s.hostOnline(id))
	hv.Health = &health
	writeJSON(w, http.StatusOK, map[string]any{"host": hv})
}

func (s *Server) handleRevokeHost(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	if !s.hostPermission(u, id, "manage") {
		writeError(w, http.StatusForbidden, "host manage permission required")
		return
	}
	if err := s.store.RevokeHostByID(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.dropHost(id, "host revoked")
	s.audit(u.Username, "host.revoked", map[string]string{"host_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// ---- sessions ----

func (s *Server) handleHostUpdate(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	hostID := r.PathValue("id")
	if !s.hostPermission(u, hostID, "view") {
		writeError(w, http.StatusForbidden, "host view permission required")
		return
	}
	if !s.hostOnline(hostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	params, _ := json.Marshal(map[string]string{"channel": channel})
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	raw, err := s.hostRequest(ctx, hostID, "update.check", params)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var info any
	if err := json.Unmarshal(raw, &info); err != nil {
		writeError(w, http.StatusBadGateway, "invalid host response")
		return
	}
	writeJSON(w, http.StatusOK, info)
}
