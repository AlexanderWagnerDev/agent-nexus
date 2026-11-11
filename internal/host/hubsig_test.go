package host

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestVerifyHubEnvelope(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{cfg: Config{HubPublicKey: base64.StdEncoding.EncodeToString(public)}, hubNonces: map[string]time.Time{}}
	envelope := protocol.Envelope{Type: "run.prompt", RunID: "run-1", Message: "hello"}
	if err := protocol.SignEnvelope(private, &envelope); err != nil {
		t.Fatal(err)
	}
	if err := d.verifyHubEnvelope(&envelope); err != nil {
		t.Fatal(err)
	}
	if err := d.verifyHubEnvelope(&envelope); err == nil {
		t.Fatal("replayed command accepted")
	}
	next := protocol.Envelope{Type: "run.prompt", RunID: "run-2", Message: "hello"}
	if err := protocol.SignEnvelope(private, &next); err != nil {
		t.Fatal(err)
	}
	next.Message = "tampered"
	if err := d.verifyHubEnvelope(&next); err == nil {
		t.Fatal("tampered command accepted")
	}
	legacy := &Daemon{cfg: Config{}}
	if err := legacy.verifyHubEnvelope(&next); err != nil {
		t.Fatalf("legacy unsigned host rejected: %v", err)
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	wrong := &Daemon{cfg: Config{HubPublicKey: base64.StdEncoding.EncodeToString(other)}, hubNonces: map[string]time.Time{}}
	if err := wrong.verifyHubEnvelope(&envelope); err == nil {
		t.Fatal("command signed by another key accepted")
	}
}
