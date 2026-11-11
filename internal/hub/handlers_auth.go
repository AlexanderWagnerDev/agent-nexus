package hub

import (
	"net/http"
	"strings"
)

func (s *Server) handleHubKey(w http.ResponseWriter, r *http.Request) {
	public := s.signer.PublicKey()
	if public == "" {
		writeError(w, http.StatusServiceUnavailable, "hub signing is not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"public_key": public})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": Version})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.db.Ping(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.UserCount()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"setup_required": n == 0,
		"hub_name":       s.cfg.HubName,
		"version":        Version,
	})
}

type setupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.UserCount()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, "hub is already set up")
		return
	}
	var req setupRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	req.Username = strings.TrimSpace(strings.ToLower(req.Username))
	if len(req.Username) < 3 || len(req.Username) > 64 {
		writeError(w, http.StatusBadRequest, "username must be 3-64 characters")
		return
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	secret := NewTOTPSecret()
	user, err := s.store.CreateUser(req.Username, hash, secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	token, _, err := s.store.CreateSession(user.ID, deviceName(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.setSessionCookie(w, token)
	s.audit(user.Username, "user.setup", map[string]string{"user_id": user.ID})
	writeJSON(w, http.StatusCreated, map[string]any{
		"username":     user.Username,
		"totp_secret":  secret,
		"totp_url":     TOTPURL("AgentNexus", user.Username, secret),
		"totp_enabled": false,
	})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTP     string `json:"totp"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	req.Username = strings.TrimSpace(strings.ToLower(req.Username))
	key := clientIP(r) + "|" + req.Username
	if !s.loginLimiter.allow(key) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "too many attempts; try again later")
		return
	}
	user, err := s.store.UserByUsername(req.Username)
	if err != nil || !VerifyPassword(user.PasswordHash, req.Password) {
		s.audit(req.Username, "auth.login_failed", map[string]string{"ip": clientIP(r)})
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if user.TOTPEnabled && !ValidateTOTP(user.TOTPSecret, req.TOTP) {
		writeError(w, http.StatusUnauthorized, "invalid one-time code")
		return
	}
	token, device, err := s.store.CreateSession(user.ID, deviceName(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.setSessionCookie(w, token)
	s.audit(user.Username, "auth.login", map[string]string{"device_id": device.ID, "ip": clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"username": user.Username, "role": user.Role})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	theme, err := s.store.UserTheme(u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"theme": theme})
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var req struct {
		Theme string `json:"theme"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	switch req.Theme {
	case "system", "dark", "light":
	default:
		writeError(w, http.StatusBadRequest, "theme must be system, dark or light")
		return
	}
	if err := s.store.SetUserTheme(u.ID, req.Theme); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "user.settings_updated", map[string]string{"theme": req.Theme})
	writeJSON(w, http.StatusOK, map[string]string{"theme": req.Theme})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if token := tokenFrom(r); token != "" {
		_ = s.store.RevokeSession(token)
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	d := deviceFrom(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":           u.ID,
		"username":     u.Username,
		"role":         u.Role,
		"totp_enabled": u.TOTPEnabled,
		"device":       d,
		"hub_name":     s.cfg.HubName,
	})
}

type totpBeginResponse struct {
	Secret string `json:"secret"`
	URL    string `json:"url"`
}

func (s *Server) handleTOTPBegin(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	secret := NewTOTPSecret()
	writeJSON(w, http.StatusOK, totpBeginResponse{Secret: secret, URL: TOTPURL("AgentNexus", u.Username, secret)})
}

type totpEnableRequest struct {
	Secret string `json:"secret"`
	Code   string `json:"code"`
}

func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) {
	var req totpEnableRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !ValidateTOTP(req.Secret, req.Code) {
		writeError(w, http.StatusBadRequest, "invalid one-time code")
		return
	}
	u := userFrom(r)
	if err := s.store.SetTOTP(u.ID, req.Secret, true); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "auth.totp_enabled", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	devices, err := s.store.ListDevices(u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	current := deviceFrom(r).ID
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices, "current_device_id": current})
}

func (s *Server) handleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == deviceFrom(r).ID {
		writeError(w, http.StatusBadRequest, "cannot revoke the current device here")
		return
	}
	u := userFrom(r)
	if err := s.store.RevokeDevice(u.ID, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.closeDevice(id)
	s.audit(u.Username, "device.revoked", map[string]string{"device_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// ---- hosts ----
