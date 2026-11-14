package hub

import (
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

type passkeyUser struct {
	user  User
	creds []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return []byte(u.user.ID) }
func (u passkeyUser) WebAuthnName() string                       { return u.user.Username }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.user.Username }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

type passkeySession struct {
	userID  string
	kind    string
	data    webauthn.SessionData
	expires time.Time
}

type passkeySessionStore struct {
	mu   sync.Mutex
	data map[string]passkeySession
}

const maxPasskeySessions = 512

// put stores one WebAuthn challenge. The map is bounded and expired entries
// are only swept when the map grows, so an unauthenticated caller cannot turn
// every request into a full-map scan.
func (p *passkeySessionStore) put(v passkeySession) (string, error) {
	id := randomToken(24)
	now := time.Now()
	v.expires = now.Add(5 * time.Minute)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.data) >= maxPasskeySessions {
		for k, old := range p.data {
			if now.After(old.expires) {
				delete(p.data, k)
			}
		}
		if len(p.data) >= maxPasskeySessions {
			return "", errors.New("too many pending passkey challenges")
		}
	}
	p.data[id] = v
	return id, nil
}

func (p *passkeySessionStore) take(id, kind string) (passkeySession, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.data[id]
	if !ok || v.kind != kind || time.Now().After(v.expires) {
		delete(p.data, id)
		return passkeySession{}, false
	}
	delete(p.data, id)
	return v, true
}

func (s *Server) webAuthnFor(r *http.Request) (*webauthn.WebAuthn, error) {
	origin := ""
	if s.cfg.PublicURL != "" {
		origin = strings.TrimRight(s.cfg.PublicURL, "/")
	} else {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		origin = scheme + "://" + r.Host
	}
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" {
		return nil, err
	}
	if net.ParseIP(u.Hostname()) != nil {
		return nil, errors.New("WebAuthn requires a hostname; configure --public-url")
	}
	return webauthn.New(&webauthn.Config{
		RPID:          u.Hostname(),
		RPDisplayName: s.cfg.HubName,
		RPOrigins:     []string{origin},
	})
}

func (s *Server) passkeyUserFor(user User) (passkeyUser, error) {
	creds, err := s.store.Passkeys(user.ID)
	if err != nil {
		return passkeyUser{}, err
	}
	return passkeyUser{user: user, creds: creds}, nil
}

func (s *Server) handlePasskeys(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	creds, err := s.store.Passkeys(u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(creds))
	for _, c := range creds {
		out = append(out, map[string]any{
			"id":          protocolBytes(c.ID),
			"transports":  c.Transport,
			"attestation": c.AttestationType,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": out})
}

func protocolBytes(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Server) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	pu, err := s.passkeyUserFor(u)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	wa, err := s.webAuthnFor(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid hub origin")
		return
	}
	creation, session, err := wa.BeginRegistration(pu, webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
		ResidentKey:      protocol.ResidentKeyRequirementPreferred,
		UserVerification: protocol.VerificationPreferred,
	}))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.passkeySessions.put(passkeySession{userID: u.ID, kind: "register", data: *session})
	if err != nil {
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session_id": id, "publicKey": creation.Response})
}

func (s *Server) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sessionID := r.URL.Query().Get("session")
	sess, ok := s.passkeySessions.take(sessionID, "register")
	if !ok || sess.userID != u.ID {
		writeError(w, http.StatusBadRequest, "registration session expired")
		return
	}
	pu, err := s.passkeyUserFor(u)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	wa, err := s.webAuthnFor(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid hub origin")
		return
	}
	cred, err := wa.FinishRegistration(pu, sess.data, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "passkey registration failed")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = "Passkey"
	}
	if err := s.store.SavePasskey(u.ID, name, cred); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "passkey.registered", map[string]string{"name": name})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "registered"})
}

type passkeyLoginBeginRequest struct {
	Username string `json:"username"`
}

func (s *Server) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimiter.allow("passkey|" + clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "too many attempts; try again later")
		return
	}
	var req passkeyLoginBeginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	u, err := s.store.UserByUsername(strings.ToLower(strings.TrimSpace(req.Username)))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "passkey login unavailable")
		return
	}
	pu, err := s.passkeyUserFor(u)
	if err != nil || len(pu.creds) == 0 {
		writeError(w, http.StatusUnauthorized, "passkey login unavailable")
		return
	}
	wa, err := s.webAuthnFor(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid hub origin")
		return
	}
	assertion, session, err := wa.BeginLogin(pu)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.passkeySessions.put(passkeySession{userID: u.ID, kind: "login", data: *session})
	if err != nil {
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session_id": id, "publicKey": assertion.Response})
}

func (s *Server) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	sess, ok := s.passkeySessions.take(sessionID, "login")
	if !ok {
		writeError(w, http.StatusUnauthorized, "login session expired")
		return
	}
	u, err := s.store.UserByID(sess.userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "passkey login failed")
		return
	}
	pu, err := s.passkeyUserFor(u)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "passkey login failed")
		return
	}
	wa, err := s.webAuthnFor(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid hub origin")
		return
	}
	cred, err := wa.FinishLogin(pu, sess.data, r)
	if err != nil {
		s.audit(u.Username, "passkey.login_failed", nil)
		writeError(w, http.StatusUnauthorized, "passkey login failed")
		return
	}
	_ = s.store.UpdatePasskey(cred)
	token, device, err := s.store.CreateSession(u.ID, deviceName(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.setSessionCookie(w, token)
	s.audit(u.Username, "passkey.login", map[string]string{"device_id": device.ID})
	writeJSON(w, http.StatusOK, map[string]any{"username": u.Username, "role": u.Role})
}
