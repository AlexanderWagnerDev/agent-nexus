//go:build windows

package host

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
Add-Content -LiteralPath $Log -Value "$(Get-Date -Format o) applying update to $Target"
schtasks.exe /End /TN $Task | Out-Null
Start-Sleep -Seconds 2
try {
  $copied = $false
  for ($i = 0; $i -lt 15; $i++) {
    try {
      Copy-Item -LiteralPath $NewBinary -Destination $Target -Force -ErrorAction Stop
      $copied = $true
      break
    } catch {
      Start-Sleep -Seconds 1
    }
  }
  if (-not $copied) { throw "binary remained locked" }
  Add-Content -LiteralPath $Log -Value "$(Get-Date -Format o) binary replaced"
  schtasks.exe /Run /TN $Task | Out-Null
  Add-Content -LiteralPath $Log -Value "$(Get-Date -Format o) service restarted"
} catch {
  Add-Content -LiteralPath $Log -Value "$(Get-Date -Format o) update failed: $_"
  $previous = "$Target.previous"
  if (Test-Path -LiteralPath $previous) {
    Copy-Item -LiteralPath $previous -Destination $Target -Force
    schtasks.exe /Run /TN $Task | Out-Null
    Add-Content -LiteralPath $Log -Value "$(Get-Date -Format o) rolled back"
  }
}`
	if err := os.WriteFile(script, []byte(content), 0o600); err != nil {
		return err
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden",
		"-ExecutionPolicy", "Bypass", "-File", script, staged, target, windowsTaskName, log)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000008 | 0x00000200}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot schedule update helper: %w", err)
	}
	return nil
}
