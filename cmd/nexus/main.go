// Command nexus is a small terminal client for a self-hosted AgentNexus hub.
// It stores a revocable device session in the user's config directory and
// supports listing sessions, attaching to a live run and answering approvals.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/host"
	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/coder/websocket"
)

type cliConfig struct {
	Hub    string `json:"hub"`
	Cookie string `json:"cookie"`
	Device string `json:"device,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "login":
		loginCmd(os.Args[2:])
	case "logout":
		logoutCmd()
	case "sessions":
		sessionsCmd(os.Args[2:])
	case "attach":
		attachCmd(os.Args[2:])
	case "approve":
		approveCmd(os.Args[2:])
	case "update":
		updateCmd(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("nexus " + host.Version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `AgentNexus terminal client

Usage:
  nexus login --hub <url> (--token <pairing-token> | --username <name>)
              [--password <password>] [--totp <code>] [--name <device>]
  nexus logout
  nexus sessions [--json] [--host <id>] [--agent <name>]
  nexus attach <run-id>
  nexus approve <run-id> <approval-id> <once|reject>
  nexus update --check|--install [--channel stable|prerelease|development]
               [--allow-checksum-only]

Pairing tokens are created in the web app under Devices & passkeys. The
session cookie is stored in the per-user config directory with mode 0600 and
can be revoked from the web app at any time.
`)
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "AgentNexus", "cli.json"), nil
}

func loadCLI() (cliConfig, error) {
	path, err := configPath()
	if err != nil {
		return cliConfig{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cliConfig{}, errors.New("not logged in; run: nexus login --hub <url>")
	}
	var cfg cliConfig
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg.Hub == "" || cfg.Cookie == "" {
		return cliConfig{}, errors.New("invalid CLI config; run nexus login again")
	}
	return cfg, nil
}

func saveCLI(cfg cliConfig) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(path, raw, 0o600)
}

func normalizeHub(raw string) (string, error) {
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", errors.New("--hub is required")
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", errors.New("invalid hub URL")
	}
	return raw, nil
}

func loginCmd(args []string) {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	hub := fs.String("hub", "", "hub URL")
	token := fs.String("token", "", "device pairing token from the web app")
	username := fs.String("username", "", "username for password login")
	password := fs.String("password", os.Getenv("NEXUS_PASSWORD"), "password (prefer NEXUS_PASSWORD)")
	totp := fs.String("totp", "", "TOTP code when enabled")
	name := fs.String("name", "", "device name")
	_ = fs.Parse(args)
	base, err := normalizeHub(*hub)
	if err != nil {
		fail(err)
	}
	body := map[string]string{}
	path := "/api/v1/auth/device-pair"
	if *token != "" {
		body["token"] = strings.TrimSpace(*token)
		body["name"] = *name
	} else {
		if *username == "" || *password == "" {
			fail(errors.New("provide --token, or --username and a password via NEXUS_PASSWORD/--password"))
		}
		path = "/api/v1/auth/login"
		body["username"] = strings.ToLower(strings.TrimSpace(*username))
		body["password"] = *password
		body["totp"] = *totp
	}
	client := &http.Client{Timeout: 20 * time.Second}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(raw))
	if err != nil {
		fail(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Nexus-Request", "1")
	resp, err := client.Do(req)
	if err != nil {
		fail(fmt.Errorf("cannot reach hub: %w", err))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		fail(fmt.Errorf("login failed: %s", strings.TrimSpace(string(data))))
	}
	cookie := ""
	for _, c := range resp.Cookies() {
		if c.Name == "nexus_session" {
			cookie = c.Value
			break
		}
	}
	if cookie == "" {
		fail(errors.New("hub did not issue a session cookie"))
	}
	if err := saveCLI(cliConfig{Hub: base, Cookie: cookie, Device: *name}); err != nil {
		fail(err)
	}
	fmt.Printf("Logged in to %s.\n", base)
}

func logoutCmd() {
	cfg, err := loadCLI()
	if err == nil {
		req, _ := http.NewRequest(http.MethodPost, cfg.Hub+"/api/v1/auth/logout", nil)
		req.AddCookie(&http.Cookie{Name: "nexus_session", Value: cfg.Cookie})
		req.Header.Set("X-Nexus-Request", "1")
		client := &http.Client{Timeout: 10 * time.Second}
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
		}
	}
	path, _ := configPath()
	_ = os.Remove(path)
	fmt.Println("Logged out.")
}

func doJSON(cfg cliConfig, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, cfg.Hub+path, reader)
	if err != nil {
		return err
	}
	req.AddCookie(&http.Cookie{Name: "nexus_session", Value: cfg.Cookie})
	req.Header.Set("X-Nexus-Request", "1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func sessionsCmd(args []string) {
	fs := flag.NewFlagSet("sessions", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	hostID := fs.String("host", "", "filter by host id")
	agent := fs.String("agent", "", "filter by agent")
	_ = fs.Parse(args)
	cfg, err := loadCLI()
	if err != nil {
		fail(err)
	}
	query := url.Values{}
	if *hostID != "" {
		query.Set("host", *hostID)
	}
	if *agent != "" {
		query.Set("agent", *agent)
	}
	query.Set("limit", "200")
	query.Set("sort", "updated")
	var response struct {
		Sessions []protocol.SessionView `json:"sessions"`
	}
	if err := doJSON(cfg, http.MethodGet, "/api/v1/sessions?"+query.Encode(), nil, &response); err != nil {
		fail(err)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(response.Sessions)
		return
	}
	fmt.Printf("%-34s %-9s %-16s %-22s %s\n", "ID", "AGENT", "STATE", "HOST", "TITLE")
	for _, s := range response.Sessions {
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Printf("%-34s %-9s %-16s %-22s %s\n", s.ID, s.Agent, s.State, s.HostName, truncate(title, 50))
	}
}

func attachCmd(args []string) {
	if len(args) != 1 {
		fail(errors.New("usage: nexus attach <run-id>"))
	}
	cfg, err := loadCLI()
	if err != nil {
		fail(err)
	}
	runID := args[0]
	var run protocol.RunView
	if err := doJSON(cfg, http.MethodGet, "/api/v1/runs/"+url.PathEscape(runID), nil, &run); err != nil {
		fail(err)
	}
	if err := doJSON(cfg, http.MethodPost, "/api/v1/runs/"+url.PathEscape(runID)+"/lease", nil, nil); err != nil {
		fail(fmt.Errorf("cannot acquire controller lease: %w", err))
	}
	parsed, _ := url.Parse(cfg.Hub)
	wsScheme := "ws"
	if parsed.Scheme == "https" {
		wsScheme = "wss"
	}
	wsURL := wsScheme + "://" + parsed.Host + "/api/v1/ws"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": []string{"nexus_session=" + cfg.Cookie}},
	})
	if err != nil {
		fail(fmt.Errorf("websocket: %w", err))
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")
	if err := conn.Write(ctx, websocket.MessageText, mustJSON(protocol.ClientEnvelope{Type: "subscribe", RunID: runID})); err != nil {
		fail(err)
	}
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			text := scanner.Text()
			_ = conn.Write(ctx, websocket.MessageText, mustJSON(protocol.ClientEnvelope{Type: "input", RunID: runID, Data: []byte(text + "\n")}))
		}
	}()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var msg protocol.ServerEnvelope
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "run.output":
			_, _ = os.Stdout.Write(msg.Data)
		case "run.event":
			if msg.Event != nil {
				printEvent(*msg.Event)
			}
		case "run.exit":
			fmt.Printf("\n[run exited: %s]\n", msg.Message)
			return
		case "error":
			fmt.Fprintf(os.Stderr, "\n[error] %s\n", msg.Error)
		}
	}
}

func printEvent(ev protocol.RunEvent) {
	switch ev.Type {
	case "message.delta":
		fmt.Print(ev.Text)
	case "message.completed":
		if ev.Text != "" {
			fmt.Println()
		}
	case "reasoning":
		fmt.Fprintf(os.Stderr, "\n[reasoning] %s\n", ev.Text)
	case "tool.started":
		fmt.Fprintf(os.Stderr, "\n[tool] %s\n", ev.ToolName)
	case "approval.requested":
		fmt.Fprintf(os.Stderr, "\n[approval %s] %s\n", ev.ApprovalID, ev.Action)
	case "run.state":
		fmt.Fprintf(os.Stderr, "\n[state] %s\n", ev.State)
	case "run.error":
		fmt.Fprintf(os.Stderr, "\n[error] %s\n", ev.Error)
	}
}

func approveCmd(args []string) {
	if len(args) != 3 {
		fail(errors.New("usage: nexus approve <run-id> <approval-id> <once|reject>"))
	}
	if args[2] != "once" && args[2] != "reject" {
		fail(errors.New("decision must be once or reject"))
	}
	cfg, err := loadCLI()
	if err != nil {
		fail(err)
	}
	parsed, _ := url.Parse(cfg.Hub)
	wsScheme := "ws"
	if parsed.Scheme == "https" {
		wsScheme = "wss"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsScheme+"://"+parsed.Host+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": []string{"nexus_session=" + cfg.Cookie}},
	})
	if err != nil {
		fail(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")
	if err := doJSON(cfg, http.MethodPost, "/api/v1/runs/"+url.PathEscape(args[0])+"/lease", nil, nil); err != nil {
		fail(err)
	}
	_ = conn.Write(ctx, websocket.MessageText, mustJSON(protocol.ClientEnvelope{
		Type: "approval", RunID: args[0], ApprovalID: args[1], Decision: args[2],
	}))
	// Read one message so the hub has processed the frame before closing.
	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	_, _, _ = conn.Read(readCtx)
	readCancel()
	fmt.Println("Approval sent.")
}

// updateCmd updates only the standalone terminal client. It never replaces
// the nexusd daemon, restarts a service or changes a host pairing.
func updateCmd(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	check := fs.Bool("check", false, "check the release feed without installing")
	install := fs.Bool("install", false, "download, verify and replace the nexus client")
	channel := fs.String("channel", "stable", "stable, prerelease or development")
	allowChecksumOnly := fs.Bool("allow-checksum-only", false, "accept checksum-only verification (weaker)")
	_ = fs.Parse(args)
	if !*check && !*install {
		*check = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	info, err := host.CheckForUpdate(ctx, host.Version, *channel)
	if err != nil {
		fail(err)
	}
	fmt.Printf("current: %s\nlatest:  %s\nasset:   %s\n", info.Current, info.Latest, info.AssetName)
	if !info.Available {
		fmt.Println("status:  up to date")
		return
	}
	if !*install {
		fmt.Println("status:  update available")
		return
	}
	dir, err := host.ConfigDir()
	if err != nil {
		fail(err)
	}
	stagedDir := filepath.Join(dir, "updates", "nexus-"+info.Latest)
	staged, _, err := host.StageUpdateNamed(ctx, info, stagedDir, "nexus", !*allowChecksumOnly)
	if err != nil {
		fail(err)
	}
	if err := host.VerifyStagedBinaryNamed(ctx, staged, "nexus", info.Latest); err != nil {
		fail(err)
	}
	if err := host.InstallOwnUpdate(staged); err != nil {
		fail(err)
	}
	fmt.Println("nexus client updated; the daemon and host config were not touched")
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func mustJSON(v any) []byte {
	raw, _ := json.Marshal(v)
	return raw
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "nexus:", err)
	os.Exit(1)
}
