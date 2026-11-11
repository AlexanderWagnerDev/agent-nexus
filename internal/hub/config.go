package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
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
