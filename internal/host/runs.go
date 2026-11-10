package host

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/aymanbagabas/go-pty"
)

// Run is one live process attached to an agent session.
type Run struct {
	id         string
	sessionKey string
	agent      string
	pty        pty.Pty
	cmd        *pty.Cmd
	cols, rows int
	seq        atomic.Uint64
	stopped    atomic.Bool
	closeOnce  sync.Once
	writeMu    sync.Mutex
}

func (r *Run) Write(data []byte) (int, error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return r.pty.Write(data)
}

func (r *Run) Resize(cols, rows int) error {
	if cols < 20 {
		cols = 20
	}
	if rows < 5 {
		rows = 5
	}
	r.cols, r.rows = cols, rows
	return r.pty.Resize(cols, rows)
}

func (r *Run) Stop() {
	r.stopped.Store(true)
	// Ctrl-C first: it lets the agent persist the session cleanly.
	_, _ = r.Write([]byte{3})
	time.Sleep(1500 * time.Millisecond)
	r.terminate()
}

func (r *Run) terminate() {
	r.closeOnce.Do(func() {
		_ = r.pty.Close()
		if r.cmd != nil && r.cmd.Process != nil {
			_ = r.cmd.Process.Kill()
		}
	})
}

func randomRunKey() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// validNativeID prevents a session identifier from being interpreted as a
// command-line flag. Agent adapters only ever pass IDs as argv values, but a
// leading dash would still be dangerous for CLIs that reorder flags.
func validNativeID(id string) bool {
	if len(id) == 0 || len(id) > 200 || id[0] == '-' {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == ':':
		default:
			return false
		}
	}
	return true
}

func (d *Daemon) startRun(ctx context.Context, spec protocol.RunSpec) {
	fail := func(msg string) {
		d.log.Warn("run rejected", "run_id", spec.RunID, "error", msg)
		_ = d.send(protocol.Envelope{Type: "run.error", RunID: spec.RunID, Error: msg})
	}
	if spec.Mode != "" && spec.Mode != protocol.ModeTerminal {
		fail("structured mode is not implemented on this host")
		return
	}
	if spec.Action != protocol.ActionNew && !validNativeID(spec.NativeID) {
		fail("invalid native session id")
		return
	}
	if spec.Action != protocol.ActionNew && !d.policy.AgentAllowed(spec.Agent) {
		fail("agent is not allowed by host policy")
		return
	}
	if spec.Action == protocol.ActionNew && !d.policy.AllowNew {
		fail("host policy does not allow new sessions")
		return
	}
	if spec.Action == protocol.ActionFork && !d.policy.AllowFork {
		fail("host policy does not allow forking")
		return
	}
	cwd := spec.Cwd
	activeElsewhere := false
	if a := d.adapter(spec.Agent); a != nil {
		if sessions, err := a.ListSessions(ctx); err == nil {
			for _, s := range sessions {
				if s.NativeID == spec.NativeID {
					if cwd == "" {
						cwd = s.Cwd
					}
					activeElsewhere = s.ActiveElsewhere
					break
				}
			}
		}
	}
	if activeElsewhere && spec.Action == protocol.ActionResume {
		fail("session is active in another terminal; close it or fork the session")
		return
	}
	if cwd == "" {
		fail("session working directory is unknown")
		return
	}
	if !d.policy.WithinRoots(cwd) {
		fail("working directory is outside the allowed roots")
		return
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		fail("working directory does not exist")
		return
	}
	adapter := d.adapter(spec.Agent)
	if adapter == nil {
		fail("agent is not installed on this host")
		return
	}
	cmd, err := adapter.BuildCommand(Action{Kind: spec.Action, NativeID: spec.NativeID, Cwd: cwd, Agent: spec.Agent})
	if err != nil {
		fail(err.Error())
		return
	}
	if err := d.policy.ArgsAllowed(cmd.Args); err != nil {
		fail(err.Error())
		return
	}
	if cmd.Cwd == "" {
		cmd.Cwd = cwd
	}
	sessionKey := spec.Agent + ":" + spec.NativeID
	if spec.Action == protocol.ActionNew {
		sessionKey = spec.Agent + ":new:" + randomRunKey()
	}
	d.runMu.Lock()
	for _, existing := range d.runs {
		if existing.sessionKey == sessionKey {
			d.runMu.Unlock()
			fail("session is already active in another run on this host")
			return
		}
	}
	d.runMu.Unlock()

	cols, rows := spec.Cols, spec.Rows
	if cols <= 0 {
		cols, rows = 120, 32
	}
	p, err := pty.New()
	if err != nil {
		fail("cannot create pty: " + err.Error())
		return
	}
	c := p.Command(cmd.Path, cmd.Args...)
	c.Dir = cmd.Cwd
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "AGENTNEXUS_RUN="+spec.RunID)
	c.Env = env
	if err := c.Start(); err != nil {
		_ = p.Close()
		fail("cannot start agent: " + err.Error())
		return
	}
	_ = p.Resize(cols, rows)
	r := &Run{id: spec.RunID, sessionKey: sessionKey, agent: spec.Agent, pty: p, cmd: c, cols: cols, rows: rows}
	d.runMu.Lock()
	d.runs[spec.RunID] = r
	d.runMu.Unlock()

	_ = d.send(protocol.Envelope{Type: "run.started", RunID: spec.RunID, Reason: spec.Action})
	d.log.Info("run started", "run_id", spec.RunID, "agent", spec.Agent, "cwd", cwd, "action", spec.Action)
	_ = d.sendSessions(true)

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				seq := r.seq.Add(1)
				data := append([]byte(nil), buf[:n]...)
				if sendErr := d.send(protocol.Envelope{Type: "run.output", RunID: spec.RunID, Seq: seq, Data: data}); sendErr != nil {
					r.terminate()
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) && !r.stopped.Load() {
					d.log.Debug("pty read ended", "run_id", spec.RunID, "error", err)
				}
				break
			}
		}
		_ = c.Wait()
		code := 0
		reason := "agent exited"
		if c.ProcessState != nil {
			code = c.ProcessState.ExitCode()
			reason = c.ProcessState.String()
		}
		if r.stopped.Load() {
			reason = "run stopped by user"
		}
		d.runMu.Lock()
		delete(d.runs, spec.RunID)
		d.runMu.Unlock()
		r.terminate()
		_ = d.send(protocol.Envelope{Type: "run.exit", RunID: spec.RunID, Code: code, Reason: reason})
		d.log.Info("run finished", "run_id", spec.RunID, "code", code, "reason", reason)
		_ = d.sendSessions(true)
	}()
}

// commandFor wraps script shims so that Windows npm CLIs start correctly.
func commandFor(path string, args []string) (string, []string) {
	if runtime.GOOS != "windows" {
		return path, args
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd.exe"
		}
		return comspec, append([]string{"/D", "/S", "/C", path}, args...)
	case ".ps1":
		return "powershell.exe", append([]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", path}, args...)
	}
	return path, args
}

func execFor(path string, args ...string) *exec.Cmd {
	name, argv := commandFor(path, args)
	return exec.Command(name, argv...)
}

func versionOf(path string, timeout time.Duration) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	name, args := commandFor(path, []string{"--version"})
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return "unknown"
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = line[:i]
	}
	if line == "" {
		return "unknown"
	}
	if len(line) > 80 {
		line = line[:80]
	}
	return line
}
