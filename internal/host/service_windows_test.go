//go:build windows

package host

import (
	"strings"
	"testing"
)

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
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("task XML is missing %q:\n%s", want, xml)
		}
	}
	if strings.Contains(xml, "<BootTrigger>") {
		t.Fatal("task must start at logon, not boot, to stay in the user context")
	}
}
