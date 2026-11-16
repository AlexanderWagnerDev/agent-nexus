//go:build windows

package host

import (
	"strings"
	"testing"
)

func TestWindowsUpdateTaskXML(t *testing.T) {
	xml := windowsUpdateTaskXML(`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, `C:\Users\me\AppData\Roaming\AgentNexus\apply-update.ps1`, "S-1-5-21-1234")
	for _, want := range []string{
		"<AllowStartOnDemand>true</AllowStartOnDemand>",
		"<UserId>S-1-5-21-1234</UserId>",
		"<RunLevel>LeastPrivilege</RunLevel>",
		"<LogonType>InteractiveToken</LogonType>",
		`<Command>C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe</Command>`,
		"apply-update.ps1",
		"<Hidden>true</Hidden>",
		"<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>",
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("update task XML is missing %q:\n%s", want, xml)
		}
	}
	if strings.Contains(xml, "<LogonTrigger>") {
		t.Fatal("update task must not start at logon")
	}
}

func TestWindowsTaskXML(t *testing.T) {
	xml := windowsTaskXML(`C:\Program Files\AgentNexus\nexusd.exe`, "S-1-5-21-1234")
	for _, want := range []string{
		"<LogonTrigger>",
		"<UserId>S-1-5-21-1234</UserId>",
		"<RunLevel>LeastPrivilege</RunLevel>",
		"<LogonType>InteractiveToken</LogonType>",
		"<RestartOnFailure>",
		"<Count>3</Count>",
		"<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>",
		`<Command>` + windowsPowerShellPath() + `</Command>`,
		`C:\Program Files\AgentNexus\nexusd.exe`,
		`-WindowStyle Hidden`,
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("task XML is missing %q:\n%s", want, xml)
		}
	}
	if strings.Contains(xml, "<BootTrigger>") {
		t.Fatal("task must start at logon, not boot, to stay in the user context")
	}
}
