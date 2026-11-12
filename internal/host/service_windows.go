//go:build windows

package host

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

// windowsTaskName is per-user, so no administrator rights are required.
const windowsTaskName = "AgentNexus Host"

func serviceInstalled() bool {
	_, err := runWindowsCommand("schtasks", "/Query", "/TN", windowsTaskName)
	return err == nil
}

func windowsTaskService(action, exe string) error {
	switch action {
	case "install":
		return installWindowsTask(exe)
	case "uninstall":
		return uninstallWindowsTask()
	case "status":
		return statusWindowsTask()
	}
	return fmt.Errorf("unknown service action %q", action)
}

func installWindowsTask(exe string) error {
	userID, err := currentWindowsUserID()
	if err != nil {
		return err
	}
	content := windowsTaskXML(exe, userID)
	file, err := os.CreateTemp("", "agentnexus-task-*.xml")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := writeUTF16LE(file, content); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if out, err := runWindowsCommand("schtasks", "/Create", "/TN", windowsTaskName, "/XML", name, "/F"); err != nil {
		return fmt.Errorf("schtasks create failed: %v: %s", err, strings.TrimSpace(out))
	}
	removeLegacyStartupLauncher()
	if out, err := runWindowsCommand("schtasks", "/Run", "/TN", windowsTaskName); err != nil {
		fmt.Printf("warning: the task was installed but could not be started yet: %v: %s\n", err, strings.TrimSpace(out))
	}
	fmt.Printf("installed scheduled task %q for user %s\n", windowsTaskName, userID)
	fmt.Println("The task starts at logon, restarts up to three times on failure, and runs with least privilege.")
	return nil
}

func uninstallWindowsTask() error {
	if _, err := runWindowsCommand("schtasks", "/Query", "/TN", windowsTaskName); err != nil {
		removeLegacyStartupLauncher()
		fmt.Printf("scheduled task %q is not installed\n", windowsTaskName)
		return nil
	}
	_, _ = runWindowsCommand("schtasks", "/End", "/TN", windowsTaskName)
	if out, err := runWindowsCommand("schtasks", "/Delete", "/TN", windowsTaskName, "/F"); err != nil {
		return fmt.Errorf("schtasks delete failed: %v: %s", err, strings.TrimSpace(out))
	}
	removeLegacyStartupLauncher()
	fmt.Printf("removed scheduled task %q\n", windowsTaskName)
	return nil
}

func statusWindowsTask() error {
	out, err := runWindowsCommand("schtasks", "/Query", "/TN", windowsTaskName, "/V", "/FO", "LIST")
	if err != nil {
		return fmt.Errorf("scheduled task %q is not installed: %v", windowsTaskName, err)
	}
	fmt.Print(out)
	fmt.Println("--- process ---")
	processOut, _ := runWindowsCommand("tasklist", "/FI", "IMAGENAME eq nexusd.exe", "/FO", "LIST")
	fmt.Print(processOut)
	return nil
}

func runWindowsCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func currentWindowsUserID() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot determine the current Windows user: %w", err)
	}
	if strings.HasPrefix(strings.ToUpper(u.Uid), "S-1-") {
		return u.Uid, nil
	}
	if u.Username != "" {
		return u.Username, nil
	}
	return "", fmt.Errorf("cannot determine the current Windows user SID")
}

func removeLegacyStartupLauncher() {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return
	}
	_ = os.Remove(filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "AgentNexus.vbs"))
}

// windowsTaskXML builds a Task Scheduler 1.2 definition. The logon trigger is
// intentional: the agents and their credentials live in the interactive user
// profile, so a SYSTEM/boot task would run in the wrong context.
func windowsTaskXML(exe, userID string) string {
	escapedExe := xmlEscape(exe)
	escapedUser := xmlEscape(userID)
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>AgentNexus host daemon</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>` + escapedUser + `</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + escapedUser + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>3</Count>
    </RestartOnFailure>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <Hidden>true</Hidden>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + escapedExe + `</Command>
      <Arguments>run</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

// writeUTF16LE writes the BOM-prefixed encoding that schtasks expects for
// /XML input. A UTF-8 declaration is rejected with "encoding cannot be changed".
func writeUTF16LE(file *os.File, content string) error {
	if _, err := file.Write([]byte{0xff, 0xfe}); err != nil {
		return err
	}
	units := utf16.Encode([]rune(content))
	buf := make([]byte, len(units)*2)
	for i, u := range units {
		buf[i*2] = byte(u)
		buf[i*2+1] = byte(u >> 8)
	}
	_, err := file.Write(buf)
	return err
}

func xmlEscape(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}
