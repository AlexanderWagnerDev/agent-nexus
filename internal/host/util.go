package host

import (
	"bufio"
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func pageMessages(all []protocol.Message, offset, limit int) (protocol.Transcript, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	total := len(all)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	tr := protocol.Transcript{Messages: all[offset:end], Total: total}
	if end < total {
		tr.Next = end
	}
	return tr, nil
}

// searchJSONL is a tolerant, linear transcript search used by adapters that
// store JSONL. It is intentionally simple: the host cache/FTS5 index is a
// later optimization, but search must already work without the hub storing
// any content.
func searchJSONL(ctx context.Context, root, agent, query string, limit int, extract func([]byte) (string, time.Time)) ([]protocol.SearchHit, error) {
	if limit <= 0 {
		limit = 50
	}
	q := strings.ToLower(query)
	var hits []protocol.SearchHit
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
		if !strings.HasSuffix(path, ".jsonl") && !strings.HasSuffix(path, ".jsonl.zst") && !strings.HasSuffix(path, ".json") {
			return nil
		}
		rc, err := openSessionFile(path)
		if err != nil {
			return nil
		}
		defer rc.Close()
		nativeID := sessionIDFromFilename(path)
		title := ""
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 64*1024), 16<<20)
		for sc.Scan() {
			line := sc.Bytes()
			if !strings.Contains(strings.ToLower(string(line)), q) {
				continue
			}
			text, ts := extract(line)
			if strings.TrimSpace(text) == "" {
				continue
			}
			snippet := searchSnippet(text, query)
			hits = append(hits, protocol.SearchHit{NativeID: nativeID, Agent: agent, Title: title, Snippet: snippet, At: ts})
			if len(hits) >= limit {
				return filepath.SkipAll
			}
		}
		return nil
	})
	return hits, err
}

// searchSnippet returns a compact window around the first case-insensitive
// match. It is shared by the JSONL and document-based search adapters.
func searchSnippet(text, query string) string {
	idx := strings.Index(strings.ToLower(text), strings.ToLower(query))
	if idx < 0 {
		idx = 0
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

func sessionIDFromFilename(path string) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".zst")
	name = strings.TrimSuffix(name, ".jsonl")
	name = strings.TrimSuffix(name, ".json")
	if strings.HasPrefix(name, "rollout-") {
		// rollout-2026-09-20T23-16-25-<uuid>
		parts := strings.Split(name, "-")
		if len(parts) >= 7 {
			return strings.Join(parts[len(parts)-5:], "-")
		}
	}
	return name
}

func sliceContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// fileURI renders a local path as a standards-compliant file URL. Windows
// drive letters need the empty authority form (file:///C:/...), otherwise a
// parser treats the drive letter as the host.
func fileURI(path string) string {
	slash := strings.ReplaceAll(path, `\`, "/")
	if len(slash) >= 2 && slash[1] == ':' {
		return "file:///" + slash
	}
	return (&url.URL{Scheme: "file", Path: slash}).String()
}
