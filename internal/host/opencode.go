package host

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	_ "modernc.org/sqlite"
)

// OpenCodeAdapter reads the OpenCode SQLite store read-only and starts the
// native OpenCode TUI for terminal runs.
type OpenCodeAdapter struct {
	bin string
	db  string
}

func NewOpenCodeAdapter(bin string) *OpenCodeAdapter {
	return &OpenCodeAdapter{bin: bin, db: openCodeDBPath(bin)}
}

func openCodeDBPath(bin string) string {
	if p := os.Getenv("OPENCODE_DB"); p != "" {
		return p
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	name, args, err := commandFor(bin, []string{"debug", "paths"})
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	}
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "db" {
				return strings.Join(fields[1:], " ")
			}
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
}

func (a *OpenCodeAdapter) Name() string { return "opencode" }

func (a *OpenCodeAdapter) WatchPaths() []string { return []string{a.db} }

func (a *OpenCodeAdapter) Detect(context.Context) (protocol.AgentInfo, error) {
	info, err := detectInfo("opencode", a.bin)
	if err != nil {
		return info, err
	}
	if st, err := os.Stat(a.db); err != nil || st.IsDir() {
		info.Supported = false
		info.Capabilities = []string{"terminal"}
	} else {
		info.Capabilities = append(info.Capabilities, "structured")
	}
	return info, nil
}

func (a *OpenCodeAdapter) open() (*sql.DB, error) {
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

func (a *OpenCodeAdapter) ListSessions(ctx context.Context) ([]protocol.Session, error) {
	db, err := a.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	byID := map[string]protocol.Session{}
	var order []string
	add := func(s protocol.Session) {
		s.Agent = "opencode"
		if s.State == "" {
			s.State = protocol.StateIdle
		}
		s.Title = cleanTitle(s.Title)
		if s.Title == "" {
			s.Title = filepath.Base(s.Cwd)
		}
		if _, ok := byID[s.NativeID]; !ok {
			order = append(order, s.NativeID)
		}
		byID[s.NativeID] = s
	}

	// Discovery replaces the daemon's last snapshot, so an unreadable store
	// must be reported instead of looking like an empty one: otherwise every
	// OpenCode session disappears from the hub and the index prunes them.
	// session_v2 is authoritative; a legacy-table failure is only fatal when
	// there is no complete v2 snapshot.
	v2Exists, err := sqliteTableExistsErr(db, "session_v2")
	if err != nil {
		return nil, fmt.Errorf("opencode schema lookup: %w", err)
	}
	legacyExists, err := sqliteTableExistsErr(db, "session")
	if err != nil {
		return nil, fmt.Errorf("opencode schema lookup: %w", err)
	}

	readV2 := func() error {
		rows, err := db.QueryContext(ctx, `
			SELECT s.id, COALESCE(s.title,''), s.directory, s.time_created, s.time_updated,
			(SELECT COUNT(*) FROM session_message m WHERE m.session_id = s.id),
			COALESCE(s.parent_id,''), COALESCE(s.fork_session_id,'')
			FROM session_v2 s
			WHERE s.time_archived IS NULL
			ORDER BY s.time_updated DESC`)
		if err != nil {
			return fmt.Errorf("opencode session_v2 query: %w", err)
		}
		defer rows.Close()
		var scanErr error
		for rows.Next() {
			var s protocol.Session
			var created, updated int64
			if err := rows.Scan(&s.NativeID, &s.Title, &s.Cwd, &created, &updated, &s.MessageCount, &s.ParentNativeID, &s.ForkedFromNativeID); err != nil {
				if scanErr == nil {
					scanErr = err
				}
				continue
			}
			s.CreatedAt = time.UnixMilli(created).UTC()
			s.UpdatedAt = time.UnixMilli(updated).UTC()
			add(s)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("opencode session_v2 read: %w", err)
		}
		if scanErr != nil {
			return fmt.Errorf("opencode session_v2 row: %w", scanErr)
		}
		return nil
	}

	readLegacy := func() ([]protocol.Session, error) {
		rows, err := db.QueryContext(ctx, `
			SELECT s.id, COALESCE(s.title,''), s.directory, s.time_created, s.time_updated,
			(SELECT COUNT(*) FROM message m WHERE m.session_id = s.id),
			COALESCE(s.parent_id,'')
			FROM session s
			WHERE s.time_archived IS NULL
			ORDER BY s.time_updated DESC`)
		if err != nil {
			return nil, fmt.Errorf("opencode session query: %w", err)
		}
		defer rows.Close()
		var found []protocol.Session
		var scanErr error
		for rows.Next() {
			var s protocol.Session
			var created, updated int64
			if err := rows.Scan(&s.NativeID, &s.Title, &s.Cwd, &created, &updated, &s.MessageCount, &s.ParentNativeID); err != nil {
				if scanErr == nil {
					scanErr = err
				}
				continue
			}
			s.CreatedAt = time.UnixMilli(created).UTC()
			s.UpdatedAt = time.UnixMilli(updated).UTC()
			found = append(found, s)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("opencode session read: %w", err)
		}
		if scanErr != nil {
			return nil, fmt.Errorf("opencode session row: %w", scanErr)
		}
		return found, nil
	}

	var v2Err error
	if v2Exists {
		v2Err = readV2()
	}
	var legacyRows []protocol.Session
	var legacyErr error
	if legacyExists {
		legacyRows, legacyErr = readLegacy()
	}
	if v2Exists {
		if v2Err != nil {
			return nil, v2Err
		}
	} else if legacyErr != nil {
		return nil, legacyErr
	}
	// Merge the legacy rows only after a complete read. A partial legacy
	// result would publish some legacy-only sessions and drop the others,
	// while a complete v2 snapshot must remain usable without it.
	if legacyErr == nil {
		for _, s := range legacyRows {
			if _, exists := byID[s.NativeID]; !exists {
				add(s)
			}
		}
	}

	out := make([]protocol.Session, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (a *OpenCodeAdapter) tableExists(db *sql.DB, table string) bool {
	return sqliteTableExists(db, table)
}

type opencodeMessage struct {
	role  string
	parts []opencodePart
}

type opencodePart struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Tool  string `json:"tool"`
	Name  string `json:"name"`
	State struct {
		Status  string          `json:"status"`
		Title   string          `json:"title"`
		Input   json.RawMessage `json:"input"`
		Output  json.RawMessage `json:"output"`
		Error   json.RawMessage `json:"error"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Metadata struct {
			Output string `json:"output"`
		} `json:"metadata"`
	} `json:"state"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Files []string `json:"files"`
	ID    string   `json:"id"`
}

type v2Message struct {
	Text     string         `json:"text"`
	Content  []opencodePart `json:"content"`
	Metadata struct {
		DisplayText string `json:"displayText"`
	} `json:"metadata"`
}

func (a *OpenCodeAdapter) Transcript(ctx context.Context, nativeID string, offset, limit int) (protocol.Transcript, error) {
	db, err := a.open()
	if err != nil {
		return protocol.Transcript{}, err
	}
	defer db.Close()
	if a.tableExists(db, "session_v2") && a.tableExists(db, "session_message") {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM session_v2 WHERE id=?`, nativeID).Scan(&exists); err == nil && exists > 0 {
			return a.v2Transcript(ctx, db, nativeID, offset, limit)
		}
	}
	return a.legacyTranscript(ctx, db, nativeID, offset, limit)
}

func (a *OpenCodeAdapter) v2Transcript(ctx context.Context, db *sql.DB, nativeID string, offset, limit int) (protocol.Transcript, error) {
	// OpenCode 2.4 moved assistant parts into session_part and left the
	// message's inline content empty; older 2.x versions kept them inline.
	parts := map[string][]opencodePart{}
	if a.tableExists(db, "session_part") {
		rows, err := db.QueryContext(ctx, `
			SELECT message_id, data FROM session_part
			WHERE session_id=? ORDER BY message_id, ordinal`, nativeID)
		if err != nil {
			return protocol.Transcript{}, err
		}
		for rows.Next() {
			var messageID, data string
			if err := rows.Scan(&messageID, &data); err != nil {
				rows.Close()
				return protocol.Transcript{}, err
			}
			var p opencodePart
			if json.Unmarshal([]byte(data), &p) == nil {
				parts[messageID] = append(parts[messageID], p)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return protocol.Transcript{}, err
		}
		rows.Close()
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, type, time_created, data FROM session_message
		WHERE session_id=? ORDER BY seq, time_created`, nativeID)
	if err != nil {
		return protocol.Transcript{}, err
	}
	defer rows.Close()
	var all []protocol.Message
	for rows.Next() {
		var id, typ, data string
		var created int64
		if err := rows.Scan(&id, &typ, &created, &data); err != nil {
			return protocol.Transcript{}, err
		}
		ts := time.UnixMilli(created).UTC()
		var m v2Message
		if json.Unmarshal([]byte(data), &m) != nil {
			continue
		}
		switch typ {
		case "user":
			text := m.Text
			if strings.TrimSpace(text) == "" {
				text = m.Metadata.DisplayText
			}
			if strings.TrimSpace(text) != "" {
				all = append(all, protocol.Message{Role: "user", Kind: "text", Text: text, Timestamp: ts})
			}
			for _, p := range m.Content {
				if p.Type == "text" && strings.TrimSpace(p.Text) != "" {
					all = append(all, protocol.Message{Role: "user", Kind: "text", Text: p.Text, Timestamp: ts})
				}
			}
		case "assistant":
			content := parts[id]
			if len(content) == 0 {
				content = m.Content
			}
			for _, p := range content {
				switch p.Type {
				case "text":
					if strings.TrimSpace(p.Text) != "" {
						all = append(all, protocol.Message{Role: "assistant", Kind: "text", Text: p.Text, Timestamp: ts})
					}
				case "reasoning":
					if strings.TrimSpace(p.Text) != "" {
						all = append(all, protocol.Message{Role: "assistant", Kind: "reasoning", Text: p.Text, Timestamp: ts})
					}
				case "tool":
					all = append(all, toolMessage(p, ts))
				}
			}
		case "shell":
			if strings.TrimSpace(m.Text) != "" {
				all = append(all, protocol.Message{Role: "tool", Kind: "tool_result", ToolOutput: m.Text, Timestamp: ts})
			}
		}
	}
	return pageMessages(all, offset, limit)
}

func toolMessage(p opencodePart, ts time.Time) protocol.Message {
	input := string(p.State.Input)
	if input == "" {
		input = p.State.Title
	}
	name := p.Tool
	if name == "" {
		name = p.Name
	}
	return protocol.Message{
		Role: "assistant", Kind: "tool_call", ToolName: name, ToolCallID: p.ID,
		ToolInput: truncate(input, 8000), ToolOutput: truncate(toolOutputText(p), 8000), Timestamp: ts,
	}
}

// toolOutputText prefers the part content and falls back to the fields older
// stores used. The live event path caps this at 8000 bytes; this helper keeps
// the full text for the on-demand tool output endpoint.
func toolOutputText(p opencodePart) string {
	var out strings.Builder
	for _, item := range p.State.Content {
		if item.Text != "" {
			out.WriteString(item.Text)
		}
	}
	if out.Len() == 0 && len(p.State.Output) > 0 {
		var s string
		if json.Unmarshal(p.State.Output, &s) == nil {
			out.WriteString(s)
		} else {
			out.Write(p.State.Output)
		}
	}
	if out.Len() == 0 {
		out.WriteString(p.State.Metadata.Output)
	}
	if out.Len() == 0 && len(p.State.Error) > 0 {
		out.WriteString(rawErrorText(p.State.Error))
	}
	return out.String()
}

// ToolOutput returns one tool call's full output. The transcript and the live
// event stream carry only a preview; the web client asks for the rest when the
// user expands a tool card.
func (a *OpenCodeAdapter) ToolOutput(ctx context.Context, nativeID, callID string) (string, error) {
	db, err := a.open()
	if err != nil {
		return "", err
	}
	defer db.Close()
	if a.tableExists(db, "session_v2") && a.tableExists(db, "session_part") {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM session_v2 WHERE id=?`, nativeID).Scan(&exists); err == nil && exists > 0 {
			return a.partOutput(ctx, db, "session_part", nativeID, callID)
		}
	}
	if a.tableExists(db, "part") {
		return a.partOutput(ctx, db, "part", nativeID, callID)
	}
	return "", fmt.Errorf("no tool output store for %s", nativeID)
}

func (a *OpenCodeAdapter) partOutput(ctx context.Context, db *sql.DB, table, nativeID, callID string) (string, error) {
	rows, err := db.QueryContext(ctx, `SELECT data FROM `+table+` WHERE session_id=?`, nativeID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return "", err
		}
		var p opencodePart
		if json.Unmarshal([]byte(data), &p) == nil && p.ID == callID {
			return toolOutputText(p), nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("tool call %s not found in %s", callID, nativeID)
}

func rawErrorText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Message string `json:"message"`
		Name    string `json:"name"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		if obj.Message != "" {
			return obj.Message
		}
		if obj.Name != "" {
			return obj.Name
		}
	}
	return string(raw)
}

func (a *OpenCodeAdapter) legacyTranscript(ctx context.Context, db *sql.DB, nativeID string, offset, limit int) (protocol.Transcript, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT m.id, m.time_created, m.data, COALESCE(p.time_created, 0), COALESCE(p.data, '')
		FROM message m LEFT JOIN part p ON p.message_id = m.id
		WHERE m.session_id = ?
		ORDER BY m.time_created, p.time_created`, nativeID)
	if err != nil {
		return protocol.Transcript{}, err
	}
	defer rows.Close()
	order := []string{}
	byID := map[string]*opencodeMessage{}
	times := map[string]time.Time{}
	for rows.Next() {
		var id, data, partData string
		var created, partCreated int64
		if err := rows.Scan(&id, &created, &data, &partCreated, &partData); err != nil {
			return protocol.Transcript{}, err
		}
		if _, ok := byID[id]; !ok {
			var meta struct {
				Role string `json:"role"`
			}
			_ = json.Unmarshal([]byte(data), &meta)
			byID[id] = &opencodeMessage{role: meta.Role}
			times[id] = time.UnixMilli(created).UTC()
			order = append(order, id)
		}
		if partData == "" {
			continue
		}
		var p opencodePart
		if json.Unmarshal([]byte(partData), &p) == nil {
			byID[id].parts = append(byID[id].parts, p)
		}
	}
	var all []protocol.Message
	for _, id := range order {
		msg := byID[id]
		ts := times[id]
		for _, p := range msg.parts {
			switch p.Type {
			case "text":
				if strings.TrimSpace(p.Text) != "" {
					all = append(all, protocol.Message{Role: msg.role, Kind: "text", Text: p.Text, Timestamp: ts})
				}
			case "reasoning":
				if strings.TrimSpace(p.Text) != "" {
					all = append(all, protocol.Message{Role: "assistant", Kind: "reasoning", Text: p.Text, Timestamp: ts})
				}
			case "tool":
				all = append(all, toolMessage(p, ts))
			case "patch":
				if len(p.Files) > 0 {
					all = append(all, protocol.Message{Role: "tool", Kind: "tool_result",
						ToolOutput: "files changed:\n" + strings.Join(p.Files, "\n"), Timestamp: ts})
				}
			}
		}
	}
	return pageMessages(all, offset, limit)
}

func (a *OpenCodeAdapter) Search(ctx context.Context, query string, limit int) ([]protocol.SearchHit, error) {
	db, err := a.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if limit <= 0 {
		limit = 50
	}
	seen := map[string]bool{}
	var hits []protocol.SearchHit
	if a.tableExists(db, "session_message") && a.tableExists(db, "session_v2") {
		rows, err := db.QueryContext(ctx, `
			SELECT s.id, COALESCE(s.title,''), m.data, m.time_created
			FROM session_message m JOIN session_v2 s ON s.id = m.session_id
			WHERE m.data LIKE ? ESCAPE '\' AND s.time_archived IS NULL
			ORDER BY m.time_created DESC LIMIT ?`, likePattern(query), limit)
		if err == nil {
			for rows.Next() {
				var id, title, data string
				var created int64
				if rows.Scan(&id, &title, &data, &created) == nil && !seen[id] {
					seen[id] = true
					hits = append(hits, protocol.SearchHit{
						NativeID: id, Agent: "opencode", Title: cleanTitle(title),
						Snippet: v2Snippet(data, query), At: time.UnixMilli(created).UTC(),
					})
				}
			}
			rows.Close()
		}
	}
	// 2.4 keeps assistant text and reasoning in session_part, so a message-only
	// query would miss everything the agent wrote.
	if a.tableExists(db, "session_part") && a.tableExists(db, "session_v2") {
		rows, err := db.QueryContext(ctx, `
			SELECT s.id, COALESCE(s.title,''), p.data, p.time_created
			FROM session_part p JOIN session_v2 s ON s.id = p.session_id
			WHERE p.data LIKE ? ESCAPE '\' AND s.time_archived IS NULL
			ORDER BY p.time_created DESC LIMIT ?`, likePattern(query), limit)
		if err == nil {
			for rows.Next() {
				var id, title, data string
				var created int64
				if rows.Scan(&id, &title, &data, &created) == nil && !seen[id] {
					seen[id] = true
					hits = append(hits, protocol.SearchHit{
						NativeID: id, Agent: "opencode", Title: cleanTitle(title),
						Snippet: v2Snippet(data, query), At: time.UnixMilli(created).UTC(),
					})
				}
			}
			rows.Close()
		}
	}
	if len(hits) == 0 && a.tableExists(db, "part") && a.tableExists(db, "session") {
		rows, err := db.QueryContext(ctx, `
			SELECT s.id, COALESCE(s.title,''), p.data, p.time_created
			FROM part p JOIN session s ON s.id = p.session_id
			WHERE p.data LIKE ? ESCAPE '\' AND s.time_archived IS NULL
			ORDER BY p.time_created DESC LIMIT ?`, likePattern(query), limit)
		if err != nil {
			return hits, nil
		}
		for rows.Next() {
			var id, title, data string
			var created int64
			if rows.Scan(&id, &title, &data, &created) == nil && !seen[id] {
				seen[id] = true
				var p opencodePart
				_ = json.Unmarshal([]byte(data), &p)
				text := p.Text
				if text == "" {
					text = string(p.State.Output)
				}
				if text == "" {
					text = p.State.Title
				}
				hits = append(hits, protocol.SearchHit{
					NativeID: id, Agent: "opencode", Title: cleanTitle(title),
					Snippet: snippetAround(text, query), At: time.UnixMilli(created).UTC(),
				})
			}
		}
		rows.Close()
	}
	return hits, nil
}

func v2Snippet(data, query string) string {
	var m v2Message
	if json.Unmarshal([]byte(data), &m) != nil {
		return ""
	}
	text := m.Text
	for _, p := range m.Content {
		if p.Type == "text" || p.Type == "reasoning" {
			text += " " + p.Text
		}
		if p.Type == "tool" {
			text += " " + string(p.State.Input)
			for _, c := range p.State.Content {
				text += " " + c.Text
			}
			if len(p.State.Error) > 0 {
				text += " " + rawErrorText(p.State.Error)
			}
		}
	}
	return snippetAround(text, query)
}

func snippetAround(text, query string) string {
	idx := strings.Index(strings.ToLower(text), strings.ToLower(query))
	if idx < 0 {
		return ""
	}
	start := idx - 80
	if start < 0 {
		start = 0
	}
	end := idx + len(query) + 120
	if end > len(text) {
		end = len(text)
	}
	return strings.TrimSpace(strings.ReplaceAll(text[start:end], "\n", " "))
}

func (a *OpenCodeAdapter) BuildCommand(action Action) (Command, error) {
	var args []string
	switch action.Kind {
	case protocol.ActionResume, "":
		if action.NativeID == "" {
			return Command{}, fmt.Errorf("native session id is required")
		}
		args = []string{"--session", action.NativeID}
	case protocol.ActionNew:
	case protocol.ActionFork:
		return Command{}, fmt.Errorf("opencode does not expose a non-interactive fork command; open the session and fork inside OpenCode")
	default:
		return Command{}, fmt.Errorf("unsupported action %q", action.Kind)
	}
	path, argv, err := commandFor(a.bin, args)
	if err != nil {
		return Command{}, err
	}
	return Command{Path: path, Args: argv, Cwd: action.Cwd}, nil
}
