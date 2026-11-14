package hub

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The prompt queue is the only endpoint that carries base64 attachments. Its
// real limit must exceed the default 1 MiB or the composer's 16 MiB total
// attachment allowance can never be used.
func TestBodyLimitAllowsQueuedPromptAttachments(t *testing.T) {
	body := bytes.Repeat([]byte("a"), 2<<20)
	var read int
	handler := bodyLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("prompt queue body was rejected: %v", err)
			return
		}
		read = len(data)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/run-1/queue", bytes.NewReader(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if read != len(body) {
		t.Fatalf("prompt queue body was truncated: %d of %d bytes", read, len(body))
	}

	// Every other endpoint keeps the small bound.
	var otherErr error
	handler = bodyLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, otherErr = io.ReadAll(r.Body)
	}))
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if otherErr == nil {
		t.Fatal("oversized login body was not bounded")
	}
}

func TestClientIPIgnoresSpoofedForwardedForFromPublicPeers(t *testing.T) {
	direct := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	direct.RemoteAddr = "203.0.113.7:4242"
	direct.Header.Set("X-Forwarded-For", "10.0.0.1")
	if got := clientIP(direct); got != "203.0.113.7" {
		t.Fatalf("public peer spoofed its address: %q", got)
	}

	// Behind a proxy on a private/loopback address the header is used, and only
	// the rightmost entry counts.
	proxied := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	proxied.RemoteAddr = "10.1.2.3:4242"
	proxied.Header.Set("X-Forwarded-For", "1.2.3.4, 198.51.100.9")
	if got := clientIP(proxied); got != "198.51.100.9" {
		t.Fatalf("proxied client address = %q", got)
	}

	ipv6 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	ipv6.RemoteAddr = "[::1]:8080"
	ipv6.Header.Set("X-Forwarded-For", "2001:db8::5")
	if got := clientIP(ipv6); got != "2001:db8::5" {
		t.Fatalf("loopback proxy address = %q", got)
	}
}
