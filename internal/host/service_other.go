//go:build !windows

package host

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func windowsTaskService(action, exe string) error {
	return fmt.Errorf("windows task service is not available on this platform")
}

func serviceInstalled() bool {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "linux":
		unit := filepath.Join(home, ".config", "systemd", "user", "agentnexus.service")
		if _, err := os.Stat(unit); err != nil {
			return false
		}
		return runCmd("systemctl", "--user", "is-enabled", "--quiet", "agentnexus.service") == nil
	case "darwin":
		plist := filepath.Join(home, "Library", "LaunchAgents", "dev.agentnexus.nexusd.plist")
		_, err := os.Stat(plist)
		return err == nil
	}
	return false
}
