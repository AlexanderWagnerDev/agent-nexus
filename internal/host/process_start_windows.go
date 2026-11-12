//go:build windows

package host

import (
	"os/exec"
	"syscall"

	"github.com/aymanbagabas/go-pty"
	"golang.org/x/sys/windows"
)

// suspendForJob makes the child start suspended. The job object is assigned
// while no user code has run yet, and the guard resumes it afterwards. This
// closes the create-then-assign window in which a fast grandchild could
// escape the job.
func suspendForJob(c *pty.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
}

func suspendExecForJob(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
}
