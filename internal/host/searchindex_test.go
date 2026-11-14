package host

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

type indexTestAdapter struct {
	sessions    []protocol.Session
	transcripts map[string]protocol.Transcript
	failList    bool
}

func (a *indexTestAdapter) Name() string { return "opencode" }
func (a *indexTestAdapter) Detect(context.Context) (protocol.AgentInfo, error) {
	return protocol.AgentInfo{Name: "opencode", Supported: true}, nil
}
func (a *indexTestAdapter) ListSessions(context.Context) ([]protocol.Session, error) {
	if a.failList {
		return nil, fmt.Errorf("store is temporarily locked")
	}
	return a.sessions, nil
}
func (a *indexTestAdapter) Transcript(_ context.Context, nativeID string, _, _ int) (protocol.Transcript, error) {
	tr, ok := a.transcripts[nativeID]
	if !ok {
		return protocol.Transcript{}, fmt.Errorf("missing transcript")
	}
	return tr, nil
}
func (a *indexTestAdapter) Search(context.Context, string, int) ([]protocol.SearchHit, error) {
	return []protocol.SearchHit{{NativeID: "fallback", Agent: "opencode", Title: "Adapter fallback"}}, nil
}
func (a *indexTestAdapter) BuildCommand(Action) (Command, error) { return Command{}, nil }

func newIndexFixture(t *testing.T) (*SearchIndex, *indexTestAdapter, string) {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	index, err := OpenSearchIndex(filepath.Join(t.TempDir(), "search-index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { index.Close() })
	updated := time.Now().UTC()
	adapter := &indexTestAdapter{
		sessions: []protocol.Session{
			{Agent: "opencode", NativeID: "inside", Title: "Fix the upload retry", Cwd: project, UpdatedAt: updated, MessageCount: 2},
			{Agent: "opencode", NativeID: "outside", Title: "Secret work", Cwd: filepath.Join(t.TempDir(), "elsewhere"), UpdatedAt: updated, MessageCount: 1},
		},
		transcripts: map[string]protocol.Transcript{
			"inside": {Total: 2, Messages: []protocol.Message{
				{Role: "user", Text: "The upload retry loop never stops", Timestamp: updated},
				{Role: "assistant", Kind: "tool_call", ToolName: "shell", ToolInput: `{"command":"go test ./..."}`, Text: "All good", Timestamp: updated},
			}},
			"outside": {Total: 1, Messages: []protocol.Message{{Role: "user", Text: "secret transcript", Timestamp: updated}}},
		},
	}
	return index, adapter, root
}

func TestSearchIndexBuildSearchAndIncrementalRefresh(t *testing.T) {
	index, adapter, root := newIndexFixture(t)
	policy := Policy{AllowedRoots: []string{root}, AllowedAgents: []string{"opencode"}}
	if err := index.Refresh(context.Background(), []Adapter{adapter}, policy, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	status := index.Status()
	if status.State != "ready" || status.Total != 1 || status.Indexed != 1 {
		t.Fatalf("status: %+v", status)
	}
	count, err := index.Count()
	if err != nil || count != 1 {
		t.Fatalf("count: %d %v", count, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(index.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("index permissions are too broad: %o", info.Mode().Perm())
		}
	}

	hits, err := index.Search(context.Background(), "retry", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].NativeID != "inside" || !strings.Contains(hits[0].Snippet, snippetMarker) {
		t.Fatalf("hits: %+v", hits)
	}
	if hits, err := index.Search(context.Background(), "go test", 10); err != nil || len(hits) != 1 {
		t.Fatalf("tool input search: %+v %v", hits, err)
	}
	if hits, err := index.Search(context.Background(), `retry" OR secret`, 10); err != nil {
		t.Fatalf("malicious query: %v", err)
	} else {
		for _, hit := range hits {
			if hit.NativeID != "inside" {
				t.Fatalf("query was not constrained: %+v", hits)
			}
		}
	}
	if hits, err := index.Search(context.Background(), "   ", 10); err != nil || len(hits) != 0 {
		t.Fatalf("empty query: %+v %v", hits, err)
	}

	// Incremental refresh reindexes only the changed session.
	adapter.sessions[0].UpdatedAt = adapter.sessions[0].UpdatedAt.Add(time.Minute)
	adapter.transcripts["inside"] = protocol.Transcript{Total: 1, Messages: []protocol.Message{
		{Role: "user", Text: "The retry cap is now fixed", Timestamp: adapter.sessions[0].UpdatedAt},
	}}
	if err := index.Refresh(context.Background(), []Adapter{adapter}, policy, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	hits, err = index.Search(context.Background(), "fixed", 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("refreshed hit: %+v %v", hits, err)
	}
	if hits, _ := index.Search(context.Background(), "never stops", 10); len(hits) != 0 {
		t.Fatalf("stale text remained: %+v", hits)
	}
}

func TestSearchIndexPolicyAndDisabled(t *testing.T) {
	index, adapter, root := newIndexFixture(t)
	policy := Policy{AllowedRoots: []string{root}, AllowedAgents: []string{"codex"}}
	if err := index.Refresh(context.Background(), []Adapter{adapter}, policy, nil); err != nil {
		t.Fatal(err)
	}
	if count, _ := index.Count(); count != 0 {
		t.Fatalf("disallowed agent was indexed: %d", count)
	}
	if (Config{}).SearchIndexEnabled() != true {
		t.Fatal("the index should be enabled by default")
	}
	off := false
	if (Config{SearchIndex: &off}).SearchIndexEnabled() {
		t.Fatal("search_index=false was ignored")
	}
	if got := ftsExpression(`"quoted" -- then`); !strings.Contains(got, `"quoted"*`) {
		t.Fatalf("expression: %q", got)
	}
}

func TestSearchIndexRebuildClearsStaleDocuments(t *testing.T) {
	index, adapter, root := newIndexFixture(t)
	policy := Policy{AllowedRoots: []string{root}, AllowedAgents: []string{"opencode"}}
	if err := index.Rebuild(context.Background(), []Adapter{adapter}, policy, nil); err != nil {
		t.Fatal(err)
	}
	if count, _ := index.Count(); count != 1 {
		t.Fatalf("count after rebuild: %d", count)
	}
	adapter.sessions = nil
	if err := index.Rebuild(context.Background(), []Adapter{adapter}, policy, nil); err != nil {
		t.Fatal(err)
	}
	if count, _ := index.Count(); count != 0 {
		t.Fatalf("stale documents remained: %d", count)
	}
}

func TestSearchIndexRefreshRemovesDeletedSessions(t *testing.T) {
	index, adapter, root := newIndexFixture(t)
	policy := Policy{AllowedRoots: []string{root}, AllowedAgents: []string{"opencode"}}
	if err := index.Refresh(context.Background(), []Adapter{adapter}, policy, nil); err != nil {
		t.Fatal(err)
	}
	if count, _ := index.Count(); count != 1 {
		t.Fatalf("initial count: %d", count)
	}
	adapter.sessions = nil
	if err := index.Refresh(context.Background(), []Adapter{adapter}, policy, nil); err != nil {
		t.Fatal(err)
	}
	if count, _ := index.Count(); count != 0 {
		t.Fatalf("deleted session stayed indexed: %d", count)
	}
	if hits, err := index.Search(context.Background(), "retry", 10); err != nil || len(hits) != 0 {
		t.Fatalf("deleted transcript remained searchable: %+v %v", hits, err)
	}
}

func TestSearchIndexRefreshKeepsDocumentsWhenStoreIsUnreadable(t *testing.T) {
	index, adapter, root := newIndexFixture(t)
	policy := Policy{AllowedRoots: []string{root}, AllowedAgents: []string{"opencode"}}
	if err := index.Refresh(context.Background(), []Adapter{adapter}, policy, nil); err != nil {
		t.Fatal(err)
	}
	adapter.failList = true
	if err := index.Refresh(context.Background(), []Adapter{adapter}, policy, nil); err != nil {
		t.Fatal(err)
	}
	if count, _ := index.Count(); count != 1 {
		t.Fatalf("a locked store emptied the index: %d", count)
	}
}

func TestDispatchSearchUsesReadyIndexAndFallsBackWhileBuilding(t *testing.T) {
	index, adapter, root := newIndexFixture(t)
	policy := Policy{AllowedRoots: []string{root}, AllowedAgents: []string{"opencode"}}
	if err := index.Refresh(context.Background(), []Adapter{adapter}, policy, nil); err != nil {
		t.Fatal(err)
	}
	d := NewDaemonWithAdapters(Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), []Adapter{adapter})
	d.policy = policy
	d.index = index
	// The fallback adapter invents a hit for "fallback"; the daemon must have
	// discovered its working directory to apply allowed_roots.
	d.sessionsMu.Lock()
	d.lastByAgent["opencode"] = []protocol.Session{
		{NativeID: "inside", Agent: "opencode", Cwd: filepath.Join(root, "project")},
		{NativeID: "fallback", Agent: "opencode", Cwd: filepath.Join(root, "project")},
	}
	d.sessionsMu.Unlock()
	raw := []byte(`{"query":"upload retry","limit":10}`)
	result, err := d.dispatchRequest("search", raw)
	if err != nil {
		t.Fatal(err)
	}
	hits, ok := result.([]protocol.SearchHit)
	if !ok || len(hits) != 1 || hits[0].NativeID != "inside" {
		t.Fatalf("indexed search: %#v", result)
	}
	index.setStatus(0, 1, "building", "")
	result, err = d.dispatchRequest("search", raw)
	if err != nil {
		t.Fatal(err)
	}
	hits, ok = result.([]protocol.SearchHit)
	if !ok || len(hits) != 1 || hits[0].NativeID != "fallback" {
		t.Fatalf("fallback search: %#v", result)
	}
	if got := d.searchIndexStatus(); got == nil || got.State != "building" {
		t.Fatalf("status: %+v", got)
	}
}
