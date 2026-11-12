//go:build windows

package host

import (
	"os/exec"
	"testing"
	"time"
)

// TestSuspendedStartIsResumedByGuard guards the CREATE_SUSPENDED handshake:
// a child that was suspended for job assignment must run and exit after
// Attach, not hang forever.
func TestSuspendedStartIsResumedByGuard(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	suspendExecForJob(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	guard := newProcessGuard()
	if err := guard.Attach(cmd.Process.Pid); err != nil {
		t.Logf("job assignment failed (resume still required): %v", err)
	}
	defer guard.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("suspended child did not exit cleanly: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("suspended child was never resumed")
	}
}
