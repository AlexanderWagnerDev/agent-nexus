package host

import (
	"strings"
	"testing"
)

func TestServicePathContainsUserAndSystemBinDirectories(t *testing.T) {
	home := "/home/demo"
	path := servicePath(home)
	for _, want := range []string{
		home + "/.local/bin",
		home + "/.npm-global/bin",
		"/usr/local/bin",
		"/usr/bin",
	} {
		if !strings.Contains(path, want) {
			t.Fatalf("service PATH is missing %s: %s", want, path)
		}
	}
}

func TestXMLEscapeText(t *testing.T) {
	if got := xmlEscapeText(`C:\demo\AgentNexus & "x"`); strings.ContainsAny(got, `<>`) {
		t.Fatalf("XML escaping left markup characters: %s", got)
	}
}
