//go:build !windows

package host

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// spawnDetachedWatchdog starts a copy of the previous binary outside the
// service's cgroup where possible. systemd-run's transient timer survives the
// restart of the daemon unit; the Setsid fallback covers hosts without
// systemd-run (for example macOS).
func spawnDetachedWatchdog(exe string, args []string) error {
	if runtime.GOOS == "linux" {
		if path, err := exec.LookPath("systemd-run"); err == nil {
			unit := fmt.Sprintf("agentnexus-update-verify-%d", time.Now().UnixNano())
			runArgs := append([]string{
				"--user", "--collect", "--unit", unit, "--on-active=5s", exe,
			}, args...)
			cmd := exec.Command(path, runArgs...)
			// Run, not Start: systemd-run reports "Failed to connect to bus"
			// through its exit status, and a silently non-existent watchdog
			// would remove the rollback guarantee.
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func serviceNeedsRecovery() bool {
	switch runtime.GOOS {
	case "linux":
		out, err := exec.Command("systemctl", "--user", "is-active", "agentnexus.service").Output()
		state := strings.TrimSpace(string(out))
		if state == "failed" || state == "activating" || state == "reloading" || state == "deactivating" {
			return true
		}
		// An unknown job means the unit disappeared; only treat that as a
		// failure when the command itself failed for a reason other than a
		// deliberately inactive service.
		return err != nil && state != "inactive" && state != "unknown"
	case "darwin":
		// The runtime check has already failed. Restoring the last known-good
		// binary is safer than leaving an unreachable host after an update.
		return true
	}
	return false
}
