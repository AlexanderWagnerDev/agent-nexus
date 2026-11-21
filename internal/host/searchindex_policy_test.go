package host

import (
	"path/filepath"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// A ready FTS index can retain documents from before a policy change or from
// an adapter whose store failed during refresh. Query-time hits must still be
// filtered by the live roots and agent policy.
func TestSearchIndexHitsRespectLivePolicy(t *testing.T) {
	idx, err := OpenSearchIndex(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if _, err := idx.db.Exec(`INSERT INTO documents(id,agent,native_id,title,cwd,updated_at,fingerprint)
		VALUES(1,'claude','hidden-session','Hidden','/outside',1,'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.db.Exec(`INSERT INTO documents_fts(rowid,body,agent,native_id)
		VALUES(1,'needle in a hidden transcript','claude','hidden-session')`); err != nil {
		t.Fatal(err)
	}
	idx.setStatus(1, 1, "ready", "")
	root := t.TempDir()
	d := &Daemon{
		index:  idx,
		policy: Policy{AllowedRoots: []string{root}},
		lastByAgent: map[string][]protocol.Session{
			"claude": {{NativeID: "hidden-session", Agent: "claude", Cwd: "/outside"}},
		},
	}
	result, err := d.dispatchRequest("search", []byte(`{"query":"needle","limit":10}`))
	if err != nil {
		t.Fatal(err)
	}
	hits, ok := result.([]protocol.SearchHit)
	if !ok {
		t.Fatalf("unexpected search result: %T", result)
	}
	if len(hits) != 0 {
		t.Fatalf("out-of-policy index hit was returned: %+v", hits)
	}
}
