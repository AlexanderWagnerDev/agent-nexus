package host

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenCodeV2(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "opencode.db")
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE session_v2 (
			id TEXT PRIMARY KEY, project_id TEXT, workspace_id TEXT, parent_id TEXT,
			fork_session_id TEXT, fork_boundary TEXT, slug TEXT, directory TEXT, path TEXT,
			title TEXT, version TEXT, share_url TEXT, summary_additions INTEGER,
			summary_deletions INTEGER, summary_files INTEGER, summary_diffs TEXT, metadata TEXT,
			cost REAL, tokens_input INTEGER, tokens_output INTEGER, tokens_reasoning INTEGER,
			tokens_cache_read INTEGER, tokens_cache_write INTEGER, revert TEXT, permission TEXT,
			agent TEXT, model TEXT, time_created INTEGER, time_updated INTEGER,
			time_compacting INTEGER, time_archived INTEGER, time_suspended INTEGER,
			resume_attempts INTEGER, time_idle INTEGER, time_viewed INTEGER, idle_outcome TEXT,
			time_busy INTEGER
		);
		CREATE TABLE session_message (
			id TEXT PRIMARY KEY, session_id TEXT, type TEXT, seq INTEGER,
			time_created INTEGER, time_updated INTEGER, data TEXT
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO session_v2(id,directory,title,time_created,time_updated) VALUES(?,?,?,?,?)`,
		"ses_v2test", "C:/tmp/project", "v2 session", 1000, 2000)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO session_message(id,session_id,type,seq,time_created,time_updated,data) VALUES(?,?,?,?,?,?,?)`,
		"msg1", "ses_v2test", "user", 1, 1000, 1000, `{"text":"hello v2"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO session_message(id,session_id,type,seq,time_created,time_updated,data) VALUES(?,?,?,?,?,?,?)`,
		"msg2", "ses_v2test", "assistant", 2, 2000, 2000,
		`{"content":[{"type":"text","text":"hi"},{"type":"tool","name":"bash","state":{"input":{"command":"echo hi"},"content":[{"type":"text","text":"hi\n"}]}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	a := &OpenCodeAdapter{bin: os.DevNull, db: dbPath}
	sessions, err := a.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].NativeID != "ses_v2test" || sessions[0].MessageCount != 2 {
		t.Fatalf("unexpected v2 sessions: %+v", sessions)
	}
	tr, err := a.Transcript(context.Background(), "ses_v2test", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Total != 3 {
		t.Fatalf("expected 3 transcript entries, got %d: %+v", tr.Total, tr.Messages)
	}
	if tr.Messages[0].Text != "hello v2" || tr.Messages[1].Text != "hi" {
		t.Fatalf("unexpected transcript: %+v", tr.Messages)
	}
	if tr.Messages[2].ToolName != "bash" || tr.Messages[2].ToolOutput != "hi\n" {
		t.Fatalf("tool message not parsed: %+v", tr.Messages[2])
	}
}
