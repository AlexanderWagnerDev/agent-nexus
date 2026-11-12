// Package protocol contains the wire types shared by the hub and the host
// daemon. The host link is a single WebSocket carrying JSON envelopes. The
// same types are used by the browser client where noted.
package protocol

import (
	"time"
)

import (
	"crypto/sha256"
	"fmt"
)

func HostSAS(hostID string, publicKey []byte) string {
	sum := sha256.Sum256(append([]byte(hostID+"|"), publicKey...))
	n := (uint32(sum[0])<<16 | uint32(sum[1])<<8 | uint32(sum[2])) % 1_000_000
	return fmt.Sprintf("%03d-%03d", n/1000, n%1000)
}

const (
	// Version is the host-link protocol version. The hub accepts the current
	// major version and reports a mismatch with an upgrade hint.
	Version = "0.1"

	ActionResume = "resume"
	ActionNew    = "new"
	ActionFork   = "fork"

	ModeTerminal   = "terminal"
	ModeStructured = "structured"

	StateIdle      = "idle"
	StateRunning   = "running"
	StateWaiting   = "waiting_for_input"
	StateCompleted = "completed"
	StateFailed    = "failed"
	StateUnknown   = "unknown"
	StateStarting  = "starting"
	StateStopping  = "stopping"
	StateExited    = "exited"
)

// AgentInfo describes one coding-agent installation on a host.
type AgentInfo struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Path         string   `json:"path"`
	Supported    bool     `json:"supported"`
	Capabilities []string `json:"capabilities,omitempty"`
	// Untested is true when the detected version is outside the tested band.
	Untested bool `json:"untested,omitempty"`
	// VersionWarning explains an untested or unrecognized version.
	VersionWarning string `json:"version_warning,omitempty"`
}

// PolicySummary is the read-only view of the host policy that the hub may
// display. Enforcement always stays on the host.
type PolicySummary struct {
	AllowedRoots  []string `json:"allowed_roots"`
	AllowedAgents []string `json:"allowed_agents"`
	AllowNew      bool     `json:"allow_new_sessions"`
	AllowFork     bool     `json:"allow_fork"`
}

// Session is the normalized metadata for one native agent session.
type Session struct {
	NativeID        string    `json:"native_id"`
	Agent           string    `json:"agent"`
	Title           string    `json:"title"`
	Cwd             string    `json:"cwd"`
	GitBranch       string    `json:"git_branch,omitempty"`
	RepoRoot        string    `json:"repo_root,omitempty"`
	RepoURL         string    `json:"repo_url,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	MessageCount    int       `json:"message_count"`
	State           string    `json:"state"`
	ActiveElsewhere bool      `json:"active_elsewhere"`
	Warning         string    `json:"warning,omitempty"`
}

// UpdateStatus is reported by a host after checking the release feed. The hub
// stores only this metadata so update availability can be shown centrally.
type UpdateStatus struct {
	Current    string    `json:"current"`
	Latest     string    `json:"latest,omitempty"`
	Available  bool      `json:"available"`
	Prerelease bool      `json:"prerelease,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
	Error      string    `json:"error,omitempty"`
}

// SessionKey identifies a native session without sending its metadata.
type SessionKey struct {
	Agent    string `json:"agent"`
	NativeID string `json:"native_id"`
}

// Attachment is a user-supplied file forwarded to the host for a prompt.
// Only the host writes it to disk; the hub never persists file content.
type Attachment struct {
	Name string `json:"name"`
	MIME string `json:"mime,omitempty"`
	Data string `json:"data"` // base64
}

// PromptOptions are optional per-prompt agent settings. Empty values keep the
// session's existing configuration.
type PromptOptions struct {
	Model       string       `json:"model,omitempty"`
	Provider    string       `json:"provider,omitempty"`
	Variant     string       `json:"variant,omitempty"`
	AgentMode   string       `json:"agent_mode,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// PromptQueueItem is one queued structured prompt. Only metadata and the
// prompt itself are stored on the hub; the agent still runs on the host.
type PromptQueueItem struct {
	ID          string         `json:"id"`
	RunID       string         `json:"run_id"`
	UserID      string         `json:"-"`
	DeviceID    string         `json:"-"`
	Text        string         `json:"text"`
	Options     *PromptOptions `json:"options,omitempty"`
	Position    int            `json:"position"`
	ScheduledAt *time.Time     `json:"scheduled_at,omitempty"`
	State       string         `json:"state"`
	Attempts    int            `json:"attempts"`
	LastError   string         `json:"last_error,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// PromptQueue is the queue view for one run.
type PromptQueue struct {
	Items  []PromptQueueItem `json:"items"`
	Paused bool              `json:"paused"`
}

// Usage is token and cost metadata reported by an agent runtime. Empty values
// mean the runtime did not expose that measurement; the hub never invents
// prices or estimates tokens.
type Usage struct {
	InputTokens      int64     `json:"input_tokens"`
	OutputTokens     int64     `json:"output_tokens"`
	CacheReadTokens  int64     `json:"cache_read_tokens"`
	CacheWriteTokens int64     `json:"cache_write_tokens"`
	CostUSD          *float64  `json:"cost_usd,omitempty"`
	Source           string    `json:"source,omitempty"` // agent, estimated
	UpdatedAt        time.Time `json:"updated_at,omitempty"`
}

// ModelOption is one model advertised by a native agent runtime.
type ModelOption struct {
	ID             string   `json:"id"`
	Provider       string   `json:"provider,omitempty"`
	Name           string   `json:"name"`
	Variants       []string `json:"variants,omitempty"`
	DefaultVariant string   `json:"default_variant,omitempty"`
	Inputs         []string `json:"inputs,omitempty"`
}

// AgentModeOption is a selectable agent/collaboration mode.
type AgentModeOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// RunOptions are the real capabilities and current settings of a live run.
type RunOptions struct {
	Models          []ModelOption     `json:"models"`
	Modes           []AgentModeOption `json:"modes"`
	CurrentModel    string            `json:"current_model,omitempty"`
	CurrentProvider string            `json:"current_provider,omitempty"`
	CurrentVariant  string            `json:"current_variant,omitempty"`
	CurrentMode     string            `json:"current_mode,omitempty"`
	SupportsFiles   bool              `json:"supports_files"`
}

// Message is one entry of a normalized transcript.
type Message struct {
	Role       string    `json:"role"`
	Kind       string    `json:"kind,omitempty"` // text, tool_call, tool_result
	Text       string    `json:"text,omitempty"`
	ToolName   string    `json:"tool_name,omitempty"`
	ToolInput  string    `json:"tool_input,omitempty"`
	ToolOutput string    `json:"tool_output,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// Transcript is a page of normalized messages.
type Transcript struct {
	Messages []Message `json:"messages"`
	Next     int       `json:"next,omitempty"`
	Total    int       `json:"total"`
	Warning  string    `json:"warning,omitempty"`
}

// SearchHit is one federated search result from a host.
type SearchHit struct {
	SessionID string    `json:"session_id,omitempty"`
	HostID    string    `json:"host_id,omitempty"`
	HostName  string    `json:"host_name,omitempty"`
	NativeID  string    `json:"native_id"`
	Agent     string    `json:"agent"`
	Title     string    `json:"title"`
	Snippet   string    `json:"snippet"`
	At        time.Time `json:"at"`
}

// RunSpec is sent from the hub to the host to start a run.
type RunSpec struct {
	RunID     string `json:"run_id"`
	SessionID string `json:"session_id,omitempty"`
	Agent     string `json:"agent"`
	NativeID  string `json:"native_id,omitempty"`
	Cwd       string `json:"cwd"`
	Action    string `json:"action"`
	Mode      string `json:"mode"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
	// Force resumes a session that a best-effort process scan reports as open
	// in a native terminal. The hub only sets it after an explicit,
	// step-up-confirmed user decision; the host still enforces run uniqueness.
	Force bool `json:"force,omitempty"`
}

// RunEvent is a normalized structured-mode event. Terminal runs do not use it.
type RunEvent struct {
	Type       string   `json:"type"`
	RunID      string   `json:"run_id,omitempty"`
	Seq        uint64   `json:"seq,omitempty"`
	MessageID  string   `json:"message_id,omitempty"`
	CallID     string   `json:"call_id,omitempty"`
	ApprovalID string   `json:"approval_id,omitempty"`
	Role       string   `json:"role,omitempty"`
	Text       string   `json:"text,omitempty"`
	ToolName   string   `json:"tool_name,omitempty"`
	ToolInput  string   `json:"tool_input,omitempty"`
	ToolOutput string   `json:"tool_output,omitempty"`
	Action     string   `json:"action,omitempty"`
	Resources  []string `json:"resources,omitempty"`
	Decision   string   `json:"decision,omitempty"`
	State      string   `json:"state,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// RunInfo is reported by a host after reconnect so the hub can adopt runs
// that survived a hub restart.
type RunInfo struct {
	RunID     string    `json:"run_id"`
	SessionID string    `json:"session_id,omitempty"`
	Agent     string    `json:"agent"`
	Mode      string    `json:"mode,omitempty"`
	State     string    `json:"state"`
	StartedAt time.Time `json:"started_at"`
}

// Envelope is the message format of the host link.
type Envelope struct {
	Type      string         `json:"type"`
	Protocol  string         `json:"protocol,omitempty"`
	HostID    string         `json:"host_id,omitempty"`
	HostName  string         `json:"host_name,omitempty"`
	OS        string         `json:"os,omitempty"`
	Arch      string         `json:"arch,omitempty"`
	Version   string         `json:"version,omitempty"`
	Nonce     string         `json:"nonce,omitempty"`
	IssuedAt  int64          `json:"issued_at,omitempty"`
	Signature string         `json:"signature,omitempty"`
	Agents    []AgentInfo    `json:"agents,omitempty"`
	Policy    *PolicySummary `json:"policy,omitempty"`
	Sessions  []Session      `json:"sessions,omitempty"`
	Removed   []SessionKey   `json:"removed,omitempty"`
	Full      bool           `json:"full,omitempty"`
	Features  []string       `json:"features,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	// TraceID carries the hub HTTP request id across the host link. It is
	// optional and never used as a metric label.
	TraceID string    `json:"trace_id,omitempty"`
	Method  string    `json:"method,omitempty"`
	Params  []byte    `json:"params,omitempty"`
	Result  []byte    `json:"result,omitempty"`
	OK      bool      `json:"ok,omitempty"`
	Error   string    `json:"error,omitempty"`
	Run     *RunSpec  `json:"run,omitempty"`
	Runs    []RunInfo `json:"runs,omitempty"`
	// NativeID reports the native session a structured fork created. The hub
	// adopts it as a new session and does not modify the source session.
	NativeID   string         `json:"native_id,omitempty"`
	Update     *UpdateStatus  `json:"update,omitempty"`
	Index      *IndexStatus   `json:"index,omitempty"`
	Event      *RunEvent      `json:"event,omitempty"`
	Usage      *Usage         `json:"usage,omitempty"`
	RunID      string         `json:"run_id,omitempty"`
	Seq        uint64         `json:"seq,omitempty"`
	Data       []byte         `json:"data,omitempty"`
	Cols       int            `json:"cols,omitempty"`
	Rows       int            `json:"rows,omitempty"`
	Code       int            `json:"code,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	Message    string         `json:"message,omitempty"`
	ApprovalID string         `json:"approval_id,omitempty"`
	Decision   string         `json:"decision,omitempty"`
	Options    *PromptOptions `json:"options,omitempty"`
}

// ClientEnvelope is sent by the browser over the client WebSocket.
type ClientEnvelope struct {
	Type       string         `json:"type"`
	RunID      string         `json:"run_id,omitempty"`
	LastSeq    uint64         `json:"last_seq,omitempty"`
	Data       []byte         `json:"data,omitempty"`
	Cols       int            `json:"cols,omitempty"`
	Rows       int            `json:"rows,omitempty"`
	Text       string         `json:"text,omitempty"`
	ApprovalID string         `json:"approval_id,omitempty"`
	Decision   string         `json:"decision,omitempty"`
	Options    *PromptOptions `json:"options,omitempty"`
}

// ServerEnvelope is sent to the browser over the client WebSocket.
type ServerEnvelope struct {
	Type     string        `json:"type"`
	Epoch    string        `json:"epoch,omitempty"`
	Host     *HostView     `json:"host,omitempty"`
	Hosts    []HostView    `json:"hosts,omitempty"`
	Session  *SessionView  `json:"session,omitempty"`
	Sessions []SessionView `json:"sessions,omitempty"`
	Run      *RunView      `json:"run,omitempty"`
	Event    *RunEvent     `json:"event,omitempty"`
	Usage    *Usage        `json:"usage,omitempty"`
	RunID    string        `json:"run_id,omitempty"`
	Seq      uint64        `json:"seq,omitempty"`
	State    string        `json:"state,omitempty"`
	Data     []byte        `json:"data,omitempty"`
	Error    string        `json:"error,omitempty"`
	Message  string        `json:"message,omitempty"`
}

// HostView is the hub-side projection of a paired host.
type HostView struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	OS       string         `json:"os"`
	Arch     string         `json:"arch"`
	Version  string         `json:"version"`
	Status   string         `json:"status"`
	LastSeen time.Time      `json:"last_seen"`
	Update   *UpdateStatus  `json:"update,omitempty"`
	Agents   []AgentInfo    `json:"agents"`
	Policy   *PolicySummary `json:"policy,omitempty"`
	Index    *IndexStatus   `json:"index,omitempty"`
	Pending  bool           `json:"pending,omitempty"`
	SAS      string         `json:"sas,omitempty"`
	Disabled bool           `json:"disabled,omitempty"`
}

// IndexStatus is the host-local full-text index progress. It never contains
// transcript content.
type IndexStatus struct {
	State   string `json:"state"` // disabled, building, ready, error
	Indexed int    `json:"indexed"`
	Total   int    `json:"total"`
	Bytes   int64  `json:"bytes,omitempty"`
	Error   string `json:"error,omitempty"`
}

// SessionView is the hub-side projection of a native session.
type SessionView struct {
	ID              string    `json:"id"`
	HostID          string    `json:"host_id"`
	HostName        string    `json:"host_name"`
	Agent           string    `json:"agent"`
	NativeID        string    `json:"native_id"`
	Title           string    `json:"title"`
	Cwd             string    `json:"cwd"`
	GitBranch       string    `json:"git_branch,omitempty"`
	RepoRoot        string    `json:"repo_root,omitempty"`
	RepoURL         string    `json:"repo_url,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	MessageCount    int       `json:"message_count"`
	State           string    `json:"state"`
	ActiveElsewhere bool      `json:"active_elsewhere"`
	Warning         string    `json:"warning,omitempty"`
	// LocalTitle and Tags are per-user hub metadata. They never modify the
	// agent's native title or transcript.
	LocalTitle string   `json:"local_title,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

// RunView is the hub-side projection of a run.
type RunView struct {
	ID            string     `json:"id"`
	SessionID     string     `json:"session_id,omitempty"`
	HostID        string     `json:"host_id"`
	HostName      string     `json:"host_name,omitempty"`
	SessionTitle  string     `json:"session_title,omitempty"`
	Agent         string     `json:"agent,omitempty"`
	Cwd           string     `json:"cwd,omitempty"`
	GitBranch     string     `json:"git_branch,omitempty"`
	Mode          string     `json:"mode"`
	State         string     `json:"state"`
	LeaseDeviceID string     `json:"lease_device_id,omitempty"`
	LastEventSeq  uint64     `json:"last_event_seq,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
}

// ProjectView groups native sessions that share a working directory on one
// host. The hub never stores file contents; this is metadata only.
type ProjectView struct {
	HostID   string    `json:"host_id"`
	HostName string    `json:"host_name"`
	Cwd      string    `json:"cwd"`
	Name     string    `json:"name"`
	Branch   string    `json:"git_branch,omitempty"`
	RepoRoot string    `json:"repo_root,omitempty"`
	RepoURL  string    `json:"repo_url,omitempty"`
	Sessions int       `json:"sessions"`
	Active   int       `json:"active"`
	Agents   []string  `json:"agents"`
	LastAt   time.Time `json:"last_at"`
}

// Notification is a content-free in-app event for the run owner.
type Notification struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	RunID     string    `json:"run_id,omitempty"`
	Link      string    `json:"link,omitempty"`
	HostID    string    `json:"host_id,omitempty"`
	HostName  string    `json:"host_name,omitempty"`
	Agent     string    `json:"agent,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Read      bool      `json:"read"`
}
