package hub

import (
	"database/sql"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// ---- projects ----

// ListProjects groups session metadata by host and working directory. It never
// reads transcripts and is bounded by the same host permissions as sessions.
func (s *Store) ListProjects(userID string, all bool, limit int) ([]protocol.ProjectView, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	q := `SELECT s.host_id,h.name,s.cwd,
		COALESCE(MAX(NULLIF(s.git_branch,'')),''),
		COALESCE(MAX(NULLIF(s.repo_root,'')),''),
		COALESCE(MAX(NULLIF(s.repo_url,'')),''),
		COUNT(*),
		SUM(CASE WHEN s.state IN ('running','waiting_for_input','starting','stopping') THEN 1 ELSE 0 END),
		GROUP_CONCAT(DISTINCT s.agent),
		MAX(s.updated_at)
		FROM sessions s JOIN hosts h ON h.id=s.host_id
		WHERE h.revoked_at IS NULL AND s.cwd <> ''`
	args := []any{}
	if !all {
		q += ` AND (h.owner_id=? OR EXISTS (SELECT 1 FROM host_grants g WHERE g.host_id=h.id AND g.user_id=?))`
		args = append(args, userID, userID)
	}
	q += ` GROUP BY s.host_id,s.cwd ORDER BY MAX(s.updated_at) DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []protocol.ProjectView{}
	for rows.Next() {
		var p protocol.ProjectView
		var agents sql.NullString
		var updated int64
		if err := rows.Scan(&p.HostID, &p.HostName, &p.Cwd, &p.Branch, &p.RepoRoot, &p.RepoURL, &p.Sessions, &p.Active, &agents, &updated); err != nil {
			return nil, err
		}
		p.Name = projectName(p.Cwd)
		p.LastAt = fromMS(updated)
		if agents.Valid && agents.String != "" {
			p.Agents = strings.Split(agents.String, ",")
		} else {
			p.Agents = []string{}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func projectName(cwd string) string {
	clean := strings.TrimRight(strings.ReplaceAll(cwd, "\\", "/"), "/")
	if clean == "" {
		return cwd
	}
	if i := strings.LastIndex(clean, "/"); i >= 0 && i+1 < len(clean) {
		return clean[i+1:]
	}
	return clean
}

// ---- favorites ----

const favoriteSessionColumns = `s.id,s.host_id,h.name,s.agent,s.native_id,s.title,s.cwd,s.git_branch,s.repo_root,s.repo_url,
	s.created_at,s.updated_at,s.message_count,s.state,s.active_elsewhere,s.warning,s.parent_native_id,s.forked_from_native_id`

func (s *Store) SetSessionFavorite(userID, sessionID string, favorite bool) error {
	if !favorite {
		_, err := s.db.Exec(`DELETE FROM session_favorites WHERE user_id=? AND session_id=?`, userID, sessionID)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO session_favorites(user_id,session_id,created_at) VALUES(?,?,?)
		ON CONFLICT(user_id,session_id) DO NOTHING`, userID, sessionID, ms(time.Now()))
	return err
}

func (s *Store) FavoriteSessionIDs(userID string) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT session_id FROM session_favorites WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (s *Store) FavoriteSessions(userID string, all bool) ([]protocol.SessionView, error) {
	q := `SELECT ` + favoriteSessionColumns + `
		FROM session_favorites f
		JOIN sessions s ON s.id=f.session_id
		JOIN hosts h ON h.id=s.host_id
		WHERE f.user_id=? AND h.revoked_at IS NULL`
	args := []any{userID}
	if !all {
		q += ` AND (h.owner_id=? OR EXISTS (SELECT 1 FROM host_grants g WHERE g.host_id=h.id AND g.user_id=?))`
		args = append(args, userID, userID)
	}
	q += ` ORDER BY f.created_at DESC LIMIT 200`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []protocol.SessionView{}
	for rows.Next() {
		v, err := scanSessionView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---- notifications ----
