//go:build windows

package host

import (
	"os"
	"path/filepath"
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
	r := &Run{id: "pty-descendant", pty: p, cmd: c}
	t.Cleanup(r.closePTY)

	exited := make(chan struct{})
	go r.superviseTerminalExit(c, exited)

	readDone := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		r.readerStarted.Store(true)
		for {
			r.readerBusy.Store(false)
			n, err := p.Read(buf)
			if n > 0 {
				r.readerBusy.Store(true)
				r.lastReadAt.Store(time.Now().UnixNano())
				r.readerBusy.Store(false)
			}
			if err != nil {
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
