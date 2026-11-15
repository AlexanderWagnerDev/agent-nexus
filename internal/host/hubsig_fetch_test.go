package host

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A malformed key must be rejected before it is pinned: the daemon only
// fetches the hub key while none is stored, so a bad pin disables every
// reconnect until an operator edits the config.
func TestFetchHubPublicKeyRejectsWrongLength(t *testing.T) {
	key := "short-key"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"public_key": base64.StdEncoding.EncodeToString([]byte(key)),
		})
	}))
	defer srv.Close()

	if _, err := FetchHubPublicKey(context.Background(), srv.URL); err == nil {
		t.Fatal("a key that is not 32 bytes must be rejected")
	}

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key = string(pub)
	got, err := FetchHubPublicKey(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("a valid Ed25519 key must be accepted: %v", err)
	}
	if got != base64.StdEncoding.EncodeToString(pub) {
		t.Fatal("the fetched key changed")
	}
}
