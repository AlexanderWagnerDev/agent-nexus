package hub

import (
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// ReplaceHostSessions applies a full snapshot from one host. It returns the
// number of upserted and deleted records. A full snapshot is also the repair
// path after a reconnect because it removes rows the host no longer reports.
func (s *Store) ReplaceHostSessions(hostID string, sessions []protocol.Session) (upserted, deleted int, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	keep := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		id := sessionID(hostID, sess.Agent, sess.NativeID)
		keep[id] = true
		if err := upsertSession(tx, hostID, sess); err != nil {
			return 0, 0, err
		}
		upserted++
	}
	rows, err := tx.Query(`SELECT id FROM sessions WHERE host_id=?`, hostID)
	if err != nil {
		return 0, 0, err
	}
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil && !keep[id] {
			stale = append(stale, id)
		}
	}
	rows.Close()
	for _, id := range stale {
		if _, err := tx.Exec(`DELETE FROM sessions WHERE id=?`, id); err != nil {
			return 0, 0, err
		}
		deleted++
	}
	return upserted, deleted, tx.Commit()
}

// ApplyHostSessionDelta applies changed sessions and explicit removals. It
// never deletes rows that are absent from the payload; only a full snapshot
// may do that.
func (s *Store) ApplyHostSessionDelta(hostID string, sessions []protocol.Session, removed []protocol.SessionKey) (upserted, deleted int, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	for _, sess := range sessions {
		if err := upsertSession(tx, hostID, sess); err != nil {
			return 0, 0, err
		}
		upserted++
	}
	for _, key := range removed {
		if key.Agent == "" || key.NativeID == "" {
			continue
		}
		res, err := tx.Exec(`DELETE FROM sessions WHERE host_id=? AND agent=? AND native_id=?`, hostID, key.Agent, key.NativeID)
		if err != nil {
			return 0, 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			deleted++
		}
	}
	return upserted, deleted, tx.Commit()
}

// SessionFilter narrows and orders a session listing. Sorting is a closed
// whitelist so no caller input reaches ORDER BY as SQL.
type SessionFilter struct {
	HostID  string
	Agent   string
	Project string
	State   string
	Query   string
	Tag     string
	Sort    string
	Order   string
	Since   time.Time
	Until   time.Time
	Limit   int
	Offset  int
}

func (s *Store) ListSessions(userID, hostID, agent, project, state, query string, limit, offset int, all bool) ([]protocol.SessionView, error) {
	return s.ListSessionsFiltered(userID, SessionFilter{
		HostID: hostID, Agent: agent, Project: project, State: state, Query: query,
		Limit: limit, Offset: offset,
	}, all)
}

func (s *Store) ListSessionsFiltered(userID string, f SessionFilter, all bool) ([]protocol.SessionView, error) {
	limit := f.Limit
	if limit <= 0 || limit > 2001 {
		limit = 500
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	where, args := s.sessionFilterClause(userID, f, all)
	q := `SELECT s.id,s.host_id,h.name,s.agent,s.native_id,s.title,s.cwd,s.git_branch,s.repo_root,s.repo_url,
		s.created_at,s.updated_at,s.message_count,s.state,s.active_elsewhere,s.warning
		FROM sessions s JOIN hosts h ON h.id=s.host_id
		LEFT JOIN session_overrides o ON o.session_id=s.id AND o.user_id=?` + where
	sortColumn := "s.updated_at"
	switch f.Sort {
	case "created":
		sortColumn = "s.created_at"
	case "title":
		sortColumn = "COALESCE(NULLIF(o.title,''), s.title) COLLATE NOCASE"
	case "messages":
		sortColumn = "s.message_count"
	}
	order := "DESC"
	if strings.EqualFold(f.Order, "asc") {
		order = "ASC"
	}
	q += ` ORDER BY ` + sortColumn + ` ` + order + `, s.id ` + order + ` LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.SessionView
	for rows.Next() {
		v, err := scanSessionView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachSessionMetadata(userID, out); err != nil {
		return nil, err
	}
	return out, nil
}

// CountSessionsFiltered returns the total number of rows matching a filter,
// independent of the page size. The virtualized browser list uses it to show
// an exact "x of y" count without loading every row.
func (s *Store) CountSessionsFiltered(userID string, f SessionFilter, all bool) (int, error) {
	where, args := s.sessionFilterClause(userID, f, all)
	q := `SELECT COUNT(*) FROM sessions s JOIN hosts h ON h.id=s.host_id
		LEFT JOIN session_overrides o ON o.session_id=s.id AND o.user_id=?` + where
	var total int
	if err := s.db.QueryRow(q, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// sessionFilterClause builds the shared FROM/WHERE arguments. The first
// argument belongs to the session_overrides join; callers append their own
// SELECT and ORDER BY around it.
func (s *Store) sessionFilterClause(userID string, f SessionFilter, all bool) (string, []any) {
	q := ` WHERE h.revoked_at IS NULL`
	args := []any{userID}
	if !all {
		q += ` AND (h.owner_id=? OR EXISTS (SELECT 1 FROM host_grants g WHERE g.host_id=h.id AND g.user_id=?))`
		args = append(args, userID, userID)
	}
	if f.HostID != "" {
		q += ` AND s.host_id=?`
		args = append(args, f.HostID)
	}
	if f.Agent != "" {
		q += ` AND s.agent=?`
		args = append(args, f.Agent)
	}
	if f.Project != "" {
		q += ` AND s.cwd LIKE ?`
		args = append(args, "%"+f.Project+"%")
	}
	if f.State != "" {
		q += ` AND s.state=?`
		args = append(args, f.State)
	}
	if f.Query != "" {
		q += ` AND (s.title LIKE ? OR COALESCE(o.title,'') LIKE ? OR s.cwd LIKE ?)`
		args = append(args, "%"+f.Query+"%", "%"+f.Query+"%", "%"+f.Query+"%")
	}
	if f.Tag != "" {
		q += ` AND EXISTS (SELECT 1 FROM session_tags t WHERE t.session_id=s.id AND t.user_id=? AND t.tag=?)`
		args = append(args, userID, f.Tag)
	}
	if !f.Since.IsZero() {
		q += ` AND s.updated_at>=?`
		args = append(args, ms(f.Since))
	}
	if !f.Until.IsZero() {
		q += ` AND s.updated_at<=?`
		args = append(args, ms(f.Until))
	}
	return q, args
}

// attachSessionMetadata fills the per-user title override and tags for a page
// of sessions. The session rows themselves are owned by the host snapshot and
// must never be rewritten here.
func (s *Store) attachSessionMetadata(userID string, sessions []protocol.SessionView) error {
	if len(sessions) == 0 {
		return nil
	}
	placeholders := make([]string, 0, len(sessions))
	args := make([]any, 0, len(sessions)+1)
	args = append(args, userID)
	for _, sess := range sessions {
		placeholders = append(placeholders, "?")
		args = append(args, sess.ID)
	}
	in := strings.Join(placeholders, ",")
	rows, err := s.db.Query(`SELECT session_id,title FROM session_overrides
		WHERE user_id=? AND session_id IN (`+in+`)`, args...)
	if err != nil {
		return err
	}
	titles := map[string]string{}
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			rows.Close()
			return err
		}
		titles[id] = title
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rows, err = s.db.Query(`SELECT session_id,tag FROM session_tags
		WHERE user_id=? AND session_id IN (`+in+`) ORDER BY tag`, args...)
	if err != nil {
		return err
	}
	tags := map[string][]string{}
	for rows.Next() {
		var id, tag string
		if err := rows.Scan(&id, &tag); err != nil {
			rows.Close()
			return err
		}
		tags[id] = append(tags[id], tag)
	}
	rows.Close()
	for i := range sessions {
		sessions[i].LocalTitle = titles[sessions[i].ID]
		sessions[i].Tags = tags[sessions[i].ID]
	}
	return rows.Err()
}

// SessionTags returns the tags in use by one user, for the filter control.
func (s *Store) SessionTags(userID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT tag FROM session_tags WHERE user_id=? ORDER BY tag`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

func (s *Store) SetSessionLocalTitle(userID, sessionID, title string) error {
	if title == "" {
		_, err := s.db.Exec(`DELETE FROM session_overrides WHERE user_id=? AND session_id=?`, userID, sessionID)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO session_overrides(user_id,session_id,title,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(user_id,session_id) DO UPDATE SET title=excluded.title, updated_at=excluded.updated_at`,
		userID, sessionID, title, ms(time.Now()))
	return err
}

func (s *Store) AddSessionTag(userID, sessionID, tag string) error {
	_, err := s.db.Exec(`INSERT INTO session_tags(user_id,session_id,tag,created_at) VALUES(?,?,?,?)
		ON CONFLICT(user_id,session_id,tag) DO NOTHING`, userID, sessionID, tag, ms(time.Now()))
	return err
}

func (s *Store) DeleteSessionTag(userID, sessionID, tag string) error {
	_, err := s.db.Exec(`DELETE FROM session_tags WHERE user_id=? AND session_id=? AND tag=?`, userID, sessionID, tag)
	return err
}

func (s *Store) SessionMetadata(userID, sessionID string) (string, []string) {
	one := []protocol.SessionView{{ID: sessionID}}
	if err := s.attachSessionMetadata(userID, one); err != nil {
		return "", nil
	}
	return one[0].LocalTitle, one[0].Tags
}

// SessionIDForNative maps a host-local native session to its hub ID.
func (s *Store) SessionIDForNative(hostID, agent, nativeID string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM sessions WHERE host_id=? AND agent=? AND native_id=?`, hostID, agent, nativeID).Scan(&id)
	return id, err
}

// SessionByID returns one session and its per-user local metadata.
func (s *Store) SessionByID(userID, id string, all bool) (protocol.SessionView, error) {
	q := `SELECT s.id,s.host_id,h.name,s.agent,s.native_id,s.title,s.cwd,s.git_branch,s.repo_root,s.repo_url,
		s.created_at,s.updated_at,s.message_count,s.state,s.active_elsewhere,s.warning
		FROM sessions s JOIN hosts h ON h.id=s.host_id
		WHERE s.id=? AND h.revoked_at IS NULL`
	args := []any{id}
	if !all {
		q += ` AND (h.owner_id=? OR EXISTS (SELECT 1 FROM host_grants g WHERE g.host_id=h.id AND g.user_id=?))`
		args = append(args, userID, userID)
	}
	v, err := scanSessionView(s.db.QueryRow(q, args...))
	if err != nil {
		return v, err
	}
	v.LocalTitle, v.Tags = s.SessionMetadata(userID, v.ID)
	return v, nil
}

// SetSessionState updates the live state of a session.
func (s *Store) SetSessionState(id, state string) {
	s.db.Exec(`UPDATE sessions SET state=? WHERE id=?`, state, id)
}

// ---- runs ----
