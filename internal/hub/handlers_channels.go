package hub

import (
	"database/sql"
	"net/http"
	"strings"
)

type channelRequest struct {
	Kind    string            `json:"kind"`
	Name    string            `json:"name"`
	Config  map[string]string `json:"config"`
	Enabled *bool             `json:"enabled,omitempty"`
}

func (s *Server) handleListNotificationChannels(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	items, err := s.store.ListNotificationChannels(u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": items})
}

func (s *Server) validateChannelRequest(req channelRequest) string {
	if msg := validateNotificationConfig(req.Kind, req.Name, req.Config, s.notificationAllowPrivate); msg != nil {
		return msg.Error()
	}
	return ""
}

func (s *Server) handleCreateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var req channelRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if msg := s.validateChannelRequest(req); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	channel, err := s.store.CreateNotificationChannel(u.ID, req.Kind, strings.TrimSpace(req.Name), req.Config)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "notification_channel.created", map[string]string{"channel_id": channel.ID, "kind": channel.Kind})
	writeJSON(w, http.StatusCreated, channel)
}

func (s *Server) handleUpdateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var req channelRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if msg := s.validateChannelRequest(req); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	channel, err := s.store.UpdateNotificationChannel(u.ID, r.PathValue("id"), req.Kind, strings.TrimSpace(req.Name), req.Config, enabled)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "notification_channel.updated", map[string]string{"channel_id": channel.ID})
	writeJSON(w, http.StatusOK, channel)
}

func (s *Server) handleDeleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if err := s.store.DeleteNotificationChannel(u.ID, r.PathValue("id")); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "notification_channel.deleted", map[string]string{"channel_id": r.PathValue("id")})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleTestNotificationChannel(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	channel, err := s.store.NotificationChannelByID(u.ID, r.PathValue("id"))
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.deliverNotificationChannel(channel, "AgentNexus test", "This is a test notification from AgentNexus.", "/#/settings"); err != nil {
		s.audit(u.Username, "notification_channel.test_failed", map[string]string{"channel_id": channel.ID, "error": err.Error()})
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.audit(u.Username, "notification_channel.tested", map[string]string{"channel_id": channel.ID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}
