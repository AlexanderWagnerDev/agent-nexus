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
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
	"github.com/klauspost/compress/zstd"
)

// CodexAdapter discovers Codex CLI rollouts and starts the native CLI.
type CodexAdapter struct {
	bin  string
	root string
}

func NewCodexAdapter(bin string) *CodexAdapter {
	root := os.Getenv("CODEX_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".codex")
	}
	return &CodexAdapter{bin: bin, root: root}
}

func (a *CodexAdapter) Name() string { return "codex" }

func (a *CodexAdapter) Detect(context.Context) (protocol.AgentInfo, error) {
	info, err := detectInfo("codex", a.bin)
	if err != nil {
		return info, err
	}
	if st, err := os.Stat(filepath.Join(a.root, "sessions")); err != nil || !st.IsDir() {
		info.Supported = false
		info.Capabilities = []string{"terminal"}
	}
	return info, nil
}

type codexLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexPayload struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	Timestamp string          `json:"timestamp"`
	Cwd       string          `json:"cwd"`
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Output    string          `json:"output"`
}

func (a *CodexAdapter) ListSessions(ctx context.Context) ([]protocol.Session, error) {
	root := filepath.Join(a.root, "sessions")
	var out []protocol.Session
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name := d.Name()
		if !strings.HasPrefix(name, "rollout-") || (!strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".jsonl.zst")) {
			return nil
		}
		s, err := a.scanMetadata(path)
		if err != nil {
			return nil
		}
		out = append(out, s)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return out, err
}

func (a *CodexAdapter) scanMetadata(path string) (protocol.Session, error) {
	rc, err := openSessionFile(path)
	if err != nil {
		return protocol.Session{}, err
	}
	defer rc.Close()
	st, _ := os.Stat(path)
	s := protocol.Session{
		Agent: "codex", State: protocol.StateIdle,
		NativeID: sessionIDFromFilename(path), Title: "",
	}
	var firstTS, lastTS time.Time
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	for sc.Scan() {
		var line codexLine
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		if line.Type == "session_meta" {
			var p codexPayload
			if json.Unmarshal(line.Payload, &p) == nil {
				if p.SessionID != "" {
					s.NativeID = p.SessionID
				} else if p.ID != "" {
					s.NativeID = p.ID
				}
				if p.Cwd != "" {
					s.Cwd = p.Cwd
				}
				if t := parseTime(p.Timestamp); !t.IsZero() {
					firstTS = t
				}
			}
			continue
		}
		if line.Type != "response_item" {
			continue
		}
		var p codexPayload
		if json.Unmarshal(line.Payload, &p) != nil {
			continue
		}
		if t := parseTime(line.Timestamp); !t.IsZero() {
			lastTS = t
		}
		if p.Type == "message" && (p.Role == "user" || p.Role == "assistant") {
			s.MessageCount++
			if p.Role == "user" && s.Title == "" {
				texts := codexContent(p.Content)
				for _, text := range texts {
					if t := usableTitle(text); t != "" {
						s.Title = t
						break
					}
				}
			}
		}
	}
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
	if s.Cwd == "" {
		return s, fmt.Errorf("no cwd in rollout")
	}
	if s.Title == "" {
		s.Title = filepath.Base(s.Cwd)
	}
	return s, nil
}

func (a *CodexAdapter) Transcript(ctx context.Context, nativeID string, offset, limit int) (protocol.Transcript, error) {
	path, err := a.findFile(nativeID)
	if err != nil {
		return protocol.Transcript{}, err
	}
	rc, err := openSessionFile(path)
	if err != nil {
		return protocol.Transcript{}, err
	}
	defer rc.Close()
	var all []protocol.Message
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	for sc.Scan() {
		var line codexLine
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "response_item" {
			continue
		}
		var p codexPayload
		if json.Unmarshal(line.Payload, &p) != nil {
			continue
		}
		ts := parseTime(line.Timestamp)
		switch p.Type {
		case "message":
			if p.Role != "user" && p.Role != "assistant" {
				continue
			}
			for _, text := range codexContent(p.Content) {
				if strings.TrimSpace(text) == "" {
					continue
				}
				all = append(all, protocol.Message{Role: p.Role, Kind: "text", Text: text, Timestamp: ts})
			}
		case "function_call":
			all = append(all, protocol.Message{Role: "assistant", Kind: "tool_call", ToolName: p.Name, ToolInput: truncate(p.Arguments, 8000), Timestamp: ts})
		case "function_call_output":
			all = append(all, protocol.Message{Role: "tool", Kind: "tool_result", ToolOutput: truncate(p.Output, 8000), Timestamp: ts})
		}
	}
	return pageMessages(all, offset, limit)
}

func (a *CodexAdapter) findFile(nativeID string) (string, error) {
	var found string
	root := filepath.Join(a.root, "sessions")
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, "rollout-") && strings.Contains(name, nativeID) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found != "" {
		return found, nil
	}
	// Fall back to a metadata scan (the native ID may differ from the file name).
	sessions, _ := a.ListSessions(context.Background())
	for _, s := range sessions {
		if s.NativeID == nativeID {
			return a.findFileByMetadata(s)
		}
	}
	return "", fmt.Errorf("codex session %s not found", nativeID)
}

func (a *CodexAdapter) findFileByMetadata(want protocol.Session) (string, error) {
	var found string
	_ = filepath.WalkDir(filepath.Join(a.root, "sessions"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".jsonl") && !strings.HasSuffix(d.Name(), ".jsonl.zst") {
			return nil
		}
		s, err := a.scanMetadata(path)
		if err == nil && s.NativeID == want.NativeID {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("codex session %s not found", want.NativeID)
	}
	return found, nil
}

func (a *CodexAdapter) BuildCommand(action Action) (Command, error) {
	var args []string
	switch action.Kind {
	case protocol.ActionResume, "":
		if action.NativeID == "" {
			return Command{}, fmt.Errorf("native session id is required")
		}
		args = []string{"resume", action.NativeID}
	case protocol.ActionFork:
		if action.NativeID == "" {
			return Command{}, fmt.Errorf("native session id is required")
		}
		args = []string{"fork", action.NativeID}
	case protocol.ActionNew:
	default:
		return Command{}, fmt.Errorf("unsupported action %q", action.Kind)
	}
	path, argv := commandFor(a.bin, args)
	return Command{Path: path, Args: argv, Cwd: action.Cwd}, nil
}

func (a *CodexAdapter) Search(ctx context.Context, query string, limit int) ([]protocol.SearchHit, error) {
	return searchJSONL(ctx, filepath.Join(a.root, "sessions"), "codex", query, limit, func(line []byte) (string, time.Time) {
		var l codexLine
		if json.Unmarshal(line, &l) != nil || l.Type != "response_item" {
			return "", time.Time{}
		}
		var p codexPayload
		if json.Unmarshal(l.Payload, &p) != nil {
			return "", time.Time{}
		}
		switch p.Type {
		case "message":
			return strings.Join(codexContent(p.Content), " "), parseTime(l.Timestamp)
		case "function_call":
			return p.Name + " " + p.Arguments, parseTime(l.Timestamp)
		case "function_call_output":
			return p.Output, parseTime(l.Timestamp)
		}
		return "", time.Time{}
	})
}

func codexContent(raw json.RawMessage) []string {
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var out []string
		for _, b := range blocks {
			if b.Text != "" {
				out = append(out, b.Text)
			}
		}
		return out
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []string{s}
	}
	return nil
}

func openSessionFile(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, ".zst") {
		zr, err := zstd.NewReader(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return &zstdReadCloser{Reader: zr, closeFn: func() { zr.Close(); f.Close() }}, nil
	}
	return f, nil
}

type zstdReadCloser struct {
	io.Reader
	closeFn func()
}

func (z *zstdReadCloser) Close() error {
	z.closeFn()
	return nil
}
