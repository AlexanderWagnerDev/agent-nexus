package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Browsers reject a web app manifest that is not served with a JSON media
// type, and a module script with nosniff only runs with a JavaScript type.
// Pin the types instead of trusting the host MIME table.
func TestHandlerServesTypedAssets(t *testing.T) {
	handler := Handler()
	cases := map[string]string{
		"/manifest.webmanifest": "application/manifest+json",
		"/app.js":               "text/javascript; charset=utf-8",
		"/styles.css":           "text/css; charset=utf-8",
		"/icon.svg":             "image/svg+xml",
	}
	for path, want := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s returned %d", path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != want {
			t.Errorf("%s Content-Type = %q, want %q", path, got, want)
		}
	}
}
