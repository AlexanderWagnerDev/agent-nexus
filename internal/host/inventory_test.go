package host

import "testing"

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
