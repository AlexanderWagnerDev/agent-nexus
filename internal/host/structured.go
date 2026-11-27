package host

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// StructuredRun drives an agent through its own local server protocol. The
// first implementation targets OpenCode's v2 HTTP API; terminal mode remains
// the universal fallback for every other agent.
type StructuredRun struct {
	id         string
	sessionID  string
	sessionKey string
	agent      string
	cwd        string
	baseURL    string
	password   string
	client     *http.Client
	cmd        *exec.Cmd
	guard      *processGuard
	cancel     context.CancelFunc
	daemon     *Daemon
	stdout     *os.File
	stderr     *os.File
	// shared marks a driver that attached to an OpenCode server owned by the
	// native desktop/TUI client. That server must never be stopped by us.
	shared bool

	mu        sync.Mutex
	calls     map[string]string
	approvals map[string]*time.Timer
	// reasoningStreamed records messages whose reasoning arrived as deltas so
	// the final ended event does not duplicate the text in the UI.
	reasoningStreamed map[string]bool
	closed            bool
	readyOnce         *sync.Once
	state             string
	usage             protocol.Usage
}

func (d *Daemon) startStructuredRun(parent context.Context, spec protocol.RunSpec) {
	fail := func(msg string) {
		d.log.Warn("structured run rejected", "run_id", spec.RunID, "error", msg)
		_ = d.send(protocol.Envelope{Type: "run.error", RunID: spec.RunID, Error: msg})
	}
	if d.updating.Load() {
		fail("host is installing an update; start the run again after it reconnects")
		return
	}
	if err := d.validateRunPolicy(spec); err != nil {
		fail(err.Error())
		return
	}
	if err := d.checkStructuredVersion(parent, spec.Agent); err != nil {
		fail(err.Error())
		return
	}
	if spec.Agent == "codex" {
		d.startCodexStructured(parent, spec)
		return
	}
	if command := d.acpCommand(spec.Agent); command != "" {
		d.startACPStructured(parent, spec, command)
		return
	}
	if spec.Agent != "opencode" {
		fail("structured mode is available for OpenCode, Codex and configured ACP agents; use terminal mode")
		return
	}
	cwd, err := d.resolveRunCwd(parent, spec)
	if err != nil {
		fail(err.Error())
		return
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		fail("working directory does not exist")
		return
	}
	sessionKey := spec.Agent + ":" + spec.NativeID
	if spec.Action == protocol.ActionNew {
		sessionKey = spec.Agent + ":new:" + randomRunKey()
	}
	if spec.Action == protocol.ActionFork {
		// A fork creates an independent session; it must not contend with a
		// concurrent run on the source session.
		sessionKey = spec.Agent + ":fork:" + randomRunKey()
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

	port, err := freeLoopbackPort()
	if err != nil {
		fail("cannot allocate a loopback port: " + err.Error())
		return
	}
	binPath := ""
	for _, a := range d.adapterList() {
		if a.Name() == "opencode" {
			binPath = d.agentBinary(a)
			break
		}
	}
	if binPath == "" {
		fail("opencode executable not found")
		return
	}
	// Prefer the service an OpenCode desktop or TUI client is already using.
	// Prompts then reach that client through its own event stream; a private
	// server would write the same SQLite store without notifying it.
	if svc, ok := openCodeServiceInfo(binPath); ok {
		runCtx, cancel := context.WithCancel(context.Background())
		sr := newStructuredRun(d, spec, cwd, svc.URL, svc.Password)
		sr.cancel = cancel
		sr.shared = true
		d.log.Info("structured run reuses the running OpenCode service", "run_id", spec.RunID, "url", svc.URL)
		if err := d.activateStructuredRun(spec, sr, runCtx, nil); err != nil {
			sr.Stop()
			fail(err.Error())
		}
		return
	}
	path, args, err := commandFor(binPath, []string{"serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port)})
	if err != nil {
		fail(err.Error())
		return
	}
	password := serverPassword()
	runCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(runCtx, path, args...)
	cmd.Dir = cwd
	// Set the password ourselves: OpenCode 2.x only prints a generated
	// password when the variable is absent, so waiting for the line would hang
	// when the daemon environment already defines it.
	cmd.Env = withEnv(append(childEnv(), "TERM=xterm-256color", "NO_COLOR=1"), "OPENCODE_SERVER_PASSWORD", password)
	stdout, stderr, closeWrite, err := stdioPipes(cmd)
	if err != nil {
		cancel()
		fail(err.Error())
		return
	}
	configureProcessGroup(cmd)
	suspendExecForJob(cmd)
	if err := cmd.Start(); err != nil {
		closeWrite()
		closePipe(stdout)
		closePipe(stderr)
		cancel()
		fail("cannot start opencode server: " + err.Error())
		return
	}
	closeWrite()
	guard := newProcessGuard()
	if err := guard.Attach(cmd.Process.Pid); err != nil {
		d.log.Warn("cannot attach a process guard", "run_id", spec.RunID, "error", err)
	}
	sr := newStructuredRun(d, spec, cwd, "", password)
	sr.cmd, sr.guard, sr.cancel = cmd, guard, cancel
	sr.stdout, sr.stderr = stdout, stderr
	ready := make(chan struct{})
	go sr.scanServerOutput(stdout, ready)
	go sr.scanServerOutput(stderr, ready)
	// Wait can run immediately because the child uses plain OS pipes; the
	// readers are released by markFinished/Stop even if a descendant kept the
	// write end open.
	waitExit := make(chan error, 1)
	go func() { waitExit <- cmd.Wait() }()
	select {
	case <-ready:
	case <-time.After(25 * time.Second):
		cancel()
		stopProcessTree(sr.guard, cmd, d.policy.ProcessGrace())
		sr.closeReaders()
		fail("opencode server did not become ready")
		return
	case <-parent.Done():
		cancel()
		stopProcessTree(sr.guard, cmd, d.policy.ProcessGrace())
		sr.closeReaders()
		return
	}
	if sr.baseURL == "" {
		cancel()
		stopProcessTree(sr.guard, cmd, d.policy.ProcessGrace())
		sr.closeReaders()
		fail("opencode server did not report its listening address")
		return
	}
	if err := d.activateStructuredRun(spec, sr, runCtx, waitExit); err != nil {
		sr.Stop()
		fail(err.Error())
	}
}

func newStructuredRun(d *Daemon, spec protocol.RunSpec, cwd, baseURL, password string) *StructuredRun {
	return &StructuredRun{
		id: spec.RunID, agent: spec.Agent, cwd: cwd, daemon: d,
		sessionKey: spec.Agent + ":" + spec.NativeID, state: protocol.StateIdle,
		baseURL: baseURL, password: password,
		calls: map[string]string{}, approvals: map[string]*time.Timer{},
		reasoningStreamed: map[string]bool{},
		client:            &http.Client{Timeout: 30 * time.Second},
		readyOnce:         new(sync.Once),
	}
}

// activateStructuredRun binds the driver to its native session and registers
// it. exited is the process wait channel for a server this daemon spawned; for
// a shared service it is nil and the run ends when it is stopped.
func (d *Daemon) activateStructuredRun(spec protocol.RunSpec, sr *StructuredRun, runCtx context.Context, exited <-chan error) error {
	reportedNativeID := ""
	switch spec.Action {
	case protocol.ActionNew:
		sessionID, err := sr.createSession(sr.cwd)
		if err != nil {
			return fmt.Errorf("cannot create opencode session: %w", err)
		}
		sr.sessionID = sessionID
		reportedNativeID = sessionID
	case protocol.ActionFork:
		sessionID, err := sr.forkSession(spec.NativeID)
		if err != nil {
			return fmt.Errorf("cannot fork opencode session: %w", err)
		}
		sr.sessionID = sessionID
		reportedNativeID = sessionID
	default:
		sr.sessionID = spec.NativeID
	}
	if sr.sessionID == "" {
		return errors.New("no native session id")
	}

	d.runMu.Lock()
	d.structured[spec.RunID] = sr
	// Bind the run to the native session the driver just created. The
	// discovery overlay and the duplicate-run guard are keyed by this value;
	// leaving the synthetic "new"/"fork" key hid a live session (reported
	// idle) and let a second run attach to it.
	sr.sessionKey = spec.Agent + ":" + sr.sessionID
	d.runMu.Unlock()
	_ = d.send(protocol.Envelope{Type: "run.started", RunID: spec.RunID, Reason: "structured", Action: spec.Action, ForkedFrom: forkSource(spec), NativeID: reportedNativeID, Agent: spec.Agent, Cwd: sr.cwd})
	if d.consumePendingStop(spec.RunID) {
		d.log.Info("structured run was stopped while starting", "run_id", spec.RunID)
		go sr.Stop()
	}
	d.log.Info("structured run started", "run_id", spec.RunID, "agent", spec.Agent, "session", sr.sessionID, "cwd", sr.cwd)
	hostAudit("structured.started", map[string]any{"run_id": spec.RunID, "agent": spec.Agent, "cwd": sr.cwd})
	_ = d.send(protocol.Envelope{Type: "run.event", RunID: spec.RunID, Event: &protocol.RunEvent{
		Type: "run.state", State: protocol.StateIdle, Text: "ready for a prompt",
	}})
	go sr.streamEvents(runCtx)
	if exited != nil {
		go func() {
			err := <-exited
			code := 0
			if sr.cmd != nil && sr.cmd.ProcessState != nil {
				code = sr.cmd.ProcessState.ExitCode()
			}
			sr.markFinished()
			d.runMu.Lock()
			delete(d.structured, spec.RunID)
			d.runMu.Unlock()
			reason := "opencode server exited"
			if err != nil {
				reason = err.Error()
			}
			_ = d.send(protocol.Envelope{Type: "run.exit", RunID: spec.RunID, Code: code, Reason: reason})
			d.log.Info("structured run exited", "run_id", spec.RunID, "code", code)
			hostAudit("structured.exited", map[string]any{"run_id": spec.RunID, "code": code})
		}()
		return nil
	}
	// A shared service outlives the run. Stop only this run's event stream and
	// registration; never terminate the native client's server.
	go func() {
		<-runCtx.Done()
		sr.markFinished()
		d.runMu.Lock()
		delete(d.structured, spec.RunID)
		d.runMu.Unlock()
		_ = d.send(protocol.Envelope{Type: "run.exit", RunID: spec.RunID, Code: 0, Reason: "run stopped"})
		d.log.Info("structured run stopped", "run_id", spec.RunID)
		hostAudit("structured.exited", map[string]any{"run_id": spec.RunID, "code": 0})
	}()
	return nil
}

func (d *Daemon) agentBinary(a Adapter) string {
	switch v := a.(type) {
	case *OpenCodeAdapter:
		return v.bin
	case *ClaudeAdapter:
		return v.bin
	case *CodexAdapter:
		return v.bin
	case *GeminiAdapter:
		return v.bin
	case *CommandCodeAdapter:
		return v.bin
	case *CursorAdapter:
		return v.bin
	case *DevinAdapter:
		return v.bin
	}
	return ""
}

// acpCommand returns the configured ACP adapter or builds "<binary> acp" from
// the CLI path found during discovery. Without that path, an agent detected
// through an alias or an installer directory stayed out of structured mode.
func (d *Daemon) acpCommand(agent string) string {
	if d.cfg.ACPCommands != nil {
		if cmd := strings.TrimSpace(d.cfg.ACPCommands[agent]); cmd != "" {
			return cmd
		}
	}
	switch agent {
	case "commandcode", "cursor", "devin":
		if a := d.adapter(agent); a != nil {
			if bin := d.agentBinary(a); bin != "" {
				return `"` + bin + `" acp`
			}
		}
	}
	return d.cfg.ACPCommand(agent)
}

func forkSource(spec protocol.RunSpec) string {
	if spec.Action == protocol.ActionFork {
		return spec.NativeID
	}
	return ""
}

func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func serverPassword() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "nexus-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// withEnv replaces an existing environment entry instead of appending a
// duplicate, so the child always sees the password the driver generated.
func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func (r *StructuredRun) scanServerOutput(reader io.Reader, ready chan<- struct{}) {
	sc := bufio.NewScanner(reader)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "server password ") {
			r.mu.Lock()
			r.password = strings.TrimSpace(strings.TrimPrefix(line, "server password "))
			r.mu.Unlock()
		}
		if strings.HasPrefix(line, "server listening on ") {
			r.mu.Lock()
			r.baseURL = strings.TrimRight(strings.TrimSpace(strings.TrimPrefix(line, "server listening on ")), "/")
			r.mu.Unlock()
		}
		r.mu.Lock()
		readyNow := r.baseURL != "" && r.password != ""
		r.mu.Unlock()
		if readyNow {
			r.readyOnce.Do(func() { close(ready) })
		}
	}
}

func (r *StructuredRun) request(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = strings.NewReader(string(raw))
	}
	r.mu.Lock()
	baseURL, password := r.baseURL, r.password
	r.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, reader)
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth("opencode", password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return data, resp.StatusCode, fmt.Errorf("opencode API %s %s: %s", method, path, strings.TrimSpace(string(data)))
	}
	return data, resp.StatusCode, nil
}

func (r *StructuredRun) createSession(cwd string) (string, error) {
	raw, _, err := r.request(context.Background(), http.MethodPost, "/api/session", map[string]any{
		"title":    "AgentNexus session",
		"location": map[string]string{"directory": cwd},
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Data.ID == "" {
		return "", errors.New("opencode returned an empty session id")
	}
	return out.Data.ID, nil
}

// forkSession creates a native OpenCode fork. The source session is not
// modified; OpenCode returns the new session with its own id.
func (r *StructuredRun) forkSession(sourceID string) (string, error) {
	if strings.TrimSpace(sourceID) == "" {
		return "", errors.New("source session id is required")
	}
	raw, status, err := r.request(context.Background(), http.MethodPost,
		"/api/session/"+url.PathEscape(sourceID)+"/fork", map[string]any{})
	if err != nil {
		// OpenCode 2.0 requires a fork boundary; 2.1 and newer made it optional.
		// "through" is an unbounded fork at the current head, which matches the
		// driver's semantics for every other version.
		if status != http.StatusBadRequest || !strings.Contains(string(raw), "boundary") {
			return "", err
		}
		raw, _, err = r.request(context.Background(), http.MethodPost,
			"/api/session/"+url.PathEscape(sourceID)+"/fork",
			map[string]any{"boundary": map[string]any{"type": "through"}})
		if err != nil {
			return "", err
		}
	}
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Data.ID == "" {
		return "", errors.New("opencode returned an empty fork session id")
	}
	return out.Data.ID, nil
}

func (r *StructuredRun) Prompt(text string, options *protocol.PromptOptions) error {
	if options != nil {
		if options.Model != "" {
			model := map[string]any{"id": options.Model, "providerID": options.Provider}
			if options.Variant != "" {
				model["variant"] = options.Variant
			}
			if _, _, err := r.request(context.Background(), http.MethodPost, "/api/session/"+url.PathEscape(r.sessionID)+"/model", map[string]any{"model": model}); err != nil {
				return fmt.Errorf("switch opencode model: %w", err)
			}
		}
		if options.AgentMode != "" {
			if _, _, err := r.request(context.Background(), http.MethodPost, "/api/session/"+url.PathEscape(r.sessionID)+"/agent", map[string]any{"agent": options.AgentMode}); err != nil {
				return fmt.Errorf("switch opencode agent: %w", err)
			}
		}
	}
	body := map[string]any{"text": text}
	if options != nil && len(options.Attachments) > 0 {
		files, err := writePromptAttachments(r.id, options.Attachments)
		if err != nil {
			return err
		}
		promptFiles := make([]map[string]any, 0, len(files))
		for _, file := range files {
			uri := fileURI(file.Path)
			promptFiles = append(promptFiles, map[string]any{
				"uri": uri, "name": file.Name,
				"source": map[string]any{"type": "uri", "uri": uri},
			})
		}
		body["files"] = promptFiles
	}
	_, _, err := r.request(context.Background(), http.MethodPost, "/api/session/"+url.PathEscape(r.sessionID)+"/prompt", body)
	if err != nil {
		r.daemon.log.Warn("structured prompt failed", "run_id", r.id, "session", r.sessionID, "error", err)
	} else {
		r.daemon.log.Info("structured prompt sent", "run_id", r.id, "session", r.sessionID)
		hostAudit("structured.prompt", map[string]any{"run_id": r.id})
	}
	return err
}

// Options returns the model, reasoning-variant and agent choices that the
// running OpenCode server actually exposes.
func (r *StructuredRun) Options(ctx context.Context) (protocol.RunOptions, error) {
	out := protocol.RunOptions{Models: []protocol.ModelOption{}, Modes: []protocol.AgentModeOption{}}
	raw, _, err := r.request(ctx, http.MethodGet, "/api/model", nil)
	if err != nil {
		return out, err
	}
	var models struct {
		Data []struct {
			ID         string `json:"id"`
			ModelID    string `json:"modelID"`
			ProviderID string `json:"providerID"`
			Name       string `json:"name"`
			Enabled    bool   `json:"enabled"`
			Status     string `json:"status"`
			Variants   []struct {
				ID string `json:"id"`
			} `json:"variants"`
			Capabilities struct {
				Input []string `json:"input"`
			} `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &models); err != nil {
		return out, err
	}
	for _, model := range models.Data {
		if !model.Enabled || model.Status == "deprecated" {
			continue
		}
		option := protocol.ModelOption{
			ID: model.ID, Provider: model.ProviderID, Name: model.Name,
			Inputs: append([]string(nil), model.Capabilities.Input...),
		}
		for _, variant := range model.Variants {
			if variant.ID != "" {
				option.Variants = append(option.Variants, variant.ID)
			}
		}
		out.Models = append(out.Models, option)
	}
	raw, _, err = r.request(ctx, http.MethodGet, "/api/agent", nil)
	if err != nil {
		return out, err
	}
	var agents struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Mode        string `json:"mode"`
			Hidden      bool   `json:"hidden"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &agents); err == nil {
		for _, agent := range agents.Data {
			if agent.Hidden || agent.Mode == "subagent" {
				continue
			}
			out.Modes = append(out.Modes, protocol.AgentModeOption{ID: agent.ID, Name: agent.Name, Description: agent.Description})
		}
	}
	raw, _, err = r.request(ctx, http.MethodGet, "/api/session/"+url.PathEscape(r.sessionID), nil)
	if err == nil {
		var session struct {
			Data struct {
				Agent string `json:"agent"`
				Model struct {
					ID         string `json:"id"`
					ProviderID string `json:"providerID"`
					Variant    string `json:"variant"`
				} `json:"model"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &session) == nil {
			out.CurrentMode = session.Data.Agent
			out.CurrentModel = session.Data.Model.ID
			out.CurrentProvider = session.Data.Model.ProviderID
			out.CurrentVariant = session.Data.Model.Variant
		}
	}
	out.SupportsFiles = len(out.Models) > 0
	return out, nil
}

func (r *StructuredRun) Interrupt() error {
	_, _, err := r.request(context.Background(), http.MethodPost, "/api/session/"+url.PathEscape(r.sessionID)+"/interrupt", nil)
	return err
}

func (r *StructuredRun) ReplyApproval(id, decision string) error {
	r.mu.Lock()
	if timer := r.approvals[id]; timer != nil {
		timer.Stop()
		delete(r.approvals, id)
	}
	r.mu.Unlock()
	reply := "once"
	switch decision {
	case "always":
		reply = "always"
	case "reject", "deny":
		reply = "reject"
	}
	_, _, err := r.request(context.Background(), http.MethodPost,
		"/api/session/"+url.PathEscape(r.sessionID)+"/permission/"+url.PathEscape(id)+"/reply",
		map[string]any{"decision": reply})
	return err
}

// closeReaders releases the stdout/stderr scanners. It is safe to call more
// than once; errors from an already closed pipe are ignored.
func (r *StructuredRun) closeReaders() {
	closePipe(r.stdout)
	closePipe(r.stderr)
}

func (r *StructuredRun) Stop() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	for id, timer := range r.approvals {
		timer.Stop()
		delete(r.approvals, id)
	}
	r.mu.Unlock()
	stopProcessTree(r.guard, r.cmd, r.daemon.policy.ProcessGrace())
	r.closeReaders()
	r.cancel()
	cleanupPromptAttachments(r.id)
}

// markFinished records a child that exited on its own. It stops the event
// stream and approval timers without killing an already dead process and
// without treating the exit as a failure.
func (r *StructuredRun) markFinished() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	for id, timer := range r.approvals {
		timer.Stop()
		delete(r.approvals, id)
	}
	r.mu.Unlock()
	r.closeReaders()
	r.cancel()
	if r.guard != nil {
		// The child exited on its own; close the job handle so a long-lived
		// daemon does not accumulate one handle per structured run.
		r.guard.Close()
	}
	cleanupPromptAttachments(r.id)
}

func (r *StructuredRun) streamEvents(ctx context.Context) {
	attempt := 0
	for {
		attempt++
		err := r.streamEventsOnce(ctx)
		r.mu.Lock()
		closed := r.closed
		r.mu.Unlock()
		if closed || ctx.Err() != nil {
			return
		}
		if err != nil {
			r.daemon.log.Warn("structured event stream failed", "run_id", r.id, "error", err, "attempt", attempt)
		} else {
			r.daemon.log.Warn("structured event stream ended", "run_id", r.id, "attempt", attempt)
		}
		if attempt >= 5 {
			ev := protocol.RunEvent{Type: "run.error", Error: "opencode event stream stopped; the run is no longer receiving events"}
			_ = r.daemon.send(protocol.Envelope{Type: "run.event", RunID: r.id, Event: &ev})
			// Without the event stream the run can never leave its last state,
			// so release it in both cases. Stop never kills a shared service;
			// for a private server it terminates the process and the exit
			// supervisor reports run.exit.
			r.Stop()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
		}
	}
}

func (r *StructuredRun) streamEventsOnce(ctx context.Context) error {
	r.mu.Lock()
	baseURL, password := r.baseURL, r.password
	r.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/event", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth("opencode", password)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("opencode event stream returned HTTP %d", resp.StatusCode)
	}
	r.daemon.log.Info("structured event stream connected", "run_id", r.id, "status", resp.StatusCode)
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		var event struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal([]byte(payload), &event) != nil {
			continue
		}
		var data map[string]any
		if json.Unmarshal(event.Data, &data) != nil {
			continue
		}
		if sid, _ := data["sessionID"].(string); sid != r.sessionID {
			continue
		}
		r.daemon.log.Debug("structured event", "run_id", r.id, "type", event.Type)
		r.handleEvent(event.Type, data)
	}
	return sc.Err()
}

func (r *StructuredRun) currentState() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == "" {
		return protocol.StateIdle
	}
	return r.state
}

func (r *StructuredRun) handleEvent(typ string, data map[string]any) {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		// A late event from a stopped run must not resurrect it or register
		// new approval timers.
		return
	}
	send := func(ev protocol.RunEvent) {
		if ev.Type == "run.state" {
			r.mu.Lock()
			r.state = ev.State
			r.mu.Unlock()
		}
		ev.RunID = r.id
		_ = r.daemon.send(protocol.Envelope{Type: "run.event", RunID: r.id, Event: &ev})
	}
	switch typ {
	case "session.text.started":
		send(protocol.RunEvent{Type: "message.started", MessageID: str(data["assistantMessageID"])})
	case "session.text.delta":
		send(protocol.RunEvent{Type: "message.delta", MessageID: str(data["assistantMessageID"]), Text: str(data["delta"])})
	case "session.text.ended":
		send(protocol.RunEvent{Type: "message.completed", MessageID: str(data["assistantMessageID"]), Text: str(data["text"])})
	case "session.reasoning.delta":
		messageID := str(data["assistantMessageID"])
		if delta := str(data["delta"]); delta != "" {
			r.mu.Lock()
			r.reasoningStreamed[messageID] = true
			r.mu.Unlock()
			send(protocol.RunEvent{Type: "reasoning", MessageID: messageID, Text: delta})
		}
	case "session.reasoning.ended":
		messageID := str(data["assistantMessageID"])
		r.mu.Lock()
		streamed := r.reasoningStreamed[messageID]
		delete(r.reasoningStreamed, messageID)
		r.mu.Unlock()
		if text := str(data["text"]); text != "" && !streamed {
			send(protocol.RunEvent{Type: "reasoning", MessageID: messageID, Text: text})
		}
	case "session.tool.input.started":
		callID := str(data["id"])
		r.mu.Lock()
		r.calls[callID] = str(data["name"])
		r.mu.Unlock()
		send(protocol.RunEvent{Type: "tool.started", CallID: callID, ToolName: str(data["name"])})
	case "session.tool.input.ended":
		send(protocol.RunEvent{Type: "tool.input", CallID: str(data["id"]), ToolInput: str(data["text"])})
	case "session.tool.called":
		callID := str(data["id"])
		input := ""
		if data["input"] != nil {
			input = jsonText(data["input"])
		}
		r.mu.Lock()
		name := r.calls[callID]
		r.mu.Unlock()
		send(protocol.RunEvent{Type: "tool.started", CallID: callID, ToolName: name, ToolInput: input})
	case "session.tool.success", "session.tool.failed":
		ev := protocol.RunEvent{Type: "tool.completed", CallID: str(data["id"]), ToolOutput: contentText(data["content"])}
		if typ == "session.tool.failed" {
			ev.Error = errorText(data["error"])
			ev.Decision = "failed"
		}
		send(ev)
	case "permission.asked":
		id := str(data["id"])
		// Default-deny if the user does not answer within ten minutes.
		r.mu.Lock()
		if old := r.approvals[id]; old != nil {
			old.Stop()
		}
		r.approvals[id] = time.AfterFunc(10*time.Minute, func() {
			_ = r.ReplyApproval(id, "reject")
			send(protocol.RunEvent{Type: "approval.resolved", ApprovalID: id, Decision: "reject", Text: "approval timed out"})
		})
		r.mu.Unlock()
		send(protocol.RunEvent{
			Type: "approval.requested", ApprovalID: id,
			Action: str(data["action"]), Resources: stringSlice(data["resources"]),
			Text: str(data["message"]),
		})
	case "permission.replied":
		id := str(data["requestID"])
		r.mu.Lock()
		if timer := r.approvals[id]; timer != nil {
			timer.Stop()
			delete(r.approvals, id)
		}
		r.mu.Unlock()
		send(protocol.RunEvent{Type: "approval.resolved", ApprovalID: id, Decision: str(data["reply"])})
	case "session.execution.started":
		send(protocol.RunEvent{Type: "run.state", State: protocol.StateRunning})
	case "session.usage.recorded":
		tokens, _ := data["tokens"].(map[string]any)
		cache, _ := tokens["cache"].(map[string]any)
		r.mu.Lock()
		r.usage.InputTokens += int64Number(tokens["input"])
		r.usage.OutputTokens += int64Number(tokens["output"])
		r.usage.CacheReadTokens += int64Number(cache["read"])
		r.usage.CacheWriteTokens += int64Number(cache["write"])
		if cost := floatNumber(data["cost"]); cost > 0 {
			total := cost
			if r.usage.CostUSD != nil {
				total += *r.usage.CostUSD
			}
			r.usage.CostUSD = &total
		}
		r.usage.Source = "agent"
		r.usage.UpdatedAt = time.Now().UTC()
		snapshot := r.usage
		r.mu.Unlock()
		_ = r.daemon.send(protocol.Envelope{Type: "run.usage", RunID: r.id, Usage: &snapshot})
	case "session.execution.succeeded", "session.idle", "session.execution.interrupted":
		send(protocol.RunEvent{Type: "run.state", State: protocol.StateIdle})
	case "session.execution.failed":
		send(protocol.RunEvent{Type: "run.error", Error: errorText(data["error"])})
	case "session.status":
		if status, ok := data["status"].(map[string]any); ok {
			switch str(status["type"]) {
			case "busy":
				send(protocol.RunEvent{Type: "run.state", State: protocol.StateRunning})
			case "idle":
				send(protocol.RunEvent{Type: "run.state", State: protocol.StateIdle})
			}
		}
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func jsonText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func contentText(v any) string {
	items, _ := v.([]any)
	var parts []string
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			if t := str(m["text"]); t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func errorText(v any) string {
	if m, ok := v.(map[string]any); ok {
		return str(m["message"])
	}
	return str(v)
}

func stringSlice(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := str(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}
