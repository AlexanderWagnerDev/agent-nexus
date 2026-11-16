//go:build windows

package host

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func scheduleWindowsUpdate(staged, target string) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	script := filepath.Join(dir, "apply-update.ps1")
	log := filepath.Join(dir, "update.log")
	content := `param([string]$NewBinary,[string]$Target,[string]$Task,[string]$Log)
$ErrorActionPreference = "Stop"
if (-not $Log) { $Log = Join-Path $PSScriptRoot "update.log" }
if (-not $NewBinary) {
  try {
    $args = Get-Content -LiteralPath (Join-Path $PSScriptRoot "apply-update.json") -Raw | ConvertFrom-Json
    $NewBinary = $args.new_binary
    $Target = $args.target
    $Task = $args.task
    if ($args.log) { $Log = $args.log }
  } catch {
    Add-Content -LiteralPath $Log -Value "$(Get-Date -Format o) cannot read apply-update.json: $_"
    exit 1
  }
}
if (-not $NewBinary -or -not $Target -or -not $Task) {
  Add-Content -LiteralPath $Log -Value "$(Get-Date -Format o) update arguments are incomplete"
  exit 1
}
if ((Test-Path -LiteralPath $Log) -and ((Get-Item -LiteralPath $Log).Length -gt 5MB)) {
  Move-Item -LiteralPath $Log -Destination "$Log.old" -Force
}
function Write-Log([string]$Message) {
  Add-Content -LiteralPath $Log -Value "$(Get-Date -Format o) $Message"
}
function Wait-TaskState([string]$Expected) {
  for ($i = 0; $i -lt 40; $i++) {
    $state = (Get-ScheduledTask -TaskName $Task -ErrorAction SilentlyContinue).State
    if ("$state" -eq $Expected) { return $true }
    Start-Sleep -Milliseconds 500
  }
  return $false
}
try {
  $expected = (& $NewBinary version 2>$null) -replace '^nexusd\s+', ''
  if (-not $expected) { throw "the staged binary did not report a version" }
  Write-Log "applying $expected to $Target"
  schtasks.exe /End /TN $Task | Out-Null
  if (-not (Wait-TaskState "Ready")) { throw "the running task did not stop" }
  taskkill.exe /F /IM nexusd.exe 2>$null | Out-Null
  Start-Sleep -Milliseconds 500
  $copied = $false
  for ($i = 0; $i -lt 30; $i++) {
    try {
      Copy-Item -LiteralPath $NewBinary -Destination $Target -Force -ErrorAction Stop
      $copied = $true
      break
    } catch {
      Start-Sleep -Seconds 1
    }
  }
  if (-not $copied) { throw "binary remained locked" }
  Write-Log "binary replaced"
  schtasks.exe /Run /TN $Task | Out-Null
  $runtime = Join-Path (Split-Path -Parent $Log) "runtime.json"
  $healthy = $false
  for ($i = 0; $i -lt 40; $i++) {
    Start-Sleep -Seconds 1
    if (Test-Path -LiteralPath $runtime) {
      try {
        $state = Get-Content -LiteralPath $runtime -Raw | ConvertFrom-Json
        if ($state.version -eq $expected) {
          $healthy = $true
          break
        }
      } catch {}
    }
  }
  if (-not $healthy) { throw "the new process did not report version $expected" }
  Write-Log "update verified: $expected"
  exit 0
} catch {
  Write-Log "update failed: $_"
  $previous = "$Target.previous"
  $restored = $false
  if (Test-Path -LiteralPath $previous) {
    schtasks.exe /End /TN $Task | Out-Null
    Wait-TaskState "Ready" | Out-Null
    taskkill.exe /F /IM nexusd.exe 2>$null | Out-Null
    Start-Sleep -Milliseconds 500
    for ($i = 0; $i -lt 20; $i++) {
      try {
        Copy-Item -LiteralPath $previous -Destination $Target -Force -ErrorAction Stop
        $restored = $true
        break
      } catch {
        Start-Sleep -Seconds 1
      }
    }
    if ($restored) { Write-Log "rolled back to the previous binary" }
    else { Write-Log "rollback failed: the previous binary could not be restored" }
  } else {
    Write-Log "no previous binary was available for rollback"
  }
  # Always restart the task: without a successful restore it runs the new
  # binary (and its own restart logic), with a restore it runs the old one.
  schtasks.exe /Run /TN $Task | Out-Null
  exit 1
}`
	if err := os.WriteFile(script, []byte(content), 0o600); err != nil {
		return err
	}
	argsPath := filepath.Join(dir, "apply-update.json")
	payload, err := json.Marshal(map[string]string{
		"new_binary": staged,
		"target":     target,
		"task":       windowsTaskName,
		"log":        log,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(argsPath, payload, 0o600); err != nil {
		return err
	}
	// A child started with CREATE_BREAKAWAY_FROM_JOB is denied when the Task
	// Scheduler job does not allow breakaway. Let a separate on-demand task
	// perform the replacement instead; it runs outside the daemon's job.
	if err := installWindowsUpdateTask(script); err != nil {
		return err
	}
	if out, err := runWindowsCommand("schtasks", "/Run", "/TN", windowsUpdateTaskName); err != nil {
		return fmt.Errorf("cannot start update helper task: %v: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// scheduleWindowsSelfReplace replaces a standalone client after its process
// exits. It never restarts or touches the AgentNexus host service.
func scheduleWindowsSelfReplace(staged, target string) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	script := filepath.Join(dir, "replace-nexus.ps1")
	log := filepath.Join(dir, "update.log")
	content := `param([int]$ParentPid,[string]$NewBinary,[string]$Target,[string]$Log)
$ErrorActionPreference = "Stop"
if ((Test-Path -LiteralPath $Log) -and ((Get-Item -LiteralPath $Log).Length -gt 5MB)) {
  Move-Item -LiteralPath $Log -Destination "$Log.old" -Force
}
Add-Content -LiteralPath $Log -Value "$(Get-Date -Format o) replacing nexus client at $Target"
for ($i = 0; $i -lt 60; $i++) {
  if (-not (Get-Process -Id $ParentPid -ErrorAction SilentlyContinue)) { break }
  Start-Sleep -Milliseconds 500
}
$copied = $false
for ($i = 0; $i -lt 30; $i++) {
  try {
    Copy-Item -LiteralPath $NewBinary -Destination $Target -Force -ErrorAction Stop
    $copied = $true
    break
  } catch {
    Start-Sleep -Seconds 1
  }
}
if (-not $copied) { Add-Content -LiteralPath $Log -Value "$(Get-Date -Format o) nexus client replacement failed" }
else { Add-Content -LiteralPath $Log -Value "$(Get-Date -Format o) nexus client replaced" }`
	if err := os.WriteFile(script, []byte(content), 0o600); err != nil {
		return err
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden",
		"-ExecutionPolicy", "Bypass", "-File", script, fmt.Sprint(os.Getpid()), staged, target, log)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008 | 0x00000200 | 0x01000000,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot schedule client replacement: %w", err)
	}
	return nil
}
