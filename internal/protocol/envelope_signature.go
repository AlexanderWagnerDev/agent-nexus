package protocol

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

// SignEnvelope signs the canonical JSON representation of an envelope. The
// signature field is excluded from the signed bytes. A fresh nonce and issue
// time are added when missing so receivers can reject stale or replayed
// commands.
func SignEnvelope(private ed25519.PrivateKey, envelope *Envelope) error {
	if len(private) != ed25519.PrivateKeySize {
		return errors.New("invalid signing key")
	}
	if envelope.Nonce == "" {
		nonce := make([]byte, 18)
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		envelope.Nonce = base64.RawURLEncoding.EncodeToString(nonce)
	}
	if envelope.IssuedAt == 0 {
		envelope.IssuedAt = time.Now().UTC().Unix()
	}
	payload, err := EnvelopePayload(*envelope)
	if err != nil {
		return err
	}
	envelope.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, payload))
	return nil
}

// VerifyEnvelope checks the signature and issue time. Nonce uniqueness is
// enforced by the receiver because only it knows which nonces it has seen.
func VerifyEnvelope(public ed25519.PublicKey, envelope Envelope, now time.Time, maxSkew time.Duration) error {
	if len(public) != ed25519.PublicKeySize {
		return errors.New("invalid hub public key")
	}
	if envelope.Signature == "" {
		return errors.New("missing command signature")
	}
	if envelope.Nonce == "" {
		return errors.New("missing command nonce")
	}
	if envelope.IssuedAt == 0 {
		return errors.New("missing command timestamp")
	}
	issued := time.Unix(envelope.IssuedAt, 0)
	if skew := now.Sub(issued); skew > maxSkew || skew < -maxSkew {
		return errors.New("command timestamp outside the accepted window")
	}
	payload, err := EnvelopePayload(envelope)
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil {
		return errors.New("invalid command signature encoding")
	}
	if !ed25519.Verify(public, payload, signature) {
		return errors.New("invalid command signature")
	}
	return nil
}

// EnvelopePayload returns the deterministic bytes covered by a command
// signature. Struct field order is stable in encoding/json.
func EnvelopePayload(envelope Envelope) ([]byte, error) {
	envelope.Signature = ""
	return json.Marshal(envelope)
}
