package hub

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// TestHostUpdateChannelIsClosedAndCached pins the fix for an unbounded
// host-side release check: the channel used to be copied into the cache key
// verbatim, so a read-only grantee could send arbitrary values and trigger one
// real GitHub request per value.
func TestHostUpdateChannelIsClosedAndCached(t *testing.T) {
	hub := newSetupHub(t)
	signer, err := OpenHubSigner(filepath.Join(t.TempDir(), "signing"))
	if err != nil {
		t.Fatal(err)
	}
	hub.server.SetSigner(signer)
	owner, _ := hub.store.UserByUsername("owner")
	host := startFakeHost(t, hub, owner, "update-channel")
	var mu sync.Mutex
	calls := 0
	host.setAnswer(func(method string, params json.RawMessage) (any, string) {
		if method == "update.check" {
			mu.Lock()
			calls++
			mu.Unlock()
			return protocol.UpdateStatus{Current: "alpha", Latest: "alpha"}, ""
		}
		return nil, "unsupported method " + method
	})
	path := "/api/v1/hosts/" + host.hostID + "/update"

	for _, channel := range []string{"attacker-1", "attacker-2", "Stable", "prerelease;rm"} {
		status, body := hub.do(nil, http.MethodGet, path+"?channel="+url.QueryEscape(channel), nil, nil)
		expectStatus(t, status, http.StatusBadRequest, body, "invalid channel "+channel)
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	if calls != 0 {
		mu.Unlock()
		t.Fatalf("invalid channel strings reached the host %d times", calls)
	}
	mu.Unlock()

	for i := 0; i < 2; i++ {
		status, body := hub.do(nil, http.MethodGet, path+"?channel=stable", nil, nil)
		expectStatus(t, status, http.StatusOK, body, "stable channel")
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	if calls != 1 {
		mu.Unlock()
		t.Fatalf("two checks for the same channel caused %d host round-trips, want 1", calls)
	}
	mu.Unlock()

	status, body := hub.do(nil, http.MethodGet, path+"?channel=prerelease", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "prerelease channel")
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	if calls != 2 {
		mu.Unlock()
		t.Fatalf("a second valid channel caused %d host round-trips, want 2", calls)
	}
	mu.Unlock()
}
