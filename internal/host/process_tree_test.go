package host

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func readPID(t *testing.T, path string) int {
	t.Helper()
	var last error
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if convErr == nil && pid > 0 {
				return pid
			}
			last = convErr
		} else {
			last = err
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid file %s: %v", path, last)
	return 0
}

// TestProcessGuardKillsDescendants starts a helper that spawns a grandchild,
// then stops the guard and checks that both processes are gone.
func TestProcessGuardKillsDescendants(t *testing.T) {
	t.Setenv("NEXUS_TEST_AGENT", "tree")
	dir := t.TempDir()
	t.Setenv("NEXUS_TEST_TREE_DIR", dir)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	configureProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	guard := newProcessGuard()
	if err := guard.Attach(cmd.Process.Pid); err != nil {
		t.Fatalf("attach process guard: %v", err)
	}
	parent := readPID(t, filepath.Join(dir, "parent.pid"))
	if err := os.WriteFile(filepath.Join(dir, "spawn"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	child := readPID(t, filepath.Join(dir, "child.pid"))

	stopProcessTree(guard, cmd, 2*time.Second)
	_ = cmd.Wait()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(parent) && !processAlive(child) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("processes survived the guard: parent=%v child=%v", processAlive(parent), processAlive(child))
}

func TestActiveElsewhereMatchesNativeProcess(t *testing.T) {
	lines := []string{
		"node /usr/local/bin/claude --resume 0f8fad5b-d9cb-469f-a165-70867728950e",
		"/usr/bin/codex resume other-session",
	}
	if !activeElsewhereIn(lines, "claude", "0f8fad5b-d9cb-469f-a165-70867728950e") {
		t.Fatal("resume of a session that is open in a native terminal was not detected")
	}
	if activeElsewhereIn(lines, "codex", "0f8fad5b-d9cb-469f-a165-70867728950e") {
		t.Fatal("a different agent with the same id was reported active")
	}
	if activeElsewhereIn(lines, "claude", "unknown-session") {
		t.Fatal("an unrelated session was reported active")
	}
}
