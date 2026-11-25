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
		`<Command>C:\Program Files\AgentNexus\nexusd.exe</Command>`,
		`<Arguments>run --hidden</Arguments>`,
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("task XML is missing %q:\n%s", want, xml)
		}
	}
	// A logon trigger fires once per session. Without the repetition a daemon
	// that died, was killed or was stopped by a failed update stays dead until
	// the next logon, which is the bug this guards against.
	if !strings.Contains(xml, "<Repetition>") || !strings.Contains(xml, "<Interval>PT5M</Interval>") {
		t.Fatalf("task XML must repeat the logon trigger so the daemon self-heals:\n%s", xml)
	}
	if strings.Index(xml, "<Repetition>") > strings.Index(xml, "<UserId>") {
		t.Fatalf("Repetition must precede UserId in the trigger schema:\n%s", xml)
	}
	if strings.Contains(xml, "<BootTrigger>") {
		t.Fatal("task must start at logon, not boot, to stay in the user context")
	}
}
