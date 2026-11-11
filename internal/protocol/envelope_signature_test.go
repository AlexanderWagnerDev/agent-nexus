package protocol

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestEnvelopeSignature(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	envelope := Envelope{Type: "run.prompt", RunID: "run-1", Message: "hello"}
	if err := SignEnvelope(private, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Nonce == "" || envelope.IssuedAt == 0 || envelope.Signature == "" {
		t.Fatalf("signature fields missing: %+v", envelope)
	}
	if err := VerifyEnvelope(public, envelope, time.Now().UTC(), 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	tampered := envelope
	tampered.Message = "tampered"
	if err := VerifyEnvelope(public, tampered, time.Now().UTC(), 5*time.Minute); err == nil {
		t.Fatal("tampered envelope accepted")
	}
	stale := envelope
	stale.IssuedAt = time.Now().Add(-time.Hour).Unix()
	if err := VerifyEnvelope(public, stale, time.Now().UTC(), 5*time.Minute); err == nil {
		t.Fatal("stale envelope accepted")
	}
	unsigned := envelope
	unsigned.Signature = ""
	if err := VerifyEnvelope(public, unsigned, time.Now().UTC(), 5*time.Minute); err == nil {
		t.Fatal("unsigned envelope accepted")
	}
}
