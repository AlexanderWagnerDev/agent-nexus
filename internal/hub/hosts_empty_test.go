package hub

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The browser's Home, Hosts and new-session views call `hosts.filter(...)`.
// A hub without visible hosts must therefore answer with an empty array, not
// null, or the first page after setup fails to render.
func TestHostsEndpointReturnsEmptyArray(t *testing.T) {
	hub := newSetupHub(t)
	status, body := hub.do(nil, http.MethodGet, "/api/v1/hosts", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "list hosts")

	var payload struct {
		Hosts []map[string]any `json:"hosts"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode hosts: %v (%s)", err, body)
	}
	if payload.Hosts == nil {
		t.Fatalf("hosts must be an empty array, got null: %s", body)
	}
	if len(payload.Hosts) != 0 {
		t.Fatalf("unexpected hosts for a fresh hub: %s", body)
	}
}
