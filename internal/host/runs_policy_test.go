package host

import (
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestValidateRunPolicyEnforcesAgentAllowlistForNewSessions(t *testing.T) {
	d := &Daemon{policy: Policy{
		AllowedAgents: []string{"codex"},
		AllowNew:      true,
		AllowFork:     true,
		ForbiddenArgs: DefaultForbiddenArgs(),
	}}
	if err := d.validateRunPolicy(protocol.RunSpec{
		Agent: "claude", Action: protocol.ActionNew,
	}); err == nil {
		t.Fatal("new session for a non-allowed agent was accepted")
	}
	if err := d.validateRunPolicy(protocol.RunSpec{
		Agent: "codex", Action: protocol.ActionNew,
	}); err != nil {
		t.Fatalf("allowed agent rejected: %v", err)
	}
	if err := d.validateRunPolicy(protocol.RunSpec{
		Agent: "claude", Action: protocol.ActionResume, NativeID: "abc-123",
	}); err == nil {
		t.Fatal("resume for a non-allowed agent was accepted")
	}
}

func TestValidateRunPolicyRejectsUnknownAction(t *testing.T) {
	d := &Daemon{policy: DefaultPolicy()}
	if err := d.validateRunPolicy(protocol.RunSpec{
		Agent: "codex", Action: "bogus", NativeID: "abc",
	}); err == nil {
		t.Fatal("unknown action was accepted")
	}
}

func TestSessionConflictAcrossModes(t *testing.T) {
	d := &Daemon{
		runs:         map[string]*Run{"terminal": {id: "terminal", sessionKey: "codex:abc"}},
		structured:   map[string]*StructuredRun{},
		codexRuns:    map[string]*CodexRun{},
		acpRuns:      map[string]*ACPRun{},
		starting:     map[string]bool{},
		startingRuns: map[string]bool{},
	}
	if !d.sessionActiveLocked("codex:abc") {
		t.Fatal("terminal run was not considered active")
	}
	if d.sessionActiveLocked("codex:other") {
		t.Fatal("unrelated session was considered active")
	}
	if !d.runActiveLocked("terminal") {
		t.Fatal("run id was not considered active")
	}
}

func TestReserveRunPreventsStartupRace(t *testing.T) {
	d := &Daemon{
		runs:         map[string]*Run{},
		structured:   map[string]*StructuredRun{},
		codexRuns:    map[string]*CodexRun{},
		acpRuns:      map[string]*ACPRun{},
		starting:     map[string]bool{},
		startingRuns: map[string]bool{},
	}
	if !d.reserveRunLocked("codex:abc", "run1") {
		t.Fatal("first reservation was rejected")
	}
	if d.reserveRunLocked("codex:abc", "run2") {
		t.Fatal("second reservation for the same session was accepted")
	}
	d.releaseRunLocked("codex:abc", "run1")
	if !d.reserveRunLocked("codex:abc", "run2") {
		t.Fatal("session was not released")
	}
}
