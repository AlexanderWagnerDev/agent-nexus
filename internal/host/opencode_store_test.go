package host

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	_ "modernc.org/sqlite"
)

func newOpenCodeStore(t *testing.T, statements ...string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_DB", dbPath)
	return dbPath
}

func TestOpenCodeDiscoveryDoesNotMaskV2FailureWithLegacySuccess(t *testing.T) {
	// session_v2 lost a column, but the empty legacy tables are still
	// readable. A legacy success must not turn the v2 failure into an empty
	// snapshot that removes every session from the hub.
	newOpenCodeStore(t,
		`CREATE TABLE session_v2 (id TEXT PRIMARY KEY, title TEXT, directory TEXT, parent_id TEXT, time_created INTEGER, time_updated INTEGER, time_archived INTEGER)`,
		`CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT, data TEXT)`,
		`CREATE TABLE session (id TEXT PRIMARY KEY, title TEXT, directory TEXT, parent_id TEXT, time_created INTEGER, time_updated INTEGER, time_archived INTEGER)`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT)`,
		`INSERT INTO session_v2(id,title,directory,time_created,time_updated) VALUES('ses_broken','Broken','/tmp/project',1,2)`,
	)
	sessions, err := NewOpenCodeAdapter("opencode").ListSessions(context.Background())
	if err == nil {
		t.Fatalf("legacy success masked a v2 schema failure: %+v", sessions)
	}
}

func TestOpenCodeDiscoveryKeepsV2WhenLegacyScanIsPartial(t *testing.T) {
	// v2 is complete. The legacy table has one valid and one undecodable row;
	// merging the valid one would publish a partial legacy result, so no
	// legacy row may be merged. The v2 session must still be discovered.
	newOpenCodeStore(t,
		`CREATE TABLE session_v2 (id TEXT PRIMARY KEY, title TEXT, directory TEXT, parent_id TEXT, fork_session_id TEXT, time_created INTEGER, time_updated INTEGER, time_archived INTEGER)`,
		`CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT, data TEXT)`,
		`CREATE TABLE session (id TEXT PRIMARY KEY, title TEXT, directory TEXT, parent_id TEXT, time_created INTEGER, time_updated INTEGER, time_archived INTEGER)`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT)`,
		`INSERT INTO session_v2(id,title,directory,time_created,time_updated) VALUES('ses_v2','Fix auth','/tmp/project',1000,2000)`,
		`INSERT INTO session(id,title,directory,time_created,time_updated) VALUES('ses_legacy_ok','Legacy','/tmp/legacy',1000,2000)`,
		`INSERT INTO session(id,title,directory,time_created,time_updated) VALUES('ses_legacy_bad','Bad','/tmp/legacy','not-a-number',2000)`,
	)
	sessions, err := NewOpenCodeAdapter("opencode").ListSessions(context.Background())
	if err != nil {
		t.Fatalf("a complete v2 snapshot must survive a partial legacy read: %v", err)
	}
	if len(sessions) != 1 || sessions[0].NativeID != "ses_v2" {
		t.Fatalf("partial legacy rows were merged: %+v", sessions)
	}
}

func TestOpenCodeDiscoveryKeepsCompleteV2WhenLegacyFails(t *testing.T) {
	// OpenCode 2.x stores can keep an empty legacy session table without the
	// legacy message table. v2 is authoritative and complete, so its sessions
	// must still be discovered.
	newOpenCodeStore(t,
		`CREATE TABLE session_v2 (id TEXT PRIMARY KEY, title TEXT, directory TEXT, parent_id TEXT, fork_session_id TEXT, time_created INTEGER, time_updated INTEGER, time_archived INTEGER)`,
		`CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT, data TEXT)`,
		`CREATE TABLE session (id TEXT PRIMARY KEY, title TEXT, directory TEXT, parent_id TEXT, time_created INTEGER, time_updated INTEGER, time_archived INTEGER)`,
		`INSERT INTO session_v2(id,title,directory,time_created,time_updated) VALUES('ses_v2','Fix auth','/tmp/project',1000,2000)`,
	)
	sessions, err := NewOpenCodeAdapter("opencode").ListSessions(context.Background())
	if err != nil {
		t.Fatalf("a complete v2 snapshot must not fail on the legacy table: %v", err)
	}
	if len(sessions) != 1 || sessions[0].NativeID != "ses_v2" {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}
}

func TestOpenCodeDiscoveryReportsPartialScanFailure(t *testing.T) {
	// SQLite stores a non-numeric time_created as text; scanning it into
	// int64 fails. A partial snapshot would drop the unreadable session from
	// the hub, so the whole read must fail instead.
	newOpenCodeStore(t,
		`CREATE TABLE session_v2 (id TEXT PRIMARY KEY, title TEXT, directory TEXT, parent_id TEXT, fork_session_id TEXT, time_created INTEGER, time_updated INTEGER, time_archived INTEGER)`,
		`CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT, data TEXT)`,
		`INSERT INTO session_v2(id,title,directory,time_created,time_updated) VALUES('ses_ok','Good','/tmp/project',1000,2000)`,
		`INSERT INTO session_v2(id,title,directory,time_created,time_updated) VALUES('ses_bad','Bad','/tmp/project','not-a-number',2000)`,
	)
	sessions, err := NewOpenCodeAdapter("opencode").ListSessions(context.Background())
	if err == nil {
		t.Fatalf("partial scan failure was reported as a successful snapshot: %+v", sessions)
	}
}

func TestOpenCodeDiscoveryReportsUnreadableStore(t *testing.T) {
	// A v2 store whose schema drifted (fork_session_id is missing) used to
	// produce an empty, successful snapshot. The daemon would then replace the
	// last good list with zero sessions and the search index would prune every
	// OpenCode document. Discovery must fail instead so the previous snapshot
	// is reused.
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE session_v2 (id TEXT PRIMARY KEY, title TEXT, directory TEXT, parent_id TEXT, time_created INTEGER, time_updated INTEGER, time_archived INTEGER)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT, data TEXT)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session_v2(id,title,directory,time_created,time_updated) VALUES('ses_broken','Broken','/tmp/project',1,2)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_DB", dbPath)
	sessions, err := NewOpenCodeAdapter("opencode").ListSessions(context.Background())
	if err == nil {
		t.Fatalf("unreadable v2 store reported a successful snapshot: %+v", sessions)
	}
}

func TestOpenCodeV2StoreAndResumeCommand(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE session_v2 (id TEXT PRIMARY KEY, title TEXT, directory TEXT, parent_id TEXT, fork_session_id TEXT, time_created INTEGER, time_updated INTEGER, time_archived INTEGER)`,
		`CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT, seq INTEGER, type TEXT, time_created INTEGER, data TEXT)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO session_v2(id,title,directory,time_created,time_updated)
		VALUES('ses_test','Fix auth','/tmp/project',1000,2000)`); err != nil {
		t.Fatal(err)
	}
	rows := []struct {
		typ, data string
	}{
		{"user", `{"text":"Fix the login"}`},
		{"assistant", `{"content":[{"type":"text","text":"Done."},{"type":"tool","tool":"bash","state":{"input":{"cmd":"go test ./..."},"content":[{"type":"text","text":"ok"}]}}]}`},
	}
	for i, row := range rows {
		if _, err := db.Exec(`INSERT INTO session_message(id,session_id,seq,type,time_created,data) VALUES(?,?,?,?,?,?)`,
			"m"+string(rune('1'+i)), "ses_test", i, row.typ, 1000+i, row.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OPENCODE_DB", dbPath)
	a := NewOpenCodeAdapter("opencode")
	sessions, err := a.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].NativeID != "ses_test" || sessions[0].MessageCount != 2 {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}
	tr, err := a.Transcript(context.Background(), "ses_test", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Total != 3 {
		t.Fatalf("expected user, text and tool entries, got %d: %+v", tr.Total, tr.Messages)
	}
	if tr.Messages[0].Text != "Fix the login" || tr.Messages[2].ToolName != "bash" {
		t.Fatalf("unexpected transcript: %+v", tr.Messages)
	}
	hits, err := a.Search(context.Background(), "login", 10)
	if err != nil || len(hits) != 1 || hits[0].NativeID != "ses_test" {
		t.Fatalf("unexpected search: %+v %v", hits, err)
	}
	// LIKE metacharacters must be matched literally; a bare "%" used to select
	// every row and return unrelated sessions.
	if wildcard, err := a.Search(context.Background(), "%", 10); err != nil || len(wildcard) != 0 {
		t.Fatalf("wildcard search matched unrelated rows: %+v %v", wildcard, err)
	}
	cmd, err := a.BuildCommand(Action{Kind: protocol.ActionResume, NativeID: "ses_test", Cwd: "/tmp/project"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "--session ses_test") {
		t.Fatalf("unexpected command: %+v", cmd)
	}
}
