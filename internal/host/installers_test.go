package host

import (
	"os"
	"strings"
	"testing"
)

// The installers promise to install the daemon and the terminal client
// together. A development artifact that shipped only nexusd left the client
// missing (fresh machine) or stale (upgrade) while the installer reported
// success, so the packaging contract is asserted here.
func TestInstallerPackagingContract(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(raw)
	}

	dev := read("../../.github/workflows/dev-build.yml")
	if !strings.Contains(dev, "for name in nexusd nexus;") || !strings.Contains(dev, `"./cmd/$name"`) {
		t.Error("dev-build.yml must build both nexusd and nexus; installers install both")
	}

	ps := read("../../scripts/install-host.ps1")
	for _, want := range []string{
		`Get-VerifiedDevBinary "nexus"`,
		"Copy-Item $newCli $cli -Force",
		`if (-not (Test-Path $newCli)) { Fail`,
	} {
		if !strings.Contains(ps, want) {
			t.Errorf("install-host.ps1 no longer installs the terminal client: missing %q", want)
		}
	}

	sh := read("../../scripts/install-host.sh")
	for _, want := range []string{
		"for name in nexusd nexus;",
		`install -m 0755 "$tmp/nexus" "$cli"`,
		`client_version_output=`,
	} {
		if !strings.Contains(sh, want) {
			t.Errorf("install-host.sh no longer installs the terminal client: missing %q", want)
		}
	}
}
