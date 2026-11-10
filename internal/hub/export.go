package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

const maxExportMessages = 10000

type transcriptExport struct {
	Session    protocol.SessionView `json:"session"`
	Messages   []protocol.Message   `json:"messages"`
	ExportedAt time.Time            `json:"exported_at"`
	Truncated  bool                 `json:"truncated,omitempty"`
	Warning    string               `json:"warning,omitempty"`
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, err := s.store.SessionByID(u.ID, r.PathValue("id"), isAdminRole(u.Role))
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if !s.sessionPermission(u, sess, "read_transcript") {
		writeError(w, http.StatusForbidden, "read_transcript permission required")
		return
	}
	if !s.hostOnline(sess.HostID) {
		writeError(w, http.StatusServiceUnavailable, "host is offline; the transcript is read from the host")
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "markdown"
	}
	if format != "markdown" && format != "md" && format != "json" {
		writeError(w, http.StatusBadRequest, "format must be markdown or json")
		return
	}

	messages, truncated, warning, err := s.fetchTranscript(r.Context(), sess, maxExportMessages)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	exp := transcriptExport{
		Session: sess, Messages: messages, ExportedAt: time.Now().UTC(),
		Truncated: truncated, Warning: warning,
	}
	shortID := sess.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	name := "agentnexus-" + shortID
	if format == "json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name+".json"))
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(exp)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name+".md"))
	_, _ = w.Write([]byte(markdownExport(exp)))
}

func (s *Server) fetchTranscript(ctx context.Context, sess protocol.SessionView, max int) ([]protocol.Message, bool, string, error) {
	offset := 0
	var all []protocol.Message
	var warning string
	truncated := false
	for {
		params, _ := json.Marshal(map[string]any{
			"native_id": sess.NativeID, "agent": sess.Agent, "offset": offset, "limit": 500,
		})
		reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		raw, err := s.hostRequest(reqCtx, sess.HostID, "transcript", params)
		cancel()
		if err != nil {
			return nil, false, "", err
		}
		var tr protocol.Transcript
		if err := json.Unmarshal(raw, &tr); err != nil {
			return nil, false, "", fmt.Errorf("invalid host response")
		}
		if tr.Warning != "" {
			warning = tr.Warning
		}
		all = append(all, tr.Messages...)
		if len(all) >= max {
			all = all[:max]
			truncated = true
			break
		}
		if tr.Next <= 0 || len(tr.Messages) == 0 || len(all) >= tr.Total {
			break
		}
		offset = tr.Next
	}
	return all, truncated, warning, nil
}

func markdownExport(exp transcriptExport) string {
	var b strings.Builder
	s := exp.Session
	fmt.Fprintf(&b, "# %s\n\n", strings.TrimSpace(s.Title))
	fmt.Fprintf(&b, "- Host: %s\n- Agent: %s\n- Directory: `%s`\n", s.HostName, s.Agent, s.Cwd)
	if s.GitBranch != "" {
		fmt.Fprintf(&b, "- Branch: %s\n", s.GitBranch)
	}
	fmt.Fprintf(&b, "- Created: %s\n- Updated: %s\n- Exported: %s\n\n", s.CreatedAt.Format(time.RFC3339), s.UpdatedAt.Format(time.RFC3339), exp.ExportedAt.Format(time.RFC3339))
	if exp.Warning != "" {
		fmt.Fprintf(&b, "> Warning: %s\n\n", exp.Warning)
	}
	if exp.Truncated {
		fmt.Fprintf(&b, "> Export truncated at %d messages.\n\n", len(exp.Messages))
	}
	for _, m := range exp.Messages {
		role := m.Role
		if role == "" {
			role = "message"
		}
		fmt.Fprintf(&b, "## %s", role)
		if !m.Timestamp.IsZero() {
			fmt.Fprintf(&b, " — %s", m.Timestamp.Format(time.RFC3339))
		}
		b.WriteString("\n\n")
		switch m.Kind {
		case "tool_call":
			fmt.Fprintf(&b, "**Tool call:** `%s`\n\n", m.ToolName)
			if m.ToolInput != "" {
				fmt.Fprintf(&b, "~~~json\n%s\n~~~\n\n", m.ToolInput)
			}
		case "tool_result":
			fmt.Fprintf(&b, "**Tool result:** `%s`\n\n", m.ToolName)
			if m.ToolOutput != "" {
				fmt.Fprintf(&b, "~~~\n%s\n~~~\n\n", m.ToolOutput)
			}
		default:
			b.WriteString(m.Text)
			b.WriteString("\n\n")
		}
	}
	return b.String()
}
