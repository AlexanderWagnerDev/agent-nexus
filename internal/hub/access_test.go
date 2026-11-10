package hub

import "testing"

func TestGrantPermissions(t *testing.T) {
	if got := normalizePermissions([]string{"VIEW", "resume", "resume", "bogus"}); got != "view,resume" {
		t.Fatalf("unexpected normalized permissions: %q", got)
	}
	if !grantAllows("view,resume", "resume") {
		t.Fatal("resume should be allowed")
	}
	if grantAllows("view", "resume") {
		t.Fatal("resume should not be allowed")
	}
	if !grantAllows("manage", "approve") {
		t.Fatal("manage should imply approve")
	}
}
