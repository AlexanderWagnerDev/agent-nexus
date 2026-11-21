package host

import (
	"path/filepath"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// A ready FTS index can retain documents from before a policy change or from
// an adapter whose store failed during refresh. Query-time hits must still be
// filtered by the live roots and agent policy, and denied hits must not crowd
// allowed matches out of the requested limit.
func TestSearchIndexHitsRespectLivePolicy(t *testing.T) {
	idx, err := OpenSearchIndex(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	for i := 1; i <= 5; i++ {
		if _, err := idx.db.Exec(`INSERT INTO documents(id,agent,native_id,title,cwd,updated_at,fingerprint)
			VALUES(?,'claude',?,'Hidden','/outside',3,'x')`, i, "hidden-"+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
		if _, err := idx.db.Exec(`INSERT INTO documents_fts(rowid,body,agent,native_id)
			VALUES(?,'needle in a hidden transcript','claude',?)`, i, "hidden-"+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := idx.db.Exec(`INSERT INTO documents(id,agent,native_id,title,cwd,updated_at,fingerprint)
		VALUES(100,'claude','allowed-session','Allowed','/root/repo',2,'y')`); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.db.Exec(`INSERT INTO documents_fts(rowid,body,agent,native_id)
		VALUES(100,'needle in an allowed transcript','claude','allowed-session')`); err != nil {
		t.Fatal(err)
	}
	idx.setStatus(6, 6, "ready", "")
	root := t.TempDir()
	d := &Daemon{
		index:  idx,
		policy: Policy{AllowedRoots: []string{root}},
		lastByAgent: map[string][]protocol.Session{
			"claude": {
				{NativeID: "hidden-a", Agent: "claude", Cwd: "/outside"},
				{NativeID: "hidden-b", Agent: "claude", Cwd: "/outside"},
				{NativeID: "hidden-c", Agent: "claude", Cwd: "/outside"},
				{NativeID: "hidden-d", Agent: "claude", Cwd: "/outside"},
				{NativeID: "hidden-e", Agent: "claude", Cwd: "/outside"},
				{NativeID: "allowed-session", Agent: "claude", Cwd: root},
			},
		},
	}
	result, err := d.dispatchRequest("search", []byte(`{"query":"needle","limit":1}`))
	if err != nil {
		t.Fatal(err)
	}
	hits, ok := result.([]protocol.SearchHit)
	if !ok {
		t.Fatalf("unexpected search result: %T", result)
	}
	if len(hits) != 1 || hits[0].NativeID != "allowed-session" {
		t.Fatalf("allowed hit was crowded out or leaked: %+v", hits)
	}
}
