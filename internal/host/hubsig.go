package host

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

const hubSignatureSkew = 5 * time.Minute

type hubKeyResponse struct {
	PublicKey string `json:"public_key"`
	Error     string `json:"error"`
}

// FetchHubPublicKey reads the hub's command-signing key. Pairing uses this to
// pin the key; an existing host also calls it on startup when it was paired
// before command signing existed (trust on first use).
func FetchHubPublicKey(ctx context.Context, hubURL string) (string, error) {
	url := strings.TrimSuffix(hubURL, "/") + "/api/v1/hub-key"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	var parsed hubKeyResponse
	_ = json.Unmarshal(data, &parsed)
	if resp.StatusCode != http.StatusOK {
		if parsed.Error == "" {
			parsed.Error = resp.Status
		}
		return "", errors.New(parsed.Error)
	}
	if _, err := base64.StdEncoding.DecodeString(parsed.PublicKey); err != nil {
		return "", fmt.Errorf("hub returned an invalid public key: %w", err)
	}
	return parsed.PublicKey, nil
}

func (d *Daemon) verifyHubEnvelope(envelope *protocol.Envelope) error {
	if d.cfg.HubPublicKey == "" {
		return nil
	}
	switch envelope.Type {
	case "hello", "auth.error":
		// These are sent before the authenticated link exists and cannot carry
		// a trusted signature.
		return nil
	}
	public, err := base64.StdEncoding.DecodeString(d.cfg.HubPublicKey)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return errors.New("pinned hub public key is invalid")
	}
	if err := protocol.VerifyEnvelope(public, *envelope, time.Now().UTC(), hubSignatureSkew); err != nil {
		return err
	}
	d.hubNonceMu.Lock()
	defer d.hubNonceMu.Unlock()
	if d.hubNonces == nil {
		d.hubNonces = map[string]time.Time{}
	}
	now := time.Now()
	for nonce, seen := range d.hubNonces {
		if now.Sub(seen) > 2*hubSignatureSkew {
			delete(d.hubNonces, nonce)
		}
	}
	if _, seen := d.hubNonces[envelope.Nonce]; seen {
		return errors.New("replayed hub command")
	}
	d.hubNonces[envelope.Nonce] = now
	return nil
}
