//go:build windows

package host

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aymanbagabas/go-pty"
)

// An agent CLI can leave a descendant (MCP server, background helper) attached
// to the pty. The direct child exits, the master read stays open, and without
// the exit supervisor the run would report "running" forever.
func TestTerminalExitClosesPTYWithLingeringDescendant(t *testing.T) {
	p, err := pty.New()
	if err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	c := p.Command(shell, "/D", "/S", "/C", "start /b ping -n 4 127.0.0.1 >nul & exit")
	c.Dir = t.TempDir()
	if err := c.Start(); err != nil {
		_ = p.Close()
		t.Fatal(err)
	}
	var once sync.Once
	closePTY := func() { once.Do(func() { _ = p.Close() }) }
	t.Cleanup(closePTY)

	exited := make(chan struct{})
	go superviseTerminalExit(c, exited, closePTY)

	readDone := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := p.Read(buf); err != nil {
				close(readDone)
				return
			}
		}
	}()

	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the agent process was not reaped")
	}
	select {
	case <-readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("pty read stayed blocked after the agent exited; the run would stay running")
	}
}
