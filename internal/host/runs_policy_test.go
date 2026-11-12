package host

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

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

func TestValidateRunPolicyEnforcesAllowedRoots(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	d := &Daemon{policy: Policy{
		AllowedRoots:  []string{root},
		AllowedAgents: []string{"codex"},
		AllowNew:      true,
		ForbiddenArgs: DefaultForbiddenArgs(),
	}}
	if err := d.validateRunPolicy(protocol.RunSpec{
		Agent: "codex", Action: protocol.ActionNew, Cwd: root,
	}); err != nil {
		t.Fatalf("inside root rejected: %v", err)
	}
	if err := d.validateRunPolicy(protocol.RunSpec{
		Agent: "codex", Action: protocol.ActionNew, Cwd: outside,
	}); err == nil {
		t.Fatal("working directory outside allowed roots was accepted")
	}
}

func TestForbiddenArgsRejected(t *testing.T) {
	p := Policy{ForbiddenArgs: DefaultForbiddenArgs()}
	if err := p.ArgsAllowed([]string{"--dangerously-skip-permissions"}); err == nil {
		t.Fatal("permission-bypass flag was accepted")
	}
	if err := p.ArgsAllowed([]string{"--yolo=1"}); err == nil {
		t.Fatal("permission-bypass flag with a value was accepted")
	}
	if err := p.ArgsAllowed([]string{"--model", "fast"}); err != nil {
		t.Fatalf("ordinary flags rejected: %v", err)
	}
}

// A compromised hub can produce a perfectly valid signature and still ask for
// something the local policy forbids. The signature must not override policy.
func TestSignedHubCommandStillSubjectToHostPolicy(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	outside := t.TempDir()
	d := &Daemon{
		cfg:       Config{HubPublicKey: base64.StdEncoding.EncodeToString(public)},
		policy:    Policy{AllowedRoots: []string{root}, AllowedAgents: []string{"codex"}, AllowNew: true, ForbiddenArgs: DefaultForbiddenArgs()},
		hubNonces: map[string]time.Time{},
	}
	spec := protocol.RunSpec{RunID: "run-evil", Agent: "codex", Action: protocol.ActionNew, Cwd: outside}
	raw, _ := json.Marshal(spec)
	envelope := protocol.Envelope{Type: "run.start", Run: &spec, Params: raw}
	if err := protocol.SignEnvelope(private, &envelope); err != nil {
		t.Fatal(err)
	}
	if err := d.verifyHubEnvelope(&envelope); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := d.validateRunPolicy(spec); err == nil {
		t.Fatal("signed command bypassed host policy")
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

func TestForceResumePolicy(t *testing.T) {
	off := false
	d := &Daemon{policy: Policy{
		AllowedAgents: []string{"codex"}, AllowNew: true,
		AllowForceResume: &off, ForbiddenArgs: DefaultForbiddenArgs(),
	}}
	if err := d.validateRunPolicy(protocol.RunSpec{
		Agent: "codex", Action: protocol.ActionResume, NativeID: "abc-123", Force: true,
	}); err == nil {
		t.Fatal("force resume was accepted although allow_force_resume is false")
	} else if !strings.Contains(err.Error(), "force resume is disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := d.validateRunPolicy(protocol.RunSpec{
		Agent: "codex", Action: protocol.ActionResume, NativeID: "abc-123",
	}); err != nil {
		t.Fatalf("ordinary resume was rejected: %v", err)
	}

	on := true
	d.policy.AllowForceResume = &on
	if err := d.validateRunPolicy(protocol.RunSpec{
		Agent: "codex", Action: protocol.ActionResume, NativeID: "abc-123", Force: true,
	}); err != nil {
		t.Fatalf("force resume with the override enabled was rejected: %v", err)
	}
	d.policy.AllowForceResume = nil
	if err := d.validateRunPolicy(protocol.RunSpec{
		Agent: "codex", Action: protocol.ActionResume, NativeID: "abc-123", Force: true,
	}); err != nil {
		t.Fatalf("default policy must allow force resume: %v", err)
	}
}

// A compromised hub can sign a force-resume command; the host policy must
// still reject it when the local switch is off.
func TestSignedForceResumeRejectedWhenDisabled(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	off := false
	root := t.TempDir()
	d := &Daemon{
		cfg: Config{HubPublicKey: base64.StdEncoding.EncodeToString(public)},
		policy: Policy{
			AllowedRoots: []string{root}, AllowedAgents: []string{"codex"}, AllowNew: true,
			AllowForceResume: &off, ForbiddenArgs: DefaultForbiddenArgs(),
		},
		hubNonces: map[string]time.Time{},
	}
	spec := protocol.RunSpec{RunID: "run-force", Agent: "codex", Action: protocol.ActionResume, NativeID: "abc-123", Cwd: root, Force: true}
	raw, _ := json.Marshal(spec)
	envelope := protocol.Envelope{Type: "run.start", Run: &spec, Params: raw}
	if err := protocol.SignEnvelope(private, &envelope); err != nil {
		t.Fatal(err)
	}
	if err := d.verifyHubEnvelope(&envelope); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := d.validateRunPolicy(spec); err == nil {
		t.Fatal("signed force-resume command bypassed allow_force_resume=false")
	}
}
