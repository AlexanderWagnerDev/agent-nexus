package host

import (
	"path/filepath"
	"testing"
)

func TestAllowAllRootsAcceptsAnyPath(t *testing.T) {
	allow := true
	p := Policy{AllowAllRoots: &allow}
	if !p.WithinRoots(filepath.Join(t.TempDir(), "outside")) {
		t.Fatal("allow_all_roots did not accept an outside path")
	}
	if p.WithinRoots("") {
		t.Fatal("an empty path was accepted")
	}
}

func TestDefaultPolicyAllowsAllRoots(t *testing.T) {
	if !DefaultPolicy().AllowAllRootsEnabled() {
		t.Fatal("new configurations should not require a per-folder root list")
	}
}

func TestLegacyHomeOnlyDefaultsToAllowAll(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	if !legacyHomeOnly([]string{home}) {
		t.Fatal("the old home-only default was not recognized")
	}
	if legacyHomeOnly([]string{home, filepath.Join(filepath.Dir(home), "projects")}) {
		t.Fatal("a customized root list was treated as the old default")
	}
}

func TestExplicitRestrictionSurvivesLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nexusd.json")
	t.Setenv("NEXUSD_CONFIG", path)
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	cfg := Config{
		HubURL: "https://hub.example", HostID: "host-1", PrivateKey: "test-key",
		Policy: Policy{AllowedRoots: []string{home}},
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Policy.AllowAllRootsEnabled() {
		t.Fatal("the old home-only default should allow all paths")
	}

	custom := Config{
		HubURL: "https://hub.example", HostID: "host-1", PrivateKey: "test-key",
		Policy: Policy{AllowedRoots: []string{home, filepath.Join(home, "projects")}},
	}
	if err := SaveConfig(custom); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Policy.AllowAllRootsEnabled() {
		t.Fatal("a custom root list was widened without an explicit opt-in")
	}
}
