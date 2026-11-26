package host

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// An agent installed after the daemon started is not on the process PATH yet.
// The fallback must still find it in the installer's per-user directory.
func TestFindAgentBinaryFindsUserInstallDir(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var dir string
	if runtime.GOOS == "windows" {
		appData := t.TempDir()
		t.Setenv("APPDATA", appData)
		dir = filepath.Join(appData, "npm")
	} else {
		home := t.TempDir()
		t.Setenv("HOME", home)
		dir = filepath.Join(home, ".local", "bin")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "opencode"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	want := filepath.Join(dir, name)
	if err := os.WriteFile(want, []byte("@echo off\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := findAgentBinary(agentSpec{name: "opencode", binary: "opencode"}, []string{dir})
	if !ok || got != want {
		t.Fatalf("findAgentBinary = %q, %v; want %q", got, ok, want)
	}
	if !sliceContains(agentSearchDirs(), dir) {
		t.Fatalf("install directory %q missing from the search path", dir)
	}
}

func TestFindAgentBinaryUsesCursorAlias(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var dir string
	if runtime.GOOS == "windows" {
		appData := t.TempDir()
		t.Setenv("APPDATA", appData)
		dir = filepath.Join(appData, "npm")
	} else {
		home := t.TempDir()
		t.Setenv("HOME", home)
		dir = filepath.Join(home, ".local", "bin")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "agent"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("@echo off\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := findAgentBinary(agentSpec{name: "cursor", binary: "cursor-agent", aliases: []string{"agent"}}, []string{dir}); !ok {
		t.Fatal("cursor was not found through its agent alias")
	}
}
