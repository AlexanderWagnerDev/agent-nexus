package hub

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
)

func (s *Server) publicBaseURL(r *http.Request) string {
	if s.cfg.PublicURL != "" {
		return strings.TrimRight(s.cfg.PublicURL, "/")
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) handleCreateDevicePairing(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !s.pairLimiter.allow("device|" + u.ID) {
		writeError(w, http.StatusTooManyRequests, "too many pairing attempts")
		return
	}
	token := randomToken(32)
	if err := s.store.CreateDevicePairing(u.ID, token, 2*time.Minute); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	link := s.publicBaseURL(r) + "/?pair=" + url.QueryEscape(token)
	png, err := qrcode.Encode(link, qrcode.Medium, 320)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot render QR code")
		return
	}
	s.audit(u.Username, "device.pairing_code_created", nil)
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":      token,
		"url":        link,
		"expires_at": time.Now().Add(2 * time.Minute).UTC(),
		"qr":         "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
	})
}

type devicePairRequest struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}

func (s *Server) handleDevicePair(w http.ResponseWriter, r *http.Request) {
	if !s.pairLimiter.allow("device-pair|" + clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many pairing attempts")
		return
	}
	var req devicePairRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	userID, err := s.store.ConsumeDevicePairing(req.Token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "pairing code is invalid or expired")
		return
	}
	u, err := s.store.UserByID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "pairing failed")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = deviceName(r)
	}
	token, device, err := s.store.CreateSession(u.ID, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.setSessionCookie(w, token)
	s.audit(u.Username, "device.paired", map[string]string{"device_id": device.ID})
	writeJSON(w, http.StatusOK, map[string]any{"username": u.Username, "role": u.Role})
}
