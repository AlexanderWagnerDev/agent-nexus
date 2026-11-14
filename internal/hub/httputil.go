package hub

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
)

// maxRequestBody bounds JSON request bodies. Attachments are only accepted by
// the prompt-queue endpoint, which has its own bound.
const (
	maxRequestBody       = 1 << 20
	maxPromptRequestBody = 24 << 20 // 6 attachments, 8 MiB each, base64-encoded
)

// bodyLimit rejects oversized request bodies before a handler decodes them.
func bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			limit := int64(maxRequestBody)
			if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/runs/") && strings.HasSuffix(r.URL.Path, "/queue") {
				limit = maxPromptRequestBody
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
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

// decodeSignedJSON decodes a JSON object and verifies the device signature over
// the exact request body when signed writes are required for this device. The
// signature therefore binds the whole action, not only the URL.
func (s *Server) decodeSignedJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	if !s.verifyHTTPDeviceSignature(w, r, deviceFrom(r).ID, body) {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

// clientIP returns the address used for rate limits and audit entries. A
// reverse proxy runs on a loopback or private address and may forward the
// client in X-Forwarded-For; a direct public client can therefore not spoof
// the header to escape per-IP limits. The rightmost entry is the one the last
// proxy observed, so prepended fake values are ignored.
func clientIP(r *http.Request) string {
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	remote = strings.Trim(remote, "[]")
	if proxyPeer(remote) {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			parts := strings.Split(forwarded, ",")
			candidate := strings.TrimSpace(parts[len(parts)-1])
			if net.ParseIP(candidate) != nil {
				return candidate
			}
		}
	}
	return remote
}

// proxyPeer reports whether the direct peer is local or private, which is
// where a reverse proxy sits in the documented deployments.
func proxyPeer(addr string) bool {
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
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
