package hub

import "github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"

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

func (s *Store) ListSessions(userID, hostID, agent, project, state, query string, limit, offset int, all bool) ([]protocol.SessionView, error) {
	if limit <= 0 || limit > 2001 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	q := `SELECT s.id,s.host_id,h.name,s.agent,s.native_id,s.title,s.cwd,s.git_branch,s.repo_root,s.repo_url,
		s.created_at,s.updated_at,s.message_count,s.state,s.active_elsewhere,s.warning
		FROM sessions s JOIN hosts h ON h.id=s.host_id
		WHERE h.revoked_at IS NULL`
	args := []any{}
	if !all {
		q += ` AND (h.owner_id=? OR EXISTS (SELECT 1 FROM host_grants g WHERE g.host_id=h.id AND g.user_id=?))`
		args = append(args, userID, userID)
	}
	if hostID != "" {
		q += ` AND s.host_id=?`
		args = append(args, hostID)
	}
	if agent != "" {
		q += ` AND s.agent=?`
		args = append(args, agent)
	}
	if project != "" {
		q += ` AND s.cwd LIKE ?`
		args = append(args, "%"+project+"%")
	}
	if state != "" {
		q += ` AND s.state=?`
		args = append(args, state)
	}
	if query != "" {
		q += ` AND (s.title LIKE ? OR s.cwd LIKE ?)`
		args = append(args, "%"+query+"%", "%"+query+"%")
	}
	q += ` ORDER BY s.updated_at DESC LIMIT ? OFFSET ?`
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
	return out, rows.Err()
}

// SessionIDForNative maps a host-local native session to its hub ID.
func (s *Store) SessionIDForNative(hostID, agent, nativeID string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM sessions WHERE host_id=? AND agent=? AND native_id=?`, hostID, agent, nativeID).Scan(&id)
	return id, err
}

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
	return scanSessionView(s.db.QueryRow(q, args...))
}

// SetSessionState updates the live state of a session.
func (s *Store) SetSessionState(id, state string) {
	s.db.Exec(`UPDATE sessions SET state=? WHERE id=?`, state, id)
}

// ---- runs ----
