//go:build !windows

package host

import (
	"fmt"
	"os"
	"runtime"
)

func restartUnixService() error {
	switch runtime.GOOS {
	case "linux":
		return runCmd("systemctl", "--user", "restart", "agentnexus.service")
	case "darwin":
		return runCmd("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/dev.agentnexus.nexusd", os.Getuid()))
	}
	return fmt.Errorf("self-update restart is not supported on %s", runtime.GOOS)
}
