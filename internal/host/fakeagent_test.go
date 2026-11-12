package host

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// TestMain doubles as a fake agent process: when NEXUS_TEST_AGENT is set, the
// test binary re-executes itself in the requested agent role. This keeps the
// structured-run tests hermetic without shipping a helper binary.
func TestMain(m *testing.M) {
	switch os.Getenv("NEXUS_TEST_AGENT") {
	case "opencode":
		os.Exit(runFakeOpenCode(os.Args[1:]))
	case "codex":
		os.Exit(runFakeCodex(os.Args[1:]))
	case "acp":
		os.Exit(runFakeACP(os.Args[1:]))
	case "tree":
		os.Exit(runFakeTree())
	}
	os.Exit(m.Run())
}

func runFakeOpenCode(args []string) int {
	port := 0
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--port" {
			port, _ = strconv.Atoi(args[i+1])
		}
	}
	if port == 0 {
		fmt.Fprintln(os.Stderr, "fake opencode: missing --port")
		return 2
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		writeFakeJSON(w, map[string]any{"data": map[string]any{"id": "fake-session"}})
	})
	mux.HandleFunc("/api/model", func(w http.ResponseWriter, r *http.Request) {
		writeFakeJSON(w, map[string]any{"data": []any{map[string]any{"id": "fake-model", "providerID": "fake", "name": "Fake Model", "enabled": true}}})
	})
	mux.HandleFunc("/api/agent", func(w http.ResponseWriter, r *http.Request) {
		writeFakeJSON(w, map[string]any{"data": []any{map[string]any{"id": "build", "name": "Build"}}})
	})
	mux.HandleFunc("/api/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, `data: {"type":"session.status","data":{"sessionID":"fake-session","status":{"type":"idle"}}}`+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/session/") {
			if strings.HasSuffix(r.URL.Path, "/fork") {
				writeFakeJSON(w, map[string]any{"data": map[string]any{"id": "fake-forked-session"}})
				return
			}
			if strings.HasSuffix(r.URL.Path, "/interrupt") || strings.HasSuffix(r.URL.Path, "/prompt") ||
				strings.HasSuffix(r.URL.Path, "/model") || strings.HasSuffix(r.URL.Path, "/agent") {
				writeFakeJSON(w, map[string]any{"data": map[string]any{}})
				return
			}
			writeFakeJSON(w, map[string]any{"data": map[string]any{"agent": "build", "model": map[string]any{"id": "fake-model", "providerID": "fake"}}})
			return
		}
		http.NotFound(w, r)
	})
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake opencode listen:", err)
		return 1
	}
	fmt.Println("server password test-password")
	fmt.Printf("server listening on http://127.0.0.1:%d\n", port)
	_ = http.Serve(ln, mux)
	return 0
}

func writeFakeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func runFakeCodex(args []string) int {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		var req struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil || req.ID == nil {
			continue
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{"userAgent": "fake-codex"}
		case "thread/start":
			result = map[string]any{"thread": map[string]any{"id": "fake-thread"}}
		case "thread/resume":
			result = map[string]any{"thread": map[string]any{"id": "fake-thread"}}
		case "thread/fork":
			result = map[string]any{"thread": map[string]any{"id": "fake-forked-thread"}}
		case "turn/start":
			result = map[string]any{"turn": map[string]any{"id": "fake-turn"}}
		case "model/list":
			result = map[string]any{"data": []any{map[string]any{"id": "fake", "model": "fake-model", "displayName": "Fake", "isDefault": true, "inputModalities": []string{"text"}}}}
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
	}
	return 0
}

// fakeACPAgent implements only the methods this client calls. Embedding the
// interface keeps the fake small; an unexpected call panics loudly in tests.
type fakeACPAgent struct {
	acp.Agent
	conn     *acp.AgentSideConnection
	sessions map[string]bool

	mu       sync.Mutex
	modes    *acp.SessionModeState
	configs  []acp.SessionConfigOption
	setCalls []string
	prompts  [][]acp.ContentBlock
}

func (a *fakeACPAgent) recordSet(call string) {
	a.mu.Lock()
	a.setCalls = append(a.setCalls, call)
	a.mu.Unlock()
}

func (a *fakeACPAgent) setSessionCalls() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.setCalls...)
}

func (a *fakeACPAgent) lastPrompt() []acp.ContentBlock {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.prompts) == 0 {
		return nil
	}
	return a.prompts[len(a.prompts)-1]
}

func (a *fakeACPAgent) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{
			PromptCapabilities: acp.PromptCapabilities{Image: true},
			SessionCapabilities: acp.SessionCapabilities{
				Fork: &acp.SessionForkCapabilities{},
			},
		},
	}, nil
}

func (a *fakeACPAgent) UnstableForkSession(_ context.Context, params acp.UnstableForkSessionRequest) (acp.UnstableForkSessionResponse, error) {
	if string(params.SessionId) != "fake-acp-session" {
		return acp.UnstableForkSessionResponse{}, fmt.Errorf("unexpected fork source %q", params.SessionId)
	}
	return acp.UnstableForkSessionResponse{SessionId: "fake-forked-acp-session"}, nil
}

func (a *fakeACPAgent) SetSessionConfigOption(_ context.Context, params acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	if params.ValueId != nil {
		a.recordSet("config:" + string(params.ValueId.ConfigId) + "=" + string(params.ValueId.Value))
	}
	return acp.SetSessionConfigOptionResponse{}, nil
}

func (a *fakeACPAgent) SetSessionMode(_ context.Context, params acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	a.recordSet("mode:" + string(params.ModeId))
	return acp.SetSessionModeResponse{}, nil
}

func (a *fakeACPAgent) Authenticate(context.Context, acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

func (a *fakeACPAgent) NewSession(_ context.Context, _ acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	a.sessions["fake-acp-session"] = true
	return acp.NewSessionResponse{
		SessionId:     acp.SessionId("fake-acp-session"),
		Modes:         a.modes,
		ConfigOptions: a.configs,
	}, nil
}

func (a *fakeACPAgent) ResumeSession(_ context.Context, params acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, nil
}

func (a *fakeACPAgent) Cancel(context.Context, acp.CancelNotification) error { return nil }

func (a *fakeACPAgent) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	a.mu.Lock()
	a.prompts = append(a.prompts, params.Prompt)
	a.mu.Unlock()
	text := ""
	for _, block := range params.Prompt {
		text += acpContentText(block)
	}
	messageID := "fake-message"
	_ = a.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: params.SessionId,
		Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Content:   acp.TextBlock("fake reply to " + text),
			MessageId: &messageID,
		}},
	})
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func runFakeACP(args []string) int {
	agent := &fakeACPAgent{sessions: map[string]bool{}}
	conn := acp.NewAgentSideConnection(agent, os.Stdout, os.Stdin)
	agent.conn = conn
	<-conn.Done()
	return 0
}

// runFakeTree waits for a signal file, then spawns a long-running child and
// writes its pid. The process-tree test kills the guard and verifies that the
// child died too.
func runFakeTree() int {
	dir := os.Getenv("NEXUS_TEST_TREE_DIR")
	if dir == "" {
		return 2
	}
	_ = os.WriteFile(dir+"/parent.pid", []byte(strconv.Itoa(os.Getpid())), 0o600)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(dir + "/spawn"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 60")
	} else {
		cmd = exec.Command("sh", "-c", "sleep 60")
	}
	if err := cmd.Start(); err != nil {
		return 1
	}
	_ = os.WriteFile(dir+"/child.pid", []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	_ = cmd.Wait()
	return 0
}
