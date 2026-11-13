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
		// --no-block prevents the daemon from waiting for a job that cannot
		// finish until this process exits.
		return runCmd("systemctl", "--user", "--no-block", "restart", "agentnexus.service")
	case "darwin":
		return runCmd("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/dev.agentnexus.nexusd", os.Getuid()))
	}
	return fmt.Errorf("self-update restart is not supported on %s", runtime.GOOS)
}
