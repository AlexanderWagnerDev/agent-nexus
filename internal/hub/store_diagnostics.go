package hub

// DiagnosticsCounts is a small, content-free summary for the admin diagnostics
// page and export. It never includes session ids, host names or prompts.
type DiagnosticsCounts struct {
	Hosts         int `json:"hosts"`
	Sessions      int `json:"sessions"`
	ActiveRuns    int `json:"active_runs"`
	Tasks         int `json:"tasks"`
	Users         int `json:"users"`
	QueuedPrompts int `json:"queued_prompts"`
}

func (s *Store) DiagnosticsCounts() (DiagnosticsCounts, error) {
	var c DiagnosticsCounts
	err := s.db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM hosts WHERE revoked_at IS NULL),
		(SELECT COUNT(*) FROM sessions),
		(SELECT COUNT(*) FROM runs WHERE ended_at IS NULL),
		(SELECT COUNT(*) FROM tasks),
		(SELECT COUNT(*) FROM users),
		(SELECT COUNT(*) FROM prompt_queue WHERE state='queued')`).
		Scan(&c.Hosts, &c.Sessions, &c.ActiveRuns, &c.Tasks, &c.Users, &c.QueuedPrompts)
	return c, err
}

// Ping checks that the metadata database accepts a query.
func (s *Store) Ping() error {
	var n int
	return s.db.QueryRow(`SELECT 1`).Scan(&n)
}
