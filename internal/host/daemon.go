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
	"runtime"
	"strings"
	"sync"
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

	runMu      sync.Mutex
	runs       map[string]*Run
	structured map[string]*StructuredRun

	sendMu sync.Mutex
	ws     *websocket.Conn

	lastSnapshot string
	lastByAgent  map[string][]protocol.Session
}

func NewDaemon(cfg Config, logger *slog.Logger) *Daemon {
	d := &Daemon{
		cfg:         cfg,
		policy:      cfg.Policy,
		log:         logger,
		runs:        map[string]*Run{},
		structured:  map[string]*StructuredRun{},
		lastByAgent: map[string][]protocol.Session{},
	}
	d.adapters = DetectAdapters(logger)
	return d
}

func (d *Daemon) Adapters() []Adapter { return d.adapters }

func (d *Daemon) Run(ctx context.Context) error {
	if d.cfg.HubURL == "" {
		return errors.New("not configured")
	}
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := d.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
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

func jitter(d time.Duration) time.Duration {
	// Full jitter without a random package dependency.
	n := time.Now().UnixNano() % int64(d+1)
	if n < 0 {
		n = -n
	}
	return time.Duration(n)
}

func (d *Daemon) connect(ctx context.Context) error {
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
	ws.SetReadLimit(16 << 20)
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

	d.log.Info("connected to hub", "hub", d.cfg.HubURL, "host_id", d.cfg.HostID)
	if err := d.sendInventory(); err != nil {
		d.log.Warn("inventory send failed", "error", err)
	}
	if err := d.sendSessions(true); err != nil {
		d.log.Warn("session snapshot failed", "error", err)
	}
	if err := d.sendActiveRuns(); err != nil {
		d.log.Warn("run snapshot failed", "error", err)
	}

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
			RunID: r.id, Agent: r.agent, Mode: protocol.ModeStructured, State: protocol.StateRunning,
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
	sessions, err := d.listSessions(context.Background())
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(sessions)
	hash := fmt.Sprintf("%x", sha256Sum(raw))
	if !force && hash == d.lastSnapshot {
		return nil
	}
	d.lastSnapshot = hash
	return d.send(protocol.Envelope{Type: "sessions", Sessions: sessions, Full: true})
}

func (d *Daemon) discoveryLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := d.sendSessions(false); err != nil {
				d.log.Debug("session refresh failed", "error", err)
			}
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
	// Overlay live run state so the hub does not mark a resumed session idle.
	d.runMu.Lock()
	active := map[string]string{}
	ours := map[string]bool{}
	for _, r := range d.runs {
		active[r.sessionKey] = protocol.StateRunning
		ours[r.sessionKey] = true
	}
	for _, r := range d.structured {
		active[r.sessionKey] = protocol.StateRunning
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
		d.runMu.Lock()
		r := d.runs[msg.RunID]
		d.runMu.Unlock()
		if r != nil {
			_, _ = r.Write(msg.Data)
		}
	case "run.prompt":
		d.runMu.Lock()
		sr := d.structured[msg.RunID]
		d.runMu.Unlock()
		if sr != nil {
			if err := sr.Prompt(msg.Message); err != nil {
				_ = d.send(protocol.Envelope{Type: "run.event", RunID: msg.RunID, Event: &protocol.RunEvent{Type: "run.error", Error: err.Error()}})
			}
		}
	case "run.approval":
		d.runMu.Lock()
		sr := d.structured[msg.RunID]
		d.runMu.Unlock()
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
		d.runMu.Unlock()
		if r != nil {
			_, _ = r.Write([]byte{3})
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
		d.runMu.Unlock()
		if r != nil {
			go r.Stop()
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
