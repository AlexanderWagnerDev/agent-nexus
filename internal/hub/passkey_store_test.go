package hub

import (
	"errors"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
)

// A WebAuthn credential ID is globally unique. The library leaves the
// cross-account check to the relying party, so the store must reject a second
// account trying to reuse an existing ID; otherwise it overwrites the victim's
// public key and turns passkey login into account takeover.
func TestSavePasskeyRejectsCrossAccountCredentialID(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	alice, err := hub.store.CreateUserWithRole("alice", "hash", "secret", "member")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := hub.store.CreateUserWithRole("bob", "hash", "secret", "member")
	if err != nil {
		t.Fatal(err)
	}
	credentialID := []byte("shared-credential-id")
	if err := hub.store.SavePasskey(alice.ID, "alice", &webauthn.Credential{
		ID: credentialID, PublicKey: []byte("alice-public-key"),
	}); err != nil {
		t.Fatal(err)
	}

	err = hub.store.SavePasskey(bob.ID, "bob", &webauthn.Credential{
		ID: credentialID, PublicKey: []byte("bob-public-key"),
	})
	if !errors.Is(err, errPasskeyOwned) {
		t.Fatalf("cross-account credential ID was accepted: %v", err)
	}

	aliceCreds, err := hub.store.Passkeys(alice.ID)
	if err != nil || len(aliceCreds) != 1 {
		t.Fatalf("alice credentials: %v %v", aliceCreds, err)
	}
	if string(aliceCreds[0].PublicKey) != "alice-public-key" {
		t.Fatalf("alice public key was overwritten: %q", aliceCreds[0].PublicKey)
	}
	bobCreds, err := hub.store.Passkeys(bob.ID)
	if err != nil || len(bobCreds) != 0 {
		t.Fatalf("bob gained a credential: %v %v", bobCreds, err)
	}

	// The same account may rotate the data for its own credential ID.
	if err := hub.store.SavePasskey(alice.ID, "alice renamed", &webauthn.Credential{
		ID: credentialID, PublicKey: []byte("alice-rotated-key"),
	}); err != nil {
		t.Fatalf("same-account update rejected: %v", err)
	}
	aliceCreds, _ = hub.store.Passkeys(alice.ID)
	if len(aliceCreds) != 1 || string(aliceCreds[0].PublicKey) != "alice-rotated-key" {
		t.Fatalf("same-account update did not apply: %v", aliceCreds)
	}
}
