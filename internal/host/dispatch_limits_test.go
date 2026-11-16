package host

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// recordAdapter is a stub for the request dispatcher: it records the limits it
// is handed and returns canned transcript data.
type recordAdapter struct {
	name            string
	transcript      protocol.Transcript
	searchHits      []protocol.SearchHit
	searchLimit     int
	transcriptCalls int
	sessions        []protocol.Session
}

func (a *recordAdapter) Name() string { return a.name }

func (a *recordAdapter) Detect(context.Context) (protocol.AgentInfo, error) {
	return protocol.AgentInfo{Name: a.name, Supported: true}, nil
}

func (a *recordAdapter) ListSessions(context.Context) ([]protocol.Session, error) {
	return a.sessions, nil
}

func (a *recordAdapter) Transcript(context.Context, string, int, int) (protocol.Transcript, error) {
	a.transcriptCalls++
	return a.transcript, nil
}

func (a *recordAdapter) Search(_ context.Context, _ string, limit int) ([]protocol.SearchHit, error) {
	a.searchLimit = limit
	return a.searchHits, nil
}

func (a *recordAdapter) BuildCommand(Action) (Command, error) { return Command{}, nil }

func (a *recordAdapter) WatchPaths() []string { return nil }

// An explicitly empty allowed_roots list is a deny-all policy
// (docs/SECURITY.md). The transcript request used to skip the check whenever
// the list was empty, which turned deny-all into allow-all.
func TestTranscriptRespectsDenyAllAllowedRoots(t *testing.T) {
	root := t.TempDir()
	adapter := &recordAdapter{
		name:       "claude",
		transcript: protocol.Transcript{Messages: []protocol.Message{{Role: "user", Text: "secret"}}},
	}
	d := &Daemon{
		policy:   Policy{AllowedRoots: []string{}},
		adapters: []Adapter{adapter},
		lastByAgent: map[string][]protocol.Session{
			"claude": {{NativeID: "s1", Agent: "claude", Cwd: root}},
		},
	}
	params, err := json.Marshal(map[string]any{"native_id": "s1", "agent": "claude", "offset": 0, "limit": 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.dispatchRequest("transcript", params); err == nil {
		t.Fatal("transcript read must be refused when allowed_roots is an explicit empty list")
	}
	if adapter.transcriptCalls != 0 {
		t.Fatal("the adapter must not be reached for a denied transcript read")
	}

	d.policy.AllowedRoots = []string{root}
	if _, err := d.dispatchRequest("transcript", params); err != nil {
		t.Fatalf("a session inside the allowed root must stay readable: %v", err)
	}
	if adapter.transcriptCalls != 1 {
		t.Fatalf("adapter calls = %d, want 1", adapter.transcriptCalls)
	}
}

// The adapter fallback is the only search path that used to trust the
// hub-supplied limit. It must apply the same bound as the local index.
func TestSearchLimitIsClampedForAdapterFallback(t *testing.T) {
	adapter := &recordAdapter{name: "claude"}
	d := &Daemon{adapters: []Adapter{adapter}}
	params, err := json.Marshal(map[string]any{"query": "needle", "limit": 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.dispatchRequest("search", params); err != nil {
		t.Fatal(err)
	}
	if adapter.searchLimit != 100 {
		t.Fatalf("adapter received limit %d, want 100", adapter.searchLimit)
	}

	params, _ = json.Marshal(map[string]any{"query": "needle", "limit": 0})
	if _, err := d.dispatchRequest("search", params); err != nil {
		t.Fatal(err)
	}
	if adapter.searchLimit != 50 {
		t.Fatalf("adapter received limit %d for an unset limit, want 50", adapter.searchLimit)
	}
}

// Discovery lowercases the process list once; matching must stay
// case-insensitive and must ignore sessions without a native id.
func TestMatchActiveSessionsStaysCaseInsensitive(t *testing.T) {
	lines := []string{
		`C:\Program Files\Codex\Codex.EXE resume ABC-123`,
		`/usr/bin/node /usr/lib/opencode --session ses_2`,
	}
	sessions := []protocol.Session{
		{Agent: "codex", NativeID: "abc-123"},
		{Agent: "opencode", NativeID: "ses_2"},
		{Agent: "codex", NativeID: "not-running"},
		{Agent: "claude"},
	}
	active := matchActiveSessions(lines, sessions)
	if !active["codex:abc-123"] {
		t.Fatal("codex session was not matched case-insensitively")
	}
	if !active["opencode:ses_2"] {
		t.Fatal("opencode session was not matched")
	}
	if active["codex:not-running"] {
		t.Fatal("a session without a matching process was reported active")
	}
	if len(active) != 2 {
		t.Fatalf("active set = %v, want exactly two matches", active)
	}
}
