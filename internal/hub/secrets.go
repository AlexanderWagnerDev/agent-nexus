package hub

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const secretPrefix = "enc:v1:"

// SecretBox encrypts small hub secrets at rest (TOTP secrets, the VAPID
// private key). The key lives next to the SQLite database with 0600
// permissions. A database copy without the key file cannot recover them.
type SecretBox struct {
	aead cipher.AEAD
}

// OpenSecretBox loads or creates the hub key file. An existing key must be
// exactly 32 bytes; a damaged key is a hard error rather than silently
// replacing it and losing every stored secret.
func OpenSecretBox(dataDir string) (*SecretBox, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("secret key directory is empty")
	}
	path := filepath.Join(dataDir, "secret.key")
	key, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			return nil, err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, key, 0o600); err != nil {
			return nil, err
		}
		if err := os.Rename(tmp, path); err != nil {
			return nil, err
		}
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secret key %s must be 32 bytes, got %d", path, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead}, nil
}

// Seal encrypts a small secret. Encryption never falls back to plaintext: a
// random-source failure is returned to the caller so the write can be aborted.
func (b *SecretBox) Seal(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if b == nil {
		return "", errors.New("secret box is not initialised")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("encrypt secret: %w", err)
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(value), []byte("agentnexus"))
	return secretPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (b *SecretBox) Open(value string) (string, error) {
	if b == nil || value == "" || !strings.HasPrefix(value, secretPrefix) {
		return value, nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, secretPrefix))
	if err != nil {
		return "", err
	}
	if len(raw) < b.aead.NonceSize() {
		return "", errors.New("encrypted secret is truncated")
	}
	nonce, ciphertext := raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():]
	plain, err := b.aead.Open(nil, nonce, ciphertext, []byte("agentnexus"))
	if err != nil {
		return "", errors.New("cannot decrypt secret: wrong key or corrupted data")
	}
	return string(plain), nil
}
