package hub

import (
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestDevicePairingSingleUse(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("owner", "hash", "secret")
	if err := s.CreateDevicePairing(u.ID, "pair-token", time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := s.ConsumeDevicePairing("pair-token")
	if err != nil || got != u.ID {
		t.Fatalf("pairing failed: %q %v", got, err)
	}
	if _, err := s.ConsumeDevicePairing("pair-token"); err == nil {
		t.Fatal("pairing token was accepted twice")
	}
}

func TestVerificationPasskeyStore(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("owner", "hash", "secret")
	cred := &webauthn.Credential{
		ID:        []byte("credential-id"),
		PublicKey: []byte("public-key"),
	}
	if err := s.SavePasskey(u.ID, "test", cred); err != nil {
		t.Fatal(err)
	}
	list, err := s.Passkeys(u.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("passkey not stored: %v %v", list, err)
	}
	if n, err := s.PasskeyCount(u.ID); err != nil || n != 1 {
		t.Fatalf("passkey count = %d, %v", n, err)
	}
}
