package host

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// GeminiAdapter is a best-effort adapter for Gemini CLI. Its on-disk chat
// format is less stable than the other agents, so unknown files are skipped
// and never break indexing.
type GeminiAdapter struct {
	bin  string
	root string
}

func NewGeminiAdapter(bin string) *GeminiAdapter {
	home, _ := os.UserHomeDir()
	return &GeminiAdapter{bin: bin, root: filepath.Join(home, ".gemini", "tmp")}
}

func (a *GeminiAdapter) Name() string { return "gemini" }

func (a *GeminiAdapter) Detect(context.Context) (protocol.AgentInfo, error) {
	info, err := detectInfo("gemini", a.bin)
	if err != nil {
		return info, err
	}
	if st, err := os.Stat(a.root); err != nil || !st.IsDir() {
		info.Supported = false
		info.Capabilities = []string{"terminal"}
	}
	return info, nil
}

func (a *GeminiAdapter) files() []string {
	var out []string
	_ = filepath.WalkDir(a.root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".json") && strings.Contains(path, string(filepath.Separator)+"chats"+string(filepath.Separator)) {
			out = append(out, path)
		}
		return nil
	})
	return out
}

func (a *GeminiAdapter) ListSessions(ctx context.Context) ([]protocol.Session, error) {
	var out []protocol.Session
	for _, path := range a.files() {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		s, err := a.readSession(path)
		if err == nil {
			out = append(out, s)
		}
	}
	return out, nil
}

func (a *GeminiAdapter) readSession(path string) (protocol.Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return protocol.Session{}, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return protocol.Session{}, err
	}
	st, _ := os.Stat(path)
	s := protocol.Session{
		Agent: "gemini", State: protocol.StateIdle,
		NativeID: strings.TrimSuffix(filepath.Base(path), ".json"),
		Title:    filepath.Base(filepath.Dir(filepath.Dir(path))),
		Cwd:      homeDir(),
		Warning:  "Gemini stores chats per project hash; verify the working directory before resuming",
	}
	for _, key := range []string{"sessionId", "session_id", "id"} {
		if v, ok := doc[key].(string); ok && v != "" {
			s.NativeID = v
			break
		}
	}
	for _, key := range []string{"startTime", "start_time", "createdAt", "timestamp"} {
		if v, ok := doc[key].(string); ok {
			if t := parseTime(v); !t.IsZero() {
				s.CreatedAt = t
			}
		}
	}
	if st != nil {
		s.UpdatedAt = st.ModTime().UTC()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = s.UpdatedAt
	}
	if msgs, ok := doc["messages"].([]any); ok {
		s.MessageCount = len(msgs)
		for _, raw := range msgs {
			m, _ := raw.(map[string]any)
			role, _ := m["role"].(string)
			text := geminiText(m["content"])
			if role == "user" && s.Title == "" && text != "" {
				s.Title = cleanTitle(text)
			}
		}
	}
	if s.Title == "" {
		s.Title = "Gemini session"
	}
	return s, nil
}

func (a *GeminiAdapter) Transcript(ctx context.Context, nativeID string, offset, limit int) (protocol.Transcript, error) {
	for _, path := range a.files() {
		s, err := a.readSession(path)
		if err != nil || s.NativeID != nativeID {
			continue
		}
		data, _ := os.ReadFile(path)
		var doc map[string]any
		_ = json.Unmarshal(data, &doc)
		var all []protocol.Message
		if msgs, ok := doc["messages"].([]any); ok {
			for _, raw := range msgs {
				m, _ := raw.(map[string]any)
				role, _ := m["role"].(string)
				if role != "user" && role != "assistant" && role != "model" {
					continue
				}
				if role == "model" {
					role = "assistant"
				}
				text := geminiText(m["content"])
				if strings.TrimSpace(text) == "" {
					continue
				}
				all = append(all, protocol.Message{Role: role, Kind: "text", Text: text, Timestamp: s.UpdatedAt})
			}
		}
		return pageMessages(all, offset, limit)
	}
	return protocol.Transcript{}, fmt.Errorf("gemini session %s not found", nativeID)
}

func (a *GeminiAdapter) Search(ctx context.Context, query string, limit int) ([]protocol.SearchHit, error) {
	if limit <= 0 {
		limit = 50
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, nil
	}
	var hits []protocol.SearchHit
	for _, path := range a.files() {
		if ctx.Err() != nil {
			return hits, ctx.Err()
		}
		if len(hits) >= limit {
			break
		}
		s, err := a.readSession(path)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(data, &doc) != nil {
			continue
		}
		messages, _ := doc["messages"].([]any)
		for _, raw := range messages {
			m, _ := raw.(map[string]any)
			text := geminiText(m["content"])
			if text == "" || !strings.Contains(strings.ToLower(text), q) {
				continue
			}
			hits = append(hits, protocol.SearchHit{
				NativeID: s.NativeID, Agent: "gemini", Title: s.Title,
				Snippet: searchSnippet(text, query), At: s.UpdatedAt,
			})
			break
		}
	}
	return hits, nil
}

func (a *GeminiAdapter) BuildCommand(action Action) (Command, error) {
	var args []string
	switch action.Kind {
	case protocol.ActionResume, "":
		if action.NativeID == "" {
			return Command{}, fmt.Errorf("native session id is required")
		}
		args = []string{"--resume", action.NativeID}
	case protocol.ActionNew:
	case protocol.ActionFork:
		return Command{}, fmt.Errorf("gemini does not expose a documented fork command")
	default:
		return Command{}, fmt.Errorf("unsupported action %q", action.Kind)
	}
	path, argv := commandFor(a.bin, args)
	return Command{Path: path, Args: argv, Cwd: action.Cwd}, nil
}

func geminiText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var parts []string
		for _, item := range x {
			switch y := item.(type) {
			case string:
				parts = append(parts, y)
			case map[string]any:
				if t, ok := y["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if t, ok := x["text"].(string); ok {
			return t
		}
	}
	return ""
}
