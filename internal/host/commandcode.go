package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// CommandCodeAdapter discovers Command Code sessions and starts the native CLI.
// Structured runs use the generic ACP driver through `command-code acp`.
type CommandCodeAdapter struct {
	bin   string
	root  string
	cache metadataCache
}

func NewCommandCodeAdapter(bin string) *CommandCodeAdapter {
	root := strings.TrimSpace(os.Getenv("COMMANDCODE_HOME"))
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".commandcode")
	}
	return &CommandCodeAdapter{bin: bin, root: root}
}

func (a *CommandCodeAdapter) Name() string { return "commandcode" }

func (a *CommandCodeAdapter) WatchPaths() []string {
	return []string{filepath.Join(a.root, "projects")}
}

func (a *CommandCodeAdapter) Detect(context.Context) (protocol.AgentInfo, error) {
	info, err := detectInfo("commandcode", a.bin)
	if err != nil {
		return info, err
	}
	if st, err := os.Stat(filepath.Join(a.root, "projects")); err != nil || !st.IsDir() {
		info.Capabilities = []string{"terminal"}
	}
	return info, nil
}

type commandCodeHeader struct {
	Type      string `json:"type"`
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
}

type commandCodeMeta struct {
	Title           string `json:"title"`
	ParentSessionID string `json:"parentSessionId"`
	ParentSession   string `json:"parentSession"`
}

type commandCodeContent struct {
	Type      string               `json:"type"`
	Text      string               `json:"text"`
	Thinking  string               `json:"thinking"`
	ID        string               `json:"id"`
	Name      string               `json:"name"`
	Input     json.RawMessage      `json:"input"`
	ToolUseID string               `json:"tool_use_id"`
	Content   []commandCodeContent `json:"content"`
}

type commandCodeLine struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	ParentID  string `json:"parentId"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role    string               `json:"role"`
		Content []commandCodeContent `json:"content"`
	} `json:"message"`
}

func commandCodeTranscriptFile(name string) bool {
	if !strings.HasSuffix(name, ".jsonl") {
		return false
	}
	return !strings.HasSuffix(name, ".checkpoints.jsonl") && !strings.HasSuffix(name, ".prompts.jsonl")
}

func (a *CommandCodeAdapter) ListSessions(ctx context.Context) ([]protocol.Session, error) {
	root := filepath.Join(a.root, "projects")
	var out []protocol.Session
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !commandCodeTranscriptFile(d.Name()) {
			return nil
		}
		session, err := a.scanMetadata(path)
		if err != nil {
			return nil
		}
		out = append(out, session)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return out, err
}

func (a *CommandCodeAdapter) scanMetadata(path string) (protocol.Session, error) {
	st, err := os.Stat(path)
	if err != nil {
		return protocol.Session{}, err
	}
	if cached, ok := a.cache.get(path, st.ModTime(), st.Size()); ok {
		return cached, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return protocol.Session{}, err
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 64*1024), 16<<20)

	session := protocol.Session{Agent: "commandcode", State: protocol.StateIdle}
	var firstTS, lastTS time.Time
	if !sc.Scan() {
		return protocol.Session{}, errors.New("empty command code session")
	}
	var header commandCodeHeader
	if json.Unmarshal(sc.Bytes(), &header) != nil || header.Type != "session" {
		return protocol.Session{}, errors.New("not a command code session")
	}
	session.NativeID = header.ID
	session.Cwd = header.Cwd
	firstTS = parseTime(header.Timestamp)
	for sc.Scan() {
		var line commandCodeLine
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "message" {
			continue
		}
		if line.Message.Role != "user" && line.Message.Role != "assistant" {
			continue
		}
		session.MessageCount++
		if ts := parseTime(line.Timestamp); !ts.IsZero() {
			lastTS = ts
		}
		if session.Title == "" && line.Message.Role == "user" {
			for _, content := range line.Message.Content {
				if content.Type != "text" {
					continue
				}
				if title := usableTitle(content.Text); title != "" {
					session.Title = title
					break
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		session.Warning = "The native session file could not be read completely; message counts and the title are partial."
	}
	if session.NativeID == "" {
		session.NativeID = sessionIDFromFilename(path)
	}
	if !validNativeID(session.NativeID) {
		return protocol.Session{}, errors.New("invalid command code session id")
	}
	if session.Cwd == "" {
		return protocol.Session{}, errors.New("no cwd in command code session")
	}
	metaPath := strings.TrimSuffix(path, ".jsonl") + ".meta.json"
	if raw, err := os.ReadFile(metaPath); err == nil {
		var meta commandCodeMeta
		if json.Unmarshal(raw, &meta) == nil {
			if title := cleanTitle(meta.Title); title != "" {
				session.Title = title
			}
			session.ForkedFromNativeID = strings.TrimSpace(meta.ParentSessionID)
			if session.ForkedFromNativeID == "" {
				candidate := strings.TrimSpace(meta.ParentSession)
				if validNativeID(candidate) {
					session.ForkedFromNativeID = candidate
				}
			}
		}
	}
	session.UpdatedAt = st.ModTime().UTC()
	if !lastTS.IsZero() {
		session.UpdatedAt = lastTS
	}
	session.CreatedAt = firstTS
	if session.CreatedAt.IsZero() {
		session.CreatedAt = session.UpdatedAt
	}
	if session.Title == "" && session.Cwd != "" {
		session.Title = filepath.Base(session.Cwd)
	}
	if session.Title == "" {
		session.Title = session.NativeID
	}
	a.cache.put(path, st.ModTime(), st.Size(), session)
	return session, nil
}

func (a *CommandCodeAdapter) findFile(nativeID string) (string, error) {
	root := filepath.Join(a.root, "projects")
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if d.Name() == nativeID+".jsonl" && commandCodeTranscriptFile(d.Name()) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if found == "" {
		return "", errors.New("command code session not found")
	}
	return found, nil
}

func (a *CommandCodeAdapter) Transcript(ctx context.Context, nativeID string, offset, limit int) (protocol.Transcript, error) {
	if !validNativeID(nativeID) {
		return protocol.Transcript{}, errors.New("invalid command code session id")
	}
	path, err := a.findFile(nativeID)
	if err != nil {
		return protocol.Transcript{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return protocol.Transcript{}, err
	}
	defer file.Close()
	var messages []protocol.Message
	toolNames := map[string]string{}
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	for sc.Scan() {
		if ctx.Err() != nil {
			return protocol.Transcript{}, ctx.Err()
		}
		var line commandCodeLine
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "message" {
			continue
		}
		ts := parseTime(line.Timestamp)
		for _, content := range line.Message.Content {
			switch line.Message.Role {
			case "user":
				switch content.Type {
				case "text":
					if strings.TrimSpace(content.Text) != "" {
						messages = append(messages, protocol.Message{Role: "user", Kind: "text", Text: content.Text, Timestamp: ts})
					}
				case "tool_result":
					output := commandCodeContentText(content.Content)
					if output != "" {
						messages = append(messages, protocol.Message{
							Role: "tool", Kind: "tool_result", ToolName: toolNames[content.ToolUseID],
							ToolOutput: output, Timestamp: ts,
						})
					}
				}
			case "assistant":
				switch content.Type {
				case "thinking":
					if strings.TrimSpace(content.Thinking) != "" {
						messages = append(messages, protocol.Message{Role: "assistant", Kind: "reasoning", Text: content.Thinking, Timestamp: ts})
					}
				case "text":
					if strings.TrimSpace(content.Text) != "" {
						messages = append(messages, protocol.Message{Role: "assistant", Kind: "text", Text: content.Text, Timestamp: ts})
					}
				case "tool_use":
					if content.ID != "" && content.Name != "" {
						toolNames[content.ID] = content.Name
					}
					messages = append(messages, protocol.Message{
						Role: "assistant", Kind: "tool_call", ToolName: content.Name,
						ToolInput: string(content.Input), Timestamp: ts,
					})
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return protocol.Transcript{}, err
	}
	return pageMessages(messages, offset, limit)
}

func commandCodeContentText(content []commandCodeContent) string {
	var out strings.Builder
	for _, item := range content {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			if out.Len() > 0 {
				out.WriteString("\n")
			}
			out.WriteString(item.Text)
		}
	}
	return out.String()
}

func commandCodeSearchText(line []byte) (string, time.Time) {
	var entry commandCodeLine
	if json.Unmarshal(line, &entry) != nil || entry.Type != "message" {
		return "", time.Time{}
	}
	ts := parseTime(entry.Timestamp)
	var out strings.Builder
	appendPart := func(value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(value)
	}
	for _, content := range entry.Message.Content {
		switch content.Type {
		case "text":
			appendPart(content.Text)
		case "thinking":
			appendPart(content.Thinking)
		case "tool_use":
			appendPart(content.Name + " " + string(content.Input))
		case "tool_result":
			appendPart(commandCodeContentText(content.Content))
		}
	}
	return strings.TrimSpace(out.String()), ts
}

func commandCodeTitle(path string) string {
	metaPath := strings.TrimSuffix(path, ".jsonl") + ".meta.json"
	if raw, err := os.ReadFile(metaPath); err == nil {
		var meta commandCodeMeta
		if json.Unmarshal(raw, &meta) == nil {
			return cleanTitle(meta.Title)
		}
	}
	return ""
}

func (a *CommandCodeAdapter) Search(ctx context.Context, query string, limit int) ([]protocol.SearchHit, error) {
	if limit <= 0 {
		limit = 50
	}
	needle := strings.ToLower(query)
	var hits []protocol.SearchHit
	root := filepath.Join(a.root, "projects")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(hits) >= limit {
			return filepath.SkipAll
		}
		if !commandCodeTranscriptFile(d.Name()) {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()
		nativeID := sessionIDFromFilename(path)
		title := commandCodeTitle(path)
		sc := bufio.NewScanner(file)
		sc.Buffer(make([]byte, 64*1024), 16<<20)
		for sc.Scan() {
			line := sc.Bytes()
			if !strings.Contains(strings.ToLower(string(line)), needle) {
				continue
			}
			text, ts := commandCodeSearchText(line)
			if strings.TrimSpace(text) == "" {
				continue
			}
			hits = append(hits, protocol.SearchHit{
				NativeID: nativeID, Agent: "commandcode", Title: title,
				Snippet: searchSnippet(text, query), At: ts,
			})
			if len(hits) >= limit {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return hits, err
}

func (a *CommandCodeAdapter) BuildCommand(action Action) (Command, error) {
	var args []string
	switch action.Kind {
	case protocol.ActionResume, "":
		if action.NativeID == "" {
			return Command{}, fmt.Errorf("native session id is required")
		}
		args = []string{"--resume", action.NativeID}
	case protocol.ActionNew:
	case protocol.ActionFork:
		if action.NativeID == "" {
			return Command{}, fmt.Errorf("native session id is required")
		}
		args = []string{"--resume", action.NativeID, "--fork-session"}
	default:
		return Command{}, fmt.Errorf("unsupported action %q", action.Kind)
	}
	path, argv, err := commandFor(a.bin, args)
	if err != nil {
		return Command{}, err
	}
	return Command{Path: path, Args: argv, Cwd: action.Cwd}, nil
}
