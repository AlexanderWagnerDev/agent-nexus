package host

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// A host must not advertise sessions it will refuse to read or resume. The
// hub showed OpenCode sessions from outside allowed_roots and every transcript
// request failed with "session is outside the allowed roots".
func TestListSessionsHidesSessionsOutsideAllowedRoots(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "repo")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), "private")
	adapter := &recordAdapter{
		name: "opencode",
		sessions: []protocol.Session{
			{NativeID: "inside", Agent: "opencode", Cwd: inside},
			{NativeID: "outside", Agent: "opencode", Cwd: outside},
			{NativeID: "unknown-cwd", Agent: "opencode"},
		},
	}
	d := NewDaemonWithAdapters(
		Config{Policy: Policy{AllowedRoots: []string{root}}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		[]Adapter{adapter},
	)
	sessions, err := d.listSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range sessions {
		ids = append(ids, s.NativeID)
	}
	if len(ids) != 1 || ids[0] != "inside" {
		t.Fatalf("advertised sessions = %v, want [inside]", ids)
	}
	// The raw snapshot stays known so a stale request still gets the explicit
	// policy error instead of "session metadata is not available yet".
	if cwd, known := d.sessionCwd("opencode", "outside"); !known || cwd != outside {
		t.Fatalf("cached cwd for the hidden session = %q, known=%v", cwd, known)
	}
}
