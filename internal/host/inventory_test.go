package host

import (
	"path/filepath"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// A configured ACP adapter is the capability source for agents whose native
// adapter does not advertise structured mode (Gemini today). The inventory
// must reflect that without claiming a verified native integration.
func TestInventoryAdvertisesConfiguredACPForGemini(t *testing.T) {
	d := &Daemon{
		cfg:      Config{ACPCommands: map[string]string{"gemini": "gemini --acp"}},
		adapters: []Adapter{NewGeminiAdapter("definitely-not-installed-gemini")},
	}
	agents := d.inventoryAgents()
	if len(agents) != 1 {
		t.Fatalf("expected one agent, got %+v", agents)
	}
	if !sliceContains(agents[0].Capabilities, "structured") {
		t.Fatalf("configured ACP capability missing: %+v", agents[0])
	}
}

func TestInventoryDoesNotInventStructuredWithoutACP(t *testing.T) {
	d := &Daemon{
		adapters: []Adapter{NewGeminiAdapter("definitely-not-installed-gemini")},
	}
	agents := d.inventoryAgents()
	if len(agents) != 1 {
		t.Fatalf("expected one agent, got %+v", agents)
	}
	if sliceContains(agents[0].Capabilities, "structured") {
		t.Fatalf("structured was advertised without an ACP adapter: %+v", agents[0])
	}
}

func TestSessionWithinRootsAppliesToTranscriptReads(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "repo")
	outside := filepath.Join(filepath.Dir(root), "private")
	d := &Daemon{
		policy: Policy{AllowedRoots: []string{root}},
		lastByAgent: map[string][]protocol.Session{
			"claude": {
				{NativeID: "inside", Agent: "claude", Cwd: inside},
				{NativeID: "outside", Agent: "claude", Cwd: outside},
			},
		},
	}
	if !d.sessionWithinRoots("claude", "inside") {
		t.Fatal("session inside the allowed root was rejected")
	}
	if d.sessionWithinRoots("claude", "outside") {
		t.Fatal("session outside the allowed root was readable")
	}
	if d.sessionWithinRoots("claude", "unknown") {
		t.Fatal("an undiscovered session failed open")
	}
	// Without a restriction every session remains readable.
	d.policy.AllowedRoots = nil
	if !d.sessionWithinRoots("claude", "unknown") {
		t.Fatal("unrestricted host rejected a session")
	}
}
