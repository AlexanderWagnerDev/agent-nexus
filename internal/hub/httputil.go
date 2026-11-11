package hub

import (
	"encoding/json"
	"net/http"
	"strings"
)

// maxRequestBody bounds JSON request bodies. Attachments travel inside host
// links, not through the hub's HTTP API, so 1 MiB is generous.
const maxRequestBody = 1 << 20

// bodyLimit rejects oversized request bodies before a handler decodes them.
func bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		next.ServeHTTP(w, r)
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

// decodeJSON decodes one JSON object from the request body. The body limit is
// enforced by the bodyLimit middleware.
func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
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
