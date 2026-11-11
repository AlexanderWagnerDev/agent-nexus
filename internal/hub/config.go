package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// Config is the hub runtime configuration.
type Config struct {
	Addr      string
	DataDir   string
	PublicURL string
	TLSCert   string
	TLSKey    string
	HubName   string
}

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxDevice
	ctxToken
	ctxStepUp
)

type authInfo struct {
	User   User
	Device Device
	Token  string
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		si, err := s.store.SessionByToken(cookie.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "session expired")
			return
		}
		s.store.TouchSession(cookie.Value, si.Device.ID)
		ctx := context.WithValue(r.Context(), ctxUser, si.User)
		ctx = context.WithValue(ctx, ctxDevice, si.Device)
		ctx = context.WithValue(ctx, ctxToken, cookie.Value)
		ctx = context.WithValue(ctx, ctxStepUp, si.StepUpUntil)
		next(w, r.WithContext(ctx))
	}
}

// requireStepUp guards actions that can widen access or change credentials.
func (s *Server) requireStepUp(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		until, _ := r.Context().Value(ctxStepUp).(time.Time)
		if !time.Now().Before(until) {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error": "step-up required", "step_up_required": true,
			})
			return
		}
		next(w, r)
	}
}

func userFrom(r *http.Request) User {
	u, _ := r.Context().Value(ctxUser).(User)
	return u
}

func deviceFrom(r *http.Request) Device {
	d, _ := r.Context().Value(ctxDevice).(Device)
	return d
}

func tokenFrom(r *http.Request) string {
	t, _ := r.Context().Value(ctxToken).(string)
	return t
}

const cookieName = "nexus_session"

func (s *Server) cookieSecure() bool {
	return s.cfg.TLSCert != "" || strings.HasPrefix(s.cfg.PublicURL, "https://")
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure(),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   30 * 24 * 60 * 60,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure(),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' ws: wss:; font-src 'self'; worker-src 'self'; manifest-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'")
		if s.cookieSecure() {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// csrfGuard rejects mutating API requests that do not come from our own
// origin and carry the custom request header. SameSite=Strict is the primary
// defence; this is defence in depth for older browsers.
func (s *Server) csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("X-Nexus-Request") != "1" {
			writeError(w, http.StatusForbidden, "missing request marker")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if !s.originAllowed(origin, r.Host) {
				writeError(w, http.StatusForbidden, "origin mismatch")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed accepts the request host and, when configured, the public URL.
// Reverse proxies that rewrite Host still work without weakening the check.
func (s *Server) originAllowed(origin, host string) bool {
	got := strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")
	if got == host {
		return true
	}
	if s.cfg.PublicURL != "" {
		want := strings.TrimPrefix(strings.TrimPrefix(strings.TrimRight(s.cfg.PublicURL, "/"), "http://"), "https://")
		return got == want
	}
	return false
}

func (s *Server) audit(actor, action string, details any) {
	_ = s.store.AppendAudit(actor, action, details)
}

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

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	q := r.URL.Query()
	limit := atoiOr(q.Get("limit"), 100)
	if limit < 1 {
		limit = 100
	}
	if limit > 2000 {
		limit = 2000
	}
	offset := atoiOr(q.Get("offset"), 0)
	sessions, err := s.store.ListSessions(u.ID, q.Get("host"), q.Get("agent"), q.Get("project"), q.Get("state"), q.Get("q"), limit+1, offset, isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hasMore := len(sessions) > limit
	if hasMore {
		sessions = sessions[:limit]
	}
	if sessions == nil {
		sessions = []protocol.SessionView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions, "has_more": hasMore, "offset": offset})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "view") {
		writeError(w, http.StatusForbidden, "view permission required")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleTranscript(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "read_transcript") {
		writeError(w, http.StatusForbidden, "read_transcript permission required")
		return
	}
	if !s.hostOnline(sess.HostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	offset := atoiOr(r.URL.Query().Get("offset"), 0)
	limit := atoiOr(r.URL.Query().Get("limit"), 100)
	if limit > 500 {
		limit = 500
	}
	params, _ := json.Marshal(map[string]any{"native_id": sess.NativeID, "agent": sess.Agent, "offset": offset, "limit": limit})
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := s.hostRequest(ctx, sess.HostID, "transcript", params)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var tr protocol.Transcript
	if err := json.Unmarshal(result, &tr); err != nil {
		writeError(w, http.StatusBadGateway, "invalid host response")
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (s *Server) handleSessionGit(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "read_transcript") {
		writeError(w, http.StatusForbidden, "read_transcript permission required")
		return
	}
	if !s.hostOnline(sess.HostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	params, _ := json.Marshal(map[string]string{"cwd": sess.Cwd})
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	raw, err := s.hostRequest(ctx, sess.HostID, "git", params)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var view map[string]any
	if err := json.Unmarshal(raw, &view); err != nil {
		writeError(w, http.StatusBadGateway, "invalid host response")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) < 2 || len(query) > 200 {
		writeError(w, http.StatusBadRequest, "query must be 2-200 characters")
		return
	}
	hosts, err := s.store.ListHosts(u.ID, isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type result struct {
		hits   []protocol.SearchHit
		host   string
		hostID string
	}
	ch := make(chan result, len(hosts))
	online := 0
	for _, h := range hosts {
		if !s.hostPermission(u, h.ID, "read_transcript") {
			continue
		}
		if !s.hostOnline(h.ID) {
			continue
		}
		online++
		go func(hostID, hostName string) {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			params, _ := json.Marshal(map[string]any{"query": query, "limit": 50})
			raw, err := s.hostRequest(ctx, hostID, "search", params)
			if err != nil {
				ch <- result{host: hostName, hostID: hostID}
				return
			}
			var hits []protocol.SearchHit
			_ = json.Unmarshal(raw, &hits)
			ch <- result{hits: hits, host: hostName, hostID: hostID}
		}(h.ID, h.Name)
	}
	all := []protocol.SearchHit{}
	seen := map[string]bool{}
	search := map[string]bool{}
	for i := 0; i < online; i++ {
		res := <-ch
		if len(res.hits) > 0 {
			for _, hit := range res.hits {
				key := res.hostID + "|" + hit.Agent + "|" + hit.NativeID
				if seen[key] {
					continue
				}
				seen[key] = true
				hit.HostID = res.hostID
				hit.HostName = res.host
				hit.Title = displayTitle(hit.Title)
				hit.Snippet = strings.TrimSpace(hit.Snippet)
				if id, err := s.store.SessionIDForNative(res.hostID, hit.Agent, hit.NativeID); err == nil {
					hit.SessionID = id
				}
				all = append(all, hit)
			}
		} else {
			search[res.host] = true
		}
	}
	if len(all) > 100 {
		all = all[:100]
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": all, "not_searched": search, "hosts_total": len(hosts)})
}

// ---- runs ----

type runRequest struct {
	HostID string `json:"host_id"`
	Agent  string `json:"agent"`
	Cwd    string `json:"cwd"`
	Mode   string `json:"mode"`
	Fork   bool   `json:"fork"`
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
}

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "resume") {
		writeError(w, http.StatusForbidden, "resume permission required")
		return
	}
	if !s.hostOnline(sess.HostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	if runID, active := s.store.ActiveRunForSession(sess.ID); active {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "session is already active", "run_id": runID,
		})
		return
	}
	var req runRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Fork && !s.sessionPermission(u, sess, "fork") {
		writeError(w, http.StatusForbidden, "fork permission required")
		return
	}
	if req.Mode == "" {
		req.Mode = protocol.ModeTerminal
	}
	if req.Mode != protocol.ModeTerminal && req.Mode != protocol.ModeStructured {
		writeError(w, http.StatusBadRequest, "unknown run mode")
		return
	}
	if req.Cols <= 0 {
		req.Cols, req.Rows = 120, 30
	}
	action := protocol.ActionResume
	if req.Fork {
		action = protocol.ActionFork
	}
	run, err := s.startRun(u, deviceFrom(r), sess, action, req.Cwd, req.Mode, req.Cols, req.Rows)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

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

func (s *Server) handleNewRun(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	hostID := r.PathValue("id")
	host, err := s.store.HostByID(hostID)
	if err != nil || host.RevokedAt != nil {
		writeError(w, http.StatusNotFound, "host not found")
		return
	}
	if !s.hostPermission(u, hostID, "create") {
		writeError(w, http.StatusForbidden, "create permission required")
		return
	}
	if !s.hostOnline(hostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	var req runRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Agent == "" || req.Cwd == "" {
		writeError(w, http.StatusBadRequest, "agent and cwd are required")
		return
	}
	if req.Mode == "" {
		req.Mode = protocol.ModeTerminal
	}
	if req.Mode != protocol.ModeTerminal && req.Mode != protocol.ModeStructured {
		writeError(w, http.StatusBadRequest, "unknown run mode")
		return
	}
	if req.Cols <= 0 {
		req.Cols, req.Rows = 120, 30
	}
	run, err := s.startRun(u, deviceFrom(r), protocol.SessionView{HostID: hostID, Agent: req.Agent}, protocol.ActionNew, req.Cwd, req.Mode, req.Cols, req.Rows)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) startRun(u User, d Device, sess protocol.SessionView, action, cwd, mode string, cols, rows int) (protocol.RunView, error) {
	id := newID()
	if cwd == "" {
		cwd = sess.Cwd
	}
	if cwd == "" && action != protocol.ActionNew {
		return protocol.RunView{}, errors.New("session has no working directory")
	}
	if err := s.store.CreateRun(id, sess.ID, sess.HostID, mode, u.ID, d.ID); err != nil {
		return protocol.RunView{}, err
	}
	spec := protocol.RunSpec{
		RunID: id, SessionID: sess.ID, Agent: sess.Agent, NativeID: sess.NativeID,
		Cwd: cwd, Action: action, Mode: mode, Cols: cols, Rows: rows,
	}
	s.mu.Lock()
	s.runHosts[id] = sess.HostID
	s.mu.Unlock()
	if err := s.sendHost(sess.HostID, protocol.Envelope{Type: "run.start", Run: &spec}); err != nil {
		s.store.UpdateRunState(id, protocol.StateFailed)
		return protocol.RunView{}, err
	}
	s.setRunState(id, protocol.StateStarting)
	s.audit(u.Username, "run.started", map[string]string{"run_id": id, "host_id": sess.HostID, "agent": sess.Agent, "action": action, "mode": mode})
	run, _, _ := s.store.RunByID(id)
	return run, nil
}

func (s *Server) handleActiveRuns(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	runs, err := s.store.ActiveRunsForUser(u.ID, isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []protocol.RunView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	run, startedBy, err := s.store.RunByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if startedBy != u.ID && !isAdminRole(u.Role) {
		writeError(w, http.StatusForbidden, "not your run")
		return
	}
	run.LastEventSeq = s.runEventCursor(run.ID)
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleRunOptions(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	run, startedBy, err := s.store.RunByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if startedBy != u.ID && !isAdminRole(u.Role) {
		writeError(w, http.StatusForbidden, "not your run")
		return
	}
	if !s.hostOnline(run.HostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline")
		return
	}
	params, _ := json.Marshal(map[string]string{"run_id": run.ID})
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	raw, err := s.hostRequest(ctx, run.HostID, "run.options", params)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var options protocol.RunOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		writeError(w, http.StatusBadGateway, "invalid host response")
		return
	}
	writeJSON(w, http.StatusOK, options)
}

func (s *Server) handleStopRun(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	run, startedBy, err := s.store.RunByID(r.PathValue("id"))
	if err != nil || (startedBy != u.ID && !isAdminRole(u.Role)) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err := s.sendHost(run.HostID, protocol.Envelope{Type: "run.stop", RunID: run.ID}); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.setRunState(run.ID, protocol.StateStopping)
	s.audit(u.Username, "run.stopped", map[string]string{"run_id": run.ID})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "stopping"})
}

func (s *Server) handleLease(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	run, startedBy, err := s.store.RunByID(r.PathValue("id"))
	if err != nil || (startedBy != u.ID && !isAdminRole(u.Role)) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	d := deviceFrom(r)
	run.LeaseDeviceID = d.ID
	_, err = s.store.db.Exec(`UPDATE runs SET lease_device_id=? WHERE id=?`, d.ID, run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.broadcast(protocol.ServerEnvelope{Type: "run.lease", RunID: run.ID, Message: d.Name})
	s.audit(u.Username, "run.lease_acquired", map[string]string{"run_id": run.ID, "device_id": d.ID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "lease_acquired"})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.Role != "owner" && u.Role != "admin" {
		writeError(w, http.StatusForbidden, "owner role required")
		return
	}
	events, err := s.store.ListAudit(atoiOr(r.URL.Query().Get("limit"), 200))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	broken, _ := s.store.VerifyAudit()
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "broken_seq": broken})
}

func isAdminRole(role string) bool {
	return role == "owner" || role == "admin"
}

func displayTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "Untitled session"
	}
	if len(title) > 200 {
		return title[:200] + "..."
	}
	return title
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.IndexByte(v, ','); i > 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		host = host[:i]
	}
	return host
}

func deviceName(r *http.Request) string {
	ua := r.UserAgent()
	switch {
	case strings.Contains(ua, "Windows"):
		return "Windows browser"
	case strings.Contains(ua, "Macintosh"):
		return "macOS browser"
	case strings.Contains(ua, "Android"):
		return "Android browser"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		return "iOS browser"
	case strings.Contains(ua, "Linux"):
		return "Linux browser"
	default:
		return "Browser"
	}
}
