package hub

import (
	"net/http"
	"strings"
	"testing"
)

func TestMCPReadOnlyTools(t *testing.T) {
	hub := newSetupHub(t)
	owner := hub.testOwner()
	_, raw, err := hub.store.CreateAPIToken(owner.ID, "mcp-test")
	if err != nil {
		t.Fatal(err)
	}
	auth := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+raw)
		// A real MCP client does not know the browser CSRF marker.
		req.Header.Del("X-Nexus-Request")
	}

	call := func(body map[string]any) (int, []byte) {
		return hub.do(nil, http.MethodPost, "/mcp", body, auth)
	}
	status, body := call(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	expectStatus(t, status, http.StatusOK, body, "initialize")
	if !strings.Contains(string(body), "protocolVersion") || !strings.Contains(string(body), "AgentNexus") {
		t.Fatalf("initialize response: %s", body)
	}
	status, body = call(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	expectStatus(t, status, http.StatusOK, body, "tools/list")
	for _, tool := range []string{"list_hosts", "list_sessions", "search_transcripts", "get_transcript_page", "get_git_status"} {
		if !strings.Contains(string(body), `"`+tool+`"`) {
			t.Fatalf("tool %s missing: %s", tool, body)
		}
	}
	for _, forbidden := range []string{"run", "prompt", "stop", "write"} {
		if strings.Contains(string(body), `"`+forbidden+`"`) {
			t.Fatalf("write-looking tool %s exposed: %s", forbidden, body)
		}
	}
	status, body = call(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "list_hosts", "arguments": map[string]any{}}})
	expectStatus(t, status, http.StatusOK, body, "list_hosts")
	if !strings.Contains(string(body), `"isError":false`) && !strings.Contains(string(body), `"isError": false`) {
		t.Fatalf("list_hosts failed: %s", body)
	}
	status, body = call(map[string]any{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": map[string]any{"name": "list_sessions", "arguments": map[string]any{"limit": 5}}})
	expectStatus(t, status, http.StatusOK, body, "list_sessions")
	if !strings.Contains(string(body), "sessions") {
		t.Fatalf("list_sessions result: %s", body)
	}
	status, body = call(map[string]any{"jsonrpc": "2.0", "id": 5, "method": "tools/call", "params": map[string]any{"name": "list_sessions", "arguments": map[string]any{"query": "x"}}})
	expectStatus(t, status, http.StatusOK, body, "list_sessions query")
	status, body = call(map[string]any{"jsonrpc": "2.0", "id": 6, "method": "tools/call", "params": map[string]any{"name": "get_git_status", "arguments": map[string]any{"session_id": "missing"}}})
	expectStatus(t, status, http.StatusOK, body, "missing session")
	if !strings.Contains(string(body), `"isError":true`) && !strings.Contains(string(body), `"isError": true`) {
		t.Fatalf("missing session did not return a tool error: %s", body)
	}

	// Invalid and revoked tokens are rejected before JSON-RPC processing.
	status, body = hub.do(nil, http.MethodPost, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 7, "method": "ping"}, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer invalid")
	})
	expectStatus(t, status, http.StatusUnauthorized, body, "invalid token")
	var token APIToken
	if tokens, err := hub.store.ListAPITokens(owner.ID); err == nil && len(tokens) > 0 {
		token = tokens[0]
	}
	if err := hub.store.RevokeAPIToken(owner.ID, token.ID); err != nil {
		t.Fatal(err)
	}
	status, body = call(map[string]any{"jsonrpc": "2.0", "id": 8, "method": "ping"})
	expectStatus(t, status, http.StatusUnauthorized, body, "revoked token")

	// Cookie-authenticated writes still need the browser marker.
	status, body = hub.do(nil, http.MethodPost, "/api/v1/me/settings", map[string]any{}, func(req *http.Request) {
		req.Header.Del("X-Nexus-Request")
	})
	expectStatus(t, status, http.StatusForbidden, body, "cookie request without marker")
}
