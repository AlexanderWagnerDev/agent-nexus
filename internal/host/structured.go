package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
	cancel     context.CancelFunc
	daemon     *Daemon

	mu        sync.Mutex
	calls     map[string]string
	approvals map[string]*time.Timer
	closed    bool
	readyOnce *sync.Once
	state     string
}

func (d *Daemon) startStructuredRun(parent context.Context, spec protocol.RunSpec) {
	fail := func(msg string) {
		d.log.Warn("structured run rejected", "run_id", spec.RunID, "error", msg)
		_ = d.send(protocol.Envelope{Type: "run.error", RunID: spec.RunID, Error: msg})
	}
	if err := d.validateRunPolicy(spec); err != nil {
		fail(err.Error())
		return
	}
	if spec.Agent == "codex" {
		d.startCodexStructured(parent, spec)
		return
	}
	if command := d.cfg.ACPCommand(spec.Agent); command != "" {
		d.startACPStructured(parent, spec, command)
		return
	}
	if spec.Agent != "opencode" {
		fail("structured mode is available for OpenCode, Codex and configured ACP agents; use terminal mode")
		return
	}
	if spec.Action == protocol.ActionFork {
		fail("structured fork is not implemented; use terminal fork")
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

	port, err := freeLoopbackPort()
	if err != nil {
		fail("cannot allocate a loopback port: " + err.Error())
		return
	}
	binPath := ""
	for _, a := range d.adapters {
		if a.Name() == "opencode" {
			binPath = d.agentBinary(a)
			break
		}
	}
	if binPath == "" {
		fail("opencode executable not found")
		return
	}
	path, args := commandFor(binPath, []string{"serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port)})
	runCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(runCtx, path, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "NO_COLOR=1")
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
		fail("cannot start opencode server: " + err.Error())
		return
	}
	sr := &StructuredRun{
		id: spec.RunID, agent: spec.Agent, cwd: cwd, daemon: d,
		sessionKey: sessionKey, state: protocol.StateIdle,
		cmd: cmd, cancel: cancel, calls: map[string]string{}, approvals: map[string]*time.Timer{},
		client:    &http.Client{Timeout: 30 * time.Second},
		readyOnce: new(sync.Once),
	}
	ready := make(chan struct{})
	go sr.scanServerOutput(stdout, ready)
	go sr.scanServerOutput(stderr, ready)
	select {
	case <-ready:
	case <-time.After(25 * time.Second):
		cancel()
		_ = cmd.Process.Kill()
		fail("opencode server did not become ready")
		return
	case <-parent.Done():
		cancel()
		_ = cmd.Process.Kill()
		return
	}
	if sr.baseURL == "" || sr.password == "" {
		cancel()
		_ = cmd.Process.Kill()
		fail("opencode server did not report its address and password")
		return
	}
	if spec.Action == protocol.ActionNew {
		sessionID, err := sr.createSession(cwd)
		if err != nil {
			sr.Stop()
			fail("cannot create opencode session: " + err.Error())
			return
		}
		sr.sessionID = sessionID
	} else {
		sr.sessionID = spec.NativeID
	}
	if sr.sessionID == "" {
		sr.Stop()
		fail("no native session id")
		return
	}

	d.runMu.Lock()
	d.structured[spec.RunID] = sr
	d.runMu.Unlock()
	_ = d.send(protocol.Envelope{Type: "run.started", RunID: spec.RunID, Reason: "structured"})
	d.log.Info("structured run started", "run_id", spec.RunID, "agent", spec.Agent, "session", sr.sessionID, "cwd", cwd)
	hostAudit("structured.started", map[string]any{"run_id": spec.RunID, "agent": spec.Agent, "cwd": cwd})
	_ = d.send(protocol.Envelope{Type: "run.event", RunID: spec.RunID, Event: &protocol.RunEvent{
		Type: "run.state", State: protocol.StateIdle, Text: "ready for a prompt",
	}})
	go sr.streamEvents(runCtx)
	go func() {
		err := cmd.Wait()
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
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
	req, err := http.NewRequestWithContext(ctx, method, r.baseURL+path, reader)
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth("opencode", r.password)
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
			uri := "file://" + filepath.ToSlash(file.Path)
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
	if decision == "reject" {
		reply = "reject"
	}
	_, _, err := r.request(context.Background(), http.MethodPost,
		"/api/session/"+url.PathEscape(r.sessionID)+"/permission/"+url.PathEscape(id)+"/reply",
		map[string]any{"decision": reply})
	return err
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
	r.cancel()
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
	}
	cleanupPromptAttachments(r.id)
}

func (r *StructuredRun) streamEvents(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/api/event", nil)
	if err != nil {
		return
	}
	req.SetBasicAuth("opencode", r.password)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		r.daemon.log.Warn("structured event stream failed", "run_id", r.id, "error", err)
		return
	}
	defer resp.Body.Close()
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
	case "session.reasoning.ended":
		if text := str(data["text"]); text != "" {
			send(protocol.RunEvent{Type: "reasoning", MessageID: str(data["assistantMessageID"]), Text: text})
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
