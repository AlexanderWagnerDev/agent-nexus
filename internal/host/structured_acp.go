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
	guard      *processGuard
	stdin      io.WriteCloser
	conn       *acp.ClientSideConnection
	cancel     context.CancelFunc
	stdout     *os.File
	stderr     *os.File

	mu             sync.Mutex
	sessionID      acp.SessionId
	models         []protocol.ModelOption
	modelConfigID  string
	thoughtID      string
	variants       []string
	currentModel   string
	currentVariant string
	modes          []protocol.AgentModeOption
	currentMode    string
	supportsFiles  bool
	approvals      map[string]chan string
	approvalSeq    int
	turnSeq        int
	turnID         string
	thought        strings.Builder
	lastMessageID  string
	state          string
	active         bool
	closed         bool
	usage          protocol.Usage
}

func (d *Daemon) startACPStructured(parent context.Context, spec protocol.RunSpec, command string) {
	fail := func(msg string) {
		d.log.Warn("ACP structured run rejected", "run_id", spec.RunID, "error", msg)
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
	if spec.Action == protocol.ActionFork {
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
		fail("cannot start ACP adapter: " + err.Error())
		return
	}
	closeWrite()
	guard := newProcessGuard()
	if err := guard.Attach(cmd.Process.Pid); err != nil {
		d.log.Warn("cannot attach a process guard", "run_id", spec.RunID, "error", err)
	}
	ar := &ACPRun{
		id: spec.RunID, sessionKey: sessionKey, agent: spec.Agent, cwd: cwd,
		daemon: d, cmd: cmd, guard: guard, stdin: stdin, cancel: cancel,
		stdout: stdout, stderr: stderr,
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
	// Plain OS pipes let Wait run immediately; the SDK reader and the stderr
	// scanner are released by markFinished/Stop.
	waitExit := make(chan error, 1)
	go func() { waitExit <- cmd.Wait() }()

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
	reportedNativeID := ""
	if spec.Action == protocol.ActionNew {
		resp, err := ar.conn.NewSession(ctx, acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
		if err != nil {
			ar.Stop()
			fail("cannot create ACP session: " + err.Error())
			return
		}
		ar.sessionID = resp.SessionId
		reportedNativeID = string(resp.SessionId)
		ar.applySessionState(init, resp.Modes, resp.ConfigOptions)
	} else if spec.Action == protocol.ActionFork {
		if init.AgentCapabilities.SessionCapabilities.Fork == nil {
			ar.Stop()
			fail("ACP adapter does not advertise the unstable session/fork capability")
			return
		}
		forked, err := ar.conn.UnstableForkSession(ctx, acp.UnstableForkSessionRequest{
			Cwd: cwd, McpServers: []acp.UnstableMcpServer{}, SessionId: acp.SessionId(spec.NativeID),
		})
		if err != nil {
			ar.Stop()
			fail("cannot fork ACP session: " + err.Error())
			return
		}
		if forked.SessionId == "" {
			ar.Stop()
			fail("ACP adapter returned an empty forked session id")
			return
		}
		ar.sessionID = forked.SessionId
		reportedNativeID = string(forked.SessionId)
		// The unstable fork response uses its own config-option type. Mode
		// state is compatible; the normal option endpoints remain available.
		ar.applySessionState(init, forked.Modes, nil)
	} else {
		if !init.AgentCapabilities.LoadSession {
			ar.Stop()
			fail("ACP adapter does not support session/load; start a new session instead")
			return
		}
		ar.sessionID = acp.SessionId(spec.NativeID)
		loaded, err := ar.conn.LoadSession(ctx, acp.LoadSessionRequest{
			Cwd: cwd, McpServers: []acp.McpServer{}, SessionId: ar.sessionID,
		})
		if err != nil {
			ar.Stop()
			fail("cannot load ACP session: " + err.Error())
			return
		}
		ar.applySessionState(init, loaded.Modes, loaded.ConfigOptions)
	}

	d.runMu.Lock()
	d.acpRuns[spec.RunID] = ar
	d.runMu.Unlock()
	_ = d.send(protocol.Envelope{Type: "run.started", RunID: spec.RunID, Reason: "structured", Action: spec.Action, ForkedFrom: forkSource(spec), NativeID: reportedNativeID, Agent: spec.Agent, Cwd: cwd})
	_ = d.send(protocol.Envelope{Type: "run.event", RunID: spec.RunID, Event: &protocol.RunEvent{Type: "run.state", State: protocol.StateIdle, Text: "ready for a prompt"}})
	d.log.Info("ACP structured run started", "run_id", spec.RunID, "agent", spec.Agent, "session", ar.sessionID, "cwd", cwd)
	hostAudit("acp.structured.started", map[string]any{"run_id": spec.RunID, "agent": spec.Agent, "cwd": cwd})
	go func() {
		err := <-waitExit
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		ar.markFinished()
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

func (r *ACPRun) Prompt(text string, options *protocol.PromptOptions) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return errors.New("run is closed")
	}
	if r.active {
		r.mu.Unlock()
		return errors.New("agent is already processing a prompt")
	}
	sessionID := r.sessionID
	modelConfig := r.modelConfigID
	thoughtConfig := r.thoughtID
	hasModes := len(r.modes) > 0
	supportsFiles := r.supportsFiles
	r.active = true
	r.turnSeq++
	r.turnID = fmt.Sprintf("%s-turn-%d", r.sessionID, r.turnSeq)
	r.thought.Reset()
	r.lastMessageID = ""
	r.mu.Unlock()

	fail := func(err error) error {
		r.mu.Lock()
		r.active = false
		r.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if options != nil {
		if options.Model != "" {
			if modelConfig == "" {
				return fail(errors.New("this ACP agent does not expose a model selector"))
			}
			_, err := r.conn.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{
				ValueId: &acp.SetSessionConfigOptionValueId{
					ConfigId: acp.SessionConfigId(modelConfig), SessionId: sessionID,
					Value: acp.SessionConfigValueId(options.Model),
				},
			})
			if err != nil {
				return fail(err)
			}
			r.mu.Lock()
			r.currentModel = options.Model
			r.mu.Unlock()
		}
		if options.Variant != "" {
			if thoughtConfig == "" {
				return fail(errors.New("this ACP agent does not expose a reasoning selector"))
			}
			_, err := r.conn.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{
				ValueId: &acp.SetSessionConfigOptionValueId{
					ConfigId: acp.SessionConfigId(thoughtConfig), SessionId: sessionID,
					Value: acp.SessionConfigValueId(options.Variant),
				},
			})
			if err != nil {
				return fail(err)
			}
			r.mu.Lock()
			r.currentVariant = options.Variant
			r.mu.Unlock()
		}
		if options.AgentMode != "" {
			if !hasModes {
				return fail(errors.New("this ACP agent does not expose session modes"))
			}
			if _, err := r.conn.SetSessionMode(ctx, acp.SetSessionModeRequest{
				SessionId: sessionID, ModeId: acp.SessionModeId(options.AgentMode),
			}); err != nil {
				return fail(err)
			}
			r.mu.Lock()
			r.currentMode = options.AgentMode
			r.mu.Unlock()
		}
	}
	blocks := []acp.ContentBlock{acp.TextBlock(text)}
	if options != nil {
		for _, attachment := range options.Attachments {
			if !strings.HasPrefix(strings.ToLower(attachment.MIME), "image/") {
				return fail(errors.New("this ACP agent only accepts image attachments"))
			}
			if !supportsFiles {
				return fail(errors.New("this ACP agent does not advertise image prompt support"))
			}
			blocks = append(blocks, acp.ImageBlock(attachment.Data, attachment.MIME))
		}
	}

	r.send(protocol.RunEvent{Type: "run.state", State: protocol.StateRunning})
	go func() {
		resp, err := r.conn.Prompt(context.Background(), acp.PromptRequest{
			SessionId: sessionID,
			Prompt:    blocks,
		})
		if err == nil && resp.Usage != nil {
			r.reportACPUsage(resp.Usage, nil)
		}
		r.finishPrompt(err)
	}()
	return nil
}

// finishPrompt settles one ACP turn. The adapter session stays open after a
// failed turn, so the run returns to idle and remains ready for the next
// prompt instead of looking busy forever.
func (r *ACPRun) finishPrompt(promptErr error) {
	r.mu.Lock()
	thought := r.thought.String()
	r.thought.Reset()
	r.active = false
	r.mu.Unlock()
	if thought != "" {
		r.send(protocol.RunEvent{Type: "reasoning", Text: thought})
	}
	if promptErr != nil {
		r.send(protocol.RunEvent{Type: "run.error", Error: promptErr.Error()})
	}
	r.send(protocol.RunEvent{Type: "run.state", State: protocol.StateIdle})
}

// Options returns only what the agent actually advertised: config options
// categorised as model/thought level and the session's mode list.
func (r *ACPRun) Options(context.Context) (protocol.RunOptions, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := protocol.RunOptions{
		Models:         append([]protocol.ModelOption(nil), r.models...),
		Modes:          append([]protocol.AgentModeOption(nil), r.modes...),
		CurrentModel:   r.currentModel,
		CurrentVariant: r.currentVariant,
		CurrentMode:    r.currentMode,
		SupportsFiles:  r.supportsFiles,
	}
	if out.Models == nil {
		out.Models = []protocol.ModelOption{}
	}
	if out.Modes == nil {
		out.Modes = []protocol.AgentModeOption{}
	}
	return out, nil
}

func (r *ACPRun) applySessionState(init acp.InitializeResponse, modes *acp.SessionModeState, configs []acp.SessionConfigOption) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.supportsFiles = init.AgentCapabilities.PromptCapabilities.Image
	r.models = nil
	r.variants = nil
	r.modes = nil
	r.currentMode = ""
	for _, config := range configs {
		if config.Select == nil {
			continue
		}
		selectConfig := config.Select
		category := ""
		if selectConfig.Category != nil {
			category = string(*selectConfig.Category)
		}
		values := selectValues(selectConfig.Options)
		switch category {
		case string(acp.SessionConfigOptionCategoryModel):
			r.modelConfigID = string(selectConfig.Id)
			r.currentModel = string(selectConfig.CurrentValue)
			for _, value := range values {
				r.models = append(r.models, protocol.ModelOption{
					ID: string(value.Value), Name: value.Name, Provider: selectConfig.Name,
				})
			}
		case string(acp.SessionConfigOptionCategoryThoughtLevel):
			r.thoughtID = string(selectConfig.Id)
			r.currentVariant = string(selectConfig.CurrentValue)
			for _, value := range values {
				r.variants = append(r.variants, string(value.Value))
			}
		}
	}
	if len(r.variants) > 0 {
		for i := range r.models {
			r.models[i].Variants = append([]string(nil), r.variants...)
		}
	}
	if modes != nil {
		r.currentMode = string(modes.CurrentModeId)
		for _, mode := range modes.AvailableModes {
			option := protocol.AgentModeOption{ID: string(mode.Id), Name: mode.Name}
			if mode.Description != nil {
				option.Description = *mode.Description
			}
			r.modes = append(r.modes, option)
		}
	}
}

func selectValues(options acp.SessionConfigSelectOptions) []acp.SessionConfigSelectOption {
	if options.Ungrouped != nil {
		return []acp.SessionConfigSelectOption(*options.Ungrouped)
	}
	if options.Grouped != nil {
		var out []acp.SessionConfigSelectOption
		for _, group := range *options.Grouped {
			out = append(out, group.Options...)
		}
		return out
	}
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

func (r *ACPRun) closeReaders() {
	closePipe(r.stdout)
	closePipe(r.stderr)
}

// markFinished records an adapter that exited on its own and releases the
// readers so a descendant holding a pipe cannot keep the run active.
func (r *ACPRun) markFinished() {
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
	r.closeReaders()
	r.cancel()
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
	stopProcessTree(r.guard, r.cmd, r.daemon.policy.ProcessGrace())
	r.closeReaders()
	r.cancel()
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
		r.send(protocol.RunEvent{Type: "approval.resolved", ApprovalID: id, Decision: "reject", Text: "approval cancelled"})
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

// reportACPUsage records the cumulative usage the adapter advertised. ACP
// costs are only stored when the adapter states USD; other currencies are not
// converted or guessed.
func (r *ACPRun) reportACPUsage(usage *acp.Usage, cost *acp.Cost) {
	if usage != nil {
		r.mu.Lock()
		r.usage.InputTokens = int64(usage.InputTokens)
		r.usage.OutputTokens = int64(usage.OutputTokens)
		if usage.CachedReadTokens != nil {
			r.usage.CacheReadTokens = int64(*usage.CachedReadTokens)
		}
		if usage.CachedWriteTokens != nil {
			r.usage.CacheWriteTokens = int64(*usage.CachedWriteTokens)
		}
		r.usage.Source = "agent"
		r.usage.UpdatedAt = time.Now().UTC()
		r.mu.Unlock()
	}
	if cost != nil && cost.Currency == "USD" {
		amount := cost.Amount
		r.mu.Lock()
		r.usage.CostUSD = &amount
		r.usage.Source = "agent"
		r.usage.UpdatedAt = time.Now().UTC()
		r.mu.Unlock()
	}
	r.mu.Lock()
	snapshot := r.usage
	r.mu.Unlock()
	if snapshot.InputTokens == 0 && snapshot.OutputTokens == 0 && snapshot.CostUSD == nil {
		return
	}
	_ = r.daemon.send(protocol.Envelope{Type: "run.usage", RunID: r.id, Usage: &snapshot})
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
	case update.UsageUpdate != nil:
		r.reportACPUsage(nil, update.UsageUpdate.Cost)
	}
	return nil
}

func acpContentText(block acp.ContentBlock) string {
	if block.Text != nil {
		return block.Text.Text
	}
	return ""
}
