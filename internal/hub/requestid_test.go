package hub

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) Reset() {
	b.mu.Lock()
	b.buf.Reset()
	b.mu.Unlock()
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestValidRequestID(t *testing.T) {
	cases := map[string]bool{
		"req-12345678":          true,
		"abc_DEF.123:456":       true,
		"short":                 false,
		"bad id with spaces":    false,
		"line\nbreak-12345678":  false,
		strings.Repeat("a", 65): false,
	}
	for value, want := range cases {
		if got := validRequestID(value); got != want {
			t.Errorf("validRequestID(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestRequestIDPropagationAndSanitization(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	box, err := OpenSecretBox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.SetSecretBox(box)
	var logs lockedBuffer
	server := NewServer(Config{HubName: "test"}, store, slog.New(slog.NewTextHandler(&logs, nil)))
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	request := func(id string) *http.Response {
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/me", nil)
		if err != nil {
			t.Fatal(err)
		}
		if id != "" {
			req.Header.Set("X-Request-ID", id)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		return res
	}

	res := request("req-12345678")
	if got := res.Header.Get("X-Request-ID"); got != "req-12345678" {
		t.Fatalf("response request id: %q", got)
	}
	if !strings.Contains(logs.String(), "request_id=req-12345678") {
		t.Fatalf("handler log is missing the request id: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "/api/v1/me") {
		t.Fatalf("handler log is missing the route: %s", logs.String())
	}

	logs.Reset()
	res = request("bad id with spaces")
	got := res.Header.Get("X-Request-ID")
	if got == "bad id with spaces" || !validRequestID(got) {
		t.Fatalf("forged request id was accepted: %q", got)
	}
	if !strings.Contains(logs.String(), "request_id="+got) {
		t.Fatalf("sanitized request id was not logged: %s", logs.String())
	}

	logs.Reset()
	res = request("")
	if got := res.Header.Get("X-Request-ID"); !validRequestID(got) {
		t.Fatalf("generated request id is invalid: %q", got)
	}
	if strings.Contains(logs.String(), "request_id= ") {
		t.Fatalf("empty request id leaked: %s", logs.String())
	}
}
