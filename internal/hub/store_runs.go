package hub

import (
	"database/sql"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func (s *Store) CreateRun(id, sessionID, hostID, mode, userID, deviceID string) error {
	return s.CreateRunAt(id, sessionID, hostID, "", mode, userID, deviceID)
}

// CreateRunAt records the working directory so per-project concurrency limits
// can be enforced without joining the session row.
func (s *Store) CreateRunAt(id, sessionID, hostID, cwd, mode, userID, deviceID string) error {
	_, err := s.db.Exec(`INSERT INTO runs(id,session_id,host_id,cwd,mode,state,started_by,lease_device_id,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		id, sessionID, hostID, cwd, mode, protocol.StateStarting, userID, deviceID, ms(time.Now()))
	return err
}

func (s *Store) RunByID(id string) (protocol.RunView, string, error) {
	var r protocol.RunView
	var created int64
	var ended sql.NullInt64
	var startedBy string
	err := s.db.QueryRow(`SELECT r.id,r.session_id,r.host_id,COALESCE(h.name,''),COALESCE(s.title,''),COALESCE(s.agent,''),
		COALESCE(s.cwd,''),COALESCE(s.git_branch,''),r.mode,r.state,r.lease_device_id,r.created_at,r.ended_at,r.started_by,
		r.error_code,r.error_message
		FROM runs r
		LEFT JOIN sessions s ON s.id=r.session_id
		LEFT JOIN hosts h ON h.id=r.host_id
		WHERE r.id=?`, id).
		Scan(&r.ID, &r.SessionID, &r.HostID, &r.HostName, &r.SessionTitle, &r.Agent, &r.Cwd, &r.GitBranch, &r.Mode, &r.State, &r.LeaseDeviceID, &created, &ended, &startedBy, &r.ErrorCode, &r.ErrorMessage)
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
	// Only AdoptRunState may reopen a finished run; a late event or a stale
	// caller must not turn a run with an end time back into a live one.
	s.db.Exec(`UPDATE runs SET state=? WHERE id=? AND ended_at IS NULL`, state, id)
}

// RecoverStaleRuns closes runs that a previous hub process left active. The
// host is authoritative about whether the agent process survived, so a
// reconnecting host re-adopts the run through AdoptRunState. Without this,
// a hub restart while a host is offline would block its sessions forever.
func (s *Store) RecoverStaleRuns() int {
	res, err := s.db.Exec(`UPDATE runs SET state=?, ended_at=?, error_code=?, error_message=?
		WHERE ended_at IS NULL`,
		protocol.StateFailed, ms(time.Now()), ErrCodeNetwork,
		"hub restarted before the host re-adopted the run")
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

// AdoptRunState records the state a host reports for a run it still owns. It
// clears a previous terminal state so an agent process that survived a hub
// restart or host reconnect is active again in the database, which keeps the
// per-session uniqueness check effective.
func (s *Store) AdoptRunState(id, state string) {
	if state == protocol.StateExited || state == protocol.StateFailed || state == protocol.StateCompleted {
		// The host now owns the final state. Drop the synthetic error written
		// when the hub closed the run, and keep an existing end time instead of
		// inflating the run duration with the reconnect delay.
		s.db.Exec(`UPDATE runs SET state=?, ended_at=COALESCE(ended_at, ?), error_code='', error_message='' WHERE id=?`,
			state, ms(time.Now()), id)
		return
	}
	s.db.Exec(`UPDATE runs SET state=?, ended_at=NULL, error_code='', error_message='' WHERE id=?`, state, id)
}

// SetRunError stores a stable classification and a bounded message. Callers
// pass text that agents or the host reported; the hub does not synthesize
// errors for successful runs.
func (s *Store) SetRunError(id, code, message string) {
	if len(message) > 2000 {
		message = message[:2000]
	}
	s.db.Exec(`UPDATE runs SET error_code=?, error_message=? WHERE id=?`, code, message, id)
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
