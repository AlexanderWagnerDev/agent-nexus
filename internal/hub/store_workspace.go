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
	s.created_at,s.updated_at,s.message_count,s.state,s.active_elsewhere,s.warning`

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

func (s *Store) NotificationExists(userID, runID, kind string) (bool, error) {
	if runID == "" {
		return false, nil
	}
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=? AND run_id=? AND kind=?`, userID, runID, kind).Scan(&count)
	return count > 0, err
}

func (s *Store) AddNotification(userID, kind, title, body, runID, link string) error {
	_, err := s.db.Exec(`INSERT INTO notifications(id,user_id,kind,title,body,run_id,link,created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		newID(), userID, kind, title, body, runID, link, ms(time.Now()))
	return err
}

func (s *Store) ListNotifications(userID string, limit int) ([]protocol.Notification, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT n.id,n.kind,n.title,n.body,n.run_id,n.link,n.created_at,n.read_at,
		COALESCE(r.host_id,''),COALESCE(h.name,''),COALESCE(s.agent,'')
		FROM notifications n
		LEFT JOIN runs r ON r.id=n.run_id AND n.run_id<>''
		LEFT JOIN hosts h ON h.id=r.host_id
		LEFT JOIN sessions s ON s.id=r.session_id
		WHERE n.user_id=? ORDER BY n.created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []protocol.Notification{}
	for rows.Next() {
		var n protocol.Notification
		var created int64
		var read sql.NullInt64
		if err := rows.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &n.RunID, &n.Link, &created, &read,
			&n.HostID, &n.HostName, &n.Agent); err != nil {
			return nil, 0, err
		}
		n.CreatedAt = fromMS(created)
		n.Read = read.Valid
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var unread int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=? AND read_at IS NULL`, userID).Scan(&unread)
	return out, unread, nil
}

// MarkNotificationsRead marks one notification (id != "") or every unread
// notification for the user. It returns the remaining unread count.
func (s *Store) MarkNotificationsRead(userID, id string) (int, error) {
	if id != "" {
		if _, err := s.db.Exec(`UPDATE notifications SET read_at=? WHERE id=? AND user_id=? AND read_at IS NULL`, ms(time.Now()), id, userID); err != nil {
			return 0, err
		}
	} else {
		if _, err := s.db.Exec(`UPDATE notifications SET read_at=? WHERE user_id=? AND read_at IS NULL`, ms(time.Now()), userID); err != nil {
			return 0, err
		}
	}
	var unread int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=? AND read_at IS NULL`, userID).Scan(&unread)
	return unread, err
}
