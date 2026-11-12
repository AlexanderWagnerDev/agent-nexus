package hub

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestRunConcurrencyLimits(t *testing.T) {
	hub := newAPITestHub(t, Config{HubName: "Limit Hub", MaxRunsPerUser: 1, MaxRunsPerHost: 1, MaxRunsPerProject: 1})
	if status, body := hub.setup(); status != http.StatusCreated {
		t.Fatalf("setup: %d %s", status, body)
	}
	owner, _ := hub.store.UserByUsername("owner")
	host := startFakeHost(t, hub, owner, "limit-host")
	host.setAnswer(func(method string, params json.RawMessage) (any, string) { return map[string]any{}, "" })

	status, body := hub.do(nil, http.MethodPost, "/api/v1/hosts/"+host.hostID+"/runs", map[string]any{
		"agent": "opencode", "cwd": "/work/one", "mode": "structured",
	}, nil)
	expectStatus(t, status, http.StatusAccepted, body, "first run")
	status, body = hub.do(nil, http.MethodPost, "/api/v1/hosts/"+host.hostID+"/runs", map[string]any{
		"agent": "opencode", "cwd": "/work/two", "mode": "structured",
	}, nil)
	expectStatus(t, status, http.StatusTooManyRequests, body, "second run")
	if !jsonContains(body, `"reason":"run_limit"`) {
		t.Fatalf("limit reason missing: %s", body)
	}
}
