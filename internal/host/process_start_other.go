//go:build !windows

package host

import (
	"os/exec"

	"github.com/aymanbagabas/go-pty"
)

func suspendForJob(*pty.Cmd)      {}
func suspendExecForJob(*exec.Cmd) {}
