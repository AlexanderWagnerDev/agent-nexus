package host

import (
	"strings"
	"testing"
)

func TestSafeDoctorReportsWithoutSensitiveValues(t *testing.T) {
	d := &Daemon{cfg: Config{}, policy: Policy{}}
	report := d.safeDoctor()
	if report.Version == "" || len(report.Checks) == 0 || report.Issues == 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	seen := map[string]bool{}
	var text strings.Builder
	for _, check := range report.Checks {
		seen[check.ID] = true
		text.WriteString(check.ID + " " + check.Detail + "\n")
	}
	for _, want := range []string{"pairing", "identity", "hub_signing", "policy_roots", "policy_agents", "agents", "search_index"} {
		if !seen[want] {
			t.Fatalf("missing check %q: %+v", want, report.Checks)
		}
	}
	if strings.Contains(text.String(), "PRIVATE") || strings.Contains(text.String(), "secret") || strings.Contains(text.String(), "BEGIN") {
		t.Fatalf("safe doctor leaked a path or secret-like value: %s", text.String())
	}
}
