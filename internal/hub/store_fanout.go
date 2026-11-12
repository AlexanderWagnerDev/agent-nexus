package hub

import (
	"database/sql"
	"time"
)

type FanoutRunView struct {
	RunID     string `json:"run_id"`
	SessionID string `json:"session_id,omitempty"`
	HostID    string `json:"host_id"`
	HostName  string `json:"host_name"`
	Agent     string `json:"agent"`
	Cwd       string `json:"cwd"`
	Title     string `json:"title,omitempty"`
	State     string `json:"state"`
	Error     string `json:"error,omitempty"`
}

type FanoutView struct {
	ID        string          `json:"id"`
	Prompt    string          `json:"prompt"`
	CreatedAt time.Time       `json:"created_at"`
	Runs      []FanoutRunView `json:"runs"`
}

func (s *Store) CreateFanout(userID, prompt string) (string, error) {
	id := newID()
	_, err := s.db.Exec(`INSERT INTO fanouts(id,user_id,prompt,created_at) VALUES(?,?,?,?)`,
		id, userID, prompt, ms(time.Now()))
	return id, err
}

func (s *Store) AddFanoutRun(fanoutID, runID, hostID, agent, lastError string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO fanout_runs(fanout_id,run_id,host_id,agent,error) VALUES(?,?,?,?,?)`,
		fanoutID, runID, hostID, agent, lastError)
	return err
}

func (s *Store) UpdateFanoutRunError(fanoutID, runID, lastError string) error {
	_, err := s.db.Exec(`UPDATE fanout_runs SET error=? WHERE fanout_id=? AND run_id=?`, lastError, fanoutID, runID)
	return err
}

func (s *Store) Fanout(id, userID string, all bool) (FanoutView, error) {
	var out FanoutView
	var created int64
	q := `SELECT f.id,f.prompt,f.created_at FROM fanouts f WHERE f.id=?`
	args := []any{id}
	if !all {
		q += ` AND f.user_id=?`
		args = append(args, userID)
	}
	if err := s.db.QueryRow(q, args...).Scan(&out.ID, &out.Prompt, &created); err != nil {
		return out, err
	}
	out.CreatedAt = fromMS(created)
	rows, err := s.db.Query(`SELECT fr.run_id,COALESCE(r.session_id,''),fr.host_id,COALESCE(h.name,''),fr.agent,
		COALESCE(s.cwd,''),COALESCE(s.title,''),COALESCE(r.state,''),fr.error
		FROM fanout_runs fr
		LEFT JOIN runs r ON r.id=fr.run_id
		LEFT JOIN sessions s ON s.id=r.session_id
		LEFT JOIN hosts h ON h.id=fr.host_id
		WHERE fr.fanout_id=? ORDER BY fr.rowid`, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Runs = []FanoutRunView{}
	for rows.Next() {
		var run FanoutRunView
		if err := rows.Scan(&run.RunID, &run.SessionID, &run.HostID, &run.HostName, &run.Agent,
			&run.Cwd, &run.Title, &run.State, &run.Error); err != nil {
			return out, err
		}
		out.Runs = append(out.Runs, run)
	}
	return out, rows.Err()
}

func (s *Store) ListFanouts(userID string, all bool, limit int) ([]FanoutView, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	q := `SELECT f.id FROM fanouts f`
	args := []any{}
	if !all {
		q += ` WHERE f.user_id=?`
		args = append(args, userID)
	}
	q += ` ORDER BY f.created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	var out []FanoutView
	for _, id := range ids {
		view, err := s.Fanout(id, userID, all)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}
