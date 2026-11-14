//go:build !windows

package host

import (
	"os/exec"
	"testing"
	"time"
)

// A descendant that inherits the child's stdout must not prevent Wait from
// returning. This is why the structured drivers use plain OS pipes instead of
// exec.StdoutPipe and call Wait immediately.
func TestStdioPipesDoNotDelayWait(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "echo started; (sleep 5) &")
	stdout, stderr, closeWrite, err := stdioPipes(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	closeWrite()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Wait blocked on a pipe inherited by a descendant")
	}
	closePipe(stdout)
	closePipe(stderr)
}
