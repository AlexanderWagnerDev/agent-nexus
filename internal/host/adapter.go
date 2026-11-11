package host

import (
	"context"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// Action describes a terminal-mode run request.
type Action struct {
	Kind     string // resume, new, fork
	NativeID string
	Cwd      string
	Agent    string
}

// Command is a resolved process launch, including a Windows shim wrapper if
// the agent is installed as .cmd/.bat/.ps1.
type Command struct {
	Path string
	Args []string
	Cwd  string
	Env  []string
}

// Adapter is implemented per coding agent. Discovery must work without
// starting the agent; Control only produces argv and never executes anything.
type Adapter interface {
	Name() string
	Detect(context.Context) (protocol.AgentInfo, error)
	ListSessions(context.Context) ([]protocol.Session, error)
	Transcript(ctx context.Context, nativeID string, offset, limit int) (protocol.Transcript, error)
	Search(ctx context.Context, query string, limit int) ([]protocol.SearchHit, error)
	BuildCommand(Action) (Command, error)
}
