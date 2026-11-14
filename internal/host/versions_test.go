package host

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestStructuredVersionBandIsEnforcedOnRunStart(t *testing.T) {
	d := &Daemon{cfg: Config{}, versions: map[string]string{}}
	d.rememberAgentVersion("opencode", "1.9.4")
	if err := d.checkStructuredVersion(context.Background(), "opencode"); err == nil {
		t.Fatal("structured mode accepted an untested OpenCode version")
	}
	d.cfg.AllowUntestedAgentVersions = true
	if err := d.checkStructuredVersion(context.Background(), "opencode"); err != nil {
		t.Fatalf("explicit override was ignored: %v", err)
	}
	d.cfg.AllowUntestedAgentVersions = false
	d.rememberAgentVersion("opencode", "2.2.7")
	if err := d.checkStructuredVersion(context.Background(), "opencode"); err != nil {
		t.Fatalf("tested version rejected: %v", err)
	}
	// A generic ACP agent without a matrix entry is not restricted here.
	if err := d.checkStructuredVersion(context.Background(), "my-acp-agent"); err != nil {
		t.Fatalf("unknown agent rejected: %v", err)
	}
}

func TestCheckAgentVersion(t *testing.T) {
	cases := []struct {
		agent, version string
		tested, known  bool
	}{
		{"opencode", "2.2.7", true, true},
		{"opencode", "1.4.2", false, true},
		{"opencode", "2.0.0", false, true},
		{"opencode", "3.0.0", false, true},
		{"opencode", "0.9.9", false, true},
		{"codex", "0.58.0", true, true},
		{"codex", "codex-cli 1.2.3", false, true},
		{"claude", "2.1.4", true, true},
		{"gemini", "0.5.0", true, true},
		{"unknown-agent", "9.9.9", true, false},
		{"opencode", "nightly", true, false},
	}
	for _, tc := range cases {
		tested, known := CheckAgentVersion(tc.agent, tc.version)
		if tested != tc.tested || known != tc.known {
			t.Errorf("CheckAgentVersion(%q, %q) = %v,%v want %v,%v", tc.agent, tc.version, tested, known, tc.tested, tc.known)
		}
	}
}

func TestApplyVersionPolicy(t *testing.T) {
	base := []string{"terminal", "structured", "attachments"}
	d := &Daemon{cfg: Config{}}
	outside := protocol.AgentInfo{Name: "opencode", Version: "9.9.9", Capabilities: append([]string(nil), base...)}
	d.applyVersionPolicy(&outside)
	if !outside.Untested || !strings.Contains(outside.VersionWarning, "outside the tested range") {
		t.Fatalf("outside version not flagged: %+v", outside)
	}
	if sliceContains(outside.Capabilities, "structured") {
		t.Fatalf("structured capability survived: %+v", outside.Capabilities)
	}
	if !sliceContains(outside.Capabilities, "terminal") {
		t.Fatalf("terminal capability was removed: %+v", outside.Capabilities)
	}

	override := &Daemon{cfg: Config{AllowUntestedAgentVersions: true}}
	allowed := protocol.AgentInfo{Name: "opencode", Version: "9.9.9", Capabilities: append([]string(nil), base...)}
	override.applyVersionPolicy(&allowed)
	if !sliceContains(allowed.Capabilities, "structured") {
		t.Fatalf("override removed structured: %+v", allowed.Capabilities)
	}
	if !strings.Contains(allowed.VersionWarning, "allow_untested_agent_versions") {
		t.Fatalf("override warning: %q", allowed.VersionWarning)
	}

	inside := protocol.AgentInfo{Name: "opencode", Version: "2.2.7", Capabilities: append([]string(nil), base...)}
	d.applyVersionPolicy(&inside)
	if inside.Untested || inside.VersionWarning != "" || !sliceContains(inside.Capabilities, "structured") {
		t.Fatalf("inside version changed: %+v", inside)
	}

	unknown := protocol.AgentInfo{Name: "opencode", Version: "nightly", Capabilities: append([]string(nil), base...)}
	d.applyVersionPolicy(&unknown)
	if unknown.Untested || !strings.Contains(unknown.VersionWarning, "could not be compared") {
		t.Fatalf("unknown version: %+v", unknown)
	}
}

// TestAgentMatrixDocumentation keeps the single Go source and the docs table
// in sync.
func TestAgentMatrixDocumentation(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "AGENT-INTEGRATIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	for _, band := range AgentMatrix() {
		structured := "no"
		if band.Structured {
			structured = "yes"
		}
		line := fmt.Sprintf("matrix: %-8s %-18s structured=%s", band.Agent, band.TestedRange(), structured)
		if !strings.Contains(doc, line) {
			t.Errorf("docs/AGENT-INTEGRATIONS.md is missing %q", line)
		}
	}
}
