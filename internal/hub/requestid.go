package hub

import (
	"context"
	"strings"
)

// validRequestID accepts a bounded, log-safe correlation id from a client.
// Anything else is replaced so a caller cannot inject whitespace, control
// characters or unbounded values into logs.
func validRequestID(value string) bool {
	if len(value) < 8 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == ':':
		default:
			return false
		}
	}
	return true
}

func requestIDFrom(ctx context.Context) string {
	if value, ok := ctx.Value(ctxRequestID).(string); ok {
		return value
	}
	return ""
}

func sanitizeRequestID(value string) string {
	value = strings.TrimSpace(value)
	if validRequestID(value) {
		return value
	}
	return randomToken(12)
}
