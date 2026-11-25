package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// CursorAdapter discovers Cursor agent transcripts and starts the Cursor CLI.
// Structured runs use the generic ACP driver through `cursor-agent acp`.
//
// Cursor stores desktop and background-agent transcripts under
// ~/.cursor/projects/<encoded-workspace>/agent-transcripts/<session>/<session>.jsonl.
// The folder name is a lossy encoding of the workspace path, so the working
// directory is read from a terminal transcript when one exists and decoded from
// the folder name as a last resort.
type CursorAdapter struct {
	bin   string
	root  string
	cache metadataCache
}

func NewCursorAdapter(bin string) *CursorAdapter {
	home, _ := os.UserHomeDir()
	return &CursorAdapter{bin: bin, root: filepath.Join(home, ".cursor", "projects")}
}

func (a *CursorAdapter) Name() string { return "cursor" }

func (a *CursorAdapter) WatchPaths() []string { return []string{a.root} }

func (a *CursorAdapter) Detect(context.Context) (protocol.AgentInfo, error) {
	if a.bin == "" {
		if !a.storeExists() {
			return protocol.AgentInfo{Name: "cursor", Version: "unknown", Supported: false, Capabilities: []string{"terminal"}}, os.ErrNotExist
		}
		return protocol.AgentInfo{
			Name: "cursor", Version: "unknown", Supported: false,
			Capabilities: []string{"discovery", "transcript"},
		}, nil
	}
	info, err := detectInfo("cursor", a.bin)
	if err != nil {
		return info, err
	}
	if !a.storeExists() {
		info.Supported = false
		info.Capabilities = []string{"terminal"}
	}
	return info, nil
}

func (a *CursorAdapter) storeExists() bool {
	st, err := os.Stat(a.root)
	return err == nil && st.IsDir()
}

type cursorContent struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
}

type cursorLine struct {
	Role    string `json:"role"`
	Message struct {
		Content []cursorContent `json:"content"`
	} `json:"message"`
}

// cursorTranscripts returns every main transcript in the projects store.
// Subagent transcripts live one directory deeper and are not sessions.
func (a *CursorAdapter) cursorTranscripts() []string {
	paths, err := filepath.Glob(filepath.Join(a.root, "*", "agent-transcripts", "*", "*.jsonl"))
	if err != nil {
		return nil
	}
	out := paths[:0]
	for _, path := range paths {
		id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		if id == "" || filepath.Base(filepath.Dir(path)) != id {
			continue
		}
		out = append(out, path)
	}
	return out
}

func cursorTranscriptPath(root, nativeID string) (string, error) {
	paths, err := filepath.Glob(filepath.Join(root, "*", "agent-transcripts", nativeID, nativeID+".jsonl"))
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", errors.New("cursor session not found")
	}
	return paths[0], nil
}

func (a *CursorAdapter) ListSessions(ctx context.Context) ([]protocol.Session, error) {
	var out []protocol.Session
	for _, path := range a.cursorTranscripts() {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		session, err := a.scanMetadata(path)
		if err != nil {
			continue
		}
		out = append(out, session)
	}
	return out, nil
}

func (a *CursorAdapter) scanMetadata(path string) (protocol.Session, error) {
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

	projectDir := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	session := protocol.Session{
		NativeID:  strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		Agent:     "cursor",
		State:     protocol.StateIdle,
		CreatedAt: st.ModTime().UTC(),
		UpdatedAt: st.ModTime().UTC(),
	}
	if !validNativeID(session.NativeID) {
		return protocol.Session{}, errors.New("invalid cursor session id")
	}
	session.Cwd, session.Warning = cursorProjectCwd(projectDir)

	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	for sc.Scan() {
		var line cursorLine
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		if line.Role != "user" && line.Role != "assistant" {
			continue
		}
		session.MessageCount++
		if session.Title == "" && line.Role == "user" {
			session.Title = cursorTitleFromContent(line.Message.Content)
		}
	}
	if session.Title == "" {
		session.Title = cleanTitle(filepath.Base(session.Cwd))
	}
	if session.Title == "" {
		session.Title = session.NativeID
	}
	a.cache.put(path, st.ModTime(), st.Size(), session)
	return session, nil
}

func cursorTitleFromContent(content []cursorContent) string {
	for _, part := range content {
		if part.Type != "text" {
			continue
		}
		if title := usableTitle(cursorUserText(part.Text)); title != "" {
			return title
		}
	}
	return ""
}

// cursorUserText removes the timestamp and user_query wrappers Cursor puts
// around prompts in its transcript.
func cursorUserText(text string) string {
	t := strings.TrimSpace(text)
	if start := strings.Index(t, "<user_query>"); start >= 0 {
		rest := t[start+len("<user_query>"):]
		if end := strings.Index(rest, "</user_query>"); end >= 0 {
			rest = rest[:end]
		}
		return strings.TrimSpace(rest)
	}
	if strings.HasPrefix(t, "<timestamp>") {
		if end := strings.Index(t, "</timestamp>"); end >= 0 {
			return strings.TrimSpace(t[end+len("</timestamp>"):])
		}
	}
	return t
}

// cursorProjectCwd prefers a cwd recorded in a terminal transcript because the
// project folder name replaces path separators and hyphens with the same
// character. Decoding the folder name is only a fallback.
func cursorProjectCwd(projectDir string) (string, string) {
	terminals, _ := filepath.Glob(filepath.Join(projectDir, "terminals", "*.txt"))
	sort.Slice(terminals, func(i, j int) bool {
		left, err := os.Stat(terminals[i])
		if err != nil {
			return false
		}
		right, err := os.Stat(terminals[j])
		if err != nil {
			return true
		}
		return left.ModTime().After(right.ModTime())
	})
	for i, path := range terminals {
		if i >= 20 {
			break
		}
		if cwd := cursorTerminalCwd(path); cwd != "" {
			return cwd, ""
		}
	}
	return cursorDecodeProject(filepath.Base(projectDir)),
		"Cursor does not record the workspace path in this transcript; the working directory was decoded from the project folder and may be wrong for paths containing hyphens. Verify it before resuming."
}

func cursorTerminalCwd(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	head, err := io.ReadAll(io.LimitReader(file, 4096))
	if err != nil {
		return ""
	}
	text := string(head)
	const marker = `cwd: "`
	idx := strings.Index(text, marker)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	value, err := strconv.Unquote(`"` + rest[:end] + `"`)
	if err != nil || !cursorAbsolutePath(value) {
		return ""
	}
	return value
}

// cursorAbsolutePath accepts the host's native absolute paths and Windows
// drive/UNC paths, so a transcript written on Windows can be parsed by a test
// or tool running on another OS.
func cursorAbsolutePath(value string) bool {
	if filepath.IsAbs(value) {
		return true
	}
	if strings.HasPrefix(value, "/") {
		return true
	}
	if len(value) >= 3 && value[1] == ':' && (value[2] == '\\' || value[2] == '/') {
		return (value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')
	}
	return strings.HasPrefix(value, `\\`)
}

func cursorDecodeProject(name string) string {
	if len(name) > 2 && name[1] == '-' {
		return strings.ToUpper(name[:1]) + `:\` + strings.ReplaceAll(name[2:], "-", `\`)
	}
	if strings.HasPrefix(name, "-") {
		return strings.ReplaceAll(name, "-", "/")
	}
	return strings.ReplaceAll(name, "-", string(filepath.Separator))
}

func (a *CursorAdapter) Transcript(ctx context.Context, nativeID string, offset, limit int) (protocol.Transcript, error) {
	if !validNativeID(nativeID) {
		return protocol.Transcript{}, errors.New("invalid cursor session id")
	}
	path, err := cursorTranscriptPath(a.root, nativeID)
	if err != nil {
		return protocol.Transcript{}, err
	}
	st, _ := os.Stat(path)
	at := time.Time{}
	if st != nil {
		at = st.ModTime().UTC()
	}
	file, err := os.Open(path)
	if err != nil {
		return protocol.Transcript{}, err
	}
	defer file.Close()

	var messages []protocol.Message
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	for sc.Scan() {
		if ctx.Err() != nil {
			return protocol.Transcript{}, ctx.Err()
		}
		var line cursorLine
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		switch line.Role {
		case "user":
			for _, part := range line.Message.Content {
				if part.Type != "text" {
					continue
				}
				if text := strings.TrimSpace(cursorUserText(part.Text)); text != "" {
					messages = append(messages, protocol.Message{Role: "user", Kind: "text", Text: text, Timestamp: at})
				}
			}
		case "assistant":
			for _, part := range line.Message.Content {
				switch part.Type {
				case "text":
					if strings.TrimSpace(part.Text) != "" {
						messages = append(messages, protocol.Message{Role: "assistant", Kind: "text", Text: part.Text, Timestamp: at})
					}
				case "thinking":
					if strings.TrimSpace(part.Thinking) != "" {
						messages = append(messages, protocol.Message{Role: "assistant", Kind: "reasoning", Text: part.Thinking, Timestamp: at})
					}
				case "tool_use":
					messages = append(messages, protocol.Message{
						Role: "assistant", Kind: "tool_call", ToolName: part.Name,
						ToolInput: string(part.Input), Timestamp: at,
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

func (a *CursorAdapter) Search(ctx context.Context, query string, limit int) ([]protocol.SearchHit, error) {
	if limit <= 0 {
		limit = 50
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil, nil
	}
	var hits []protocol.SearchHit
	for _, path := range a.cursorTranscripts() {
		if ctx.Err() != nil {
			return hits, ctx.Err()
		}
		if len(hits) >= limit {
			break
		}
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		st, _ := file.Stat()
		nativeID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		title := ""
		sc := bufio.NewScanner(file)
		sc.Buffer(make([]byte, 64*1024), 16<<20)
		for sc.Scan() {
			if ctx.Err() != nil {
				file.Close()
				return hits, ctx.Err()
			}
			line := sc.Bytes()
			if !strings.Contains(strings.ToLower(string(line)), needle) {
				continue
			}
			var entry cursorLine
			if json.Unmarshal(line, &entry) != nil {
				continue
			}
			text := cursorSearchText(entry)
			if text == "" || !strings.Contains(strings.ToLower(text), needle) {
				continue
			}
			if title == "" {
				title = cursorFileTitle(path)
			}
			at := time.Time{}
			if st != nil {
				at = st.ModTime().UTC()
			}
			hits = append(hits, protocol.SearchHit{
				NativeID: nativeID, Agent: "cursor", Title: title,
				Snippet: searchSnippet(text, query), At: at,
			})
			if len(hits) >= limit {
				break
			}
		}
		file.Close()
	}
	return hits, nil
}

func cursorSearchText(line cursorLine) string {
	var out strings.Builder
	for _, part := range line.Message.Content {
		switch part.Type {
		case "text":
			text := part.Text
			if line.Role == "user" {
				text = cursorUserText(text)
			}
			if text != "" {
				if out.Len() > 0 {
					out.WriteString("\n")
				}
				out.WriteString(text)
			}
		case "thinking":
			if part.Thinking != "" {
				if out.Len() > 0 {
					out.WriteString("\n")
				}
				out.WriteString(part.Thinking)
			}
		case "tool_use":
			if part.Name != "" {
				if out.Len() > 0 {
					out.WriteString("\n")
				}
				out.WriteString(part.Name + " " + string(part.Input))
			}
		}
	}
	return strings.TrimSpace(out.String())
}

func cursorFileTitle(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	for sc.Scan() {
		var line cursorLine
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Role != "user" {
			continue
		}
		if title := cursorTitleFromContent(line.Message.Content); title != "" {
			return title
		}
	}
	return ""
}

func (a *CursorAdapter) BuildCommand(action Action) (Command, error) {
	if a.bin == "" {
		return Command{}, fmt.Errorf("the cursor-agent CLI is not installed; install it to resume sessions")
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
		return Command{}, fmt.Errorf("cursor does not expose a documented fork command")
	default:
		return Command{}, fmt.Errorf("unsupported action %q", action.Kind)
	}
	path, argv, err := commandFor(a.bin, args)
	if err != nil {
		return Command{}, err
	}
	return Command{Path: path, Args: argv, Cwd: action.Cwd}, nil
}
