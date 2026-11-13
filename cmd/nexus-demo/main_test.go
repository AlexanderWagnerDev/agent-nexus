package main

import "testing"

func TestRunOptionsAreAgentSpecific(t *testing.T) {
	codex := runOptionsFor("codex")
	if len(codex.Models) == 0 || codex.Models[0].Provider != "openai" {
		t.Fatalf("codex options: %+v", codex)
	}
	if len(codex.Modes) != 0 {
		t.Fatalf("codex must not expose agent modes: %+v", codex.Modes)
	}
	claude := runOptionsFor("claude")
	if len(claude.Models) == 0 || claude.CurrentProvider != "anthropic" {
		t.Fatalf("claude options: %+v", claude)
	}
	if len(claude.Modes) != 0 {
		t.Fatalf("claude ACP demo must only announce options: %+v", claude.Modes)
	}
	opencode := runOptionsFor("opencode")
	if len(opencode.Modes) == 0 || opencode.CurrentProvider != "opencode" {
		t.Fatalf("opencode options: %+v", opencode)
	}
}

func TestGitFixtureFollowsProject(t *testing.T) {
	f := fixturesFor("windows")
	checkout := f.gitByProject["web-checkout"]
	if checkout == nil || checkout["branch"] != "fix/flaky-checkout" {
		t.Fatalf("missing web-checkout fixture: %+v", f.gitByProject)
	}
}
