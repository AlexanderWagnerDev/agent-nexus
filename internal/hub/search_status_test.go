package hub

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// TestSearchReportsOnlySkippedHosts verifies that an online host which answers
// with zero matches is not reported as "not searched", while an offline host
// is. The UI labels every entry of not_searched as not covered by the query.
func TestSearchReportsOnlySkippedHosts(t *testing.T) {
	hub := newSetupHub(t)
	owner, err := hub.store.UserByUsername("owner")
	if err != nil {
		t.Fatal(err)
	}

	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	offlineID, err := hub.store.CreateHost(owner.ID, "offline-host", public, "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateHostConfirmation(offlineID, "111-111"); err != nil {
		t.Fatal(err)
	}
	if err := hub.store.ConfirmHost(owner.ID, offlineID); err != nil {
		t.Fatal(err)
	}

	online := startFakeHost(t, hub, owner, "empty-host")
	online.setAnswer(func(method string, _ json.RawMessage) (any, string) {
		if method == "search" {
			return []protocol.SearchHit{}, ""
		}
		return nil, "unsupported method " + method
	})
	waitFor(t, "online host", func() bool { return hub.server.hostOnline(online.hostID) })

	status, body := hub.do(nil, http.MethodGet, "/api/v1/search?q=needle", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "search")

	var parsed struct {
		Results     []protocol.SearchHit `json:"results"`
		NotSearched map[string]bool      `json:"not_searched"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("search response: %v %s", err, body)
	}
	if len(parsed.Results) != 0 {
		t.Fatalf("expected no hits: %+v", parsed.Results)
	}
	if !parsed.NotSearched["offline-host"] {
		t.Fatalf("offline host is missing from not_searched: %s", body)
	}
	if parsed.NotSearched["empty-host"] {
		t.Fatalf("host answered with an empty result but was reported as not searched: %s", body)
	}
}

// TestMCPToolSearchReportsOnlySkippedHosts covers the same semantics for the
// read-only MCP tool, which returns a list instead of a map.
func TestMCPToolSearchReportsOnlySkippedHosts(t *testing.T) {
	hub := newSetupHub(t)
	owner, err := hub.store.UserByUsername("owner")
	if err != nil {
		t.Fatal(err)
	}

	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	offlineID, err := hub.store.CreateHost(owner.ID, "offline-host", public, "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateHostConfirmation(offlineID, "111-111"); err != nil {
		t.Fatal(err)
	}
	if err := hub.store.ConfirmHost(owner.ID, offlineID); err != nil {
		t.Fatal(err)
	}

	online := startFakeHost(t, hub, owner, "empty-host")
	online.setAnswer(func(method string, _ json.RawMessage) (any, string) {
		if method == "search" {
			return []protocol.SearchHit{}, ""
		}
		return nil, "unsupported method " + method
	})
	waitFor(t, "online host", func() bool { return hub.server.hostOnline(online.hostID) })

	raw, err := hub.server.mcpSearch(t.Context(), owner, "needle")
	if err != nil {
		t.Fatal(err)
	}
	result, _ := raw.(map[string]any)
	skipped, _ := result["not_searched"].([]string)
	if len(skipped) != 1 || skipped[0] != "offline-host" {
		t.Fatalf("not_searched = %v, want [offline-host]", skipped)
	}
}
