package host

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type pairResponse struct {
	HostID string `json:"host_id"`
	SAS    string `json:"sas"`
	Error  string `json:"error"`
}

// Pair redeems a one-time pairing code and stores the host identity. It
// returns the short authentication string the user must compare with the hub.
func Pair(ctx context.Context, hubURL, code, name string) (Config, string, error) {
	hubURL = strings.TrimSuffix(strings.TrimSpace(hubURL), "/")
	if !strings.HasPrefix(hubURL, "http://") && !strings.HasPrefix(hubURL, "https://") {
		hubURL = "http://" + hubURL
	}
	if name == "" {
		name = ReadHostname()
	}
	pub, priv, err := NewIdentity()
	if err != nil {
		return Config{}, "", err
	}
	body, _ := json.Marshal(map[string]string{
		"code":       strings.ToUpper(strings.TrimSpace(code)),
		"name":       name,
		"public_key": base64.StdEncoding.EncodeToString(pub),
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"version":    Version,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubURL+"/api/v1/hosts/pair", bytes.NewReader(body))
	if err != nil {
		return Config{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Nexus-Request", "1")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Config{}, "", fmt.Errorf("cannot reach hub: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var pr pairResponse
	_ = json.Unmarshal(data, &pr)
	if resp.StatusCode != http.StatusCreated {
		if pr.Error == "" {
			pr.Error = resp.Status
		}
		return Config{}, "", errors.New(pr.Error)
	}
	hubKey, _ := FetchHubPublicKey(ctx, hubURL)
	cfg := Config{
		HubURL:             hubURL,
		HostID:             pr.HostID,
		HostName:           name,
		PrivateKey:         base64.StdEncoding.EncodeToString(priv),
		HubPublicKey:       hubKey,
		HubSigningRequired: hubKey != "",
		Policy:             DefaultPolicy(),
	}
	if err := SaveConfig(cfg); err != nil {
		return Config{}, "", err
	}
	return cfg, pr.SAS, nil
}

// Doctor prints configuration problems and returns the number of issues.
func Doctor(cfg Config, cfgErr error, out io.Writer) int {
	issues := 0
	path, _ := ConfigPath()
	fmt.Fprintf(out, "version:    %s\n", Version)
	fmt.Fprintf(out, "config:     %s\n", path)
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o077 != 0 {
			fmt.Fprintf(out, "permissions: WARNING config is readable by other users (%o)\n", info.Mode().Perm())
			issues++
		}
	}
	if cfgErr != nil {
		fmt.Fprintf(out, "status:     NOT PAIRED (%v)\n", cfgErr)
		issues++
	} else {
		fmt.Fprintf(out, "host id:    %s\n", cfg.HostID)
		fmt.Fprintf(out, "hub:        %s\n", cfg.HubURL)
		fmt.Fprintf(out, "host name:  %s\n", cfg.HostName)
		if _, err := cfg.SigningKey(); err != nil {
			fmt.Fprintf(out, "identity:   BROKEN (%v)\n", err)
			issues++
		} else {
			fmt.Fprintln(out, "identity:   ok")
		}
		if cfg.HubPublicKey == "" {
			if cfg.HubSigningRequired {
				fmt.Fprintln(out, "hub signing: REQUIRED but no key pinned")
				issues++
			} else {
				fmt.Fprintln(out, "hub signing: not pinned yet (pinned on the next connection to a signing hub)")
			}
		} else if raw, err := base64.StdEncoding.DecodeString(cfg.HubPublicKey); err != nil || len(raw) != ed25519.PublicKeySize {
			fmt.Fprintln(out, "hub signing: INVALID pinned key")
			issues++
		} else {
			fmt.Fprintln(out, "hub signing: pinned and required")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(cfg.HubURL, "/")+"/healthz", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			fmt.Fprintf(out, "hub health: UNREACHABLE (%v)\n", err)
			issues++
		} else {
			resp.Body.Close()
			fmt.Fprintf(out, "hub health: HTTP %d\n", resp.StatusCode)
			if resp.StatusCode != 200 {
				issues++
			}
		}
		for _, root := range cfg.Policy.AllowedRoots {
			if st, err := os.Stat(root); err != nil || !st.IsDir() {
				fmt.Fprintf(out, "policy:     allowed root %q does not exist\n", root)
				issues++
			}
		}
		if len(cfg.Policy.AllowedAgents) == 1 && cfg.Policy.AllowedAgents[0] == "" {
			fmt.Fprintln(out, "policy:     invalid empty agent entry")
			issues++
		}
	}
	adapters := DetectAdapters(discardLogger())
	if len(adapters) == 0 {
		fmt.Fprintln(out, "agents:     none detected (claude, codex, opencode, gemini)")
		issues++
	}
	for _, a := range adapters {
		info, err := a.Detect(context.Background())
		if err != nil {
			fmt.Fprintf(out, "agent %-9s error: %v\n", a.Name(), err)
			issues++
			continue
		}
		fmt.Fprintf(out, "agent %-9s %s (%s)\n", a.Name(), info.Version, info.Path)
		if !info.Supported {
			fmt.Fprintf(out, "             store not found; discovery disabled\n")
		}
	}
	if issues == 0 {
		fmt.Fprintln(out, "result:     ok")
	} else {
		fmt.Fprintf(out, "result:     %d issue(s)\n", issues)
	}
	return issues
}

// ServiceAction installs, uninstalls or queries the per-user background
// service. It intentionally never creates a root/SYSTEM service.
func ServiceAction(action string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "linux":
		return systemdService(action, exe)
	case "darwin":
		return launchdService(action, exe)
	case "windows":
		return windowsService(action, exe)
	default:
		return fmt.Errorf("service installation is not supported on %s", runtime.GOOS)
	}
}

func systemdService(action, exe string) error {
	home, _ := os.UserHomeDir()
	unit := filepath.Join(home, ".config", "systemd", "user", "agentnexus.service")
	switch action {
	case "install":
		if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
			return err
		}
		content := fmt.Sprintf(`[Unit]
Description=AgentNexus host daemon
After=network-online.target

[Service]
Type=simple
ExecStart=%q run
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`, exe)
		if err := os.WriteFile(unit, []byte(content), 0o644); err != nil {
			return err
		}
		if err := runCmd("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		return runCmd("systemctl", "--user", "enable", "--now", "agentnexus.service")
	case "uninstall":
		_ = runCmd("systemctl", "--user", "disable", "--now", "agentnexus.service")
		return os.Remove(unit)
	case "status":
		return runCmd("systemctl", "--user", "status", "agentnexus.service", "--no-pager")
	}
	return fmt.Errorf("unknown service action %q", action)
}

func launchdService(action, exe string) error {
	home, _ := os.UserHomeDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", "dev.agentnexus.nexusd.plist")
	switch action {
	case "install":
		if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
			return err
		}
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>dev.agentnexus.nexusd</string>
  <key>ProgramArguments</key><array><string>%s</string><string>run</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict></plist>
`, exe)
		if err := os.WriteFile(plist, []byte(content), 0o644); err != nil {
			return err
		}
		return runCmd("launchctl", "load", plist)
	case "uninstall":
		_ = runCmd("launchctl", "unload", plist)
		return os.Remove(plist)
	case "status":
		return runCmd("launchctl", "list", "dev.agentnexus.nexusd")
	}
	return fmt.Errorf("unknown service action %q", action)
}

func windowsService(action, exe string) error {
	return windowsTaskService(action, exe)
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
