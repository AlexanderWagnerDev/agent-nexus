package hub

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func registerTestDeviceKey(t *testing.T, hub *apiTestHub, deviceID string) ed25519.PrivateKey {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	status, body := hub.do(nil, http.MethodPut, "/api/v1/devices/"+deviceID+"/key", map[string]string{
		"algorithm": "ed25519", "public_key": base64.StdEncoding.EncodeToString(public),
	}, nil)
	expectStatus(t, status, http.StatusOK, body, "register device key")
	return private
}

func TestDeviceSignedStopAndReplayProtection(t *testing.T) {
	hub := newSetupHub(t)
	me, body := hub.do(nil, http.MethodGet, "/api/v1/me", nil, nil)
	expectStatus(t, me, http.StatusOK, body, "current user")
	var session struct {
		Device Device `json:"device"`
	}
	if err := json.Unmarshal(body, &session); err != nil {
		t.Fatal(err)
	}
	key := registerTestDeviceKey(t, hub, session.Device.ID)

	timestamp := time.Now().Unix()
	nonce := "nonce-stop-1"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/run-1/stop", nil)
	req.Header.Set("X-Nexus-Device-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-Nexus-Device-Nonce", nonce)
	canonical := httpCommandCanonical(req, nil, timestamp, nonce)
	req.Header.Set("X-Nexus-Device-Signature", base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(canonical))))

	rec := httptest.NewRecorder()
	if !hub.server.verifyHTTPDeviceSignature(rec, req, session.Device.ID, nil) {
		t.Fatalf("valid signature rejected: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	if hub.server.verifyHTTPDeviceSignature(rec, req, session.Device.ID, nil) {
		t.Fatal("replayed nonce was accepted")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("replay status %d", rec.Code)
	}
}

func TestDeviceSignedWebSocketPrompt(t *testing.T) {
	hub := newSetupHub(t)
	_, body := hub.do(nil, http.MethodGet, "/api/v1/me", nil, nil)
	var session struct {
		Device Device `json:"device"`
	}
	if err := json.Unmarshal(body, &session); err != nil {
		t.Fatal(err)
	}
	key := registerTestDeviceKey(t, hub, session.Device.ID)

	msg := protocol.ClientEnvelope{Type: "prompt", RunID: "run-1", Text: "continue", DeviceTimestamp: time.Now().Unix(), DeviceNonce: "nonce-prompt-1"}
	canonical := wsCommandCanonical(msg, msg.DeviceTimestamp, msg.DeviceNonce)
	msg.DeviceSignature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(canonical)))
	if err := hub.server.verifyWSDeviceSignature(session.Device.ID, msg); err != nil {
		t.Fatalf("valid prompt signature rejected: %v", err)
	}
	if err := hub.server.verifyWSDeviceSignature(session.Device.ID, msg); err != errSignatureReplay {
		t.Fatalf("replayed prompt signature: %v", err)
	}
	unsigned := protocol.ClientEnvelope{Type: "approval", RunID: "run-1", ApprovalID: "a1", Decision: "once"}
	if err := hub.server.verifyWSDeviceSignature(session.Device.ID, unsigned); err == nil {
		t.Fatal("unsigned approval was accepted after key registration")
	}
}

func TestP256SignatureMatchesWebCryptoShape(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicRaw := elliptic.Marshal(elliptic.P256(), key.X, key.Y)
	canonical := "ws\nprompt\nrun-1\nabc\n1\nn"
	digest := sha256.Sum256([]byte(canonical))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 64)
	r.FillBytes(raw[:32])
	s.FillBytes(raw[32:])
	if err := verifyDeviceSignature("p256",
		base64.StdEncoding.EncodeToString(publicRaw),
		base64.StdEncoding.EncodeToString(raw),
		canonical); err != nil {
		t.Fatalf("P1363 signature rejected: %v", err)
	}
}
