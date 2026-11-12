package hub

import (
	"errors"
	"net/http"
)

// errRunLimit is returned when a configured concurrency limit rejects a new
// run. Handlers translate it to HTTP 429; the host never sees the run.
var errRunLimit = errors.New("run concurrency limit reached")

func writeRunStartError(w http.ResponseWriter, err error) {
	if errors.Is(err, errRunLimit) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error": "run concurrency limit reached", "reason": "run_limit",
		})
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}

func (s *Store) CountActiveRunsForHost(hostID string) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM runs WHERE host_id=? AND ended_at IS NULL`, hostID).Scan(&n)
	return n
}

func (s *Store) CountActiveRunsForUser(userID string) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM runs WHERE started_by=? AND ended_at IS NULL`, userID).Scan(&n)
	return n
}

func (s *Store) CountActiveRunsForProject(hostID, cwd string) int {
	if cwd == "" {
		return 0
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM runs WHERE host_id=? AND cwd=? AND ended_at IS NULL`, hostID, cwd).Scan(&n)
	return n
}

// checkRunLimits enforces the configured per-host, per-user and per-project
// ceilings. Zero disables a check; the caller has already validated ownership
// and host access.
func (s *Server) checkRunLimits(userID, hostID, cwd string) error {
	if s.cfg.MaxRunsPerHost > 0 && s.store.CountActiveRunsForHost(hostID) >= s.cfg.MaxRunsPerHost {
		s.metrics.runLimitRejected()
		return errRunLimit
	}
	if s.cfg.MaxRunsPerUser > 0 && s.store.CountActiveRunsForUser(userID) >= s.cfg.MaxRunsPerUser {
		s.metrics.runLimitRejected()
		return errRunLimit
	}
	if s.cfg.MaxRunsPerProject > 0 && s.store.CountActiveRunsForProject(hostID, cwd) >= s.cfg.MaxRunsPerProject {
		s.metrics.runLimitRejected()
		return errRunLimit
	}
	return nil
}
