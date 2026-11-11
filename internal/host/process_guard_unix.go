//go:build !windows

package host

import (
	"os/exec"
	"syscall"
)

// processGuard terminates the whole process group. go-pty starts terminal
// commands with Setsid, and structured commands set Setpgid, so the child pid
// is also the process-group id on Unix.
type processGuard struct {
	pgid int
}

func newProcessGuard() *processGuard { return &processGuard{} }

func (g *processGuard) Attach(pid int) error {
	if pid > 0 {
		g.pgid = pid
	}
	return nil
}

// configureProcessGroup makes a plain exec.Cmd the leader of its own group.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// Terminate asks the group to exit cleanly.
func (g *processGuard) Terminate() {
	if g.pgid > 0 {
		_ = syscall.Kill(-g.pgid, syscall.SIGTERM)
	}
}

// Kill forces the group down.
func (g *processGuard) Kill() {
	if g.pgid > 0 {
		_ = syscall.Kill(-g.pgid, syscall.SIGKILL)
	}
}

func (g *processGuard) Close() {}

// processAlive reports whether pid still exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
