package protocol

import (
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"
)

func FuzzEnvelopeDecode(f *testing.F) {
	f.Add([]byte(`{"type":"run.start","run_id":"r1"}`))
	f.Add([]byte(`{"type":"sessions","full":true,"sessions":[]}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		var envelope Envelope
		if json.Unmarshal(raw, &envelope) != nil {
			return
		}
		if _, err := json.Marshal(envelope); err != nil {
			t.Fatalf("decode/encode round trip failed: %v", err)
		}
		// Verification must reject rather than panic on arbitrary fields.
		_ = VerifyEnvelope(make(ed25519.PublicKey, ed25519.PublicKeySize), envelope, time.Now(), 5*time.Minute)
	})
}
