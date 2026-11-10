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

func TestOpenCodeV2StoreAndResumeCommand(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE session_v2 (id TEXT PRIMARY KEY, title TEXT, directory TEXT, time_created INTEGER, time_updated INTEGER, time_archived INTEGER)`,
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
	cmd, err := a.BuildCommand(Action{Kind: protocol.ActionResume, NativeID: "ses_test", Cwd: "/tmp/project"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "--session ses_test") {
		t.Fatalf("unexpected command: %+v", cmd)
	}
}
