package host

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// updateWatchdogResult records what the detached start check did. It is
// informational and never contains paths, tokens or transcripts.
type updateWatchdogResult struct {
	At       time.Time `json:"at"`
	Expected string    `json:"expected"`
	Current  string    `json:"current,omitempty"`
	Rollback bool      `json:"rollback"`
	Error    string    `json:"error,omitempty"`
}

// StartUpdateWatchdog copies the current (known working) executable and starts
// it in a detached scope before the binary is replaced. If the new daemon does
// not report the expected version within the startup window, the copy restores
// nexusd.previous and restarts the service. Windows uses the Task Scheduler
// helper instead because a running image cannot be replaced in place.
func StartUpdateWatchdog(expected string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	target, err := os.Executable()
	if err != nil {
		return err
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	helper := filepath.Join(dir, "update-watchdog")
	if err := copyFile(target, helper, 0o755); err != nil {
		return err
	}
	return spawnDetachedWatchdog(helper, []string{
		"update", "--verify-rollback",
		"--target", target,
		"--previous", target + ".previous",
		"--expected", strings.TrimPrefix(expected, "v"),
	})
}

// VerifyUpdateAndRollback is the detached half of StartUpdateWatchdog. It
// waits for the new daemon to publish its runtime status and rolls back only
// when the service is present but not healthy.
func VerifyUpdateAndRollback(target, previous, expected string) error {
	time.Sleep(45 * time.Second)
	if expected == "" || target == "" || previous == "" {
		return errors.New("watchdog parameters are incomplete")
	}
	status, fresh := readRuntimeStatus()
	result := updateWatchdogResult{At: time.Now().UTC(), Expected: expected}
	if fresh {
		result.Current = status.Version
	}
	healthy := fresh && strings.TrimPrefix(status.Version, "v") == strings.TrimPrefix(expected, "v")
	if healthy {
		return writeWatchdogResult(result)
	}
	if !serviceNeedsRecovery() {
		result.Error = "service is not in a failed or starting state; no rollback"
		return writeWatchdogResult(result)
	}
	previousInfo, err := os.Stat(previous)
	if err != nil {
		result.Error = "no previous binary available for rollback"
		return writeWatchdogResult(result)
	}
	// A stale copy belongs to an earlier update and must not replace the
	// currently installed binary merely because this attempt failed early.
	if time.Since(previousInfo.ModTime()) > 5*time.Minute {
		result.Error = "rollback copy is stale; the current update did not replace the binary"
		return writeWatchdogResult(result)
	}
	if err := copyFile(previous, target, 0o755); err != nil {
		result.Error = err.Error()
		_ = writeWatchdogResult(result)
		return err
	}
	result.Rollback = true
	if err := restartUnixService(); err != nil {
		result.Error = "restart after rollback failed: " + err.Error()
	} else {
		result.Error = ""
	}
	return writeWatchdogResult(result)
}

func writeWatchdogResult(result updateWatchdogResult) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "update-watchdog.json"), data, 0o600)
}

// AddUpdateWatchdogArgs is used by the CLI parser tests to keep the flag names
// in one place.
func AddUpdateWatchdogArgs(target, previous, expected string) []string {
	return []string{
		"update", "--verify-rollback",
		"--target", target,
		"--previous", previous,
		"--expected", expected,
	}
}
