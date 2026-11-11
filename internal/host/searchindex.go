package host

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// SearchIndex is a host-local SQLite FTS5 cache. It never leaves the host:
// the hub only receives hit metadata and an index status. The index is a
// second local copy of transcript text, so the file is created with 0600
// permissions and can be disabled with search_index=false.
type SearchIndex struct {
	db   *sql.DB
	path string

	mu     sync.RWMutex
	status protocol.IndexStatus
}

const (
	indexBodyLimit = 512 << 10
	indexMessages  = 4000
	snippetMarker  = "\u0001"
	snippetEnd     = "\u0002"
)

// OpenSearchIndex opens or creates the index in a host-local directory.
func OpenSearchIndex(path string) (*SearchIndex, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("search index path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	x := &SearchIndex{db: db, path: path, status: protocol.IndexStatus{State: "building"}}
	if err := x.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	x.chmod()
	return x, nil
}

func (x *SearchIndex) migrate() error {
	_, err := x.db.Exec(`
CREATE TABLE IF NOT EXISTS documents(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent TEXT NOT NULL,
  native_id TEXT NOT NULL,
  title TEXT NOT NULL,
  cwd TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  fingerprint TEXT NOT NULL,
  UNIQUE(agent, native_id)
);
CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
  body, agent UNINDEXED, native_id UNINDEXED,
  tokenize='unicode61 remove_diacritics 2'
);`)
	return err
}

func (x *SearchIndex) chmod() {
	for _, name := range []string{x.path, x.path + "-wal", x.path + "-shm"} {
		_ = os.Chmod(name, 0o600)
	}
}

// Close releases the database handle.
func (x *SearchIndex) Close() error {
	if x == nil || x.db == nil {
		return nil
	}
	return x.db.Close()
}

// Status returns the current progress.
func (x *SearchIndex) Status() protocol.IndexStatus {
	if x == nil {
		return protocol.IndexStatus{State: "disabled"}
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	status := x.status
	if info, err := os.Stat(x.path); err == nil {
		status.Bytes = info.Size()
	}
	return status
}

func (x *SearchIndex) setStatus(indexed, total int, state, errText string) {
	x.mu.Lock()
	x.status.State = state
	x.status.Indexed = indexed
	x.status.Total = total
	x.status.Error = errText
	x.mu.Unlock()
}

// SetDisabled reports the configured-off state without opening a database.
func (x *SearchIndex) SetDisabled() {
	x.mu.Lock()
	x.status = protocol.IndexStatus{State: "disabled"}
	x.mu.Unlock()
}

type indexedSession struct {
	agent    string
	nativeID string
	title    string
	cwd      string
	updated  time.Time
	count    int
}

// Rebuild drops and rebuilds the whole index.
func (x *SearchIndex) Rebuild(ctx context.Context, adapters []Adapter, policy Policy, log *slog.Logger) error {
	if _, err := x.db.Exec(`DELETE FROM documents_fts`); err != nil {
		return err
	}
	if _, err := x.db.Exec(`DELETE FROM documents`); err != nil {
		return err
	}
	return x.build(ctx, adapters, policy, log, true)
}

// Refresh incrementally indexes changed sessions.
func (x *SearchIndex) Refresh(ctx context.Context, adapters []Adapter, policy Policy, log *slog.Logger) error {
	return x.build(ctx, adapters, policy, log, false)
}

func (x *SearchIndex) build(ctx context.Context, adapters []Adapter, policy Policy, log *slog.Logger, rebuild bool) error {
	corpus := collectIndexSessions(ctx, adapters, policy)
	x.setStatus(0, len(corpus), "building", "")
	for i, sess := range corpus {
		if ctx.Err() != nil {
			x.setStatus(i, len(corpus), "building", "")
			return ctx.Err()
		}
		if err := x.indexOne(ctx, adapters, sess, rebuild); err != nil {
			if log != nil {
				log.Warn("search index session failed", "agent", sess.agent, "native_id", sess.nativeID, "error", err)
			}
		}
		if i%50 == 0 {
			x.setStatus(i, len(corpus), "building", "")
			// Keep the background build from saturating the disk or CPU on a
			// developer machine; the delay is negligible for 10k sessions.
			time.Sleep(5 * time.Millisecond)
		}
	}
	x.setStatus(len(corpus), len(corpus), "ready", "")
	x.chmod()
	return nil
}

func collectIndexSessions(ctx context.Context, adapters []Adapter, policy Policy) []indexedSession {
	var out []indexedSession
	for _, a := range adapters {
		if !policy.AgentAllowed(a.Name()) {
			continue
		}
		sessions, err := a.ListSessions(ctx)
		if err != nil {
			continue
		}
		for _, s := range sessions {
			if s.NativeID == "" || s.Cwd == "" || !policy.WithinRoots(s.Cwd) {
				continue
			}
			out = append(out, indexedSession{
				agent: a.Name(), nativeID: s.NativeID, title: s.Title, cwd: s.Cwd,
				updated: s.UpdatedAt, count: s.MessageCount,
			})
		}
	}
	return out
}

func (x *SearchIndex) indexOne(ctx context.Context, adapters []Adapter, sess indexedSession, rebuild bool) error {
	fingerprint := fmt.Sprintf("%d|%d|%s", sess.updated.UnixNano(), sess.count, sess.title)
	if !rebuild {
		var current string
		err := x.db.QueryRow(`SELECT fingerprint FROM documents WHERE agent=? AND native_id=?`, sess.agent, sess.nativeID).Scan(&current)
		if err == nil && current == fingerprint {
			return nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	var adapter Adapter
	for _, a := range adapters {
		if a.Name() == sess.agent {
			adapter = a
			break
		}
	}
	if adapter == nil {
		return fmt.Errorf("adapter %s is not available", sess.agent)
	}
	body, err := transcriptBody(ctx, adapter, sess.nativeID)
	if err != nil {
		return err
	}
	title := displayIndexTitle(sess.title, sess.nativeID)
	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRow(`SELECT id FROM documents WHERE agent=? AND native_id=?`, sess.agent, sess.nativeID).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, execErr := tx.Exec(`INSERT INTO documents(agent,native_id,title,cwd,updated_at,fingerprint) VALUES(?,?,?,?,?,?)`,
			sess.agent, sess.nativeID, title, sess.cwd, sess.updated.UnixMilli(), fingerprint)
		if execErr != nil {
			return execErr
		}
		id, err = res.LastInsertId()
		if err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if _, err := tx.Exec(`UPDATE documents SET title=?,cwd=?,updated_at=?,fingerprint=? WHERE id=?`,
			title, sess.cwd, sess.updated.UnixMilli(), fingerprint, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM documents_fts WHERE rowid=?`, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO documents_fts(rowid,body,agent,native_id) VALUES(?,?,?,?)`, id, body, sess.agent, sess.nativeID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func displayIndexTitle(title, nativeID string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return nativeID
	}
	if len(title) > 200 {
		return title[:200] + "..."
	}
	return title
}

func transcriptBody(ctx context.Context, adapter Adapter, nativeID string) (string, error) {
	var b strings.Builder
	offset := 0
	for pages := 0; pages < 20 && b.Len() < indexBodyLimit; pages++ {
		if ctx.Err() != nil {
			break
		}
		tr, err := adapter.Transcript(ctx, nativeID, offset, 500)
		if err != nil {
			if pages == 0 {
				return "", err
			}
			break
		}
		for _, m := range tr.Messages {
			for _, value := range []string{m.Text, m.ToolName, m.ToolInput, m.ToolOutput} {
				value = strings.TrimSpace(value)
				if value == "" {
					continue
				}
				if b.Len()+len(value) > indexBodyLimit {
					value = value[:indexBodyLimit-b.Len()]
				}
				b.WriteString(value)
				b.WriteByte('\n')
				if b.Len() >= indexBodyLimit {
					break
				}
			}
		}
		if tr.Next <= 0 || len(tr.Messages) == 0 || (tr.Total > 0 && tr.Next >= tr.Total) {
			break
		}
		offset = tr.Next
	}
	return b.String(), nil
}

// Search runs a ranked FTS5 query. Only metadata is returned.
func (x *SearchIndex) Search(ctx context.Context, query string, limit int) ([]protocol.SearchHit, error) {
	expression := ftsExpression(query)
	if expression == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := x.db.QueryContext(ctx, `
SELECT d.agent, d.native_id, d.title, d.updated_at,
       snippet(documents_fts, 0, ?, ?, ' … ', 18)
FROM documents_fts
JOIN documents d ON d.id = documents_fts.rowid
WHERE documents_fts MATCH ?
ORDER BY bm25(documents_fts), d.updated_at DESC
LIMIT ?`, snippetMarker, snippetEnd, expression, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []protocol.SearchHit
	for rows.Next() {
		var hit protocol.SearchHit
		var updated int64
		if err := rows.Scan(&hit.Agent, &hit.NativeID, &hit.Title, &updated, &hit.Snippet); err != nil {
			return nil, err
		}
		hit.At = time.UnixMilli(updated).UTC()
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

// ftsExpression turns free text into a safe FTS5 prefix query. Punctuation is
// dropped instead of interpreted as query syntax.
func ftsExpression(query string) string {
	var terms []string
	for _, field := range strings.Fields(strings.ToLower(query)) {
		var b strings.Builder
		for _, r := range field {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
				b.WriteRune(r)
			}
		}
		term := strings.Trim(b.String(), "-")
		if term == "" {
			continue
		}
		terms = append(terms, `"`+strings.ReplaceAll(term, `"`, `""`)+`"*`)
	}
	return strings.Join(terms, " AND ")
}

// Count returns the number of indexed sessions.
func (x *SearchIndex) Count() (int, error) {
	var n int
	err := x.db.QueryRow(`SELECT COUNT(*) FROM documents`).Scan(&n)
	return n, err
}

// MarshalStatus is a small helper for the CLI.
func (x *SearchIndex) MarshalStatus() string {
	raw, _ := json.Marshal(x.Status())
	return string(raw)
}
