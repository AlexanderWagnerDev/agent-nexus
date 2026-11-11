package hub

import (
	"context"
	"net/http"
	"strings"
	"time"
)

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
