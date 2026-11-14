package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// InstallStagedUpdate replaces the running daemon binary after asking the
// per-user service to restart. force must be set explicitly because the
// restart interrupts active agent runs; callers should verify that no runs are
// active before setting it.
func InstallStagedUpdate(staged string, force bool) error {
	release, err := acquireUpdateLock()
	if err != nil {
		return err
	}
	defer release()
	if !force {
		if status, ok := readRuntimeStatus(); ok && status.ActiveRuns > 0 {
			return fmt.Errorf("refusing update: %d active agent run(s); wait until idle or pass --force", status.ActiveRuns)
		}
	}
	if !serviceInstalled() {
		return errors.New("the host service is not installed; install it with: nexusd service install")
	}
	if runtime.GOOS != "windows" {
		required := "systemctl"
		if runtime.GOOS == "darwin" {
			required = "launchctl"
		}
		if _, err := exec.LookPath(required); err != nil {
			return fmt.Errorf("%s is required to restart the host service after an update", required)
		}
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
	// Do not overwrite the last known-good rollback copy with a binary that is
	// currently in a failed or unverified state.
	preservePrevious := false
	if state, err := loadUpdateState(); err == nil {
		sameTarget := strings.TrimPrefix(state.TargetVersion, "v") == strings.TrimPrefix(Version, "v") && state.TargetVersion != ""
		if sameTarget && (state.Phase == UpdatePhaseFailed || state.Phase == UpdatePhaseRolledBack || state.Phase == UpdatePhaseVerifying) {
			preservePrevious = true
		}
	}
	if !preservePrevious {
		if err := copyFile(target, previous, 0o755); err != nil {
			return fmt.Errorf("cannot keep rollback copy: %w", err)
		}
	} else if _, err := os.Stat(previous); err != nil {
		return fmt.Errorf("refusing update without a usable rollback copy: %w", err)
	}
	if runtime.GOOS == "windows" {
		return scheduleWindowsUpdate(staged, target)
	}
	if err := copyFile(staged, target, 0o755); err != nil {
		return err
	}
	return restartUnixService()
}

// InstallOwnUpdate replaces the currently running executable without touching
// the daemon service or host identity. It is used by the standalone `nexus`
// client. On Windows a small detached helper waits for this process to exit
// before replacing the locked image.
func InstallOwnUpdate(staged string) error {
	release, err := acquireUpdateLock()
	if err != nil {
		return err
	}
	defer release()
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
	if err := copyFile(target, target+".previous", 0o755); err != nil {
		return fmt.Errorf("cannot keep rollback copy: %w", err)
	}
	if runtime.GOOS == "windows" {
		return scheduleWindowsSelfReplace(staged, target)
	}
	return copyFile(staged, target, 0o755)
}

// RollbackUpdate restores nexusd.previous through the same service restart
// path. The currently installed binary is kept as nexusd.failed for diagnosis.
func RollbackUpdate(force bool) error {
	release, err := acquireUpdateLock()
	if err != nil {
		return err
	}
	defer release()
	if !force {
		if status, ok := readRuntimeStatus(); ok && status.ActiveRuns > 0 {
			return fmt.Errorf("refusing rollback: %d active agent run(s); wait until idle or pass --force", status.ActiveRuns)
		}
	}
	if !serviceInstalled() {
		return errors.New("the host service is not installed; install it with: nexusd service install")
	}
	if runtime.GOOS != "windows" {
		required := "systemctl"
		if runtime.GOOS == "darwin" {
			required = "launchctl"
		}
		if _, err := exec.LookPath(required); err != nil {
			return fmt.Errorf("%s is required to restart the host service after an update", required)
		}
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
		recordRollbackBlock(Version)
		return scheduleWindowsUpdate(previous, target)
	}
	if err := copyFile(previous, target, 0o755); err != nil {
		return err
	}
	recordRollbackBlock(Version)
	return restartUnixService()
}

// acquireUpdateLock prevents two installer processes from replacing the same
// binary concurrently. A lock older than 30 minutes is considered stale, so a
// crashed updater cannot block updates forever.
func acquireUpdateLock() (func(), error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "update.lock")
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
			_ = file.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) < 30*time.Minute {
			return nil, errors.New("another update is already in progress (update.lock); wait for it to finish")
		}
		_ = os.Remove(path)
	}
	return nil, errors.New("cannot acquire update.lock")
}

// clearStaleUpdateLock removes an update lock left behind by a daemon that was
// replaced while holding it. The 30-minute guard in acquireUpdateLock still
// protects a genuinely running installer.
// clearStaleUpdateLock removes an update lock left behind by a daemon that was
// killed mid-install. A lock whose recorded process is still alive is left in
// place, even at startup: deleting it would let a second installer replace the
// binary concurrently. The age limit still recovers from PID reuse.
func clearStaleUpdateLock(maxAge time.Duration) {
	dir, err := ConfigDir()
	if err != nil {
		return
	}
	path := filepath.Join(dir, "update.lock")
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	age := time.Since(info.ModTime())
	if maxAge > 0 && age > maxAge {
		_ = os.Remove(path)
		return
	}
	if age > 30*time.Minute {
		_ = os.Remove(path)
		return
	}
	data, _ := os.ReadFile(path)
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		// Unreadable lock: only discard it when it is old enough to be stale.
		if age > time.Minute {
			_ = os.Remove(path)
		}
		return
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || !processAlive(pid) {
		_ = os.Remove(path)
	}
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := out.Name()
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Chmod(mode); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
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
