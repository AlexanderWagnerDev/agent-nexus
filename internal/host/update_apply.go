package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// InstallStagedUpdate replaces the running daemon binary after asking the
// per-user service to restart. force must be set explicitly because the
// restart interrupts active agent runs; callers should verify that no runs are
// active before setting it.
func InstallStagedUpdate(staged string, force bool) error {
	if !force {
		if status, ok := readRuntimeStatus(); ok && status.ActiveRuns > 0 {
			return fmt.Errorf("refusing update: %d active agent run(s); wait until idle or pass --force", status.ActiveRuns)
		}
	}
	if !serviceInstalled() {
		return errors.New("the host service is not installed; install it with: nexusd service install")
	}
	target, err := os.Executable()
	if err != nil {
		return err
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	if _, err := os.Stat(staged); err != nil {
		return fmt.Errorf("staged binary is missing: %w", err)
	}
	previous := target + ".previous"
	if err := copyFile(target, previous, 0o755); err != nil {
		return fmt.Errorf("cannot keep rollback copy: %w", err)
	}
	if runtime.GOOS == "windows" {
		return scheduleWindowsUpdate(staged, target)
	}
	if err := copyFile(staged, target, 0o755); err != nil {
		return err
	}
	return restartUnixService()
}

// RollbackUpdate restores nexusd.previous through the same service restart
// path. The currently installed binary is kept as nexusd.failed for diagnosis.
func RollbackUpdate(force bool) error {
	if !force {
		if status, ok := readRuntimeStatus(); ok && status.ActiveRuns > 0 {
			return fmt.Errorf("refusing rollback: %d active agent run(s); wait until idle or pass --force", status.ActiveRuns)
		}
	}
	if !serviceInstalled() {
		return errors.New("the host service is not installed; install it with: nexusd service install")
	}
	target, err := os.Executable()
	if err != nil {
		return err
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	previous := target + ".previous"
	if _, err := os.Stat(previous); err != nil {
		return errors.New("no rollback binary (nexusd.previous) is available")
	}
	if err := copyFile(target, target+".failed", 0o755); err != nil {
		return err
	}
	// Do not route through InstallStagedUpdate: it first copies the currently
	// installed binary over target.previous, which would replace the rollback
	// binary with the broken one before restoring it.
	if runtime.GOOS == "windows" {
		return scheduleWindowsUpdate(previous, target)
	}
	if err := copyFile(previous, target, 0o755); err != nil {
		return err
	}
	return restartUnixService()
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// RuntimeStatus is the last published active-run snapshot. A status older
// than two minutes is treated as stale by ReadRuntimeStatus.
type RuntimeStatus = runtimeStatus

// ReadRuntimeStatus exposes the running daemon's runtime snapshot to the
// installer and status command. It never blocks on the daemon.
func ReadRuntimeStatus() (RuntimeStatus, bool) {
	return readRuntimeStatus()
}

func readRuntimeStatus() (runtimeStatus, bool) {
	dir, err := ConfigDir()
	if err != nil {
		return runtimeStatus{}, false
	}
	data, err := os.ReadFile(filepath.Join(dir, "runtime.json"))
	if err != nil {
		return runtimeStatus{}, false
	}
	var status runtimeStatus
	if json.Unmarshal(data, &status) != nil {
		return runtimeStatus{}, false
	}
	if time.Since(status.UpdatedAt) > 2*time.Minute {
		return runtimeStatus{}, false
	}
	return status, true
}
