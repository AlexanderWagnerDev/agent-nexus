package hub

import (
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const stepUpWindow = 5 * time.Minute

type stepUpPasswordRequest struct {
	Password string `json:"password"`
	TOTP     string `json:"totp"`
}

func (s *Server) handleStepUpStatus(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	until, _ := r.Context().Value(ctxStepUp).(time.Time)
	creds, _ := s.store.Passkeys(u.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"valid_until":  until,
		"passkeys":     len(creds),
		"totp_enabled": u.TOTPEnabled,
	})
}

// handleStepUpBegin starts a passkey assertion when one is registered.
// Otherwise it tells the client to fall back to password + TOTP.
func (s *Server) handleStepUpBegin(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	pu, err := s.passkeyUserFor(u)
	if err != nil || len(pu.creds) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"method": "password", "totp_enabled": u.TOTPEnabled})
		return
	}
	wa, err := s.webAuthnFor(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid hub origin")
		return
	}
	assertion, session, err := wa.BeginLogin(pu, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.passkeySessions.put(passkeySession{userID: u.ID, kind: "stepup", data: *session})
	if err != nil {
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"method": "passkey", "session_id": id, "publicKey": assertion.Response})
}

func (s *Server) handleStepUpPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, ok := s.passkeySessions.take(r.URL.Query().Get("session"), "stepup")
	if !ok || sess.userID != u.ID {
		writeError(w, http.StatusUnauthorized, "step-up session expired")
		return
	}
	pu, err := s.passkeyUserFor(u)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "step-up failed")
		return
	}
	wa, err := s.webAuthnFor(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid hub origin")
		return
	}
	cred, err := wa.FinishLogin(pu, sess.data, r)
	if err != nil {
		s.audit(u.Username, "auth.step_up_failed", map[string]string{"method": "passkey"})
		writeError(w, http.StatusUnauthorized, "step-up failed")
		return
	}
	_ = s.store.UpdatePasskey(cred)
	if err := s.store.MarkStepUp(tokenFrom(r), stepUpWindow); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "auth.step_up", map[string]string{"method": "passkey"})
	writeJSON(w, http.StatusOK, map[string]any{"valid_for_seconds": int(stepUpWindow.Seconds())})
}

func (s *Server) handleStepUpPassword(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if !s.loginLimiter.allow(clientIP(r) + "|stepup|" + u.Username) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "too many attempts; try again later")
		return
	}
	var req stepUpPasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !VerifyPasswordBounded(u.PasswordHash, req.Password) {
		s.audit(u.Username, "auth.step_up_failed", map[string]string{"method": "password"})
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if u.TOTPEnabled && !ValidateTOTP(u.TOTPSecret, req.TOTP) {
		s.audit(u.Username, "auth.step_up_failed", map[string]string{"method": "totp"})
		writeError(w, http.StatusUnauthorized, "invalid one-time code")
		return
	}
	if err := s.store.MarkStepUp(tokenFrom(r), stepUpWindow); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(u.Username, "auth.step_up", map[string]string{"method": "password"})
	writeJSON(w, http.StatusOK, map[string]any{"valid_for_seconds": int(stepUpWindow.Seconds())})
}
