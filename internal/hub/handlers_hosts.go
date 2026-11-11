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
		if s.hostOnline(hosts[i].ID) {
			hosts[i].Status = "online"
		}
		s.updateMu.Lock()
		if status, ok := s.hostUpdates[hosts[i].ID]; ok {
			copyStatus := status
			hosts[i].Update = &copyStatus
		}
		s.updateMu.Unlock()
	}
	writeJSON(w, http.StatusOK, map[string]any{"hosts": hosts})
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
