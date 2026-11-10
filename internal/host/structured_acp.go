package host

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	acp "github.com/coder/acp-go-sdk"
)

// ACPRun drives any Agent Client Protocol adapter over stdio. This is how
// agents without a native structured driver (for example Claude Code via
// claude-code-acp) get text streaming, tool cards and approvals.
type ACPRun struct {
	id         string
	sessionKey string
	agent      string
	cwd        string
	daemon     *Daemon
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	conn       *acp.ClientSideConnection
	cancel     context.CancelFunc

	mu            sync.Mutex
	sessionID     acp.SessionId
	approvals     map[string]chan string
	approvalSeq   int
	turnSeq       int
	turnID        string
	thought       strings.Builder
	lastMessageID string
	state         string
	active        bool
	closed        bool
}

func (d *Daemon) startACPStructured(parent context.Context, spec protocol.RunSpec, command string) {
	fail := func(msg string) {
		d.log.Warn("ACP structured run rejected", "run_id", spec.RunID, "error", msg)
		_ = d.send(protocol.Envelope{Type: "run.error", RunID: spec.RunID, Error: msg})
	}
	if !d.policy.AgentAllowed(spec.Agent) {
		fail("agent is not allowed by host policy")
		return
	}
	if spec.Action == protocol.ActionNew && !d.policy.AllowNew {
		fail("host policy does not allow new sessions")
		return
	}
	if spec.Action == protocol.ActionFork {
		fail("structured fork is not implemented; use terminal fork")
		return
	}
	if spec.Action != protocol.ActionNew && !validNativeID(spec.NativeID) {
		fail("invalid native session id")
		return
	}
	cwd := spec.Cwd
	if cwd == "" {
		if a := d.adapter(spec.Agent); a != nil {
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

	parts := splitCommandLine(command)
	if len(parts) == 0 {
		fail("ACP adapter command is empty")
		return
	}
	bin, err := exec.LookPath(parts[0])
	if err != nil {
		fail("ACP adapter not found: " + parts[0])
		return
	}
	path, args := commandFor(bin, parts[1:])
	runCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(runCtx, path, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "NO_COLOR=1")
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
	if err := cmd.Start(); err != nil {
		cancel()
		fail("cannot start ACP adapter: " + err.Error())
		return
	}
	ar := &ACPRun{
		id: spec.RunID, sessionKey: sessionKey, agent: spec.Agent, cwd: cwd,
		daemon: d, cmd: cmd, stdin: stdin, cancel: cancel,
		approvals: map[string]chan string{}, state: protocol.StateIdle,
	}
	ar.conn = acp.NewClientSideConnection(ar, stdin, stdout)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" {
				d.log.Debug("ACP adapter", "agent", spec.Agent, "line", line)
			}
		}
	}()

	ctx, cancelInit := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelInit()
	init, err := ar.conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientInfo:      &acp.Implementation{Name: "agentnexus", Version: Version},
	})
	if err != nil {
		ar.Stop()
		fail("ACP initialization failed: " + err.Error())
		return
	}
	if spec.Action == protocol.ActionNew {
		resp, err := ar.conn.NewSession(ctx, acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
		if err != nil {
			ar.Stop()
			fail("cannot create ACP session: " + err.Error())
			return
		}
		ar.sessionID = resp.SessionId
	} else {
		if !init.AgentCapabilities.LoadSession {
			ar.Stop()
			fail("ACP adapter does not support session/load; start a new session instead")
			return
		}
		ar.sessionID = acp.SessionId(spec.NativeID)
		if _, err := ar.conn.LoadSession(ctx, acp.LoadSessionRequest{
			Cwd: cwd, McpServers: []acp.McpServer{}, SessionId: ar.sessionID,
		}); err != nil {
			ar.Stop()
			fail("cannot load ACP session: " + err.Error())
			return
		}
	}

	d.runMu.Lock()
	d.acpRuns[spec.RunID] = ar
	d.runMu.Unlock()
	_ = d.send(protocol.Envelope{Type: "run.started", RunID: spec.RunID, Reason: "structured"})
	_ = d.send(protocol.Envelope{Type: "run.event", RunID: spec.RunID, Event: &protocol.RunEvent{Type: "run.state", State: protocol.StateIdle, Text: "ready for a prompt"}})
	d.log.Info("ACP structured run started", "run_id", spec.RunID, "agent", spec.Agent, "session", ar.sessionID, "cwd", cwd)
	hostAudit("acp.structured.started", map[string]any{"run_id": spec.RunID, "agent": spec.Agent, "cwd": cwd})
	go func() {
		err := cmd.Wait()
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		d.runMu.Lock()
		delete(d.acpRuns, spec.RunID)
		d.runMu.Unlock()
		reason := "ACP adapter exited"
		if err != nil {
			reason = err.Error()
		}
		_ = d.send(protocol.Envelope{Type: "run.exit", RunID: spec.RunID, Code: code, Reason: reason})
	}()
}

func (r *ACPRun) currentState() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == "" {
		return protocol.StateIdle
	}
	return r.state
}

func (r *ACPRun) send(ev protocol.RunEvent) {
	if ev.Type == "run.state" {
		r.mu.Lock()
		r.state = ev.State
		r.mu.Unlock()
	}
	ev.RunID = r.id
	_ = r.daemon.send(protocol.Envelope{Type: "run.event", RunID: r.id, Event: &ev})
}

func (r *ACPRun) Prompt(text string) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return errors.New("run is closed")
	}
	if r.active {
		r.mu.Unlock()
		return errors.New("agent is already processing a prompt")
	}
	r.active = true
	r.turnSeq++
	r.turnID = fmt.Sprintf("%s-turn-%d", r.sessionID, r.turnSeq)
	r.thought.Reset()
	r.lastMessageID = ""
	sessionID := r.sessionID
	r.mu.Unlock()

	r.send(protocol.RunEvent{Type: "run.state", State: protocol.StateRunning})
	go func() {
		_, err := r.conn.Prompt(context.Background(), acp.PromptRequest{
			SessionId: sessionID,
			Prompt:    []acp.ContentBlock{acp.TextBlock(text)},
		})
		r.mu.Lock()
		thought := r.thought.String()
		r.thought.Reset()
		r.active = false
		r.mu.Unlock()
		if thought != "" {
			r.send(protocol.RunEvent{Type: "reasoning", Text: thought})
		}
		if err != nil {
			r.send(protocol.RunEvent{Type: "run.error", Error: err.Error()})
			return
		}
		r.send(protocol.RunEvent{Type: "run.state", State: protocol.StateIdle})
	}()
	return nil
}

func (r *ACPRun) Interrupt() error {
	r.mu.Lock()
	closed := r.closed
	sessionID := r.sessionID
	r.mu.Unlock()
	if closed {
		return errors.New("run is closed")
	}
	return r.conn.Cancel(context.Background(), acp.CancelNotification{SessionId: sessionID})
}

func (r *ACPRun) ReplyApproval(id, decision string) error {
	r.mu.Lock()
	ch := r.approvals[id]
	delete(r.approvals, id)
	r.mu.Unlock()
	if ch == nil {
		return errors.New("unknown approval id")
	}
	select {
	case ch <- decision:
	default:
	}
	r.send(protocol.RunEvent{Type: "approval.resolved", ApprovalID: id, Decision: decision})
	return nil
}

func (r *ACPRun) Stop() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	for id, ch := range r.approvals {
		delete(r.approvals, id)
		select {
		case ch <- "reject":
		default:
		}
	}
	r.mu.Unlock()
	r.cancel()
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
	}
}

// ReadTextFile is not advertised to the agent, so this is only a safety net.
func (r *ACPRun) ReadTextFile(context.Context, acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	return acp.ReadTextFileResponse{}, errors.New("filesystem access is not supported by this client")
}

func (r *ACPRun) WriteTextFile(context.Context, acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	return acp.WriteTextFileResponse{}, errors.New("filesystem access is not supported by this client")
}

func (r *ACPRun) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, errors.New("terminal access is not supported by this client")
}

func (r *ACPRun) KillTerminal(context.Context, acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, errors.New("terminal access is not supported by this client")
}

func (r *ACPRun) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, errors.New("terminal access is not supported by this client")
}

func (r *ACPRun) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, errors.New("terminal access is not supported by this client")
}

func (r *ACPRun) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, errors.New("terminal access is not supported by this client")
}

func (r *ACPRun) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	r.mu.Lock()
	r.approvalSeq++
	id := fmt.Sprintf("acp-%d", r.approvalSeq)
	ch := make(chan string, 1)
	r.approvals[id] = ch
	r.mu.Unlock()

	action := string(params.ToolCall.ToolCallId)
	if params.ToolCall.Title != nil && *params.ToolCall.Title != "" {
		action = *params.ToolCall.Title
	}
	r.send(protocol.RunEvent{
		Type: "approval.requested", ApprovalID: id,
		Action: string(action), Text: truncate(jsonText(params.ToolCall.RawInput), 4000),
	})

	var decision string
	select {
	case decision = <-ch:
	case <-time.After(10 * time.Minute):
		decision = "reject"
		r.send(protocol.RunEvent{Type: "approval.resolved", ApprovalID: id, Decision: "reject", Text: "approval timed out"})
	case <-ctx.Done():
		decision = "reject"
	}
	r.mu.Lock()
	delete(r.approvals, id)
	r.mu.Unlock()

	allow := decision == "once" || decision == "always"
	chosen := choosePermissionOption(params.Options, allow)
	if chosen == nil {
		return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
	}
	return acp.RequestPermissionResponse{
		Outcome: acp.NewRequestPermissionOutcomeSelected(chosen.OptionId),
	}, nil
}

func choosePermissionOption(options []acp.PermissionOption, allow bool) *acp.PermissionOption {
	want := []acp.PermissionOptionKind{acp.PermissionOptionKindRejectOnce, acp.PermissionOptionKindRejectAlways}
	if allow {
		want = []acp.PermissionOptionKind{acp.PermissionOptionKindAllowOnce, acp.PermissionOptionKindAllowAlways}
	}
	for _, kind := range want {
		for i := range options {
			if options[i].Kind == kind {
				return &options[i]
			}
		}
	}
	return nil
}

func (r *ACPRun) SessionUpdate(_ context.Context, params acp.SessionNotification) error {
	update := params.Update
	switch {
	case update.AgentMessageChunk != nil:
		text := acpContentText(update.AgentMessageChunk.Content)
		if text == "" {
			return nil
		}
		r.mu.Lock()
		messageID := r.turnID
		if messageID == "" {
			messageID = string(r.sessionID) + "-assistant"
		}
		if update.AgentMessageChunk.MessageId != nil && *update.AgentMessageChunk.MessageId != "" {
			messageID = *update.AgentMessageChunk.MessageId
		}
		first := r.lastMessageID != messageID
		r.lastMessageID = messageID
		r.mu.Unlock()
		if first {
			r.send(protocol.RunEvent{Type: "message.started", MessageID: messageID})
		}
		r.send(protocol.RunEvent{Type: "message.delta", MessageID: messageID, Text: text})
	case update.AgentThoughtChunk != nil:
		text := acpContentText(update.AgentThoughtChunk.Content)
		if text == "" {
			return nil
		}
		r.mu.Lock()
		r.thought.WriteString(text)
		r.mu.Unlock()
	case update.ToolCall != nil:
		r.send(protocol.RunEvent{
			Type: "tool.started", CallID: string(update.ToolCall.ToolCallId),
			ToolName: update.ToolCall.Title, ToolInput: truncate(jsonText(update.ToolCall.RawInput), 8000),
		})
	case update.ToolCallUpdate != nil:
		done := update.ToolCallUpdate.Status != nil &&
			(*update.ToolCallUpdate.Status == acp.ToolCallStatusCompleted || *update.ToolCallUpdate.Status == acp.ToolCallStatusFailed)
		if done {
			ev := protocol.RunEvent{
				Type: "tool.completed", CallID: string(update.ToolCallUpdate.ToolCallId),
				ToolOutput: truncate(jsonText(update.ToolCallUpdate.RawOutput), 8000),
			}
			if *update.ToolCallUpdate.Status == acp.ToolCallStatusFailed {
				ev.Error = "tool call failed"
			}
			if ev.ToolOutput == "" {
				ev.ToolOutput = truncate(jsonText(update.ToolCallUpdate.Content), 8000)
			}
			r.send(ev)
			return nil
		}
		if update.ToolCallUpdate.RawInput != nil {
			r.send(protocol.RunEvent{
				Type: "tool.input", CallID: string(update.ToolCallUpdate.ToolCallId),
				ToolInput: truncate(jsonText(update.ToolCallUpdate.RawInput), 8000),
			})
		}
	}
	return nil
}

func acpContentText(block acp.ContentBlock) string {
	if block.Text != nil {
		return block.Text.Text
	}
	return ""
}
