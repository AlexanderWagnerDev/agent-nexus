package host

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenCodeLegacyReasoning(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "opencode.db")
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, data TEXT);
		CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT, session_id TEXT, time_created INTEGER, data TEXT);
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO message(id,session_id,time_created,data) VALUES(?,?,?,?)`,
		"msg1", "ses_legacy", 1000, `{"role":"assistant"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO part(id,message_id,session_id,time_created,data) VALUES(?,?,?,?,?)`,
		"part1", "msg1", "ses_legacy", 1000, `{"type":"reasoning","text":"legacy thinking"}`)
	if err != nil {
		t.Fatal(err)
	}
	a := &OpenCodeAdapter{bin: os.DevNull, db: dbPath}
	tr, err := a.Transcript(context.Background(), "ses_legacy", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Total != 1 || tr.Messages[0].Kind != "reasoning" || tr.Messages[0].Text != "legacy thinking" {
		t.Fatalf("legacy reasoning part was dropped: %+v", tr.Messages)
	}
}

func TestOpenCodeV2AssistantParts(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "opencode.db")
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE session_v2 (id TEXT PRIMARY KEY, directory TEXT, title TEXT,
			time_created INTEGER, time_updated INTEGER, time_archived INTEGER);
		CREATE TABLE session_message (
			id TEXT PRIMARY KEY, session_id TEXT, type TEXT, seq INTEGER,
			time_created INTEGER, time_updated INTEGER, data TEXT
		);
		CREATE TABLE session_part (
			message_id TEXT, session_id TEXT, ordinal INTEGER, type TEXT, call_id TEXT,
			time_created INTEGER, time_updated INTEGER, data TEXT
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO session_v2(id,directory,title,time_created,time_updated) VALUES(?,?,?,?,?)`,
		"ses_parts", "C:/tmp/project", "v2 parts", 1000, 2000)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO session_message(id,session_id,type,seq,time_created,time_updated,data) VALUES(?,?,?,?,?,?,?)`,
		"msg1", "ses_parts", "user", 1, 1000, 1000, `{"text":"hello parts"}`)
	if err != nil {
		t.Fatal(err)
	}
	// 2.4 keeps assistant content in session_part and leaves content empty.
	_, err = db.Exec(`INSERT INTO session_message(id,session_id,type,seq,time_created,time_updated,data) VALUES(?,?,?,?,?,?,?)`,
		"msg2", "ses_parts", "assistant", 2, 2000, 2000, `{"content":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []struct {
		ordinal int
		data    string
	}{
		{0, `{"type":"reasoning","text":"we should reply"}`},
		{1, `{"type":"text","text":"hi from a part"}`},
		{2, `{"type":"tool","name":"bash","state":{"input":{"command":"echo hi"},"content":[{"type":"text","text":"hi\n"}]}}`},
	} {
		if _, err := db.Exec(`INSERT INTO session_part(message_id,session_id,ordinal,type,time_created,time_updated,data) VALUES(?,?,?,?,?,?,?)`,
			"msg2", "ses_parts", part.ordinal, "part", 2000, 2000, part.data); err != nil {
			t.Fatal(err)
		}
	}
	a := &OpenCodeAdapter{bin: os.DevNull, db: dbPath}
	tr, err := a.Transcript(context.Background(), "ses_parts", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Total != 4 {
		t.Fatalf("expected 4 transcript entries, got %d: %+v", tr.Total, tr.Messages)
	}
	if tr.Messages[1].Kind != "reasoning" || tr.Messages[1].Text != "we should reply" {
		t.Fatalf("reasoning part was dropped: %+v", tr.Messages)
	}
	if tr.Messages[2].Text != "hi from a part" || tr.Messages[3].ToolName != "bash" {
		t.Fatalf("assistant parts were dropped: %+v", tr.Messages)
	}
	hits, err := a.Search(context.Background(), "reply", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].NativeID != "ses_parts" {
		t.Fatalf("search missed a reply stored in session_part: %+v", hits)
	}
}

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
		`{"content":[{"type":"reasoning","text":"we should reply"},{"type":"text","text":"hi"},{"type":"tool","name":"bash","state":{"input":{"command":"echo hi"},"content":[{"type":"text","text":"hi\n"}]}}]}`)
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
	if tr.Total != 4 {
		t.Fatalf("expected 4 transcript entries, got %d: %+v", tr.Total, tr.Messages)
	}
	if tr.Messages[0].Text != "hello v2" || tr.Messages[1].Text != "we should reply" || tr.Messages[2].Text != "hi" {
		t.Fatalf("unexpected transcript: %+v", tr.Messages)
	}
	if tr.Messages[3].ToolName != "bash" || tr.Messages[3].ToolOutput != "hi\n" {
		t.Fatalf("tool message not parsed: %+v", tr.Messages[3])
	}
}
