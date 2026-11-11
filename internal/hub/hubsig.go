package hub

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// HubSigner is the hub's command-signing identity. It is independent of the
// host identity keys and is pinned by hosts during pairing or first connection.
type HubSigner struct {
	private ed25519.PrivateKey
	public  ed25519.PublicKey
}

// OpenHubSigner loads or creates the hub signing key next to the database.
// Losing this file invalidates signed commands until hosts re-pin the key;
// a damaged file fails startup instead of silently rotating.
func OpenHubSigner(dataDir string) (*HubSigner, error) {
	if dataDir == "" {
		return nil, errors.New("hub signing key directory is empty")
	}
	path := filepath.Join(dataDir, "hub-signing.key")
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		public, private, genErr := ed25519.GenerateKey(rand.Reader)
		if genErr != nil {
			return nil, genErr
		}
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			return nil, err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, private, 0o600); err != nil {
			return nil, err
		}
		if err := os.Rename(tmp, path); err != nil {
			return nil, err
		}
		return &HubSigner{private: private, public: public}, nil
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("hub signing key %s must be %d bytes, got %d", path, ed25519.PrivateKeySize, len(raw))
	}
	private := ed25519.PrivateKey(raw)
	public, ok := private.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("invalid hub signing key")
	}
	return &HubSigner{private: private, public: public}, nil
}

func (s *HubSigner) Sign(envelope *protocol.Envelope) error {
	if s == nil {
		return nil
	}
	return protocol.SignEnvelope(s.private, envelope)
}

func (s *HubSigner) PublicKey() string {
	if s == nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(s.public)
}
