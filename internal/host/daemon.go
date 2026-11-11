package host

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/coder/websocket"
)

// Daemon owns the hub link, discovery loop and run manager.
type Daemon struct {
	cfg      Config
	policy   Policy
	log      *slog.Logger
	adapters []Adapter

	runMu        sync.Mutex
	runs         map[string]*Run
	structured   map[string]*StructuredRun
	codexRuns    map[string]*CodexRun
	acpRuns      map[string]*ACPRun
	starting     map[string]bool
	startingRuns map[string]bool

	sendMu     sync.Mutex
	snapshotMu sync.Mutex
	ws         *websocket.Conn

	lastSnapshot map[string]string
	lastByAgent  map[string][]protocol.Session
	connectedAt  atomic.Int64
	sessionDelta atomic.Bool

	hubNonceMu sync.Mutex
	hubNonces  map[string]time.Time

	statusMu   sync.Mutex
	updateMu   sync.Mutex
	lastUpdate *protocol.UpdateStatus

	gitMu       sync.Mutex
	gitBranches map[string]gitRepoCache
}

func NewDaemon(cfg Config, logger *slog.Logger) *Daemon {
	d := &Daemon{
		cfg:          cfg,
		policy:       cfg.Policy,
		log:          logger,
		runs:         map[string]*Run{},
		structured:   map[string]*StructuredRun{},
		codexRuns:    map[string]*CodexRun{},
		acpRuns:      map[string]*ACPRun{},
		starting:     map[string]bool{},
		startingRuns: map[string]bool{},
		lastByAgent:  map[string][]protocol.Session{},
		lastSnapshot: map[string]string{},
		hubNonces:    map[string]time.Time{},
		gitBranches:  map[string]gitRepoCache{},
	}
	d.adapters = DetectAdapters(logger)
	return d
}

// NewDaemonWithAdapters builds a daemon with an explicit adapter set. It is
// used by tests and by embedding scenarios; production uses DetectAdapters.
func NewDaemonWithAdapters(cfg Config, logger *slog.Logger, adapters []Adapter) *Daemon {
	d := NewDaemon(cfg, logger)
	d.adapters = adapters
	return d
}

func (d *Daemon) Adapters() []Adapter { return d.adapters }

func (d *Daemon) Run(ctx context.Context) error {
	defer d.stopAllRuns()
	if d.cfg.HubURL == "" {
		return errors.New("not configured")
	}
	cleanupStaleAttachments(24 * time.Hour)
	d.writeRuntimeStatus()
	go d.updateLoop(ctx)
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := d.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.connectedAt.Swap(0) != 0 {
			// A previous connection was established, so this was a normal
			// interruption rather than a failed first dial.
			backoff = time.Second
		}
		d.log.Warn("hub connection ended; reconnecting", "error", err, "backoff", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(jitter(backoff)):
		}
		backoff *= 2
		if backoff > 60*time.Second {
			backoff = 60 * time.Second
		}
	}
}

// stopAllRuns terminates every live run and its process tree. It runs when
// the daemon stops (SIGTERM, service stop or Windows shutdown).
func (d *Daemon) stopAllRuns() {
	d.runMu.Lock()
	runs := make([]*Run, 0, len(d.runs))
	for _, r := range d.runs {
		runs = append(runs, r)
	}
	structured := make([]*StructuredRun, 0, len(d.structured))
	for _, r := range d.structured {
		structured = append(structured, r)
	}
	codex := make([]*CodexRun, 0, len(d.codexRuns))
	for _, r := range d.codexRuns {
		codex = append(codex, r)
	}
	acp := make([]*ACPRun, 0, len(d.acpRuns))
	for _, r := range d.acpRuns {
		acp = append(acp, r)
	}
	d.runMu.Unlock()

	var wg sync.WaitGroup
	for _, r := range runs {
		wg.Add(1)
		go func(r *Run) { defer wg.Done(); r.Stop() }(r)
	}
	for _, r := range structured {
		wg.Add(1)
		go func(r *StructuredRun) { defer wg.Done(); r.Stop() }(r)
	}
	for _, r := range codex {
		wg.Add(1)
		go func(r *CodexRun) { defer wg.Done(); r.Stop() }(r)
	}
	for _, r := range acp {
		wg.Add(1)
		go func(r *ACPRun) { defer wg.Done(); r.Stop() }(r)
	}
	wg.Wait()
}

func jitter(d time.Duration) time.Duration {
	// Full jitter without a random package dependency.
	n := time.Now().UnixNano() % int64(d+1)
	if n < 0 {
		n = -n
	}
	return time.Duration(n)
}

func (d *Daemon) connect(ctx context.Context) error {
	if d.cfg.HubPublicKey == "" {
		key, err := FetchHubPublicKey(ctx, d.cfg.HubURL)
		if err != nil || key == "" {
			if d.cfg.HubSigningRequired {
				return fmt.Errorf("hub signing is required but no key could be pinned: %w", err)
			}
			d.log.Warn("hub command signing is not pinned; continuing unsigned", "error", err)
		} else {
			d.cfg.HubPublicKey = key
			d.cfg.HubSigningRequired = true
			if err := SaveConfig(d.cfg); err != nil {
				d.log.Warn("cannot save pinned hub key", "error", err)
			} else {
				d.log.Info("pinned hub command-signing key")
			}
		}
	}
	u := strings.TrimSuffix(d.cfg.HubURL, "/")
	u = strings.Replace(u, "https://", "wss://", 1)
	u = strings.Replace(u, "http://", "ws://", 1)
	u += "/v1/host/connect?host_id=" + d.cfg.HostID

	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(dialCtx, u, &websocket.DialOptions{
		HTTPHeader: map[string][]string{"X-Nexus-Request": {"1"}},
	})
	if err != nil {
		return fmt.Errorf("dial hub: %w", err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "closing")
	ws.SetReadLimit(32 << 20)
	d.sendMu.Lock()
	d.ws = ws
	d.sendMu.Unlock()
	defer func() {
		d.sendMu.Lock()
		if d.ws == ws {
			d.ws = nil
		}
		d.sendMu.Unlock()
	}()

	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()
	ctx = connCtx
	helloCtx, hcancel := context.WithTimeout(ctx, 15*time.Second)
	defer hcancel()
	_, data, err := ws.Read(helloCtx)
	if err != nil {
		return err
	}
	var hello protocol.Envelope
	if err := json.Unmarshal(data, &hello); err != nil || hello.Type != "hello" {
		return errors.New("hub did not send a protocol hello")
	}
	key, err := d.cfg.SigningKey()
	if err != nil {
		return err
	}
	sig := base64.StdEncoding.EncodeToString(sign(key, []byte(hello.Nonce)))
	if err := d.send(protocol.Envelope{
		Type: "auth", Protocol: protocol.Version, Signature: sig,
		HostName: d.cfg.HostName, OS: platformOS(), Arch: platformArch(), Version: Version,
	}); err != nil {
		return err
	}
	_, data, err = ws.Read(helloCtx)
	if err != nil {
		return err
	}
	var auth protocol.Envelope
	if err := json.Unmarshal(data, &auth); err != nil || auth.Type != "auth.ok" {
		if auth.Error != "" {
			return errors.New(auth.Error)
		}
		return errors.New("host authentication rejected")
	}
	if err := d.verifyHubEnvelope(&auth); err != nil {
		return fmt.Errorf("hub auth signature: %w", err)
	}

	d.sessionDelta.Store(false)
	for _, feature := range auth.Features {
		if feature == "session-delta-v1" {
			d.sessionDelta.Store(true)
		}
	}
	d.connectedAt.Store(time.Now().UnixNano())
	d.log.Info("connected to hub", "hub", d.cfg.HubURL, "host_id", d.cfg.HostID, "session_delta", d.sessionDelta.Load())
	if err := d.sendInventory(); err != nil {
		d.log.Warn("inventory send failed", "error", err)
	}
	if err := d.sendSessions(true); err != nil {
		d.log.Warn("session snapshot failed", "error", err)
	}
	if err := d.sendActiveRuns(); err != nil {
		d.log.Warn("run snapshot failed", "error", err)
	}
	d.sendStoredUpdateStatus()

	// Discovery refresh: rescan on start and every 30 seconds. Change
	// detection is a hash of the normalized snapshot; the hub only receives
	// a full snapshot when something actually changed.
	go d.discoveryLoop(ctx)
	go d.inventoryLoop(ctx)

	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return err
		}
		var msg protocol.Envelope
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if err := d.verifyHubEnvelope(&msg); err != nil {
			d.log.Warn("rejected hub command", "type", msg.Type, "error", err)
			return fmt.Errorf("hub command rejected: %w", err)
		}
		go d.handleMessage(ctx, msg)
	}
}

func (d *Daemon) send(msg protocol.Envelope) error {
	d.sendMu.Lock()
	defer d.sendMu.Unlock()
	if d.ws == nil {
		return errors.New("not connected")
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return d.ws.Write(ctx, websocket.MessageText, data)
}

func (d *Daemon) sendActiveRuns() error {
	d.runMu.Lock()
	runs := make([]protocol.RunInfo, 0, len(d.runs)+len(d.structured))
	for _, r := range d.runs {
		runs = append(runs, protocol.RunInfo{
			RunID: r.id, Agent: r.agent, Mode: protocol.ModeTerminal, State: protocol.StateRunning,
		})
	}
	for _, r := range d.structured {
		runs = append(runs, protocol.RunInfo{
			RunID: r.id, Agent: r.agent, Mode: protocol.ModeStructured, State: r.currentState(),
		})
	}
	for _, r := range d.codexRuns {
		runs = append(runs, protocol.RunInfo{
			RunID: r.id, Agent: r.agent, Mode: protocol.ModeStructured, State: r.currentState(),
		})
	}
	for _, r := range d.acpRuns {
		runs = append(runs, protocol.RunInfo{
			RunID: r.id, Agent: r.agent, Mode: protocol.ModeStructured, State: r.currentState(),
		})
	}
	d.runMu.Unlock()
	return d.send(protocol.Envelope{Type: "active_runs", Runs: runs})
}

func (d *Daemon) sendInventory() error {
	agents := make([]protocol.AgentInfo, 0, len(d.adapters))
	for _, a := range d.adapters {
		info, err := a.Detect(context.Background())
		if err != nil {
			continue
		}
		if d.cfg.ACPCommand(info.Name) != "" && !sliceContains(info.Capabilities, "structured") {
			info.Capabilities = append(info.Capabilities, "structured")
		}
		agents = append(agents, info)
	}
	summary := &protocol.PolicySummary{
		AllowedRoots:  d.policy.AllowedRoots,
		AllowedAgents: d.policy.AllowedAgents,
		AllowNew:      d.policy.AllowNew,
		AllowFork:     d.policy.AllowFork,
	}
	return d.send(protocol.Envelope{
		Type: "inventory", Version: Version, Agents: agents, Policy: summary,
	})
}

func (d *Daemon) sendSessions(force bool) error {
	d.snapshotMu.Lock()
	defer d.snapshotMu.Unlock()
	sessions, err := d.listSessions(context.Background())
	if err != nil {
		return err
	}
	// Send a full snapshot on first connect and deltas afterwards. One active
	// session changes its updated_at on every discovery tick; resending all
	// thousands of rows for that would rewrite the whole hub table every 15s.
	full := force || !d.sessionDelta.Load()
	changed, removed, next := diffSessionSnapshot(d.lastSnapshot, sessions, full)
	if !full && len(changed) == 0 && len(removed) == 0 {
		return nil
	}
	envelope := protocol.Envelope{Type: "sessions", Sessions: changed, Removed: removed, Full: full}
	if full {
		envelope.Sessions = sessions
		envelope.Removed = nil
	}
	if err := d.send(envelope); err != nil {
		return err
	}
	d.lastSnapshot = next
	d.writeRuntimeStatus()
	return nil
}

func (d *Daemon) discoveryLoop(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := d.sendSessions(false); err != nil {
				d.log.Debug("session refresh failed", "error", err)
			}
			d.writeRuntimeStatus()
		}
	}
}

func (d *Daemon) inventoryLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := d.sendInventory(); err != nil {
				return
			}
		}
	}
}

func (d *Daemon) listSessions(ctx context.Context) ([]protocol.Session, error) {
	var out []protocol.Session
	for _, a := range d.adapters {
		sessions, err := a.ListSessions(ctx)
		if err != nil {
			// A broken parser must not look like "all sessions disappeared".
			// Reuse the last successful snapshot for that agent.
			d.log.Warn("discovery failed", "agent", a.Name(), "error", err)
			out = append(out, d.lastByAgent[a.Name()]...)
			continue
		}
		d.lastByAgent[a.Name()] = sessions
		out = append(out, sessions...)
	}
	// Normalize path separators so the hub groups Windows sessions from
	// different adapters under one project. Forward slashes work in all Go
	// filesystem and exec paths on Windows.
	for i := range out {
		out[i].Cwd = filepath.ToSlash(out[i].Cwd)
	}
	// Fill in branch names for adapters whose store does not record them.
	// Branches are cached by directory so discovery stays cheap.
	for i := range out {
		if out[i].Cwd == "" {
			continue
		}
		if out[i].RepoRoot == "" && out[i].RepoURL == "" {
			repo := d.gitRepo(ctx, out[i].Cwd)
			if out[i].GitBranch == "" {
				out[i].GitBranch = repo.branch
			}
			out[i].RepoRoot = repo.root
			out[i].RepoURL = repo.url
		} else if out[i].GitBranch == "" {
			out[i].GitBranch = d.gitBranch(ctx, out[i].Cwd)
		}
	}
	// Overlay live run state so the hub does not mark a resumed session idle.
	d.runMu.Lock()
	active := map[string]string{}
	ours := map[string]bool{}
	for _, r := range d.runs {
		active[r.sessionKey] = protocol.StateRunning
		ours[r.sessionKey] = true
	}
	for _, r := range d.structured {
		active[r.sessionKey] = r.currentState()
		ours[r.sessionKey] = true
	}
	for _, r := range d.codexRuns {
		active[r.sessionKey] = r.currentState()
		ours[r.sessionKey] = true
	}
	for _, r := range d.acpRuns {
		active[r.sessionKey] = r.currentState()
		ours[r.sessionKey] = true
	}
	d.runMu.Unlock()
	// Best-effort detection of sessions open in a native terminal. This never
	// affects our own runs and is reported as a hint, not a guarantee.
	outside := detectActiveElsewhere(ctx, out)
	for i := range out {
		key := out[i].Agent + ":" + out[i].NativeID
		if state, ok := active[key]; ok {
			out[i].State = state
			continue
		}
		if outside[key] && !ours[key] {
			out[i].ActiveElsewhere = true
			out[i].State = protocol.StateRunning
		}
	}
	return out, nil
}

func (d *Daemon) handleMessage(ctx context.Context, msg protocol.Envelope) {
	switch msg.Type {
	case "request":
		go d.handleRequest(msg)
	case "run.start":
		if msg.Run == nil {
			return
		}
		go d.startRun(ctx, *msg.Run)
	case "run.input":
		if len(msg.Data) > 2<<20 {
			_ = d.send(protocol.Envelope{Type: "run.error", RunID: msg.RunID, Error: "terminal input is too large"})
			return
		}
		d.runMu.Lock()
		r := d.runs[msg.RunID]
		d.runMu.Unlock()
		if r != nil {
			_, _ = r.Write(msg.Data)
		}
	case "run.prompt":
		if len(msg.Message) > 256<<10 {
			_ = d.send(protocol.Envelope{Type: "run.event", RunID: msg.RunID, Event: &protocol.RunEvent{Type: "run.error", Error: "prompt is too large"}})
			return
		}
		d.runMu.Lock()
		sr := d.structured[msg.RunID]
		cr := d.codexRuns[msg.RunID]
		ar := d.acpRuns[msg.RunID]
		d.runMu.Unlock()
		if ar != nil {
			if err := ar.Prompt(msg.Message, msg.Options); err != nil {
				_ = d.send(protocol.Envelope{Type: "run.event", RunID: msg.RunID, Event: &protocol.RunEvent{Type: "run.error", Error: err.Error()}})
			}
			return
		}
		if cr != nil {
			if err := cr.Prompt(msg.Message, msg.Options); err != nil {
				_ = d.send(protocol.Envelope{Type: "run.event", RunID: msg.RunID, Event: &protocol.RunEvent{Type: "run.error", Error: err.Error()}})
			}
			return
		}
		if sr != nil {
			if err := sr.Prompt(msg.Message, msg.Options); err != nil {
				_ = d.send(protocol.Envelope{Type: "run.event", RunID: msg.RunID, Event: &protocol.RunEvent{Type: "run.error", Error: err.Error()}})
			}
		}
	case "run.approval":
		d.runMu.Lock()
		sr := d.structured[msg.RunID]
		cr := d.codexRuns[msg.RunID]
		ar := d.acpRuns[msg.RunID]
		d.runMu.Unlock()
		if ar != nil {
			if err := ar.ReplyApproval(msg.ApprovalID, msg.Decision); err != nil {
				_ = d.send(protocol.Envelope{Type: "run.event", RunID: msg.RunID, Event: &protocol.RunEvent{Type: "run.error", Error: err.Error()}})
			}
			return
		}
		if cr != nil {
			if err := cr.ReplyApproval(msg.ApprovalID, msg.Decision); err != nil {
				_ = d.send(protocol.Envelope{Type: "run.event", RunID: msg.RunID, Event: &protocol.RunEvent{Type: "run.error", Error: err.Error()}})
			}
			return
		}
		if sr != nil {
			if err := sr.ReplyApproval(msg.ApprovalID, msg.Decision); err != nil {
				_ = d.send(protocol.Envelope{Type: "run.event", RunID: msg.RunID, Event: &protocol.RunEvent{Type: "run.error", Error: err.Error()}})
			}
		}
	case "run.resize":
		d.runMu.Lock()
		r := d.runs[msg.RunID]
		d.runMu.Unlock()
		if r != nil {
			_ = r.Resize(msg.Cols, msg.Rows)
		}
	case "run.interrupt":
		d.runMu.Lock()
		r := d.runs[msg.RunID]
		sr := d.structured[msg.RunID]
		cr := d.codexRuns[msg.RunID]
		ar := d.acpRuns[msg.RunID]
		d.runMu.Unlock()
		if r != nil {
			_, _ = r.Write([]byte{3})
		}
		if ar != nil {
			_ = ar.Interrupt()
		}
		if cr != nil {
			_ = cr.Interrupt()
		}
		if sr != nil {
			if err := sr.Interrupt(); err != nil {
				d.log.Warn("structured interrupt failed", "run_id", msg.RunID, "error", err)
			}
		}
	case "run.stop":
		d.runMu.Lock()
		r := d.runs[msg.RunID]
		sr := d.structured[msg.RunID]
		cr := d.codexRuns[msg.RunID]
		ar := d.acpRuns[msg.RunID]
		d.runMu.Unlock()
		if r != nil {
			go r.Stop()
		}
		if ar != nil {
			ar.Stop()
		}
		if cr != nil {
			cr.Stop()
		}
		if sr != nil {
			sr.Stop()
		}
	}
}

func (d *Daemon) handleRequest(msg protocol.Envelope) {
	result, err := d.dispatchRequest(msg.Method, msg.Params)
	if err != nil {
		_ = d.send(protocol.Envelope{Type: "result", RequestID: msg.RequestID, OK: false, Error: err.Error()})
		return
	}
	raw, _ := json.Marshal(result)
	_ = d.send(protocol.Envelope{Type: "result", RequestID: msg.RequestID, OK: true, Result: raw})
}

func (d *Daemon) dispatchRequest(method string, params []byte) (any, error) {
	switch method {
	case "transcript":
		var p struct {
			NativeID string `json:"native_id"`
			Agent    string `json:"agent"`
			Offset   int    `json:"offset"`
			Limit    int    `json:"limit"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		a := d.adapter(p.Agent)
		if a == nil {
			return nil, errors.New("agent not available")
		}
		return a.Transcript(context.Background(), p.NativeID, p.Offset, p.Limit)
	case "search":
		var p struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		var hits []protocol.SearchHit
		for _, a := range d.adapters {
			h, err := a.Search(context.Background(), p.Query, p.Limit)
			if err != nil {
				d.log.Debug("search failed", "agent", a.Name(), "error", err)
				continue
			}
			hits = append(hits, h...)
		}
		return hits, nil
	case "rescan":
		return map[string]string{"status": "ok"}, d.sendSessions(true)
	case "git":
		var p struct {
			Cwd string `json:"cwd"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return d.gitView(context.Background(), p.Cwd)
	case "update.check":
		var p struct {
			Channel string `json:"channel"`
		}
		_ = json.Unmarshal(params, &p)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return CheckForUpdate(ctx, Version, p.Channel)
	case "run.options":
		var p struct {
			RunID string `json:"run_id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		d.runMu.Lock()
		sr := d.structured[p.RunID]
		cr := d.codexRuns[p.RunID]
		d.runMu.Unlock()
		if sr != nil {
			return sr.Options(context.Background())
		}
		if cr != nil {
			return cr.Options(context.Background())
		}
		return protocol.RunOptions{Models: []protocol.ModelOption{}, Modes: []protocol.AgentModeOption{}}, nil
	default:
		return nil, fmt.Errorf("unknown method %q", method)
	}
}

func (d *Daemon) adapter(name string) Adapter {
	for _, a := range d.adapters {
		if a.Name() == name {
			return a
		}
	}
	return nil
}

// sessionActiveLocked reports whether any execution mode currently owns the
// session key. Callers must hold runMu.
func (d *Daemon) sessionActiveLocked(key string) bool {
	for _, r := range d.runs {
		if r.sessionKey == key {
			return true
		}
	}
	for _, r := range d.structured {
		if r.sessionKey == key {
			return true
		}
	}
	for _, r := range d.codexRuns {
		if r.sessionKey == key {
			return true
		}
	}
	for _, r := range d.acpRuns {
		if r.sessionKey == key {
			return true
		}
	}
	return false
}

// reserveRunLocked claims a session and run id for the short window between
// validation and process registration. Callers must hold runMu.
func (d *Daemon) reserveRunLocked(sessionKey, runID string) bool {
	if d.sessionActiveLocked(sessionKey) || d.starting[sessionKey] ||
		d.runActiveLocked(runID) || d.startingRuns[runID] {
		return false
	}
	d.starting[sessionKey] = true
	d.startingRuns[runID] = true
	return true
}

func (d *Daemon) releaseRunLocked(sessionKey, runID string) {
	delete(d.starting, sessionKey)
	delete(d.startingRuns, runID)
}

// runActiveLocked reports whether a run id already exists. Callers must hold
// runMu.
func (d *Daemon) runActiveLocked(id string) bool {
	if _, ok := d.runs[id]; ok {
		return true
	}
	if _, ok := d.structured[id]; ok {
		return true
	}
	if _, ok := d.codexRuns[id]; ok {
		return true
	}
	_, ok := d.acpRuns[id]
	return ok
}

// validateRunPolicy applies the local policy to every terminal action before
// any process is started. It is deliberately independent of the hub: a
// compromised hub must not be able to bypass host policy.
func (d *Daemon) validateRunPolicy(spec protocol.RunSpec) error {
	switch spec.Action {
	case protocol.ActionNew, protocol.ActionResume, protocol.ActionFork, "":
	default:
		return fmt.Errorf("unknown run action %q", spec.Action)
	}
	if !d.policy.AgentAllowed(spec.Agent) {
		return fmt.Errorf("agent %q is not allowed by host policy", spec.Agent)
	}
	if spec.Action == protocol.ActionNew && !d.policy.AllowNew {
		return errors.New("host policy does not allow new sessions")
	}
	if spec.Action == protocol.ActionFork && !d.policy.AllowFork {
		return errors.New("host policy does not allow forking")
	}
	if spec.Action != protocol.ActionNew && !validNativeID(spec.NativeID) {
		return errors.New("invalid native session id")
	}
	if spec.Cwd != "" && !d.policy.WithinRoots(spec.Cwd) {
		return errors.New("working directory is outside the allowed roots")
	}
	return nil
}

func platformOS() string   { return runtime.GOOS }
func platformArch() string { return runtime.GOARCH }

func sign(key ed25519.PrivateKey, message []byte) []byte {
	return ed25519.Sign(key, message)
}

func sha256Sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}
func ReadHostname() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "agentnexus-host"
}

// diffSessionSnapshot compares the current session metadata with the last
// successfully sent snapshot. The hash is over the full normalized session,
// so title, state, message count, branch and repository changes are detected.
func diffSessionSnapshot(previous map[string]string, sessions []protocol.Session, force bool) (changed []protocol.Session, removed []protocol.SessionKey, next map[string]string) {
	next = make(map[string]string, len(sessions))
	changed = make([]protocol.Session, 0, len(sessions))
	for _, session := range sessions {
		raw, _ := json.Marshal(session)
		key := session.Agent + "\x00" + session.NativeID
		hash := fmt.Sprintf("%x", sha256Sum(raw))
		next[key] = hash
		if force || previous[key] != hash {
			changed = append(changed, session)
		}
	}
	if force {
		return changed, nil, next
	}
	for key := range previous {
		if _, ok := next[key]; ok {
			continue
		}
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) == 2 {
			removed = append(removed, protocol.SessionKey{Agent: parts[0], NativeID: parts[1]})
		}
	}
	return changed, removed, next
}

type runtimeStatus struct {
	PID        int       `json:"pid"`
	Version    string    `json:"version"`
	ActiveRuns int       `json:"active_runs"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (d *Daemon) activeRunCount() int {
	d.runMu.Lock()
	defer d.runMu.Unlock()
	return len(d.runs) + len(d.structured) + len(d.codexRuns) + len(d.acpRuns) + len(d.startingRuns)
}

// writeRuntimeStatus publishes the active-run count for the updater. A stale
// file is ignored, so a crashed daemon cannot block a recovery update.
func (d *Daemon) writeRuntimeStatus() {
	dir, err := ConfigDir()
	if err != nil {
		return
	}
	d.statusMu.Lock()
	defer d.statusMu.Unlock()
	data, _ := json.Marshal(runtimeStatus{
		PID: os.Getpid(), Version: Version,
		ActiveRuns: d.activeRunCount(), UpdatedAt: time.Now().UTC(),
	})
	path := filepath.Join(dir, "runtime.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err == nil {
		_ = os.Rename(tmp, path)
	}
}

func (d *Daemon) updateLoop(ctx context.Context) {
	d.checkAndReportUpdate(ctx)
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.checkAndReportUpdate(ctx)
		}
	}
}

func (d *Daemon) checkAndReportUpdate(ctx context.Context) {
	channel := strings.TrimSpace(os.Getenv("NEXUS_UPDATE_CHANNEL"))
	info, err := CheckForUpdate(ctx, Version, channel)
	status := &protocol.UpdateStatus{
		Current: Version, Latest: info.Latest, Available: info.Available,
		Prerelease: info.Prerelease, CheckedAt: time.Now().UTC(),
	}
	if err != nil {
		if errors.Is(err, ErrNoRelease) {
			status.Error = ErrNoRelease.Error()
		} else {
			status.Error = err.Error()
		}
	}
	d.updateMu.Lock()
	d.lastUpdate = status
	d.updateMu.Unlock()
	d.sendUpdateStatus(status)
}

func (d *Daemon) sendStoredUpdateStatus() {
	d.updateMu.Lock()
	status := d.lastUpdate
	d.updateMu.Unlock()
	if status != nil {
		d.sendUpdateStatus(status)
	}
}

func (d *Daemon) sendUpdateStatus(status *protocol.UpdateStatus) {
	if status == nil {
		return
	}
	copyStatus := *status
	_ = d.send(protocol.Envelope{Type: "update.status", Update: &copyStatus})
}
