package host

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/coder/websocket"
)

type echoTestAdapter struct{}

func (echoTestAdapter) Name() string { return "echo" }
func (echoTestAdapter) Detect(context.Context) (protocol.AgentInfo, error) {
	return protocol.AgentInfo{Name: "echo", Version: "test", Supported: true}, nil
}
func (echoTestAdapter) ListSessions(context.Context) ([]protocol.Session, error) { return nil, nil }
func (echoTestAdapter) Transcript(context.Context, string, int, int) (protocol.Transcript, error) {
	return protocol.Transcript{}, nil
}
func (echoTestAdapter) Search(context.Context, string, int) ([]protocol.SearchHit, error) {
	return nil, nil
}
func (echoTestAdapter) BuildCommand(Action) (Command, error) {
	if runtime.GOOS == "windows" {
		shell := os.Getenv("COMSPEC")
		if shell == "" {
			shell = `C:\Windows\System32\cmd.exe`
		}
		return Command{Path: shell, Args: []string{"/D", "/S", "/C", "echo echo-hello"}}, nil
	}
	return Command{Path: "/bin/sh", Args: []string{"-c", "echo echo-hello"}}, nil
}

// TestDaemonHubLinkAndTerminalRun drives the real host link against a fake hub:
// authentication, inventory, session snapshot, a permitted run and a policy
// rejection. It also proves that signature verification is separate from
// policy enforcement.
func TestDaemonHubLinkAndTerminalRun(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("NEXUSD_CONFIG", filepath.Join(t.TempDir(), "nexusd.json"))
	root := t.TempDir()
	outside := t.TempDir()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		HubURL:     "",
		HostID:     "test-host",
		HostName:   "test-host",
		PrivateKey: base64.StdEncoding.EncodeToString(private),
		Policy: Policy{
			AllowedRoots: []string{root}, AllowNew: true, AllowFork: true,
			ForbiddenArgs: DefaultForbiddenArgs(),
		},
	}
	daemon := NewDaemonWithAdapters(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), []Adapter{echoTestAdapter{}})

	runStarted := make(chan struct{}, 1)
	outputSeen := make(chan string, 8)
	policyRejected := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/hub-key" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/v1/host/connect" {
			http.NotFound(w, r)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, envelopeBytes(protocol.Envelope{Type: "hello", Protocol: protocol.Version, Nonce: "nonce-test"}))
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var msg protocol.Envelope
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "auth":
				_ = conn.Write(ctx, websocket.MessageText, envelopeBytes(protocol.Envelope{Type: "auth.ok", Protocol: protocol.Version}))
				// A valid run inside the allowed root.
				spec := protocol.RunSpec{RunID: "run-ok", Agent: "echo", Action: protocol.ActionNew, Cwd: root, Mode: "terminal"}
				_ = conn.Write(ctx, websocket.MessageText, envelopeBytes(protocol.Envelope{Type: "run.start", Run: &spec}))
				// A signed-looking but policy-violating run outside the root.
				bad := protocol.RunSpec{RunID: "run-bad", Agent: "echo", Action: protocol.ActionNew, Cwd: outside, Mode: "terminal"}
				_ = conn.Write(ctx, websocket.MessageText, envelopeBytes(protocol.Envelope{Type: "run.start", Run: &bad}))
			case "run.started":
				select {
				case runStarted <- struct{}{}:
				default:
				}
			case "run.output":
				select {
				case outputSeen <- string(msg.Data):
				default:
				}
			case "run.error":
				select {
				case policyRejected <- msg.Error:
				default:
				}

			}
		}
	}))
	defer server.Close()
	cfg.HubURL = server.URL
	daemon.cfg.HubURL = server.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx) }()

	select {
	case <-runStarted:
	case <-time.After(15 * time.Second):
		t.Fatal("run did not start")
	}
	deadline := time.After(15 * time.Second)
	var output strings.Builder
	for !strings.Contains(output.String(), "echo-hello") {
		select {
		case chunk := <-outputSeen:
			output.WriteString(chunk)
		case <-deadline:
			t.Fatalf("run produced no expected output: %q", output.String())
		}
	}
	select {
	case errText := <-policyRejected:
		if !strings.Contains(errText, "allowed roots") {
			t.Fatalf("policy error: %q", errText)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("out-of-root run was not rejected")
	}
	cancel()
	<-done
	_ = os.Remove(filepath.Join(root, "runtime.json"))
}

func envelopeBytes(envelope protocol.Envelope) []byte {
	data, _ := json.Marshal(envelope)
	return data
}
