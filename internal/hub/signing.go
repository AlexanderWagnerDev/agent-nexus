package hub

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

const deviceSignatureWindow = 2 * time.Minute

var (
	errSignedCommandRequired = errors.New("device signature required")
	errSignatureInvalid      = errors.New("invalid device signature")
	errSignatureReplay       = errors.New("replayed device signature")
	errSignatureStale        = errors.New("device signature outside the allowed time window")
)

// replayGuard keeps nonces per device for a short window. It is bounded by the
// practical number of writes a device can issue inside that window.
type replayGuard struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newReplayGuard() *replayGuard {
	return &replayGuard{seen: map[string]time.Time{}}
}

func (g *replayGuard) accept(deviceID, nonce string, timestamp int64, now time.Time) error {
	if nonce == "" {
		return errSignedCommandRequired
	}
	signedAt := time.Unix(timestamp, 0)
	if signedAt.Before(now.Add(-deviceSignatureWindow)) || signedAt.After(now.Add(deviceSignatureWindow)) {
		return errSignatureStale
	}
	key := deviceID + "|" + nonce
	g.mu.Lock()
	defer g.mu.Unlock()
	for candidate, expiry := range g.seen {
		if expiry.Before(now) {
			delete(g.seen, candidate)
		}
	}
	if _, exists := g.seen[key]; exists {
		return errSignatureReplay
	}
	g.seen[key] = now.Add(deviceSignatureWindow)
	return nil
}

// signingRequired is true once a device registers a key, or globally when the
// hub operator enables signed-command enforcement. Cookie-only browsers keep
// working unless an operator explicitly opts in.
func (s *Server) signingRequired(deviceID string) bool {
	if s.cfg.RequireSignedCommands {
		return true
	}
	_, _, err := s.store.DevicePublicKey(deviceID)
	// Fail closed on a database error: an operator who registered keys must not
	// lose enforcement because the lookup was temporarily unavailable.
	return err == nil || !errors.Is(err, sql.ErrNoRows)
}

func canonicalDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func httpCommandCanonical(r *http.Request, body []byte, timestamp int64, nonce string) string {
	return strings.Join([]string{
		"http", r.Method, r.URL.Path, canonicalDigest(string(body)),
		strconv.FormatInt(timestamp, 10), nonce,
	}, "\n")
}

func wsCommandCanonical(msg protocol.ClientEnvelope, timestamp int64, nonce string) string {
	payload := ""
	switch msg.Type {
	case "prompt":
		payload = msg.Text
	case "approval":
		payload = msg.ApprovalID + "\n" + msg.Decision
	case "input":
		payload = base64.StdEncoding.EncodeToString(msg.Data)
	case "interrupt":
		payload = ""
	}
	return strings.Join([]string{
		"ws", msg.Type, msg.RunID, canonicalDigest(payload),
		strconv.FormatInt(timestamp, 10), nonce,
	}, "\n")
}

func verifyDeviceSignature(algorithm, publicKey, signature, canonical string) error {
	publicRaw, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil {
		return fmt.Errorf("%w: public key encoding", errSignatureInvalid)
	}
	signatureRaw, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("%w: signature encoding", errSignatureInvalid)
	}
	switch strings.ToLower(algorithm) {
	case "ed25519":
		if len(publicRaw) != ed25519.PublicKeySize || !ed25519.Verify(ed25519.PublicKey(publicRaw), []byte(canonical), signatureRaw) {
			return errSignatureInvalid
		}
	case "p256":
		x, y := elliptic.Unmarshal(elliptic.P256(), publicRaw)
		if x == nil {
			return fmt.Errorf("%w: p256 public key", errSignatureInvalid)
		}
		key := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
		digest := sha256.Sum256([]byte(canonical))
		// WebCrypto emits IEEE P1363 (r||s); native clients may emit ASN.1.
		if !ecdsa.VerifyASN1(key, digest[:], signatureRaw) {
			if len(signatureRaw) != 64 {
				return errSignatureInvalid
			}
			r := new(big.Int).SetBytes(signatureRaw[:32])
			s := new(big.Int).SetBytes(signatureRaw[32:])
			if !ecdsa.Verify(key, digest[:], r, s) {
				return errSignatureInvalid
			}
		}
	default:
		return fmt.Errorf("%w: unsupported algorithm %q", errSignatureInvalid, algorithm)
	}
	return nil
}

func (s *Server) verifyHTTPDeviceSignature(w http.ResponseWriter, r *http.Request, deviceID string, body []byte) bool {
	if !s.signingRequired(deviceID) {
		return true
	}
	timestamp, err := strconv.ParseInt(r.Header.Get("X-Nexus-Device-Timestamp"), 10, 64)
	if err != nil {
		writeError(w, http.StatusForbidden, "device signature required")
		return false
	}
	nonce := r.Header.Get("X-Nexus-Device-Nonce")
	algorithm, publicKey, err := s.store.DevicePublicKey(deviceID)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusForbidden, "device signature required")
		return false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	canonical := httpCommandCanonical(r, body, timestamp, nonce)
	if err := verifyDeviceSignature(algorithm, publicKey, r.Header.Get("X-Nexus-Device-Signature"), canonical); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return false
	}
	if err := s.replay.accept(deviceID, nonce, timestamp, time.Now()); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return false
	}
	return true
}

func (s *Server) verifyWSDeviceSignature(deviceID string, msg protocol.ClientEnvelope) error {
	if !s.signingRequired(deviceID) {
		return nil
	}
	algorithm, publicKey, err := s.store.DevicePublicKey(deviceID)
	if err == sql.ErrNoRows {
		return errSignedCommandRequired
	}
	if err != nil {
		return err
	}
	canonical := wsCommandCanonical(msg, msg.DeviceTimestamp, msg.DeviceNonce)
	if err := verifyDeviceSignature(algorithm, publicKey, msg.DeviceSignature, canonical); err != nil {
		return err
	}
	return s.replay.accept(deviceID, msg.DeviceNonce, msg.DeviceTimestamp, time.Now())
}

func signedWriteMessage(messageType string) bool {
	switch messageType {
	case "prompt", "approval", "input", "interrupt":
		return true
	default:
		return false
	}
}
