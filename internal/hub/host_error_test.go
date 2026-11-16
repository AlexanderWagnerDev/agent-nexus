package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A slow host used to reach the browser as a bare 502, indistinguishable from a
// broken hub. Each failure class must keep its own status and reason.
func TestWriteHostErrorMapsStatusAndReason(t *testing.T) {
	s := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	cases := []struct {
		name   string
		err    error
		status int
		reason string
	}{
		{"timeout", context.DeadlineExceeded, http.StatusGatewayTimeout, "host_timeout"},
		{"offline", errHostOffline, http.StatusServiceUnavailable, "host_offline"},
		{"busy", errHostBusy, http.StatusServiceUnavailable, "host_busy"},
		{"other", errors.New("invalid host response"), http.StatusBadGateway, "host_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.writeHostError(rec, "host1", "transcript", transcriptTimeout, tc.err)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body: %v", err)
			}
			if body["reason"] != tc.reason {
				t.Fatalf("reason = %q, want %q", body["reason"], tc.reason)
			}
			wantMsg := tc.err.Error()
			if tc.reason == "host_timeout" {
				wantMsg = "the host did not answer in time"
			}
			if body["error"] != wantMsg {
				t.Fatalf("error = %q, want %q", body["error"], wantMsg)
			}
		})
	}
}
