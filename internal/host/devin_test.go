package host

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	_ "modernc.org/sqlite"
)

func devinFixture(t *testing.T) *DevinAdapter {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE sessions (
			id TEXT PRIMARY KEY, working_directory TEXT NOT NULL, backend_type TEXT NOT NULL,
			model TEXT NOT NULL, agent_mode TEXT NOT NULL, created_at INTEGER NOT NULL,
			last_activity_at INTEGER NOT NULL, title TEXT, main_chain_id INTEGER,
			hidden INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE message_nodes (
			row_id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL,
			node_id INTEGER NOT NULL, parent_node_id INTEGER, chat_message TEXT NOT NULL,
			created_at INTEGER NOT NULL, UNIQUE(session_id, node_id))`,
		`CREATE TABLE prompt_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT, content TEXT NOT NULL,
			timestamp INTEGER NOT NULL, session_id TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	created := time.Now().Add(-time.Minute).UnixMilli()
	activity := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO sessions
		(id, working_directory, backend_type, model, agent_mode, created_at, last_activity_at, title, main_chain_id, hidden)
		VALUES ('dev-visible', '/work/demo', 'local', 'adaptive', 'normal', ?, ?, '', 3, 0)`, created, activity); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions
		(id, working_directory, backend_type, model, agent_mode, created_at, last_activity_at, title, main_chain_id, hidden)
		VALUES ('dev-hidden', '/work/hidden', 'local', 'adaptive', 'normal', ?, ?, 'Summarizer', 1, 1)`, created, activity); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO prompt_history (content, timestamp, session_id) VALUES (?, ?, ?)`,
		"Fix the checkout retry", created, "dev-visible"); err != nil {
		t.Fatal(err)
	}
	nodes := []struct {
		node    int
		parent  any
		message string
		created int64
	}{
		{1, nil, `{"role":"user","content":"Fix the checkout retry","metadata":{"is_user_input":true}}`, created},
		{2, 1, `{"role":"assistant","content":"I will inspect it.","reasoning_details":"Need to inspect the test.","tool_calls":[{"id":"call_1","name":"read_file","arguments":"{\"path\":\"/work/demo/checkout.ts\"}"}]}`, created + 10},
		{3, 2, `{"role":"tool","content":"export function retry() {}","tool_call_id":"call_1"}`, created + 20},
		{4, 3, `{"role":"assistant","content":"Discarded branch"}`, created + 30},
	}
	for _, node := range nodes {
		if _, err := db.Exec(`INSERT INTO message_nodes
			(session_id, node_id, parent_node_id, chat_message, created_at)
			VALUES ('dev-visible', ?, ?, ?, ?)`, node.node, node.parent, node.message, node.created); err != nil {
			t.Fatal(err)
		}
	}
	return &DevinAdapter{bin: "devin", db: dbPath}
}

func TestDevinTranscriptCycleIsBounded(t *testing.T) {
	adapter := devinFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Capture the acyclic chain first. It walks 3 -> 2 -> 1.
	baseline, err := adapter.Transcript(ctx, "dev-visible", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Total == 0 {
		t.Fatal("baseline transcript is empty")
	}

	// Close the same chain into a cycle: node 1 now points back at node 3, so
	// the recursive parent walk would never reach a root. A corrupt store must
	// not turn a transcript read into an unbounded query.
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(adapter.db))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE message_nodes SET parent_node_id=3 WHERE session_id='dev-visible' AND node_id=1`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var transcript protocol.Transcript
	var readErr error
	go func() {
		transcript, readErr = adapter.Transcript(ctx, "dev-visible", 0, 100)
		close(done)
	}()
	select {
	case <-done:
		if readErr != nil {
			t.Fatalf("cyclic transcript: %v", readErr)
		}
		if transcript.Total == 0 || len(transcript.Messages) == 0 {
			t.Fatalf("cyclic transcript returned no bounded messages: %+v", transcript)
		}
		// The depth cap bounds the walk and the DISTINCT projection collapses
		// repeated nodes, so the cyclic read must contain exactly the same
		// distinct messages as the acyclic baseline.
		if transcript.Total != baseline.Total {
			t.Fatalf("cyclic transcript repeated nodes: %d messages, baseline %d", transcript.Total, baseline.Total)
		}
	case <-time.After(15 * time.Second):
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		t.Fatal("transcript walk did not terminate on a cyclic message graph")
	}
}

func TestDevinStoreOnlyDetect(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	if err := os.WriteFile(dbPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := (&DevinAdapter{db: dbPath}).Detect(context.Background())
	if err != nil || info.Supported || !sliceContains(info.Capabilities, "discovery") {
		t.Fatalf("store-only detect: %+v %v", info, err)
	}
}

func TestDevinAdapter(t *testing.T) {
	adapter := devinFixture(t)
	sessions, err := adapter.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions: %+v", sessions)
	}
	session := sessions[0]
	if session.NativeID != "dev-visible" || session.Agent != "devin" || session.Title != "Fix the checkout retry" {
		t.Fatalf("session: %+v", session)
	}
	if session.Cwd != "/work/demo" || session.MessageCount != 3 {
		t.Fatalf("metadata: %+v", session)
	}
	if session.UpdatedAt.Year() < 2020 {
		t.Fatalf("timestamp was not decoded: %+v", session)
	}

	transcript, err := adapter.Transcript(context.Background(), "dev-visible", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if transcript.Total != 5 {
		t.Fatalf("transcript total: %d (%+v)", transcript.Total, transcript.Messages)
	}
	var sawReasoning, sawTool bool
	for _, message := range transcript.Messages {
		if message.Kind == "reasoning" && strings.Contains(message.Text, "inspect") {
			sawReasoning = true
		}
		if message.Kind == "tool_result" && message.ToolName == "read_file" && strings.Contains(message.ToolOutput, "retry") {
			sawTool = true
		}
		if strings.Contains(message.Text, "Discarded branch") {
			t.Fatalf("off-chain node leaked into the transcript: %+v", transcript.Messages)
		}
	}
	if !sawReasoning || !sawTool {
		t.Fatalf("reasoning/tool pairing missing: %+v", transcript.Messages)
	}

	hits, err := adapter.Search(context.Background(), "checkout", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Agent != "devin" || hits[0].Title != "Fix the checkout retry" {
		t.Fatalf("search: %+v", hits)
	}

	resume, err := adapter.BuildCommand(Action{Kind: protocol.ActionResume, NativeID: "dev-visible", Cwd: "/work/demo"})
	if err != nil || strings.Join(resume.Args, " ") != "--resume dev-visible" {
		t.Fatalf("resume: %+v %v", resume, err)
	}
	if _, err := adapter.BuildCommand(Action{Kind: protocol.ActionFork, NativeID: "dev-visible"}); err == nil {
		t.Fatal("devin fork was accepted")
	}
}

func TestDevinTextShapes(t *testing.T) {
	raw := func(value string) json.RawMessage { return json.RawMessage(value) }
	if got := devinText(raw(`"plain"`)); got != "plain" {
		t.Fatalf("string: %q", got)
	}
	if got := devinText(raw(`[{"type":"text","text":"one"},{"type":"thinking","thinking":"two"}]`)); got != "one\ntwo" {
		t.Fatalf("blocks: %q", got)
	}
	if got := devinText(raw(`{"content":"three"}`)); got != "three" {
		t.Fatalf("object: %q", got)
	}
	if got := devinText(nil); got != "" {
		t.Fatalf("empty: %q", got)
	}
	if calls := devinToolCalls(raw(`[{"id":"a","name":"read"}]`)); len(calls) != 1 || calls[0].Name != "read" {
		t.Fatalf("tool array: %+v", calls)
	}
	if calls := devinToolCalls(raw(`{"id":"b","function":{"name":"write"}}`)); len(calls) != 1 || calls[0].Function.Name != "write" {
		t.Fatalf("tool object: %+v", calls)
	}
	if calls := devinToolCalls(raw(`"[{\"id\":\"c\",\"name\":\"exec\"}]"`)); len(calls) != 1 || calls[0].Name != "exec" {
		t.Fatalf("tool string: %+v", calls)
	}
	if calls := devinToolCalls(nil); calls != nil {
		t.Fatalf("tool empty: %+v", calls)
	}
}

func TestDevinTimeUnits(t *testing.T) {
	if got := devinTime(1_700_000_000); got.Year() != 2023 {
		t.Fatalf("seconds: %v", got)
	}
	if got := devinTime(1_700_000_000_000); got.Year() != 2023 {
		t.Fatalf("milliseconds: %v", got)
	}
	if got := devinTime(1_700_000_000_000_000); got.Year() != 2023 {
		t.Fatalf("microseconds: %v", got)
	}
	if !devinTime(0).IsZero() {
		t.Fatal("zero was not zero")
	}
}
