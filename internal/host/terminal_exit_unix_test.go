//go:build !windows

package host

import (
	"testing"
	"time"

	"github.com/aymanbagabas/go-pty"
)

// A quiet descendant that inherited the pty keeps a blocking master read open
// on Unix. Closing the master does not interrupt that read, so the supervisor
// must terminate the contained process group to make the read return.
func TestTerminalExitSupervisorKillsLingeringDescendant(t *testing.T) {
	p, err := pty.New()
	if err != nil {
		t.Fatal(err)
	}
	c := p.Command("/bin/sh", "-c", "sleep 30 & exit")
	if err := c.Start(); err != nil {
		_ = p.Close()
		t.Fatal(err)
	}
	closePTYSlave(p)
	guard := newProcessGuard()
	if err := guard.Attach(c.Process.Pid); err != nil {
		t.Fatal(err)
	}
	r := &Run{id: "pty-descendant", pty: p, cmd: c, guard: guard}
	t.Cleanup(func() {
		r.closePTY()
		guard.Close()
	})

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
	case <-time.After(10 * time.Second):
		t.Fatal("pty read stayed blocked after the agent exited; the run would stay running")
	}
}
