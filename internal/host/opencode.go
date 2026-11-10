package host

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	name, args := commandFor(bin, []string{"debug", "paths"})
	out, err := execFor(name, args...).Output()
	_ = ctx
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

func (a *OpenCodeAdapter) Detect(context.Context) (protocol.AgentInfo, error) {
	info, err := detectInfo("opencode", a.bin)
	if err != nil {
		return info, err
	}
	if st, err := os.Stat(a.db); err != nil || st.IsDir() {
		info.Supported = false
		info.Capabilities = []string{"terminal"}
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
	rows, err := db.QueryContext(ctx, `
		SELECT s.id, s.title, s.directory, s.time_created, s.time_updated,
		(SELECT COUNT(*) FROM message m WHERE m.session_id = s.id)
		FROM session s
		WHERE s.time_archived IS NULL
		ORDER BY s.time_updated DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Session
	for rows.Next() {
		var s protocol.Session
		var created, updated int64
		if err := rows.Scan(&s.NativeID, &s.Title, &s.Cwd, &created, &updated, &s.MessageCount); err != nil {
			return nil, err
		}
		s.Agent = "opencode"
		s.State = protocol.StateIdle
		s.CreatedAt = time.UnixMilli(created).UTC()
		s.UpdatedAt = time.UnixMilli(updated).UTC()
		s.Title = cleanTitle(s.Title)
		if s.Title == "" {
			s.Title = filepath.Base(s.Cwd)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type opencodeMessage struct {
	role  string
	parts []opencodePart
}

type opencodePart struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Tool  string `json:"tool"`
	State struct {
		Status string          `json:"status"`
		Title  string          `json:"title"`
		Input  json.RawMessage `json:"input"`
		Output json.RawMessage `json:"output"`
	} `json:"state"`
	Files []string `json:"files"`
}

func (a *OpenCodeAdapter) Transcript(ctx context.Context, nativeID string, offset, limit int) (protocol.Transcript, error) {
	db, err := a.open()
	if err != nil {
		return protocol.Transcript{}, err
	}
	defer db.Close()
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
			case "tool":
				input := string(p.State.Input)
				if input == "" {
					input = p.State.Title
				}
				out := string(p.State.Output)
				var outStr string
				if json.Unmarshal(p.State.Output, &outStr) == nil {
					out = outStr
				}
				all = append(all, protocol.Message{
					Role: "assistant", Kind: "tool_call", ToolName: p.Tool,
					ToolInput: truncate(input, 8000), ToolOutput: truncate(out, 8000), Timestamp: ts,
				})
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
	rows, err := db.QueryContext(ctx, `
		SELECT s.id, s.title, p.data, p.time_created
		FROM part p JOIN session s ON s.id = p.session_id
		WHERE p.data LIKE ? AND s.time_archived IS NULL
		ORDER BY p.time_created DESC LIMIT ?`, "%"+query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []protocol.SearchHit
	for rows.Next() {
		var id, title, data string
		var created int64
		if err := rows.Scan(&id, &title, &data, &created); err != nil {
			return nil, err
		}
		var p opencodePart
		_ = json.Unmarshal([]byte(data), &p)
		text := p.Text
		if text == "" {
			text = string(p.State.Output)
		}
		if text == "" {
			text = p.State.Title
		}
		idx := strings.Index(strings.ToLower(text), strings.ToLower(query))
		start := idx - 80
		if start < 0 {
			start = 0
		}
		end := idx + len(query) + 120
		if end > len(text) {
			end = len(text)
		}
		snippet := ""
		if idx >= 0 && start < end {
			snippet = strings.TrimSpace(strings.ReplaceAll(text[start:end], "\n", " "))
		}
		hits = append(hits, protocol.SearchHit{
			NativeID: id, Agent: "opencode", Title: cleanTitle(title),
			Snippet: snippet, At: time.UnixMilli(created).UTC(),
		})
	}
	return hits, rows.Err()
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
	path, argv := commandFor(a.bin, args)
	return Command{Path: path, Args: argv, Cwd: action.Cwd}, nil
}
