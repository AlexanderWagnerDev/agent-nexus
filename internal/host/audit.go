package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var hostAuditMu sync.Mutex

type hostAuditEntry struct {
	At      time.Time      `json:"at"`
	Action  string         `json:"action"`
	Details map[string]any `json:"details,omitempty"`
}

// hostAudit appends a local, append-only JSONL record. It never contains
// transcript content or agent credentials.
func hostAudit(action string, details map[string]any) {
	dir, err := ConfigDir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	entry := hostAuditEntry{At: time.Now().UTC(), Action: action, Details: details}
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	raw = append(raw, '\n')
	hostAuditMu.Lock()
	defer hostAuditMu.Unlock()
	f, err := os.OpenFile(filepath.Join(dir, "audit.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(raw)
	_ = f.Close()
}
