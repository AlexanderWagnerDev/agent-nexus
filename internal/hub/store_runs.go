package hub

import (
	"database/sql"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func (s *Store) CreateRun(id, sessionID, hostID, mode, userID, deviceID string) error {
	_, err := s.db.Exec(`INSERT INTO runs(id,session_id,host_id,mode,state,started_by,lease_device_id,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		id, sessionID, hostID, mode, protocol.StateStarting, userID, deviceID, ms(time.Now()))
	return err
}

func (s *Store) RunByID(id string) (protocol.RunView, string, error) {
	var r protocol.RunView
	var created int64
	var ended sql.NullInt64
	var startedBy string
	err := s.db.QueryRow(`SELECT r.id,r.session_id,r.host_id,COALESCE(h.name,''),COALESCE(s.title,''),COALESCE(s.agent,''),
		COALESCE(s.cwd,''),COALESCE(s.git_branch,''),r.mode,r.state,r.lease_device_id,r.created_at,r.ended_at,r.started_by
		FROM runs r
		LEFT JOIN sessions s ON s.id=r.session_id
		LEFT JOIN hosts h ON h.id=r.host_id
		WHERE r.id=?`, id).
		Scan(&r.ID, &r.SessionID, &r.HostID, &r.HostName, &r.SessionTitle, &r.Agent, &r.Cwd, &r.GitBranch, &r.Mode, &r.State, &r.LeaseDeviceID, &created, &ended, &startedBy)
	r.StartedAt = fromMS(created)
	if ended.Valid {
		t := fromMS(ended.Int64)
		r.EndedAt = &t
	}
	return r, startedBy, err
}

func (s *Store) ActiveRunForSession(sessionID string) (string, bool) {
	var id string
	if sessionID == "" {
		return "", false
	}
	if err := s.db.QueryRow(`SELECT id FROM runs WHERE session_id=? AND ended_at IS NULL ORDER BY created_at DESC LIMIT 1`, sessionID).Scan(&id); err != nil {
		return "", false
	}
	return id, true
}

// CountActiveRuns is used by the metrics endpoint only; it never returns
// transcript or session metadata.
func (s *Store) CountActiveRuns() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM runs WHERE ended_at IS NULL`).Scan(&n)
	return n, err
}

func (s *Store) UpdateRunState(id, state string) {
	if state == protocol.StateExited || state == protocol.StateFailed || state == protocol.StateCompleted {
		s.db.Exec(`UPDATE runs SET state=?, ended_at=? WHERE id=?`, state, ms(time.Now()), id)
		return
	}
	s.db.Exec(`UPDATE runs SET state=? WHERE id=?`, state, id)
}

func (s *Store) ActiveRuns(hostID string) ([]protocol.RunView, error) {
	rows, err := s.db.Query(`SELECT id,session_id,host_id,mode,state,lease_device_id,created_at FROM runs WHERE host_id=? AND ended_at IS NULL`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.RunView
	for rows.Next() {
		var r protocol.RunView
		var created int64
		if err := rows.Scan(&r.ID, &r.SessionID, &r.HostID, &r.Mode, &r.State, &r.LeaseDeviceID, &created); err != nil {
			return nil, err
		}
		r.StartedAt = fromMS(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ActiveRunsForUser lists live runs visible through host ownership or a view
// grant. It is intentionally bounded: the dashboard only needs recent runs.
func (s *Store) ActiveRunsForUser(userID string, all bool) ([]protocol.RunView, error) {
	q := `SELECT r.id,r.session_id,r.host_id,COALESCE(h.name,''),COALESCE(s.title,''),
		COALESCE(s.agent,''),COALESCE(s.cwd,''),COALESCE(s.git_branch,''),r.mode,r.state,r.created_at
		FROM runs r
		JOIN hosts h ON h.id=r.host_id
		LEFT JOIN sessions s ON s.id=r.session_id
		WHERE r.ended_at IS NULL AND h.revoked_at IS NULL`
	args := []any{}
	if !all {
		q += ` AND (h.owner_id=? OR EXISTS (SELECT 1 FROM host_grants g WHERE g.host_id=h.id AND g.user_id=?))`
		args = append(args, userID, userID)
	}
	q += ` ORDER BY r.created_at DESC LIMIT 200`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.RunView
	for rows.Next() {
		var r protocol.RunView
		var created int64
		if err := rows.Scan(&r.ID, &r.SessionID, &r.HostID, &r.HostName, &r.SessionTitle, &r.Agent, &r.Cwd, &r.GitBranch, &r.Mode, &r.State, &created); err != nil {
			return nil, err
		}
		r.StartedAt = fromMS(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- audit ----

type AuditEvent struct {
	Seq      int64     `json:"seq"`
	At       time.Time `json:"at"`
	Actor    string    `json:"actor"`
	Action   string    `json:"action"`
	Details  string    `json:"details"`
	PrevHash string    `json:"prev_hash"`
	Hash     string    `json:"hash"`
}
