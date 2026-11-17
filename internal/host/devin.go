package host

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// DevinAdapter reads the Devin CLI session database read-only and starts the
// Devin CLI for terminal runs. Structured runs use the generic ACP driver
// through `devin acp`. Devin Desktop bundles the same CLI and session store.
type DevinAdapter struct {
	bin string
	db  string
}

func NewDevinAdapter(bin string) *DevinAdapter {
	return &DevinAdapter{bin: bin, db: devinDBPath()}
}

func devinDBPath() string {
	var base string
	if runtime.GOOS == "windows" {
		if dir, err := os.UserConfigDir(); err == nil {
			base = dir
		}
		if base == "" {
			base = strings.TrimSpace(os.Getenv("APPDATA"))
		}
	}
	if base == "" {
		base = strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
		if base == "" {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, ".local", "share")
		}
	}
	// Air-gapped federal builds use cli-federal so they can coexist with the
	// standard CLI on the same machine.
	for _, channel := range []string{"cli", "cli-federal"} {
		path := filepath.Join(base, "devin", channel, "sessions.db")
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path
		}
	}
	return filepath.Join(base, "devin", "cli", "sessions.db")
}

func (a *DevinAdapter) Name() string { return "devin" }

func (a *DevinAdapter) WatchPaths() []string { return []string{a.db} }

func (a *DevinAdapter) Detect(context.Context) (protocol.AgentInfo, error) {
	if a.bin == "" {
		if !a.storeExists() {
			return protocol.AgentInfo{Name: "devin", Version: "unknown", Supported: false, Capabilities: []string{"terminal"}}, os.ErrNotExist
		}
		return protocol.AgentInfo{
			Name: "devin", Version: "unknown", Supported: false,
			Capabilities: []string{"discovery", "transcript"},
		}, nil
	}
	info, err := detectInfo("devin", a.bin)
	if err != nil {
		return info, err
	}
	if !a.storeExists() {
		info.Supported = false
		info.Capabilities = []string{"terminal"}
	}
	return info, nil
}

func (a *DevinAdapter) storeExists() bool {
	st, err := os.Stat(a.db)
	return err == nil && !st.IsDir()
}

func (a *DevinAdapter) open() (*sql.DB, error) {
	if _, err := os.Stat(a.db); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(a.db) + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func (a *DevinAdapter) ListSessions(ctx context.Context) ([]protocol.Session, error) {
	db, err := a.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if !sqliteTableExists(db, "sessions") {
		return nil, errors.New("devin session store has no sessions table")
	}
	columns := sqliteTableColumns(db, "sessions")
	if columns == nil {
		return nil, errors.New("devin session store schema could not be read")
	}
	titleExpr := "''"
	if columns["title"] {
		titleExpr = "COALESCE(title,'')"
	}
	where := ""
	if columns["hidden"] {
		where = " WHERE hidden = 0"
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, working_directory, `+titleExpr+`, created_at, last_activity_at
		FROM sessions`+where+`
		ORDER BY last_activity_at DESC`)
	if err != nil {
		return nil, err
	}
	var out []protocol.Session
	for rows.Next() {
		var id, cwd, title string
		var created, activity int64
		if err := rows.Scan(&id, &cwd, &title, &created, &activity); err != nil {
			continue
		}
		if !validNativeID(id) {
			continue
		}
		session := protocol.Session{
			NativeID:  id,
			Agent:     "devin",
			Title:     cleanTitle(title),
			Cwd:       cwd,
			State:     protocol.StateIdle,
			CreatedAt: devinTime(created),
			UpdatedAt: devinTime(activity),
		}
		if session.UpdatedAt.IsZero() {
			session.UpdatedAt = session.CreatedAt
		}
		if session.CreatedAt.IsZero() {
			session.CreatedAt = session.UpdatedAt
		}
		out = append(out, session)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	counts := map[string]int{}
	table := ""
	switch {
	case sqliteTableExists(db, "message_nodes"):
		table = "message_nodes"
	case sqliteTableExists(db, "messages"):
		table = "messages"
	}
	if table != "" {
		countQuery := `SELECT session_id, COUNT(*) FROM ` + table + ` GROUP BY session_id`
		if table == "message_nodes" {
			countQuery = `SELECT session_id, COUNT(*) FROM message_nodes
				WHERE json_extract(chat_message, '$.role') IN ('user', 'assistant')
				GROUP BY session_id`
		}
		countRows, err := db.QueryContext(ctx, countQuery)
		if err == nil {
			for countRows.Next() {
				var id string
				var count int
				if countRows.Scan(&id, &count) == nil {
					counts[id] = count
				}
			}
			countRows.Close()
		}
	}
	for i := range out {
		out[i].MessageCount = counts[out[i].NativeID]
		if out[i].Title == "" {
			out[i].Title = a.sessionTitle(ctx, db, out[i].NativeID)
		}
		if out[i].Title == "" {
			out[i].Title = cleanTitle(filepath.Base(out[i].Cwd))
		}
		if out[i].Title == "" {
			out[i].Title = out[i].NativeID
		}
	}
	return out, nil
}

func (a *DevinAdapter) sessionTitle(ctx context.Context, db *sql.DB, nativeID string) string {
	var content string
	if err := db.QueryRowContext(ctx,
		`SELECT content FROM prompt_history WHERE session_id = ? ORDER BY timestamp ASC, id ASC LIMIT 1`,
		nativeID).Scan(&content); err == nil {
		if title := usableTitle(content); title != "" {
			return title
		}
	}
	var raw sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT json_extract(chat_message, '$.content') FROM message_nodes
		 WHERE session_id = ? AND json_extract(chat_message, '$.role') = 'user'
		 ORDER BY node_id ASC LIMIT 1`, nativeID).Scan(&raw); err == nil && raw.Valid {
		if title := usableTitle(devinText(json.RawMessage(raw.String))); title != "" {
			return title
		}
	}
	return ""
}

type devinToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	ToolName  string          `json:"tool_name"`
	Arguments json.RawMessage `json:"arguments"`
	Input     json.RawMessage `json:"input"`
	Function  struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type devinChatMessage struct {
	MessageID        string          `json:"message_id"`
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	ToolCallID       string          `json:"tool_call_id"`
	ToolCalls        json.RawMessage `json:"tool_calls"`
	ReasoningDetails json.RawMessage `json:"reasoning_details"`
	Metadata         struct {
		IsUserInput *bool `json:"is_user_input"`
	} `json:"metadata"`
}

// devinToolCalls accepts the array shape stored by current builds, a
// double-encoded JSON string from older builds, and a single object.
func devinToolCalls(raw json.RawMessage) []devinToolCall {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	var calls []devinToolCall
	if json.Unmarshal(b, &calls) == nil {
		return calls
	}
	var single devinToolCall
	if json.Unmarshal(b, &single) == nil && (single.ID != "" || single.Name != "" || single.Function.Name != "") {
		return []devinToolCall{single}
	}
	var encoded string
	if json.Unmarshal(b, &encoded) == nil {
		var decoded []devinToolCall
		if json.Unmarshal([]byte(encoded), &decoded) == nil {
			return decoded
		}
	}
	return nil
}

func (a *DevinAdapter) Transcript(ctx context.Context, nativeID string, offset, limit int) (protocol.Transcript, error) {
	if !validNativeID(nativeID) {
		return protocol.Transcript{}, errors.New("invalid devin session id")
	}
	db, err := a.open()
	if err != nil {
		return protocol.Transcript{}, err
	}
	defer db.Close()
	if !sqliteTableExists(db, "message_nodes") {
		return protocol.Transcript{}, errors.New("this devin version stores no transcript nodes")
	}
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, nativeID).Scan(&exists); err != nil {
		return protocol.Transcript{}, err
	}
	if exists == 0 {
		return protocol.Transcript{}, errors.New("devin session not found")
	}

	messages := a.readChain(ctx, db, nativeID)
	if len(messages) == 0 {
		messages = a.readAllNodes(ctx, db, nativeID)
	}
	return pageMessages(messages, offset, limit)
}

func (a *DevinAdapter) readChain(ctx context.Context, db *sql.DB, nativeID string) []protocol.Message {
	var head sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT main_chain_id FROM sessions WHERE id = ?`, nativeID).Scan(&head); err != nil || !head.Valid {
		return nil
	}
	rows, err := db.QueryContext(ctx, `
		WITH RECURSIVE chain(node_id, parent_node_id, chat_message, created_at) AS (
			SELECT node_id, parent_node_id, chat_message, created_at
			FROM message_nodes WHERE session_id = ? AND node_id = ?
			UNION ALL
			SELECT mn.node_id, mn.parent_node_id, mn.chat_message, mn.created_at
			FROM message_nodes mn JOIN chain c ON mn.node_id = c.parent_node_id
			WHERE mn.session_id = ?
		)
		SELECT chat_message, created_at FROM chain ORDER BY node_id ASC`,
		nativeID, head.Int64, nativeID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return a.scanNodes(rows)
}

func (a *DevinAdapter) readAllNodes(ctx context.Context, db *sql.DB, nativeID string) []protocol.Message {
	rows, err := db.QueryContext(ctx, `
		SELECT chat_message, created_at FROM message_nodes
		WHERE session_id = ? ORDER BY node_id ASC`, nativeID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return a.scanNodes(rows)
}

func (a *DevinAdapter) scanNodes(rows *sql.Rows) []protocol.Message {
	var messages []protocol.Message
	toolNames := map[string]string{}
	for rows.Next() {
		var raw string
		var created int64
		if rows.Scan(&raw, &created) != nil {
			continue
		}
		var msg devinChatMessage
		if json.Unmarshal([]byte(raw), &msg) != nil {
			continue
		}
		at := devinTime(created)
		switch msg.Role {
		case "user":
			if msg.Metadata.IsUserInput != nil && !*msg.Metadata.IsUserInput {
				continue
			}
			if text := strings.TrimSpace(devinText(msg.Content)); text != "" {
				messages = append(messages, protocol.Message{Role: "user", Kind: "text", Text: text, Timestamp: at})
			}
		case "assistant":
			if reasoning := strings.TrimSpace(devinText(msg.ReasoningDetails)); reasoning != "" {
				messages = append(messages, protocol.Message{Role: "assistant", Kind: "reasoning", Text: reasoning, Timestamp: at})
			}
			if text := strings.TrimSpace(devinText(msg.Content)); text != "" {
				messages = append(messages, protocol.Message{Role: "assistant", Kind: "text", Text: text, Timestamp: at})
			}
			for _, call := range devinToolCalls(msg.ToolCalls) {
				name := devinToolName(call)
				if name == "" {
					name = "tool"
				}
				if call.ID != "" {
					toolNames[call.ID] = name
				}
				messages = append(messages, protocol.Message{
					Role: "assistant", Kind: "tool_call", ToolName: name,
					ToolInput: devinToolInput(call), Timestamp: at,
				})
			}
		case "tool":
			if output := strings.TrimSpace(devinText(msg.Content)); output != "" {
				messages = append(messages, protocol.Message{
					Role: "tool", Kind: "tool_result", ToolName: toolNames[msg.ToolCallID],
					ToolOutput: output, Timestamp: at,
				})
			}
		}
	}
	return messages
}

func devinToolName(call devinToolCall) string {
	switch {
	case call.Name != "":
		return call.Name
	case call.ToolName != "":
		return call.ToolName
	default:
		return call.Function.Name
	}
}

func devinToolInput(call devinToolCall) string {
	raw := call.Arguments
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = call.Function.Arguments
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = call.Input
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return string(raw)
}

// devinText unwraps the string or content-block shapes Devin uses for message
// text, reasoning and tool output.
func devinText(raw json.RawMessage) string {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 || string(b) == "null" {
		return ""
	}
	switch b[0] {
	case '"':
		var text string
		if json.Unmarshal(b, &text) == nil {
			return text
		}
	case '[':
		var blocks []struct {
			Type     string          `json:"type"`
			Text     string          `json:"text"`
			Thinking string          `json:"thinking"`
			Content  json.RawMessage `json:"content"`
		}
		if json.Unmarshal(b, &blocks) == nil {
			var parts []string
			for _, block := range blocks {
				before := len(parts)
				for _, value := range []string{block.Text, block.Thinking} {
					if strings.TrimSpace(value) != "" {
						parts = append(parts, value)
					}
				}
				if len(parts) == before && len(block.Content) > 0 {
					if text := devinText(block.Content); text != "" {
						parts = append(parts, text)
					}
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "\n")
			}
		}
		var stringsOnly []string
		if json.Unmarshal(b, &stringsOnly) == nil {
			return strings.Join(stringsOnly, "\n")
		}
	case '{':
		var object map[string]any
		if json.Unmarshal(b, &object) == nil {
			for _, key := range []string{"text", "thinking", "content"} {
				if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
					return value
				}
			}
		}
	}
	return string(b)
}

func devinLikePattern(query string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(query) + "%"
}

func (a *DevinAdapter) Search(ctx context.Context, query string, limit int) ([]protocol.SearchHit, error) {
	if limit <= 0 {
		limit = 50
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil, nil
	}
	db, err := a.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if !sqliteTableExists(db, "message_nodes") {
		return nil, nil
	}
	columns := sqliteTableColumns(db, "sessions")
	hidden := ""
	if columns["hidden"] {
		hidden = " AND COALESCE(s.hidden, 0) = 0"
	}
	titleExpr := "''"
	if columns["title"] {
		titleExpr = "COALESCE(s.title,'')"
		if sqliteTableExists(db, "prompt_history") {
			titleExpr = `COALESCE(NULLIF(s.title,''), (SELECT content FROM prompt_history ph WHERE ph.session_id = s.id ORDER BY ph.timestamp ASC, ph.id ASC LIMIT 1), '')`
		}
	}
	rows, err := db.QueryContext(ctx, `
		SELECT mn.session_id, `+titleExpr+`, mn.chat_message, mn.created_at
		FROM message_nodes mn JOIN sessions s ON s.id = mn.session_id
		WHERE mn.chat_message LIKE ? ESCAPE '\'`+hidden+`
		LIMIT 500`, devinLikePattern(query))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []protocol.SearchHit
	for rows.Next() {
		if ctx.Err() != nil {
			return hits, ctx.Err()
		}
		var id, title, raw string
		var created int64
		if rows.Scan(&id, &title, &raw, &created) != nil {
			continue
		}
		var msg devinChatMessage
		if json.Unmarshal([]byte(raw), &msg) != nil {
			continue
		}
		text := devinMessageText(msg)
		if !strings.Contains(strings.ToLower(text), needle) {
			continue
		}
		hits = append(hits, protocol.SearchHit{
			NativeID: id, Agent: "devin", Title: cleanTitle(title),
			Snippet: searchSnippet(text, query), At: devinTime(created),
		})
		if len(hits) >= limit {
			break
		}
	}
	return hits, nil
}

func devinMessageText(msg devinChatMessage) string {
	var parts []string
	for _, raw := range []json.RawMessage{msg.Content, msg.ReasoningDetails} {
		if text := strings.TrimSpace(devinText(raw)); text != "" {
			parts = append(parts, text)
		}
	}
	for _, call := range devinToolCalls(msg.ToolCalls) {
		parts = append(parts, strings.TrimSpace(devinToolName(call)+" "+devinToolInput(call)))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// devinTime accepts the second, millisecond, microsecond and nanosecond epoch
// values used by different Devin schema revisions.
func devinTime(value int64) time.Time {
	switch {
	case value <= 0:
		return time.Time{}
	case value >= 1e16:
		return time.Unix(0, value).UTC()
	case value >= 1e14:
		return time.Unix(0, value*1000).UTC()
	case value >= 1e12:
		return time.UnixMilli(value).UTC()
	default:
		return time.Unix(value, 0).UTC()
	}
}

func (a *DevinAdapter) BuildCommand(action Action) (Command, error) {
	if a.bin == "" {
		return Command{}, fmt.Errorf("the devin CLI is not installed; install it to resume sessions")
	}
	var args []string
	switch action.Kind {
	case protocol.ActionResume, "":
		if action.NativeID == "" {
			return Command{}, fmt.Errorf("native session id is required")
		}
		args = []string{"--resume", action.NativeID}
	case protocol.ActionNew:
	case protocol.ActionFork:
		return Command{}, fmt.Errorf("devin does not expose a documented fork command")
	default:
		return Command{}, fmt.Errorf("unsupported action %q", action.Kind)
	}
	path, argv := commandFor(a.bin, args)
	return Command{Path: path, Args: argv, Cwd: action.Cwd}, nil
}
