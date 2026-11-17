package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestOpenCodeAPIContract starts a real `opencode serve` and verifies the
// endpoints AgentNexus uses for structured runs. It is opt-in because it needs
// a downloaded OpenCode binary:
//
//	OPENCODE_BIN=/path/to/opencode go test ./internal/host -run TestOpenCodeAPIContract -v
//
// CI runs it against several @opencode/cli versions; locally it turns the
// version matrix from an assumption into an executable check.
func TestOpenCodeAPIContract(t *testing.T) {
	bin := strings.TrimSpace(os.Getenv("OPENCODE_BIN"))
	if bin == "" {
		t.Skip("OPENCODE_BIN is not set")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("OPENCODE_BIN is not a local file: %v", err)
	}
	root := t.TempDir()
	for _, dir := range []string{"config", "data", "home", "appdata", "localappdata", "workspace"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	port := freeTestPort(t)
	password := "agentnexus-contract-" + fmt.Sprint(time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	path, args := commandFor(bin, []string{"serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port)})
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = filepath.Join(root, "workspace")
	cmd.Env = append(os.Environ(),
		"OPENCODE_SERVER_PASSWORD="+password,
		"XDG_CONFIG_HOME="+filepath.Join(root, "config"),
		"XDG_DATA_HOME="+filepath.Join(root, "data"),
		"HOME="+filepath.Join(root, "home"),
		"APPDATA="+filepath.Join(root, "appdata"),
		"LOCALAPPDATA="+filepath.Join(root, "localappdata"),
	)
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("opencode serve output:\n%s", logs.String())
		}
	})

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 5 * time.Second}
	request := func(method, path string, body any) (int, []byte) {
		var reader io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth("opencode", password)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, []byte(err.Error())
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		return resp.StatusCode, data
	}

	deadline := time.Now().Add(60 * time.Second)
	for {
		status, _ := request(http.MethodGet, "/api/session", nil)
		if status == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("opencode serve did not become ready; last status %d", status)
		}
		time.Sleep(500 * time.Millisecond)
	}

	status, raw := request(http.MethodGet, "/openapi.json", nil)
	if status != http.StatusOK {
		t.Fatalf("openapi status %d", status)
	}
	var doc struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("openapi json: %v", err)
	}
	for _, path := range []string{
		"/api/session",
		"/api/event",
		"/api/session/{sessionID}/prompt",
		"/api/session/{sessionID}/interrupt",
		"/api/session/{sessionID}/fork",
		"/api/session/{sessionID}/permission/{requestID}/reply",
	} {
		if _, ok := doc.Paths[path]; !ok {
			t.Errorf("openapi is missing %s", path)
		}
	}

	status, raw = request(http.MethodPost, "/api/session", map[string]any{
		"title": "AgentNexus contract test", "location": map[string]string{"directory": cmd.Dir},
	})
	if status != http.StatusOK {
		t.Fatalf("create session: %d %s", status, raw)
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &created); err != nil || created.Data.ID == "" {
		t.Fatalf("create session response: %v %s", err, raw)
	}

	status, raw = request(http.MethodPost, "/api/session/"+created.Data.ID+"/fork", map[string]any{})
	if status == http.StatusOK || (status == http.StatusBadRequest && strings.Contains(string(raw), "Cannot fork empty session")) {
		return
	}
	if status != http.StatusBadRequest || !strings.Contains(string(raw), "boundary") {
		t.Fatalf("fork: %d %s", status, raw)
	}
	status, raw = request(http.MethodPost, "/api/session/"+created.Data.ID+"/fork",
		map[string]any{"boundary": map[string]any{"type": "through"}})
	if status == http.StatusOK {
		return
	}
	// An empty source session is the documented terminal state for a valid
	// fork request: it proves the boundary payload was accepted.
	if status == http.StatusBadRequest && strings.Contains(string(raw), "Cannot fork empty session") {
		return
	}
	t.Fatalf("fork with boundary: %d %s", status, raw)
}

func freeTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
