package protocol

import "time"

// Task lifecycle. Only explicit transitions are accepted by the API; the hub
// never starts a task implicitly.
const (
	TaskDraft           = "draft"
	TaskReady           = "ready"
	TaskQueued          = "queued"
	TaskRunning         = "running"
	TaskWaitingApproval = "waiting_for_approval"
	TaskBlocked         = "blocked"
	TaskCompleted       = "completed"
	TaskFailed          = "failed"
	TaskCancelled       = "cancelled"
)

// Task is one user-owned unit of agent work. It links to native sessions and
// runs by id; the hub stores no transcript content with it.
type Task struct {
	ID          string     `json:"id"`
	UserID      string     `json:"-"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Project     string     `json:"project,omitempty"`
	HostID      string     `json:"host_id,omitempty"`
	HostName    string     `json:"host_name,omitempty"`
	Agent       string     `json:"agent,omitempty"`
	Model       string     `json:"model,omitempty"`
	Mode        string     `json:"mode,omitempty"`
	Priority    string     `json:"priority"`
	Status      string     `json:"status"`
	SessionID   string     `json:"session_id,omitempty"`
	RunID       string     `json:"run_id,omitempty"`
	Result      string     `json:"result,omitempty"`
	Error       string     `json:"error,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	DependsOn   []string   `json:"depends_on,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// TaskRef is a compact dependency reference.
type TaskRef struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}
