package host

import (
	"strings"
	"testing"
)

func TestChildEnvStripsDaemonSecrets(t *testing.T) {
	t.Setenv("NEXUS_GITHUB_TOKEN", "nexus-secret")
	t.Setenv("GITHUB_TOKEN", "github-secret")
	t.Setenv("GH_TOKEN", "gh-secret")
	t.Setenv("NEXUS_TEST_AGENT", "kept-for-tests")
	t.Setenv("AUDIT_KEEP_ME", "kept")
	env := strings.Join(childEnv(), "\n")
	for _, secret := range []string{"nexus-secret", "github-secret", "gh-secret"} {
		if strings.Contains(env, secret) {
			t.Fatalf("daemon secret %q leaked into the agent environment", secret)
		}
	}
	if !strings.Contains(env, "AUDIT_KEEP_ME=kept") {
		t.Fatal("normal environment entry was removed")
	}
	if !strings.Contains(env, "NEXUS_TEST_AGENT=kept-for-tests") {
		t.Fatal("non-secret NEXUS_* environment entry was removed")
	}
}
