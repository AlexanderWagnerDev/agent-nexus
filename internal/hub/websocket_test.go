package hub

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/coder/websocket"
)

func wsURL(hub *apiTestHub, path string) string {
	u, _ := url.Parse(hub.http.URL)
	u.Scheme = "ws"
	if i := strings.IndexByte(path, '?'); i >= 0 {
		u.Path = path[:i]
		u.RawQuery = path[i+1:]
	} else {
		u.Path = path
	}
	return u.String()
}

func (h *apiTestHub) readEnvelope(t *testing.T, conn *websocket.Conn, timeout time.Duration) protocol.Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read envelope: %v", err)
	}
	var envelope protocol.Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return envelope
}

func (h *apiTestHub) writeEnvelope(t *testing.T, conn *websocket.Conn, envelope protocol.Envelope) {
	t.Helper()
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write envelope: %v", err)
	}
}

func TestHostWebSocketChallengeAndReplay(t *testing.T) {
	hub := newSetupHub(t)
	owner, _ := hub.store.UserByUsername("owner")
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := hub.store.CreateHost(owner.ID, "ws-host", public, "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateHostConfirmation(hostID, "111-222"); err != nil {
		t.Fatal(err)
	}
	if err := hub.store.ConfirmHost(owner.ID, hostID); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(hub, "/v1/host/connect?host_id="+hostID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	hello := hub.readEnvelope(t, conn, 5*time.Second)
	if hello.Type != "hello" || hello.Nonce == "" {
		t.Fatalf("hello: %+v", hello)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte(hello.Nonce)))
	hub.writeEnvelope(t, conn, protocol.Envelope{
		Type: "auth", Protocol: protocol.Version, Signature: signature,
		HostName: "ws-host", OS: "linux", Arch: "amd64", Version: "alpha",
	})
	auth := hub.readEnvelope(t, conn, 5*time.Second)
	if auth.Type != "auth.ok" {
		t.Fatalf("auth: %+v", auth)
	}

	// A fresh connection gets a new nonce; the old signature must be rejected.
	replay, _, err := websocket.Dial(ctx, wsURL(hub, "/v1/host/connect?host_id="+hostID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close(websocket.StatusNormalClosure, "done")
	if hello2 := hub.readEnvelope(t, replay, 5*time.Second); hello2.Nonce == "" || hello2.Nonce == hello.Nonce {
		t.Fatalf("nonce was not fresh: %+v", hello2)
	}
	hub.writeEnvelope(t, replay, protocol.Envelope{Type: "auth", Protocol: protocol.Version, Signature: signature})
	if err := hub.readEnvelope(t, replay, 5*time.Second); err.Type != "auth.error" {
		t.Fatalf("replayed signature accepted: %+v", err)
	}

	// An unknown host is rejected before the upgrade.
	unknown, _, err := websocket.Dial(ctx, wsURL(hub, "/v1/host/connect?host_id=missing"), nil)
	if err == nil {
		unknown.Close(websocket.StatusNormalClosure, "unexpected")
		t.Fatal("unknown host was accepted")
	}
}

func TestHostWebSocketRequiresConfirmation(t *testing.T) {
	hub := newSetupHub(t)
	owner, _ := hub.store.UserByUsername("owner")
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := hub.store.CreateHost(owner.ID, "pending-host", public, "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateHostConfirmation(hostID, "333-444"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(hub, "/v1/host/connect?host_id="+hostID), nil)
	if err == nil {
		conn.Close(websocket.StatusNormalClosure, "unexpected")
		t.Fatal("unconfirmed host was accepted")
	}
}

func TestClientWebSocketRequiresAuthAndRejectsForeignRun(t *testing.T) {
	hub := newSetupHub(t)
	base, _ := url.Parse(hub.http.URL)
	cookies := hub.client.Jar.Cookies(base)
	var cookie string
	for _, c := range cookies {
		if c.Name == cookieName {
			cookie = c.Name + "=" + c.Value
		}
	}
	if cookie == "" {
		t.Fatal("session cookie missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(hub, "/api/v1/ws"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {cookie}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	ready := hub.readEnvelope(t, conn, 5*time.Second)
	if ready.Type != "ready" {
		t.Fatalf("ready: %+v", ready)
	}
	hub.writeEnvelope(t, conn, protocol.Envelope{Type: "subscribe", RunID: "does-not-exist"})
	reply := hub.readEnvelope(t, conn, 5*time.Second)
	if reply.Type != "error" || !strings.Contains(reply.Error, "run not found") {
		t.Fatalf("foreign run subscribe: %+v", reply)
	}

	unauth, _, err := websocket.Dial(ctx, wsURL(hub, "/api/v1/ws"), nil)
	if err == nil {
		unauth.Close(websocket.StatusNormalClosure, "unexpected")
		t.Fatal("unauthenticated WebSocket was accepted")
	}
}
func TestClientWebSocketRejectsOversizedMessages(t *testing.T) {
	hub := newSetupHub(t)
	base, _ := url.Parse(hub.http.URL)
	var cookie string
	for _, c := range hub.client.Jar.Cookies(base) {
		if c.Name == cookieName {
			cookie = c.Name + "=" + c.Value
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(hub, "/api/v1/ws"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {cookie}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	if ready := hub.readEnvelope(t, conn, 5*time.Second); ready.Type != "ready" {
		t.Fatalf("ready: %+v", ready)
	}
	huge := strings.Repeat("x", clientReadLimit+1024)
	if err := conn.Write(ctx, websocket.MessageText, []byte(huge)); err != nil {
		return // the peer already closed the connection, which is the expected outcome
	}
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("oversized client message did not close the connection")
	}
}
