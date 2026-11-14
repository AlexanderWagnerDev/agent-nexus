package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// codexApprovalTimeout bounds how long a Codex approval may block a turn. It
// matches the OpenCode and ACP adapters.
var codexApprovalTimeout = 10 * time.Minute

// CodexRun drives Codex CLI's experimental app-server over JSONL/JSON-RPC.
type CodexRun struct {
	id         string
	sessionKey string
	threadID   string
	turnID     string
	agent      string
	cwd        string
	daemon     *Daemon
	cmd        *exec.Cmd
	guard      *processGuard
	stdin      io.WriteCloser
	cancel     context.CancelFunc

	writeMu        sync.Mutex
	mu             sync.Mutex
	pending        map[int64]chan json.RawMessage
	approvals      map[int64]string
	approvalTimers map[int64]*time.Timer
	nextID         int64
	closed         bool
	state          string
	currentModel   string
	currentEffort  string
}

type codexEvent struct {
	ID     *int64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

func (d *Daemon) startCodexStructured(parent context.Context, spec protocol.RunSpec) {
	fail := func(msg string) {
		d.log.Warn("codex structured run rejected", "run_id", spec.RunID, "error", msg)
		_ = d.send(protocol.Envelope{Type: "run.error", RunID: spec.RunID, Error: msg})
	}
	if err := d.validateRunPolicy(spec); err != nil {
		fail(err.Error())
		return
	}
	if spec.Action == protocol.ActionNew && !d.policy.AllowNew {
		fail("host policy does not allow new sessions")
		return
	}
	cwd := spec.Cwd
	if cwd == "" {
		if a := d.adapter("codex"); a != nil {
			if sessions, err := a.ListSessions(parent); err == nil {
				for _, s := range sessions {
					if s.NativeID == spec.NativeID {
						cwd = s.Cwd
						break
					}
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
	if spec.Action != protocol.ActionNew && !validNativeID(spec.NativeID) {
		fail("invalid native session id")
		return
	}
	sessionKey := "codex:" + spec.NativeID
	if spec.Action == protocol.ActionNew {
		sessionKey = "codex:new:" + randomRunKey()
	}
	if spec.Action == protocol.ActionFork {
		// A fork creates an independent thread; it must not contend with a
		// concurrent run on the source thread.
		sessionKey = "codex:fork:" + randomRunKey()
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

	bin := ""
	for _, a := range d.adapters {
		if a.Name() == "codex" {
			bin = d.agentBinary(a)
			break
		}
	}
	if bin == "" {
		fail("codex executable not found")
		return
	}
	path, args := commandFor(bin, []string{"app-server", "--stdio"})
	runCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(runCtx, path, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		fail(err.Error())
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		fail(err.Error())
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		fail(err.Error())
		return
	}
	configureProcessGroup(cmd)
	suspendExecForJob(cmd)
	if err := cmd.Start(); err != nil {
		cancel()
		fail("cannot start codex app-server: " + err.Error())
		return
	}
	guard := newProcessGuard()
	if err := guard.Attach(cmd.Process.Pid); err != nil {
		d.log.Warn("cannot attach a process guard", "run_id", spec.RunID, "error", err)
	}
	cr := &CodexRun{
		id: spec.RunID, sessionKey: sessionKey, agent: "codex", cwd: cwd,
		daemon: d, cmd: cmd, guard: guard, stdin: stdin, cancel: cancel,
		pending: map[int64]chan json.RawMessage{}, approvals: map[int64]string{}, approvalTimers: map[int64]*time.Timer{}, state: protocol.StateIdle,
	}
	readers := make(chan struct{}, 2)
	go func() { cr.readLoop(stdout); readers <- struct{}{} }()
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" {
				d.log.Debug("codex app-server", "line", line)
			}
		}
		readers <- struct{}{}
	}()
	// Wait after the pipe readers so their output is not truncated, but on
	// every path so a failed start does not leak a zombie process.
	waitExit := make(chan error, 1)
	go func() {
		<-readers
		<-readers
		waitExit <- cmd.Wait()
	}()
	if err := cr.initialize(); err != nil {
		cr.Stop()
		fail("codex app-server initialization failed: " + err.Error())
		return
	}
	reportedNativeID := ""
	if spec.Action == protocol.ActionNew {
		threadID, err := cr.startThread(cwd)
		if err != nil {
			cr.Stop()
			fail("cannot start codex thread: " + err.Error())
			return
		}
		cr.threadID = threadID
		reportedNativeID = threadID
	} else if spec.Action == protocol.ActionFork {
		threadID, err := cr.forkThread(cwd, spec.NativeID)
		if err != nil {
			cr.Stop()
			fail("cannot fork codex thread: " + err.Error())
			return
		}
		cr.threadID = threadID
		reportedNativeID = threadID
	} else {
		cr.threadID = spec.NativeID
		if err := cr.resumeThread(cwd); err != nil {
			cr.Stop()
			fail("cannot resume codex thread: " + err.Error())
			return
		}
	}
	d.runMu.Lock()
	d.codexRuns[spec.RunID] = cr
	d.runMu.Unlock()
	_ = d.send(protocol.Envelope{Type: "run.started", RunID: spec.RunID, Reason: "structured", Action: spec.Action, ForkedFrom: forkSource(spec), NativeID: reportedNativeID, Agent: spec.Agent, Cwd: cwd})
	_ = d.send(protocol.Envelope{Type: "run.event", RunID: spec.RunID, Event: &protocol.RunEvent{Type: "run.state", State: protocol.StateIdle, Text: "ready for a prompt"}})
	d.log.Info("codex structured run started", "run_id", spec.RunID, "thread", cr.threadID, "cwd", cwd)
	hostAudit("codex.structured.started", map[string]any{"run_id": spec.RunID, "cwd": cwd})
	go func() {
		err := <-waitExit
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		cr.markFinished()
		d.runMu.Lock()
		delete(d.codexRuns, spec.RunID)
		d.runMu.Unlock()
		reason := "codex app-server exited"
		if err != nil {
			reason = err.Error()
		}
		_ = d.send(protocol.Envelope{Type: "run.exit", RunID: spec.RunID, Code: code, Reason: reason})
	}()
}

func (r *CodexRun) send(msg map[string]any) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	_, err = r.stdin.Write(append(raw, '\n'))
	return err
}

func (r *CodexRun) request(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	r.mu.Lock()
	r.nextID++
	id := r.nextID
	ch := make(chan json.RawMessage, 1)
	r.pending[id] = ch
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.pending, id)
		r.mu.Unlock()
	}()
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if err := r.send(msg); err != nil {
		return nil, err
	}
	select {
	case raw := <-ch:
		return raw, nil
	case <-time.After(timeout):
		return nil, errors.New("codex app-server request timed out")
	}
}

func (r *CodexRun) readLoop(reader io.Reader) {
	sc := bufio.NewScanner(reader)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	for sc.Scan() {
		var ev codexEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.ID != nil {
			// Server-initiated requests use their own id namespace, separate
			// from client request ids. A request with a method must never be
			// mistaken for the response to a pending client call.
			if ev.Method != "" {
				r.handleServerRequest(*ev.ID, ev.Method, ev.Params)
				continue
			}
			r.mu.Lock()
			ch := r.pending[*ev.ID]
			r.mu.Unlock()
			if ch != nil {
				// A duplicate response for the same id must never block the
				// only reader of the app-server stream.
				select {
				case ch <- ev.Result:
				default:
				}
			}
			continue
		}
		r.handleNotification(ev.Method, ev.Params)
	}
}

func (r *CodexRun) initialize() error {
	_, err := r.request("initialize", map[string]any{
		"clientInfo": map[string]string{"name": "agentnexus", "version": Version},
	}, 20*time.Second)
	return err
}

func (r *CodexRun) startThread(cwd string) (string, error) {
	raw, err := r.request("thread/start", map[string]any{
		"cwd": cwd, "approvalPolicy": "on-request",
	}, 30*time.Second)
	if err != nil {
		return "", err
	}
	var out struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Thread.ID == "" {
		return "", errors.New("empty thread id")
	}
	return out.Thread.ID, nil
}

func (r *CodexRun) resumeThread(cwd string) error {
	_, err := r.request("thread/resume", map[string]any{
		"threadId": r.threadID, "cwd": cwd, "excludeTurns": true,
	}, 30*time.Second)
	return err
}

// forkThread creates an independent thread from the source thread. Codex
// returns the new thread id; the source thread is not modified.
func (r *CodexRun) forkThread(cwd, sourceThreadID string) (string, error) {
	raw, err := r.request("thread/fork", map[string]any{
		"threadId": sourceThreadID, "cwd": cwd, "excludeTurns": true,
	}, 30*time.Second)
	if err != nil {
		return "", err
	}
	var out struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Thread.ID == "" {
		return "", errors.New("codex returned an empty forked thread id")
	}
	return out.Thread.ID, nil
}

func (r *CodexRun) Prompt(text string, options *protocol.PromptOptions) error {
	params := map[string]any{
		"threadId": r.threadID,
		"input":    []map[string]any{{"type": "text", "text": text}},
	}
	if options != nil {
		if options.Model != "" {
			params["model"] = options.Model
		}
		if options.Variant != "" {
			params["effort"] = options.Variant
		}
		if len(options.Attachments) > 0 {
			files, err := writePromptAttachments(r.id, options.Attachments)
			if err != nil {
				return err
			}
			input := params["input"].([]map[string]any)
			for _, file := range files {
				if strings.HasPrefix(file.MIME, "image/") {
					input = append(input, map[string]any{"type": "localImage", "path": file.Path})
				} else {
					input = append(input, map[string]any{"type": "mention", "name": file.Name, "path": file.Path})
				}
			}
			params["input"] = input
		}
		r.mu.Lock()
		if options.Model != "" {
			r.currentModel = options.Model
		}
		if options.Variant != "" {
			r.currentEffort = options.Variant
		}
		r.mu.Unlock()
	}
	raw, err := r.request("turn/start", params, 30*time.Second)
	if err != nil {
		r.daemon.log.Warn("codex prompt failed", "run_id", r.id, "error", err)
		return err
	}
	r.daemon.log.Info("codex prompt sent", "run_id", r.id, "thread", r.threadID)
	var out struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &out) == nil {
		r.mu.Lock()
		r.turnID = out.Turn.ID
		r.mu.Unlock()
	}
	return nil
}

func (r *CodexRun) Interrupt() error {
	r.mu.Lock()
	turnID := r.turnID
	r.mu.Unlock()
	if turnID == "" {
		return errors.New("no active turn")
	}
	_, err := r.request("turn/interrupt", map[string]any{"threadId": r.threadID, "turnId": turnID}, 10*time.Second)
	return err
}

func (r *CodexRun) ReplyApproval(id, decision string) error {
	requestID, err := parseRequestID(id)
	if err != nil {
		return err
	}
	return r.respondApproval(requestID, decision)
}

// approvalDecision maps a hub decision onto the Codex response vocabulary.
func approvalDecision(method, decision string) map[string]any {
	allow := decision == "once" || decision == "always"
	result := map[string]any{"decision": "decline"}
	lower := strings.ToLower(method)
	commandish := strings.Contains(lower, "execcommand") || strings.Contains(lower, "applypatch")
	if allow {
		if commandish {
			result["decision"] = "approved"
		} else {
			result["decision"] = "accept"
		}
	} else if commandish {
		result["decision"] = "denied"
	}
	return result
}

// respondApproval answers one pending server request and reports the
// resolution to the hub, so the approval card is cleared on every device.
func (r *CodexRun) respondApproval(requestID int64, decision string) error {
	r.mu.Lock()
	method, pending := r.approvals[requestID]
	delete(r.approvals, requestID)
	timer := r.approvalTimers[requestID]
	delete(r.approvalTimers, requestID)
	closed := r.closed
	r.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	if closed {
		return errors.New("run is closed")
	}
	if !pending {
		return errors.New("approval is no longer pending")
	}
	err := r.send(map[string]any{"jsonrpc": "2.0", "id": requestID, "result": approvalDecision(method, decision)})
	ev := protocol.RunEvent{Type: "approval.resolved", ApprovalID: fmt.Sprintf("%d", requestID), Decision: decision}
	if err == nil {
		_ = r.daemon.send(protocol.Envelope{Type: "run.event", RunID: r.id, Event: &ev})
	}
	return err
}

func parseRequestID(id string) (int64, error) {
	var n int64
	if _, err := fmt.Sscanf(id, "%d", &n); err != nil {
		return 0, errors.New("invalid approval id")
	}
	return n, nil
}

// Options returns Codex's catalog models and their advertised reasoning
// efforts. Models are filtered to the non-hidden catalog entries.
func (r *CodexRun) Options(_ context.Context) (protocol.RunOptions, error) {
	out := protocol.RunOptions{Models: []protocol.ModelOption{}, Modes: []protocol.AgentModeOption{}}
	cursor := ""
	for page := 0; page < 5; page++ {
		params := map[string]any{"includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := r.request("model/list", params, 20*time.Second)
		if err != nil {
			return out, err
		}
		var response struct {
			Data []struct {
				ID                        string   `json:"id"`
				Model                     string   `json:"model"`
				DisplayName               string   `json:"displayName"`
				Hidden                    bool     `json:"hidden"`
				IsDefault                 bool     `json:"isDefault"`
				DefaultReasoningEffort    string   `json:"defaultReasoningEffort"`
				InputModalities           []string `json:"inputModalities"`
				SupportedReasoningEfforts []struct {
					ReasoningEffort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			return out, err
		}
		for _, model := range response.Data {
			if model.Hidden || model.Model == "" {
				continue
			}
			option := protocol.ModelOption{
				ID: model.Model, Provider: "codex", Name: model.DisplayName,
				DefaultVariant: model.DefaultReasoningEffort,
				Inputs:         append([]string(nil), model.InputModalities...),
			}
			for _, effort := range model.SupportedReasoningEfforts {
				if effort.ReasoningEffort != "" {
					option.Variants = append(option.Variants, effort.ReasoningEffort)
				}
			}
			out.Models = append(out.Models, option)
			r.mu.Lock()
			if r.currentModel == "" && model.IsDefault {
				r.currentModel = model.Model
				r.currentEffort = model.DefaultReasoningEffort
			}
			r.mu.Unlock()
		}
		if response.NextCursor == nil || *response.NextCursor == "" {
			break
		}
		cursor = *response.NextCursor
	}
	r.mu.Lock()
	out.CurrentModel = r.currentModel
	out.CurrentVariant = r.currentEffort
	r.mu.Unlock()
	out.SupportsFiles = len(out.Models) > 0
	return out, nil
}

func (r *CodexRun) Stop() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	timers := make([]*time.Timer, 0, len(r.approvalTimers))
	for id, timer := range r.approvalTimers {
		timers = append(timers, timer)
		delete(r.approvalTimers, id)
	}
	pending := make([]int64, 0, len(r.approvals))
	for id := range r.approvals {
		pending = append(pending, id)
	}
	r.approvals = map[int64]string{}
	r.mu.Unlock()
	for _, timer := range timers {
		timer.Stop()
	}
	r.cancel()
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
	}
	for _, id := range pending {
		ev := protocol.RunEvent{Type: "approval.resolved", ApprovalID: fmt.Sprintf("%d", id), Decision: "reject", Text: "run stopped"}
		_ = r.daemon.send(protocol.Envelope{Type: "run.event", RunID: r.id, Event: &ev})
	}
	cleanupPromptAttachments(r.id)
}

// markFinished records an app-server that exited on its own. Without it the
// read loop could still deliver buffered state events after run.exit, and the
// hub would see a finished run turn active again.
func (r *CodexRun) markFinished() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	timers := make([]*time.Timer, 0, len(r.approvalTimers))
	for id, timer := range r.approvalTimers {
		timers = append(timers, timer)
		delete(r.approvalTimers, id)
	}
	r.approvals = map[int64]string{}
	r.mu.Unlock()
	for _, timer := range timers {
		timer.Stop()
	}
	r.cancel()
	cleanupPromptAttachments(r.id)
}

func (r *CodexRun) handleServerRequest(id int64, method string, params json.RawMessage) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	if r.approvals == nil {
		r.approvals = map[int64]string{}
	}
	if r.approvalTimers == nil {
		r.approvalTimers = map[int64]*time.Timer{}
	}
	if timer := r.approvalTimers[id]; timer != nil {
		timer.Stop()
	}
	r.approvals[id] = method
	r.approvalTimers[id] = time.AfterFunc(codexApprovalTimeout, func() {
		if err := r.respondApproval(id, "reject"); err == nil {
			r.daemon.log.Info("codex approval timed out", "run_id", r.id, "approval_id", id)
		}
	})
	r.mu.Unlock()
	ev := protocol.RunEvent{
		Type: "approval.requested", ApprovalID: fmt.Sprintf("%d", id),
		Action: method, Text: truncate(string(params), 4000),
	}
	_ = r.daemon.send(protocol.Envelope{Type: "run.event", RunID: r.id, Event: &ev})
}

func (r *CodexRun) currentState() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == "" {
		return protocol.StateIdle
	}
	return r.state
}

func int64Number(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case json.Number:
		value, _ := n.Int64()
		return value
	default:
		return 0
	}
}

func floatNumber(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case json.Number:
		value, _ := n.Float64()
		return value
	default:
		return 0
	}
}

func (r *CodexRun) handleNotification(method string, params json.RawMessage) {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return
	}
	r.daemon.log.Debug("codex event", "run_id", r.id, "type", method)
	var p map[string]any
	_ = json.Unmarshal(params, &p)
	send := func(ev protocol.RunEvent) {
		if ev.Type == "run.state" {
			r.mu.Lock()
			r.state = ev.State
			r.mu.Unlock()
		}
		ev.RunID = r.id
		_ = r.daemon.send(protocol.Envelope{Type: "run.event", RunID: r.id, Event: &ev})
	}
	switch method {
	case "turn/started":
		send(protocol.RunEvent{Type: "run.state", State: protocol.StateRunning})
	case "turn/completed":
		send(protocol.RunEvent{Type: "run.state", State: protocol.StateIdle})
	case "thread/status/changed":
		if status, ok := p["status"].(map[string]any); ok {
			switch str(status["type"]) {
			case "active":
				send(protocol.RunEvent{Type: "run.state", State: protocol.StateRunning})
			case "idle":
				send(protocol.RunEvent{Type: "run.state", State: protocol.StateIdle})
			}
		}
	case "thread/tokenUsage/updated":
		if usage, ok := p["tokenUsage"].(map[string]any); ok {
			if total, ok := usage["total"].(map[string]any); ok {
				u := protocol.Usage{
					InputTokens:      int64Number(total["inputTokens"]),
					OutputTokens:     int64Number(total["outputTokens"]),
					CacheReadTokens:  int64Number(total["cachedInputTokens"]),
					CacheWriteTokens: int64Number(total["cacheWriteInputTokens"]),
					Source:           "agent",
					UpdatedAt:        time.Now().UTC(),
				}
				_ = r.daemon.send(protocol.Envelope{Type: "run.usage", RunID: r.id, Usage: &u})
			}
		}
	case "item/agentMessage/delta":
		send(protocol.RunEvent{Type: "message.delta", MessageID: str(p["itemId"]), Text: str(p["delta"])})
	case "item/started", "item/completed":
		item, _ := p["item"].(map[string]any)
		if item == nil {
			return
		}
		if str(item["type"]) == "agentMessage" {
			if method == "item/completed" {
				send(protocol.RunEvent{Type: "message.completed", MessageID: str(item["id"]), Text: str(item["text"])})
			} else {
				send(protocol.RunEvent{Type: "message.started", MessageID: str(item["id"])})
			}
			return
		}
		if str(item["type"]) == "userMessage" {
			return
		}
		if method == "item/completed" {
			send(protocol.RunEvent{Type: "tool.completed", CallID: str(item["id"]), ToolOutput: truncate(jsonText(item), 8000)})
		} else {
			send(protocol.RunEvent{Type: "tool.started", CallID: str(item["id"]), ToolName: str(item["type"]), ToolInput: truncate(jsonText(item), 8000)})
		}
	case "warning":
		// Non-fatal server warnings are intentionally not rendered as chat
		// messages; they would be confused with agent output.
	}
}
