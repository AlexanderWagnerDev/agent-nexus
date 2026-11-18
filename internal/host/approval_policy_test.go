package host

import (
	"io"
	"log/slog"
	"testing"
)

// TestApprovalDecisionHonoursPersistentPolicy pins the host-side default for
// docs/SECURITY.md section 11: the agent's persistent "always" option is
// downgraded to a one-time approval unless allow_persistent_approvals is set,
// so neither a stale browser nor a compromised hub can widen the policy.
func TestApprovalDecisionHonoursPersistentPolicy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	restricted := NewDaemonWithAdapters(Config{Policy: DefaultPolicy()}, logger, nil)
	for _, decision := range []string{"once", "reject", ""} {
		if got := restricted.approvalDecisionForHost(decision); got != decision {
			t.Fatalf("decision %q changed to %q under the default policy", decision, got)
		}
	}
	if got := restricted.approvalDecisionForHost("always"); got != "once" {
		t.Fatalf("always must be downgraded by default, got %q", got)
	}

	allowed := NewDaemonWithAdapters(Config{Policy: Policy{AllowPersistentApprovals: true}}, logger, nil)
	if got := allowed.approvalDecisionForHost("always"); got != "always" {
		t.Fatalf("always must pass with allow_persistent_approvals=true, got %q", got)
	}
}
