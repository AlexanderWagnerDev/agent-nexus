package host

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const Version = "0.1.0"

// Policy is enforced on the host and cannot be overridden by the hub.
type Policy struct {
	AllowedRoots  []string `json:"allowed_roots"`
	AllowedAgents []string `json:"allowed_agents"`
	AllowNew      bool     `json:"allow_new_sessions"`
	AllowFork     bool     `json:"allow_fork"`
	ForbiddenArgs []string `json:"forbidden_args"`
}

// Config is stored next to the host key in the per-user config directory.
type Config struct {
	HubURL     string `json:"hub_url"`
	HostID     string `json:"host_id"`
	HostName   string `json:"host_name"`
	PrivateKey string `json:"private_key"`
	Policy     Policy `json:"policy"`
}

func ConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "AgentNexus"), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "nexusd.json"), nil
}

func LoadConfig() (Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("not paired yet; run: nexusd pair --hub <url> --code <code>")
		}
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("invalid config %s: %w", path, err)
	}
	if cfg.HubURL == "" || cfg.HostID == "" || cfg.PrivateKey == "" {
		return Config{}, fmt.Errorf("incomplete config %s", path)
	}
	if cfg.Policy.AllowedRoots == nil {
		cfg.Policy.AllowedRoots = []string{homeDir()}
	}
	if cfg.Policy.ForbiddenArgs == nil {
		cfg.Policy.ForbiddenArgs = DefaultForbiddenArgs()
	}
	return cfg, nil
}

func SaveConfig(cfg Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c Config) SigningKey() (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(c.PrivateKey)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid host private key")
	}
	return ed25519.PrivateKey(raw), nil
}

func NewIdentity() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

func DefaultForbiddenArgs() []string {
	return []string{
		"--dangerously-skip-permissions",
		"--dangerously-bypass-approvals-and-sandbox",
		"--yolo",
		"--auto-approve",
	}
}

func DefaultPolicy() Policy {
	return Policy{
		AllowedRoots:  []string{homeDir()},
		AllowedAgents: nil, // all detected agents
		AllowNew:      true,
		AllowFork:     true,
		ForbiddenArgs: DefaultForbiddenArgs(),
	}
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// WithinRoots reports whether path is inside one of the allowed roots.
func (p Policy) WithinRoots(path string) bool {
	if path == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	for _, root := range p.AllowedRoots {
		r, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		r = filepath.Clean(r)
		if resolved, err := filepath.EvalSymlinks(r); err == nil {
			r = resolved
		}
		rel, err := filepath.Rel(r, abs)
		if err != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}

// AgentAllowed checks a name against the policy; an empty list means "all
// detected agents".
func (p Policy) AgentAllowed(name string) bool {
	if len(p.AllowedAgents) == 0 {
		return true
	}
	for _, a := range p.AllowedAgents {
		if a == name {
			return true
		}
	}
	return false
}

// ArgsAllowed rejects permission-bypass flags even if the hub asks for them.
func (p Policy) ArgsAllowed(args []string) error {
	for _, arg := range args {
		for _, forbidden := range p.ForbiddenArgs {
			if arg == forbidden || strings.HasPrefix(arg, forbidden+"=") {
				return fmt.Errorf("argument %q is forbidden by host policy", arg)
			}
		}
	}
	return nil
}

func PlatformName() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
