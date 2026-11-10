// Package protocol contains the wire types shared by the hub and the host
// daemon. The host link is a single WebSocket carrying JSON envelopes. The
// same types are used by the browser client where noted.
package protocol

import (
	"time"
)

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
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	MessageCount    int       `json:"message_count"`
	State           string    `json:"state"`
	ActiveElsewhere bool      `json:"active_elsewhere"`
	Warning         string    `json:"warning,omitempty"`
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
	NativeID string    `json:"native_id"`
	Agent    string    `json:"agent"`
	Title    string    `json:"title"`
	Snippet  string    `json:"snippet"`
	At       time.Time `json:"at"`
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
}

// RunInfo is reported by a host after reconnect so the hub can adopt runs
// that survived a hub restart.
type RunInfo struct {
	RunID     string    `json:"run_id"`
	SessionID string    `json:"session_id,omitempty"`
	Agent     string    `json:"agent"`
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
	Signature string         `json:"signature,omitempty"`
	Agents    []AgentInfo    `json:"agents,omitempty"`
	Policy    *PolicySummary `json:"policy,omitempty"`
	Sessions  []Session      `json:"sessions,omitempty"`
	Full      bool           `json:"full,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	Method    string         `json:"method,omitempty"`
	Params    []byte         `json:"params,omitempty"`
	Result    []byte         `json:"result,omitempty"`
	OK        bool           `json:"ok,omitempty"`
	Error     string         `json:"error,omitempty"`
	Run       *RunSpec       `json:"run,omitempty"`
	Runs      []RunInfo      `json:"runs,omitempty"`
	RunID     string         `json:"run_id,omitempty"`
	Seq       uint64         `json:"seq,omitempty"`
	Data      []byte         `json:"data,omitempty"`
	Cols      int            `json:"cols,omitempty"`
	Rows      int            `json:"rows,omitempty"`
	Code      int            `json:"code,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	Message   string         `json:"message,omitempty"`
}

// ClientEnvelope is sent by the browser over the client WebSocket.
type ClientEnvelope struct {
	Type    string `json:"type"`
	RunID   string `json:"run_id,omitempty"`
	LastSeq uint64 `json:"last_seq,omitempty"`
	Data    []byte `json:"data,omitempty"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
}

// ServerEnvelope is sent to the browser over the client WebSocket.
type ServerEnvelope struct {
	Type     string        `json:"type"`
	Host     *HostView     `json:"host,omitempty"`
	Hosts    []HostView    `json:"hosts,omitempty"`
	Session  *SessionView  `json:"session,omitempty"`
	Sessions []SessionView `json:"sessions,omitempty"`
	Run      *RunView      `json:"run,omitempty"`
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
	Agents   []AgentInfo    `json:"agents"`
	Policy   *PolicySummary `json:"policy,omitempty"`
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
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	MessageCount    int       `json:"message_count"`
	State           string    `json:"state"`
	ActiveElsewhere bool      `json:"active_elsewhere"`
	Warning         string    `json:"warning,omitempty"`
}

// RunView is the hub-side projection of a run.
type RunView struct {
	ID            string     `json:"id"`
	SessionID     string     `json:"session_id,omitempty"`
	HostID        string     `json:"host_id"`
	Mode          string     `json:"mode"`
	State         string     `json:"state"`
	LeaseDeviceID string     `json:"-"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
}
