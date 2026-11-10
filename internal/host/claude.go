package host

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// ClaudeAdapter discovers Claude Code sessions from its JSONL store and
// starts the native CLI for terminal runs.
type ClaudeAdapter struct {
	bin  string
	root string
}

func NewClaudeAdapter(bin string) *ClaudeAdapter {
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".claude")
	}
	return &ClaudeAdapter{bin: bin, root: root}
}

func (a *ClaudeAdapter) Name() string { return "claude" }

func (a *ClaudeAdapter) Detect(context.Context) (protocol.AgentInfo, error) {
	info, err := detectInfo("claude", a.bin)
	if err != nil {
		return info, err
	}
	if st, err := os.Stat(a.root); err != nil || !st.IsDir() {
		info.Supported = false
		info.Capabilities = []string{"terminal"}
	}
	return info, nil
}

type claudeEntry struct {
	Type      string         `json:"type"`
	Summary   string         `json:"summary"`
	SessionID string         `json:"sessionId"`
	Cwd       string         `json:"cwd"`
	GitBranch string         `json:"gitBranch"`
	Timestamp string         `json:"timestamp"`
	IsMeta    bool           `json:"isMeta"`
	IsSide    bool           `json:"isSidechain"`
	Message   *claudeMessage `json:"message"`
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func (a *ClaudeAdapter) ListSessions(ctx context.Context) ([]protocol.Session, error) {
	projects := filepath.Join(a.root, "projects")
	entries, err := os.ReadDir(projects)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []protocol.Session
	for _, project := range entries {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if !project.IsDir() {
			continue
		}
		dir := filepath.Join(projects, project.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(dir, f.Name())
			nativeID := strings.TrimSuffix(f.Name(), ".jsonl")
			sub := ""
			if i := strings.Index(nativeID, "agent-"); i == 0 {
				// Subagent transcripts are not top-level sessions.
				continue
			}
			meta, err := a.scanMetadata(path, nativeID, sub)
			if err != nil {
				continue
			}
			out = append(out, meta)
		}
	}
	return out, nil
}

func (a *ClaudeAdapter) scanMetadata(path, nativeID, _ string) (protocol.Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return protocol.Session{}, err
	}
	defer f.Close()
	st, _ := f.Stat()
	s := protocol.Session{
		NativeID: nativeID,
		Agent:    "claude",
		State:    protocol.StateIdle,
		Cwd:      filepath.Dir(path),
		Title:    "",
	}
	var firstTS, lastTS time.Time
	var branch string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	for sc.Scan() {
		var e claudeEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		if e.Cwd != "" {
			s.Cwd = e.Cwd
		}
		if e.GitBranch != "" {
			branch = e.GitBranch
		}
		if t := parseTime(e.Timestamp); !t.IsZero() {
			if firstTS.IsZero() {
				firstTS = t
			}
			lastTS = t
		}
		if e.Summary != "" && s.Title == "" {
			s.Title = usableTitle(e.Summary)
		}
		if e.Message == nil || e.IsMeta || e.IsSide {
			continue
		}
		switch e.Message.Role {
		case "user", "assistant":
			s.MessageCount++
			if s.Title == "" && e.Message.Role == "user" {
				texts, _, _ := claudeContent(e.Message.Content)
				for _, text := range texts {
					if t := usableTitle(text); t != "" {
						s.Title = t
						break
					}
				}
			}
		}
	}
	s.GitBranch = branch
	if st != nil {
		s.UpdatedAt = st.ModTime().UTC()
	}
	if !lastTS.IsZero() {
		s.UpdatedAt = lastTS
	}
	s.CreatedAt = firstTS
	if s.CreatedAt.IsZero() {
		s.CreatedAt = s.UpdatedAt
	}
	if s.Title == "" {
		s.Title = filepath.Base(s.Cwd)
	}
	return s, nil
}

func (a *ClaudeAdapter) Transcript(ctx context.Context, nativeID string, offset, limit int) (protocol.Transcript, error) {
	path, err := a.findFile(nativeID)
	if err != nil {
		return protocol.Transcript{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return protocol.Transcript{}, err
	}
	defer f.Close()
	var all []protocol.Message
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	for sc.Scan() {
		var e claudeEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Message == nil || e.IsMeta || e.IsSide {
			continue
		}
		ts := parseTime(e.Timestamp)
		texts, tools, results := claudeContent(e.Message.Content)
		role := e.Message.Role
		for _, text := range texts {
			if strings.TrimSpace(text) == "" {
				continue
			}
			all = append(all, protocol.Message{Role: role, Kind: "text", Text: text, Timestamp: ts})
		}
		all = append(all, tools...)
		all = append(all, results...)
	}
	return pageMessages(all, offset, limit)
}

func (a *ClaudeAdapter) findFile(nativeID string) (string, error) {
	projects := filepath.Join(a.root, "projects")
	dirs, _ := os.ReadDir(projects)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		p := filepath.Join(projects, d.Name(), nativeID+".jsonl")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("claude session %s not found", nativeID)
}

func (a *ClaudeAdapter) BuildCommand(action Action) (Command, error) {
	var args []string
	switch action.Kind {
	case protocol.ActionResume, "":
		if action.NativeID == "" {
			return Command{}, fmt.Errorf("native session id is required")
		}
		args = []string{"--resume", action.NativeID}
	case protocol.ActionFork:
		if action.NativeID == "" {
			return Command{}, fmt.Errorf("native session id is required")
		}
		args = []string{"--resume", action.NativeID, "--fork-session"}
	case protocol.ActionNew:
	default:
		return Command{}, fmt.Errorf("unsupported action %q", action.Kind)
	}
	path, argv := commandFor(a.bin, args)
	return Command{Path: path, Args: argv, Cwd: action.Cwd}, nil
}

func (a *ClaudeAdapter) Search(ctx context.Context, query string, limit int) ([]protocol.SearchHit, error) {
	return searchJSONL(ctx, filepath.Join(a.root, "projects"), "claude", query, limit, func(line []byte) (string, time.Time) {
		var e claudeEntry
		if json.Unmarshal(line, &e) != nil || e.Message == nil {
			return "", time.Time{}
		}
		texts, _, _ := claudeContent(e.Message.Content)
		return strings.Join(texts, " "), parseTime(e.Timestamp)
	})
}

func claudeContent(raw json.RawMessage) (texts []string, tools []protocol.Message, results []protocol.Message) {
	if len(raw) == 0 {
		return nil, nil, nil
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return []string{s}, nil, nil
		}
		return nil, nil, nil
	}
	var blocks []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Name    string          `json:"name"`
		Input   json.RawMessage `json:"input"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return nil, nil, nil
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			texts = append(texts, b.Text)
		case "tool_use":
			tools = append(tools, protocol.Message{
				Role: "assistant", Kind: "tool_call", ToolName: b.Name,
				ToolInput: truncate(string(b.Input), 8000),
			})
		case "tool_result":
			out := string(b.Content)
			if len(b.Content) > 0 && b.Content[0] == '[' {
				inner, _, _ := claudeContent(b.Content)
				out = strings.Join(inner, "\n")
			} else {
				var s string
				if json.Unmarshal(b.Content, &s) == nil {
					out = s
				}
			}
			results = append(results, protocol.Message{Role: "tool", Kind: "tool_result", ToolOutput: truncate(out, 8000)})
		}
	}
	return
}

func cleanTitle(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return s
}

// usableTitle filters injected context blocks that some agents put in the
// first "user" message. A title should describe the human's request, not the
// desktop app's plugin catalogue.
func usableTitle(s string) string {
	t := strings.TrimSpace(s)
	lower := strings.ToLower(t)
	if t == "" {
		return ""
	}
	if strings.HasPrefix(t, "<") {
		if i := strings.LastIndex(t, ">"); i >= 0 {
			t = strings.TrimSpace(t[i+1:])
		}
	}
	if t == "" || strings.HasPrefix(t, "<") {
		return ""
	}
	if strings.Contains(lower, "<recommended_plugins>") || strings.Contains(lower, "here is a list of plugins") {
		return ""
	}
	if strings.HasPrefix(lower, "you are ") || strings.HasPrefix(lower, "# agents.md") || strings.HasPrefix(lower, "<user_action>") {
		return ""
	}
	t = cleanTitle(t)
	if len(t) < 2 {
		return ""
	}
	return t
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n... (truncated)"
}

func parseTime(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
