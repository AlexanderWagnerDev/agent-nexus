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
	guard      *processGuard
	grace      time.Duration
	seq        atomic.Uint64
	stopped    atomic.Bool
	closeOnce  sync.Once
	writeMu    sync.Mutex
	buffer     *outputBuffer
}

// outputBuffer keeps the most recent terminal bytes for one run. It is bounded
// per run, copies every chunk and drops the oldest chunks first. Sequence
// numbers are never rewritten, so the hub can discard duplicates after a
// reconnect.
type outputBuffer struct {
	mu      sync.Mutex
	chunks  []bufferedChunk
	bytes   int
	limit   int
	dropped int
}

type bufferedChunk struct {
	seq  uint64
	data []byte
}

func newOutputBuffer(limit int) *outputBuffer {
	if limit <= 0 {
		limit = 1 << 20
	}
	return &outputBuffer{limit: limit}
}

func (b *outputBuffer) append(seq uint64, data []byte) {
	if len(data) == 0 {
		return
	}
	cp := append([]byte(nil), data...)
	if len(cp) > b.limit {
		b.dropped += len(cp) - b.limit
		cp = cp[len(cp)-b.limit:]
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.chunks = append(b.chunks, bufferedChunk{seq: seq, data: cp})
	b.bytes += len(cp)
	for b.bytes > b.limit && len(b.chunks) > 1 {
		b.bytes -= len(b.chunks[0].data)
		b.chunks = b.chunks[1:]
	}
}

func (b *outputBuffer) snapshot() []bufferedChunk {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]bufferedChunk, len(b.chunks))
	copy(out, b.chunks)
	return out
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
		if r.guard != nil {
			r.guard.Terminate()
		}
		if r.cmd != nil && r.cmd.Process != nil {
			deadline := time.Now().Add(r.grace)
			for time.Now().Before(deadline) && processAlive(r.cmd.Process.Pid) {
				time.Sleep(50 * time.Millisecond)
			}
		}
		if r.guard != nil {
			r.guard.Kill()
		}
		_ = r.pty.Close()
		if r.cmd != nil && r.cmd.Process != nil {
			_ = r.cmd.Process.Kill()
		}
		if r.guard != nil {
			r.guard.Close()
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
	if d.updating.Load() {
		fail("host is installing an update; start the run again after it reconnects")
		return
	}
	if spec.Action == protocol.ActionResume && !spec.Force {
		if d.activeElsewhere(ctx, spec.Agent, spec.NativeID) {
			fail("session is active in another terminal; close it or fork the session")
			return
		}
	}
	if spec.Mode == protocol.ModeStructured {
		d.startStructuredRun(ctx, spec)
		return
	}
	if spec.Mode != "" && spec.Mode != protocol.ModeTerminal {
		fail("unknown run mode")
		return
	}
	if err := d.validateRunPolicy(spec); err != nil {
		fail(err.Error())
		return
	}
	cwd := spec.Cwd
	if a := d.adapter(spec.Agent); a != nil {
		if sessions, err := a.ListSessions(ctx); err == nil {
			for _, s := range sessions {
				if s.NativeID == spec.NativeID {
					if cwd == "" {
						cwd = s.Cwd
					}
					break
				}
			}
		}
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
	reserved := d.reserveRunLocked(sessionKey, spec.RunID)
	d.runMu.Unlock()
	if !reserved {
		fail("session is already active in another run on this host")
		return
	}
	defer func() {
		d.runMu.Lock()
		d.releaseRunLocked(sessionKey, spec.RunID)
		d.runMu.Unlock()
	}()

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
	suspendForJob(c)
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
	guard := newProcessGuard()
	if c.Process != nil {
		if err := guard.Attach(c.Process.Pid); err != nil {
			d.log.Warn("cannot attach a process guard", "run_id", spec.RunID, "error", err)
		}
	}
	r := &Run{id: spec.RunID, sessionKey: sessionKey, agent: spec.Agent, pty: p, cmd: c,
		guard: guard, grace: d.policy.ProcessGrace(),
		buffer: newOutputBuffer(d.cfg.RunBufferBytesOrDefault())}
	d.runMu.Lock()
	d.runs[spec.RunID] = r
	d.runMu.Unlock()

	_ = d.send(protocol.Envelope{Type: "run.started", RunID: spec.RunID, Reason: spec.Action})
	d.log.Info("run started", "run_id", spec.RunID, "agent", spec.Agent, "cwd", cwd, "action", spec.Action)
	hostAudit("run.started", map[string]any{"run_id": spec.RunID, "agent": spec.Agent, "cwd": cwd, "action": spec.Action, "forced_active_elsewhere": spec.Force})
	_ = d.sendSessions(true)

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				seq := r.seq.Add(1)
				data := append([]byte(nil), buf[:n]...)
				// Keep a bounded replay buffer so a short hub outage or
				// browser reconnect does not lose terminal output. The
				// native session remains the source of truth.
				if r.buffer != nil {
					r.buffer.append(seq, data)
				}
				if sendErr := d.send(protocol.Envelope{Type: "run.output", RunID: spec.RunID, Seq: seq, Data: data}); sendErr != nil {
					d.log.Debug("run output buffered while hub is offline", "run_id", spec.RunID)
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
		hostAudit("run.exited", map[string]any{"run_id": spec.RunID, "code": code, "reason": reason})
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
