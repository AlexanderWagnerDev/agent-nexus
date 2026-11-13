package hub

import (
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestPendingApprovalSurvivesResolvedEntries(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.server.appendRunEvent("run-1", protocol.RunEvent{Type: "approval.requested", ApprovalID: "a1", Action: "edit"})
	hub.server.appendRunEvent("run-1", protocol.RunEvent{Type: "approval.requested", ApprovalID: "a2", Action: "shell"})
	hub.server.appendRunEvent("run-1", protocol.RunEvent{Type: "approval.resolved", ApprovalID: "a1", Decision: "once"})
	pending := hub.server.pendingApproval("run-1")
	if pending == nil || pending.ApprovalID != "a2" {
		t.Fatalf("unexpected pending approval: %+v", pending)
	}
	hub.server.appendRunEvent("run-1", protocol.RunEvent{Type: "approval.resolved", ApprovalID: "a2", Decision: "reject"})
	if pending := hub.server.pendingApproval("run-1"); pending != nil {
		t.Fatalf("resolved approval still pending: %+v", pending)
	}
}
