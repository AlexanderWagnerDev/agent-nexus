package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// Update phases reported to the hub and persisted across restarts. The hub
// only displays them; it cannot advance or bypass the local updater policy.
const (
	UpdatePhaseIdle        = "idle"
	UpdatePhaseChecking    = "checking"
	UpdatePhaseDownloading = "downloading"
	UpdatePhaseReady       = "ready"
	UpdatePhaseWaitingIdle = "waiting_for_idle"
	UpdatePhaseInstalling  = "installing"
	UpdatePhaseVerifying   = "verifying"
	UpdatePhaseSuccess     = "success"
	UpdatePhaseFailed      = "failed"
	UpdatePhaseRolledBack  = "rolled_back"
	UpdatePhaseBlocked     = "blocked"
	UpdatePhaseDisabled    = "disabled"
)

const (
	updateVerifyWindow = 60 * time.Second
	updateMaxAttempts  = 3
	updateIdleTimeout  = 30 * time.Minute
)

// updateState is persisted in the host config directory. It holds only
// update metadata; no credentials, transcripts or agent data.
type updateState struct {
	ETag            string     `json:"etag,omitempty"`
	Info            UpdateInfo `json:"info"`
	Channel         string     `json:"channel,omitempty"`
	Phase           string     `json:"phase,omitempty"`
	PhaseAt         time.Time  `json:"phase_at,omitempty"`
	LastCheck       time.Time  `json:"last_check,omitempty"`
	NextCheck       time.Time  `json:"next_check,omitempty"`
	LastAttempt     time.Time  `json:"last_attempt,omitempty"`
	LastSuccess     time.Time  `json:"last_success,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	Message         string     `json:"message,omitempty"`
	SignatureMode   string     `json:"signature_mode,omitempty"`
	TargetVersion   string     `json:"target_version,omitempty"`
	PreviousVersion string     `json:"previous_version,omitempty"`
	VerifyAt        time.Time  `json:"verify_at,omitempty"`
	Attempts        int        `json:"attempts,omitempty"`
	Failures        int        `json:"consecutive_failures,omitempty"`
	InstallFailures int        `json:"install_failures,omitempty"`
	BlockedVersion  string     `json:"blocked_version,omitempty"`
	Blocked         []string   `json:"blocked_versions,omitempty"`
	Rollback        bool       `json:"rollback,omitempty"`
}

func updateStatePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "update-state.json"), nil
}

func loadUpdateState() (updateState, error) {
	path, err := updateStatePath()
	if err != nil {
		return updateState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return updateState{}, err
	}
	var state updateState
	if err := json.Unmarshal(data, &state); err != nil {
		return updateState{}, fmt.Errorf("invalid update state: %w", err)
	}
	return state, nil
}

func saveUpdateState(state updateState) error {
	path, err := updateStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// UpdateState is the persisted updater snapshot exposed to the CLI.
type UpdateState = updateState

// ReadUpdateState returns the persisted updater state, if any.
func ReadUpdateState() (UpdateState, bool) {
	state, err := loadUpdateState()
	return state, err == nil
}

func updateConfigured() bool {
	if value := strings.ToLower(strings.TrimSpace(os.Getenv("NEXUS_AUTO_UPDATE"))); value != "" {
		switch value {
		case "0", "off", "false", "no", "disabled":
			return false
		}
	}
	return true
}

// nextUpdateDelay applies exponential backoff on consecutive failures and a
// bounded jitter so a fleet of hosts does not query GitHub in lockstep.
func nextUpdateDelay(interval time.Duration, failures int) time.Duration {
	if interval <= 0 {
		interval = DefaultUpdateInterval
	}
	delay := interval
	if failures > 0 {
		backoff := interval
		for i := 0; i < failures && backoff < MaxUpdateInterval; i++ {
			backoff *= 2
		}
		if backoff > MaxUpdateInterval {
			backoff = MaxUpdateInterval
		}
		delay = backoff
	}
	window := delay / 5
	if window <= 0 {
		return delay
	}
	return delay + jitter(window)
}

// updatePrefs reloads the host-local settings so `nexusd update --configure`
// takes effect without restarting the daemon. The in-memory config is the
// fallback when the file is temporarily unreadable.
func (d *Daemon) updatePrefs() UpdateSettings {
	if cfg, err := LoadConfig(); err == nil {
		return cfg.UpdatePrefs()
	}
	return d.cfg.UpdatePrefs()
}

func (d *Daemon) loadUpdateState() updateState {
	state, err := loadUpdateState()
	if err != nil {
		return updateState{Phase: UpdatePhaseIdle}
	}
	if state.Phase == "" {
		state.Phase = UpdatePhaseIdle
	}
	return state
}

func (d *Daemon) saveUpdateState(state updateState) {
	if err := saveUpdateState(state); err != nil {
		d.log.Warn("cannot persist update state", "error", err)
	}
}

// updateLoop checks at startup, then at the configured interval. Reconnects
// wake the loop early so an update check follows restored connectivity.
func (d *Daemon) updateLoop(ctx context.Context) {
	d.resumeUpdateVerification(ctx)
	d.runUpdateCycle(ctx)
	for {
		prefs := d.updatePrefs()
		delay := nextUpdateDelay(prefs.Interval(), 0)
		if state, err := loadUpdateState(); err == nil && time.Until(state.NextCheck) > 0 {
			delay = time.Until(state.NextCheck)
		}
		if delay < time.Minute {
			delay = time.Minute
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-d.updateWake:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
		d.runUpdateCycle(ctx)
	}
}

func (d *Daemon) runUpdateCycle(ctx context.Context) {
	if !d.updateRunMu.TryLock() {
		d.log.Debug("update cycle already running")
		return
	}
	defer d.updateRunMu.Unlock()
	prefs := d.updatePrefs()
	state := d.loadUpdateState()
	now := time.Now().UTC()
	channel := prefs.Channel
	if env := strings.TrimSpace(os.Getenv("NEXUS_UPDATE_CHANNEL")); env != "" {
		channel = NormalizeUpdateChannel(env, Version)
	}
	etag := state.ETag
	if state.Channel != "" && state.Channel != channel {
		// A cached ETag from another channel can return 304 with a
		// prerelease payload on a stable host. Discard both.
		etag = ""
		state.Info = UpdateInfo{}
	}
	state.Channel = channel
	if prefs.Disabled || !updateConfigured() {
		state.Phase = UpdatePhaseDisabled
		state.Message = "automatic updates are disabled on this host"
		state.NextCheck = time.Time{}
		d.saveUpdateState(state)
		d.reportUpdateStatus()
		return
	}
	state.Phase = UpdatePhaseChecking
	state.LastCheck = now
	d.saveUpdateState(state)
	d.reportUpdateStatus()

	info, etag, notModified, err := CheckForUpdateCached(ctx, Version, channel, etag)
	if err != nil {
		state.LastError = err.Error()
		if errors.Is(err, ErrNoRelease) {
			state.Phase = UpdatePhaseIdle
			state.LastError = ErrNoRelease.Error()
			state.Message = "no published release for this channel yet"
			state.Failures = 0
			state.Info = UpdateInfo{Current: Version, Channel: channel}
		} else {
			state.Phase = UpdatePhaseFailed
			state.Message = "update check failed"
			state.Failures++
		}
		state.NextCheck = now.Add(nextUpdateDelay(prefs.Interval(), state.Failures))
		d.saveUpdateState(state)
		d.reportUpdateStatus()
		return
	}
	if etag != "" {
		state.ETag = etag
	}
	if notModified {
		info = state.Info
		info.Current = Version
		info.Channel = channel
		info.Available = info.Latest != "" && compareVersions(info.Latest, Version) > 0
		if info.Latest == "" {
			state.LastError = "release feed returned 304 without cached metadata"
			state.Phase = UpdatePhaseFailed
			state.Message = "update check failed"
			state.Failures++
			state.NextCheck = now.Add(nextUpdateDelay(prefs.Interval(), state.Failures))
			d.saveUpdateState(state)
			d.reportUpdateStatus()
			return
		}
	}
	info.Current = Version
	info.Channel = channel
	state.Info = info
	state.LastError = ""
	state.Failures = 0
	state.NextCheck = now.Add(nextUpdateDelay(prefs.Interval(), 0))
	if !info.Available {
		state.Phase = UpdatePhaseIdle
		state.Message = "up to date"
		d.saveUpdateState(state)
		d.reportUpdateStatus()
		return
	}
	if prefs.IsBlocked(info.Latest) || sameVersion(state.BlockedVersion, info.Latest) {
		state.Phase = UpdatePhaseBlocked
		state.BlockedVersion = info.Latest
		state.Message = "version " + info.Latest + " is blocked after repeated failures; remove it from updates.blocked_versions to retry"
		d.saveUpdateState(state)
		d.reportUpdateStatus()
		return
	}
	// A version removed from the configured block list is retried; clear the
	// stale display state as well.
	state.Blocked = nil
	state.BlockedVersion = ""
	if !prefs.InstallIdle() {
		state.Phase = UpdatePhaseReady
		state.Message = "update " + info.Latest + " is ready; automatic installation is disabled"
		d.saveUpdateState(state)
		d.reportUpdateStatus()
		return
	}
	if !ServiceInstalled() {
		state.Phase = UpdatePhaseReady
		state.Message = "update " + info.Latest + " is ready, but automatic installation needs the per-user service"
		d.saveUpdateState(state)
		d.reportUpdateStatus()
		return
	}
	d.autoInstall(ctx, prefs, state, info)
}

func (d *Daemon) autoInstall(ctx context.Context, prefs UpdateSettings, state updateState, info UpdateInfo) {
	dir, err := ConfigDir()
	if err != nil {
		state.Phase = UpdatePhaseFailed
		state.LastError = err.Error()
		d.saveUpdateState(state)
		d.reportUpdateStatus()
		return
	}
	state.Phase = UpdatePhaseDownloading
	state.Message = "downloading and verifying " + info.Latest
	state.LastAttempt = time.Now().UTC()
	d.saveUpdateState(state)
	d.reportUpdateStatus()

	cleanupStagedUpdates(filepath.Join(dir, "updates"), strings.TrimPrefix(info.Latest, "v"))
	stagedDir := filepath.Join(dir, "updates", strings.TrimPrefix(info.Latest, "v"))
	staged, mode, err := StageUpdateWithMode(ctx, info, stagedDir, prefs.SignatureRequired())
	if err != nil {
		d.updateFailed(state, info.Latest, err, prefs)
		return
	}
	if err := VerifyStagedBinary(ctx, staged, info.Latest); err != nil {
		d.updateFailed(state, info.Latest, err, prefs)
		return
	}
	state.SignatureMode = mode
	state.TargetVersion = info.Latest
	state.Phase = UpdatePhaseWaitingIdle
	state.Message = "waiting for active agent runs to finish"
	d.saveUpdateState(state)
	d.reportUpdateStatus()

	if !d.waitForIdleAndGate(ctx, updateIdleTimeout) {
		state.Phase = UpdatePhaseWaitingIdle
		state.Message = "still waiting for active agent runs"
		d.saveUpdateState(state)
		d.reportUpdateStatus()
		return
	}

	state.Phase = UpdatePhaseInstalling
	state.Message = "installing " + info.Latest
	state.PhaseAt = time.Now().UTC()
	state.Attempts++
	d.saveUpdateState(state)
	d.reportUpdateStatus()

	if err := StartUpdateWatchdog(info.Latest); err != nil {
		d.log.Warn("cannot start the update watchdog; continuing with the service health check", "error", err)
	}
	if err := InstallStagedUpdate(staged, true); err != nil {
		d.updating.Store(false)
		d.updateFailed(state, info.Latest, err, prefs)
		return
	}
	// On Windows the helper replaces the binary while this process exits. On
	// Unix the service restart normally stops this process. Keep the gate set
	// until the replacement; if this process survives an unusual restart path,
	// release it after a bounded fallback instead of blocking runs forever.
	time.AfterFunc(10*time.Minute, func() {
		if d.updating.Swap(false) {
			d.log.Warn("update gate released without a process restart")
		}
	})
	state.Phase = UpdatePhaseVerifying
	state.VerifyAt = time.Now().UTC().Add(updateVerifyWindow)
	d.saveUpdateState(state)
	go d.confirmUpdate(info.Latest, state.Attempts)
}

// waitForIdleAndGate waits without blocking new runs, then atomically gates
// starts and re-checks the run maps. A run that reserved a slot just before
// the gate is still visible in startingRuns; a run that arrives after the
// gate is rejected. This closes the check-then-replace race without stopping
// users from working during the idle wait.
func (d *Daemon) waitForIdleAndGate(ctx context.Context, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if ctx.Err() != nil {
			return false
		}
		if d.activeRunCount() == 0 {
			d.updating.Store(true)
			if d.activeRunCount() == 0 {
				return true
			}
			d.updating.Store(false)
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
}

func (d *Daemon) updateFailed(state updateState, version string, cause error, prefs UpdateSettings) {
	state.Phase = UpdatePhaseFailed
	state.LastError = cause.Error()
	state.Message = "update " + version + " failed: " + cause.Error()
	state.Failures++
	state.InstallFailures++
	if state.Attempts >= updateMaxAttempts || state.InstallFailures >= updateMaxAttempts {
		state.Phase = UpdatePhaseBlocked
		state.BlockedVersion = version
		state.Blocked = appendUnique(state.Blocked, version)
		prefs.BlockedVersions = appendUnique(prefs.BlockedVersions, version)
		_ = SaveUpdatePrefs(prefs)
		state.Message = "update " + version + " is blocked after repeated failures; remove it from updates.blocked_versions to retry"
	}
	d.saveUpdateState(state)
	d.reportUpdateStatus()
}

// cleanupStagedUpdates removes old staged versions while keeping the one that
// is about to be installed. It never touches the installed binary or config.
func cleanupStagedUpdates(root, keep string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == keep {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, entry.Name()))
	}
}

// ClearUpdateBlock removes a version from the persisted display state after
// an operator explicitly unblocks it in the local configuration.
func ClearUpdateBlock(version string) {
	state, err := loadUpdateState()
	if err != nil {
		return
	}
	if strings.TrimPrefix(state.BlockedVersion, "v") == strings.TrimPrefix(version, "v") {
		state.BlockedVersion = ""
		state.Phase = UpdatePhaseIdle
		state.Message = "block removed for " + version
	}
	if state.Phase == UpdatePhaseBlocked {
		state.InstallFailures = 0
		state.Attempts = 0
		state.Failures = 0
	}
	kept := state.Blocked[:0]
	for _, existing := range state.Blocked {
		if strings.TrimPrefix(existing, "v") != strings.TrimPrefix(version, "v") {
			kept = append(kept, existing)
		}
	}
	state.Blocked = kept
	_ = saveUpdateState(state)
}

func sameVersion(a, b string) bool {
	return strings.TrimPrefix(strings.TrimSpace(a), "v") == strings.TrimPrefix(strings.TrimSpace(b), "v") &&
		strings.TrimPrefix(strings.TrimSpace(a), "v") != ""
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.TrimPrefix(existing, "v") == value {
			return values
		}
	}
	return append(values, value)
}

func (d *Daemon) confirmUpdate(target string, attempts int) {
	time.Sleep(updateVerifyWindow)
	if strings.TrimPrefix(Version, "v") != strings.TrimPrefix(target, "v") {
		return
	}
	state := d.loadUpdateState()
	if state.TargetVersion != target {
		return
	}
	state.Phase = UpdatePhaseSuccess
	state.Attempts = 0
	state.Failures = 0
	state.InstallFailures = 0
	state.Rollback = false
	state.LastSuccess = time.Now().UTC()
	state.Message = "updated to " + target
	d.saveUpdateState(state)
	d.updating.Store(false)
	d.log.Info("update verified", "version", target, "attempts", attempts)
	d.reportUpdateStatus()
}

// resumeUpdateVerification settles an interrupted update. A new binary that
// starts and stays up for the verification window is accepted; repeated
// restarts inside that window trigger a rollback to nexusd.previous.
func (d *Daemon) resumeUpdateVerification(ctx context.Context) {
	state := d.loadUpdateState()
	if state.Phase != UpdatePhaseInstalling && state.Phase != UpdatePhaseVerifying {
		return
	}
	target := strings.TrimPrefix(state.TargetVersion, "v")
	current := strings.TrimPrefix(Version, "v")
	if target == "" {
		state.Phase = UpdatePhaseFailed
		state.LastError = "pending update state has no target version"
		state.Message = "pending update state was incomplete"
		d.saveUpdateState(state)
		return
	}
	if current != target {
		if state.Phase == UpdatePhaseInstalling || state.Phase == UpdatePhaseVerifying {
			state.Phase = UpdatePhaseBlocked
			state.LastError = "installed version does not match the pending update"
			state.Message = "update " + target + " is blocked because the expected version did not start; remove it from updates.blocked_versions to retry"
			state.BlockedVersion = target
			state.Blocked = appendUnique(state.Blocked, target)
			state.InstallFailures++
			prefs := d.updatePrefs()
			prefs.BlockedVersions = appendUnique(prefs.BlockedVersions, target)
			_ = SaveUpdatePrefs(prefs)
			d.saveUpdateState(state)
			d.reportUpdateStatus()
		}
		return
	}
	now := time.Now().UTC()
	if state.Phase == UpdatePhaseInstalling {
		state.Attempts = 1
		state.Phase = UpdatePhaseVerifying
		state.PhaseAt = now
		state.VerifyAt = now.Add(updateVerifyWindow)
		d.saveUpdateState(state)
		go d.confirmUpdate(target, 1)
		return
	}
	// A restart inside the verification window is treated as a failed start.
	if now.Sub(state.PhaseAt) < 2*updateVerifyWindow {
		state.Attempts++
		if state.Attempts >= updateMaxAttempts {
			state.Rollback = true
			state.Phase = UpdatePhaseRolledBack
			state.BlockedVersion = target
			state.Blocked = appendUnique(state.Blocked, target)
			state.Message = "rolled back after " + fmt.Sprint(state.Attempts) + " failed starts"
			d.saveUpdateState(state)
			prefs := d.updatePrefs()
			prefs.BlockedVersions = appendUnique(prefs.BlockedVersions, target)
			_ = SaveUpdatePrefs(prefs)
			if err := RollbackUpdate(true); err != nil {
				d.log.Error("automatic rollback failed", "error", err)
				state.LastError = err.Error()
				d.saveUpdateState(state)
			}
			d.reportUpdateStatus()
			return
		}
		state.Phase = UpdatePhaseVerifying
		state.PhaseAt = now
		state.VerifyAt = now.Add(updateVerifyWindow)
		d.saveUpdateState(state)
		go d.confirmUpdate(target, state.Attempts)
		return
	}
	// The process stayed up long enough; finish an interrupted verification.
	state.Phase = UpdatePhaseSuccess
	state.Attempts = 0
	state.Failures = 0
	state.InstallFailures = 0
	state.LastSuccess = now
	state.Message = "updated to " + target
	d.saveUpdateState(state)
	d.updating.Store(false)
}

// buildUpdateStatus converts the local state into the hub wire format.
func (d *Daemon) buildUpdateStatus() *protocol.UpdateStatus {
	prefs := d.updatePrefs()
	state := d.loadUpdateState()
	channel := prefs.Channel
	if state.Channel != "" {
		channel = state.Channel
	}
	status := &protocol.UpdateStatus{
		Current:       Version,
		Latest:        state.Info.Latest,
		Available:     state.Info.Latest != "" && compareVersions(state.Info.Latest, Version) > 0,
		Prerelease:    state.Info.Prerelease,
		CheckedAt:     state.LastCheck,
		Error:         state.LastError,
		Channel:       channel,
		AutoEnabled:   !prefs.Disabled && updateConfigured(),
		InstallIdle:   prefs.InstallIdle(),
		State:         state.Phase,
		Message:       state.Message,
		Signature:     state.SignatureMode,
		LastAttempt:   state.LastAttempt,
		LastSuccess:   state.LastSuccess,
		Blocked:       state.BlockedVersion,
		RemoteInstall: prefs.AllowRemoteInstall,
	}
	if status.State == "" {
		status.State = UpdatePhaseIdle
	}
	return status
}

func (d *Daemon) reportUpdateStatus() {
	status := d.buildUpdateStatus()
	d.updateMu.Lock()
	d.lastUpdate = status
	d.updateMu.Unlock()
	d.sendUpdateStatus(status)
}

// CheckUpdateNow triggers a host-local check, for example from the hub's
// "Check for updates" button. It never installs anything.
func (d *Daemon) CheckUpdateNow(ctx context.Context, channel string) (UpdateInfo, error) {
	prefs := d.updatePrefs()
	if channel == "" {
		channel = NormalizeUpdateChannel(prefs.Channel, Version)
	}
	// A manual check must not block behind a 30-minute idle wait. When an
	// installation is in progress, return the feed result without touching the
	// persisted update state.
	if !d.updateRunMu.TryLock() {
		return CheckForUpdate(ctx, Version, channel)
	}
	defer d.updateRunMu.Unlock()
	info, err := CheckForUpdate(ctx, Version, channel)
	state := d.loadUpdateState()
	state.LastCheck = time.Now().UTC()
	state.Channel = info.Channel
	if err != nil {
		state.LastError = err.Error()
		if errors.Is(err, ErrNoRelease) {
			state.LastError = ErrNoRelease.Error()
			state.Phase = UpdatePhaseIdle
			state.Message = "no published release for this channel yet"
			state.Info = UpdateInfo{Current: Version, Channel: info.Channel}
		} else {
			state.Phase = UpdatePhaseFailed
			state.Message = "update check failed"
		}
		d.saveUpdateState(state)
		d.reportUpdateStatus()
		return info, err
	}
	info.Current = Version
	state.Info = info
	state.LastError = ""
	if info.Available {
		state.Phase = UpdatePhaseReady
		state.Message = "update " + info.Latest + " is available"
	} else {
		state.Phase = UpdatePhaseIdle
		state.Message = "up to date"
	}
	d.saveUpdateState(state)
	d.reportUpdateStatus()
	return info, nil
}

// InstallUpdateNow honours the host-local remote-install policy. The caller
// may be the hub, but only the host decides whether that request is allowed.
func (d *Daemon) InstallUpdateNow(ctx context.Context) error {
	prefs := d.updatePrefs()
	if !prefs.AllowRemoteInstall {
		return errors.New("remote update installation is disabled by host policy (updates.allow_remote_install)")
	}
	if prefs.Disabled || !updateConfigured() {
		return errors.New("automatic updates are disabled on this host")
	}
	go d.runUpdateCycle(ctx)
	return nil
}
