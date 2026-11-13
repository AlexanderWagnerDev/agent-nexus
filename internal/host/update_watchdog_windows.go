//go:build windows

package host

import "errors"

func spawnDetachedWatchdog(string, []string) error {
	return errors.New("the watchdog runs through the Windows update helper")
}

func serviceNeedsRecovery() bool { return false }
