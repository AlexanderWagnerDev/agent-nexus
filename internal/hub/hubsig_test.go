package hub

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestHubSignerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	signer, err := OpenHubSigner(dir)
	if err != nil {
		t.Fatal(err)
	}
	envelope := protocol.Envelope{Type: "run.stop", RunID: "run-1"}
	if err := signer.Sign(&envelope); err != nil {
		t.Fatal(err)
	}
	public, _ := base64.StdEncoding.DecodeString(signer.PublicKey())
	reloaded, err := OpenHubSigner(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.PublicKey() != signer.PublicKey() {
		t.Fatal("signing key was not stable across reload")
	}
	if err := signer.Sign(&envelope); err != nil {
		t.Fatal(err)
	}
	if _, err := base64.StdEncoding.DecodeString(signer.PublicKey()); err != nil {
		t.Fatalf("public key is not base64: %v", err)
	}
	if err := protocol.VerifyEnvelope(ed25519.PublicKey(public), envelope, time.Now().UTC(), 5*time.Minute); err != nil {
		t.Fatalf("signed envelope did not verify: %v", err)
	}
}
