//go:build !windows

package host

import (
	"strings"
	"testing"
	"time"

	"github.com/aymanbagabas/go-pty"
)

// On Unix the daemon used to keep its own pty slave descriptor open, so the
// master never returned EOF after the child exited and a terminal run stayed
// "running" forever. The child's exit must end the read loop.
func TestPTYMasterReachesEOFAfterChildExit(t *testing.T) {
	p, err := pty.New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	c := p.Command("/bin/sh", "-c", "echo agentnexus-pty-done")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	closePTYSlave(p)

	done := make(chan string, 1)
	go func() {
		var out strings.Builder
		buf := make([]byte, 1024)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				out.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		done <- out.String()
	}()
	select {
	case out := <-done:
		if !strings.Contains(out, "agentnexus-pty-done") {
			t.Fatalf("output was not delivered before EOF: %q", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pty read did not end after the child exited")
	}
	_ = c.Wait()
}
