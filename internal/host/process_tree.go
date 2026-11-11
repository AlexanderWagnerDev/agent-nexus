package host

import (
	"os/exec"
	"time"
)

// stopProcessTree asks the whole tree to exit, waits up to grace for the
// direct child, then forces the tree down. guard may be nil when job or group
// creation failed; the direct process is still killed.
func stopProcessTree(guard *processGuard, cmd *exec.Cmd, grace time.Duration) {
	if guard != nil {
		guard.Terminate()
	}
	pid := 0
	if cmd != nil && cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	deadline := time.Now().Add(grace)
	for pid > 0 && time.Now().Before(deadline) && processAlive(pid) {
		time.Sleep(50 * time.Millisecond)
	}
	if guard != nil {
		guard.Kill()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if guard != nil {
		guard.Close()
	}
}
