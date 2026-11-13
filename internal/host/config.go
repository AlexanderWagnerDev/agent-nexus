package host

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var Version = "alpha"

// Policy is enforced on the host and cannot be overridden by the hub.
type Policy struct {
	AllowedRoots  []string `json:"allowed_roots"`
	AllowedAgents []string `json:"allowed_agents"`
	AllowNew      bool     `json:"allow_new_sessions"`
	AllowFork     bool     `json:"allow_fork"`
	ForbiddenArgs []string `json:"forbidden_args"`
	// AllowForceResume permits the hub to ask for a resume even when a
	// best-effort process scan reports the native session as open elsewhere.
	// Omitted means enabled for backward compatibility.
	AllowForceResume *bool `json:"allow_force_resume,omitempty"`
	// ProcessGraceSeconds is how long a stopped run may take to exit before
	// its process tree is killed. Zero means the five-second default.
	ProcessGraceSeconds int `json:"process_grace_seconds,omitempty"`
}

// ForceResumeAllowed reports whether a force-resume request is acceptable.
// The zero value is enabled so existing configurations keep working.
func (p Policy) ForceResumeAllowed() bool {
	return p.AllowForceResume == nil || *p.AllowForceResume
}

// ProcessGrace returns the configured grace period, clamped to a sane range.
func (p Policy) ProcessGrace() time.Duration {
	seconds := p.ProcessGraceSeconds
	if seconds <= 0 {
		seconds = 5
	}
	if seconds > 60 {
		seconds = 60
	}
	return time.Duration(seconds) * time.Second
}

// UpdateSettings controls the host-local auto-updater. The settings live on
// the host and are never pushed by the hub. A nil Updates field in an existing
// config means "official defaults": enabled, stable, every six hours, install
// when idle, signed releases only.
type UpdateSettings struct {
	Disabled             bool     `json:"disabled,omitempty"`
	Channel              string   `json:"channel,omitempty"`
	CheckIntervalMinutes int      `json:"check_interval_minutes,omitempty"`
	InstallWhenIdle      *bool    `json:"install_when_idle,omitempty"`
	RequireSignature     *bool    `json:"require_signature,omitempty"`
	AllowRemoteInstall   bool     `json:"allow_remote_install,omitempty"`
	BlockedVersions      []string `json:"blocked_versions,omitempty"`
}

const (
	UpdateChannelStable      = "stable"
	UpdateChannelPrerelease  = "prerelease"
	UpdateChannelDevelopment = "development"

	DefaultUpdateInterval = 6 * time.Hour
	MinUpdateInterval     = 15 * time.Minute
	MaxUpdateInterval     = 7 * 24 * time.Hour
)

// Resolve returns a normalized copy of the settings with defaults applied.
func (u UpdateSettings) Resolve() UpdateSettings {
	if u.Channel == "" {
		u.Channel = UpdateChannelStable
	}
	switch strings.ToLower(strings.TrimSpace(u.Channel)) {
	case UpdateChannelStable, "auto":
		u.Channel = UpdateChannelStable
	case UpdateChannelPrerelease:
		u.Channel = UpdateChannelPrerelease
	case UpdateChannelDevelopment:
		u.Channel = UpdateChannelDevelopment
	default:
		u.Channel = UpdateChannelStable
	}
	if u.CheckIntervalMinutes <= 0 {
		u.CheckIntervalMinutes = int(DefaultUpdateInterval / time.Minute)
	}
	interval := time.Duration(u.CheckIntervalMinutes) * time.Minute
	if interval < MinUpdateInterval {
		interval = MinUpdateInterval
	}
	if interval > MaxUpdateInterval {
		interval = MaxUpdateInterval
	}
	u.CheckIntervalMinutes = int(interval / time.Minute)
	return u
}

// Interval returns the configured check interval.
func (u UpdateSettings) Interval() time.Duration {
	minutes := u.Resolve().CheckIntervalMinutes
	return time.Duration(minutes) * time.Minute
}

// InstallIdle reports whether updates may be installed automatically.
func (u UpdateSettings) InstallIdle() bool {
	return u.InstallWhenIdle == nil || *u.InstallWhenIdle
}

// SignatureRequired reports whether a cosign bundle must verify successfully.
// The default is true; checksum-only is an explicit local decision.
func (u UpdateSettings) SignatureRequired() bool {
	return u.RequireSignature == nil || *u.RequireSignature
}

// IsBlocked reports whether a version was temporarily blocked after failures.
func (u UpdateSettings) IsBlocked(version string) bool {
	for _, blocked := range u.BlockedVersions {
		if strings.TrimPrefix(strings.TrimSpace(blocked), "v") == strings.TrimPrefix(strings.TrimSpace(version), "v") {
			return true
		}
	}
	return false
}

// Config is stored next to the host key in the per-user config directory.
type Config struct {
	HubURL             string `json:"hub_url"`
	HostID             string `json:"host_id"`
	HostName           string `json:"host_name"`
	PrivateKey         string `json:"private_key"`
	HubPublicKey       string `json:"hub_public_key,omitempty"`
	HubSigningRequired bool   `json:"hub_signing_required,omitempty"`
	Policy             Policy `json:"policy"`
	// Updates configures the host-local auto-updater. Omitted means enabled
	// with the official defaults; a hub cannot change these settings.
	Updates *UpdateSettings `json:"updates,omitempty"`
	// RunBufferBytes bounds the per-run terminal replay buffer. Omitted means
	// 1 MiB; values below 64 KiB or above 16 MiB are clamped.
	RunBufferBytes int `json:"run_buffer_bytes,omitempty"`
	// ACPCommands maps an agent name to an Agent Client Protocol adapter
	// command line, for example {"claude": "claude-code-acp"}. When empty the
	// daemon looks for the claude-agent-acp or claude-code-acp binary on PATH.
	ACPCommands map[string]string `json:"acp_commands,omitempty"`
	// AllowUntestedAgentVersions keeps structured mode enabled for agents
	// outside the tested version band. Every use is audited.
	AllowUntestedAgentVersions bool `json:"allow_untested_agent_versions,omitempty"`
	// SearchIndex enables the host-local FTS5 transcript cache. Omitted means
	// enabled; set it to false to keep transcript text out of a local index.
	SearchIndex *bool `json:"search_index,omitempty"`
}

// RunBufferBytesOrDefault returns the bounded terminal replay size.
func (c Config) RunBufferBytesOrDefault() int {
	const min, max = 64 << 10, 16 << 20
	value := c.RunBufferBytes
	if value <= 0 {
		value = 1 << 20
	}
	if value < min {
		value = min
	}
	if value > max {
		value = max
	}
	return value
}

// SearchIndexEnabled reports whether the local search index may be built.
func (c Config) SearchIndexEnabled() bool {
	return c.SearchIndex == nil || *c.SearchIndex
}

// UpdatePrefs returns the host-local updater settings with defaults applied.
func (c Config) UpdatePrefs() UpdateSettings {
	if c.Updates == nil {
		return UpdateSettings{}.Resolve()
	}
	return c.Updates.Resolve()
}

// SaveUpdatePrefs stores normalized updater settings without touching any
// other field. It never changes the host identity or pairing.
func SaveUpdatePrefs(prefs UpdateSettings) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	resolved := prefs.Resolve()
	cfg.Updates = &resolved
	return SaveConfig(cfg)
}

// ACPCommand returns the ACP adapter command configured for an agent.
func (c Config) ACPCommand(agent string) string {
	if c.ACPCommands != nil {
		if cmd := strings.TrimSpace(c.ACPCommands[agent]); cmd != "" {
			return cmd
		}
	}
	if agent == "claude" {
		for _, name := range []string{"claude-agent-acp", "claude-code-acp"} {
			if path, err := exec.LookPath(name); err == nil {
				return path
			}
		}
	}
	return ""
}

func ConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "AgentNexus"), nil
}

func ConfigPath() (string, error) {
	// NEXUSD_CONFIG allows a second isolated daemon instance for testing or
	// running a host under a different identity without touching the user's
	// normal pairing.
	if override := strings.TrimSpace(os.Getenv("NEXUSD_CONFIG")); override != "" {
		return override, nil
	}
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

// splitCommandLine splits an ACP adapter command. Double quotes group a path
// with spaces; backslash escaping is intentionally not interpreted so Windows
// paths remain unchanged.
func splitCommandLine(s string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case (r == ' ' || r == '\t') && !quoted:
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
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
