package host

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func waitForHost(t *testing.T, label string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", label)
}

func testBinary(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func testDaemon(t *testing.T, allowed []string, adapters []Adapter, acpCommands map[string]string, root string) *Daemon {
	t.Helper()
	cfg := Config{
		Policy: Policy{
			AllowedRoots: []string{root}, AllowedAgents: allowed, AllowNew: true,
			AllowFork: true, ForbiddenArgs: DefaultForbiddenArgs(),
		},
		ACPCommands: acpCommands,
	}
	return NewDaemonWithAdapters(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), adapters)
}

func TestStartStructuredRunAgainstFakeOpenCode(t *testing.T) {
	t.Setenv("NEXUS_TEST_AGENT", "opencode")
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	exe := testBinary(t)
	d := testDaemon(t, []string{"opencode"}, []Adapter{&OpenCodeAdapter{bin: exe}}, nil, root)

	spec := protocol.RunSpec{RunID: "run-open", Agent: "opencode", Action: protocol.ActionNew, Cwd: root, Mode: protocol.ModeStructured}
	d.startStructuredRun(context.Background(), spec)

	waitForHost(t, "structured run", func() bool {
		d.runMu.Lock()
		defer d.runMu.Unlock()
		return d.structured["run-open"] != nil
	})
	d.runMu.Lock()
	run := d.structured["run-open"]
	d.runMu.Unlock()
	if run.sessionID != "fake-session" {
		t.Fatalf("session: %q", run.sessionID)
	}
	if err := run.Prompt("hello", nil); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	opts, err := run.Options(context.Background())
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	if len(opts.Models) != 1 || opts.Models[0].ID != "fake-model" {
		t.Fatalf("options: %+v", opts)
	}
	if err := run.Interrupt(); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	run.Stop()
	waitForHost(t, "structured cleanup", func() bool {
		d.runMu.Lock()
		defer d.runMu.Unlock()
		return d.structured["run-open"] == nil
	})
}

func TestStartCodexStructuredAgainstFakeAppServer(t *testing.T) {
	t.Setenv("NEXUS_TEST_AGENT", "codex")
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	exe := testBinary(t)
	d := testDaemon(t, []string{"codex"}, []Adapter{&CodexAdapter{bin: exe}}, nil, root)

	spec := protocol.RunSpec{RunID: "run-codex", Agent: "codex", Action: protocol.ActionNew, Cwd: root, Mode: protocol.ModeStructured}
	d.startCodexStructured(context.Background(), spec)

	waitForHost(t, "codex run", func() bool {
		d.runMu.Lock()
		defer d.runMu.Unlock()
		return d.codexRuns["run-codex"] != nil
	})
	d.runMu.Lock()
	run := d.codexRuns["run-codex"]
	d.runMu.Unlock()
	if run.threadID != "fake-thread" {
		t.Fatalf("thread: %q", run.threadID)
	}
	if err := run.Prompt("hello", nil); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	opts, err := run.Options(context.Background())
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	if len(opts.Models) != 1 || opts.Models[0].ID != "fake-model" {
		t.Fatalf("options: %+v", opts)
	}
	run.Stop()
	waitForHost(t, "codex cleanup", func() bool {
		d.runMu.Lock()
		defer d.runMu.Unlock()
		return d.codexRuns["run-codex"] == nil
	})
}

func TestStartCodexForkAgainstFakeAppServer(t *testing.T) {
	t.Setenv("NEXUS_TEST_AGENT", "codex")
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	exe := testBinary(t)
	d := testDaemon(t, []string{"codex"}, []Adapter{&CodexAdapter{bin: exe}}, nil, root)

	spec := protocol.RunSpec{
		RunID: "run-codex-fork", Agent: "codex", Action: protocol.ActionFork,
		NativeID: "source-thread", Cwd: root, Mode: protocol.ModeStructured,
	}
	d.startCodexStructured(context.Background(), spec)

	waitForHost(t, "codex fork", func() bool {
		d.runMu.Lock()
		defer d.runMu.Unlock()
		return d.codexRuns["run-codex-fork"] != nil
	})
	d.runMu.Lock()
	run := d.codexRuns["run-codex-fork"]
	d.runMu.Unlock()
	if run.threadID != "fake-forked-thread" {
		t.Fatalf("forked thread: %q", run.threadID)
	}
	run.Stop()
	waitForHost(t, "codex fork cleanup", func() bool {
		d.runMu.Lock()
		defer d.runMu.Unlock()
		return d.codexRuns["run-codex-fork"] == nil
	})
}

func TestStartACPForkAgainstFakeAgent(t *testing.T) {
	t.Setenv("NEXUS_TEST_AGENT", "acp")
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	exe := testBinary(t)
	command := exe + " acp"
	d := testDaemon(t, []string{"fake-acp"}, nil, map[string]string{"fake-acp": command}, root)

	spec := protocol.RunSpec{
		RunID: "run-acp-fork", Agent: "fake-acp", Action: protocol.ActionFork,
		NativeID: "fake-acp-session", Cwd: root, Mode: protocol.ModeStructured,
	}
	d.startACPStructured(context.Background(), spec, command)

	waitForHost(t, "acp fork", func() bool {
		d.runMu.Lock()
		defer d.runMu.Unlock()
		return d.acpRuns["run-acp-fork"] != nil
	})
	d.runMu.Lock()
	run := d.acpRuns["run-acp-fork"]
	d.runMu.Unlock()
	if run.sessionID != "fake-forked-acp-session" {
		t.Fatalf("forked session: %q", run.sessionID)
	}
	run.Stop()
	waitForHost(t, "acp fork cleanup", func() bool {
		d.runMu.Lock()
		defer d.runMu.Unlock()
		return d.acpRuns["run-acp-fork"] == nil
	})
}

func TestStartACPStructuredAgainstFakeAgent(t *testing.T) {
	t.Setenv("NEXUS_TEST_AGENT", "acp")
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	exe := testBinary(t)
	command := exe + " acp"
	d := testDaemon(t, []string{"fake-acp"}, nil, map[string]string{"fake-acp": command}, root)

	spec := protocol.RunSpec{RunID: "run-acp", Agent: "fake-acp", Action: protocol.ActionNew, Cwd: root, Mode: protocol.ModeStructured}
	d.startACPStructured(context.Background(), spec, command)

	waitForHost(t, "acp run", func() bool {
		d.runMu.Lock()
		defer d.runMu.Unlock()
		return d.acpRuns["run-acp"] != nil
	})
	d.runMu.Lock()
	run := d.acpRuns["run-acp"]
	d.runMu.Unlock()
	if run.sessionID != "fake-acp-session" {
		t.Fatalf("session: %q", run.sessionID)
	}
	if err := run.Prompt("hello", nil); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	waitForHost(t, "acp idle", func() bool { return run.currentState() == protocol.StateIdle })
	if err := run.Interrupt(); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	run.Stop()
	waitForHost(t, "acp cleanup", func() bool {
		d.runMu.Lock()
		defer d.runMu.Unlock()
		return d.acpRuns["run-acp"] == nil
	})
}
