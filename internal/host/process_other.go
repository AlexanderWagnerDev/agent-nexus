//go:build !windows

package host

import "os/exec"

func hideProcessWindow(*exec.Cmd) {}
