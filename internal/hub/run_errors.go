package hub

import "strings"

// Run error codes are stable, machine-readable categories. They describe what
// the hub observed and intentionally do not attempt to replace the agent's own
// message.
const (
	ErrCodeNetwork          = "network"
	ErrCodeAuthentication   = "authentication"
	ErrCodeAuthorization    = "authorization"
	ErrCodeAgentUnavailable = "agent_unavailable"
	ErrCodeUnsupported      = "unsupported_version"
	ErrCodeSessionConflict  = "session_conflict"
	ErrCodeHostPolicy       = "host_policy"
	ErrCodeAgentProtocol    = "agent_protocol"
	ErrCodeProcessCrash     = "process_crash"
	ErrCodeTimeout          = "timeout"
	ErrCodeDatabase         = "database"
	ErrCodeUpdate           = "update"
	ErrCodeInternal         = "internal"
)

// ClassifyRunError maps an observed error message to one stable code. The
// classification is deliberately keyword-based and only runs against text the
// host or agent already produced.
func ClassifyRunError(message string) string {
	m := strings.ToLower(message)
	switch {
	case containsAny(m, "timed out", "timeout", "deadline exceeded"):
		return ErrCodeTimeout
	case containsAny(m, "unauthorized", "401", "authentication", "token expired", "invalid token"):
		return ErrCodeAuthentication
	case containsAny(m, "forbidden", "403", "permission denied", "permission required", "not authorized"):
		return ErrCodeAuthorization
	case containsAny(m, "already active", "active elsewhere", "session is already active", "session conflict"):
		return ErrCodeSessionConflict
	case containsAny(m, "outside the allowed roots", "host policy", "policy rejected", "not allowed", "forbidden args"):
		return ErrCodeHostPolicy
	case containsAny(m, "unsupported", "untested", "outside the tested range", "version mismatch"):
		return ErrCodeUnsupported
	case containsAny(m, "executable not found", "agent not found", "not installed", "no such file", "cannot find"):
		return ErrCodeAgentUnavailable
	case containsAny(m, "protocol", "invalid host response", "json-rpc", "unexpected event", "malformed"):
		return ErrCodeAgentProtocol
	case containsAny(m, "exit status", "process exited", "process crashed", "killed", "crash"):
		return ErrCodeProcessCrash
	case containsAny(m, "database", "sqlite", "sql:"):
		return ErrCodeDatabase
	case containsAny(m, "update"):
		return ErrCodeUpdate
	case containsAny(m, "connection", "websocket", "network", "dial tcp", "eof", "broken pipe", "reset by peer"):
		return ErrCodeNetwork
	default:
		return ErrCodeInternal
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
