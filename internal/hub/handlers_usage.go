package hub

import (
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	now := time.Now().UTC()
	since := now.Add(-30 * 24 * time.Hour)
	until := now
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "since must be an RFC 3339 timestamp")
			return
		}
		since = parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("until")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "until must be an RFC 3339 timestamp")
			return
		}
		until = parsed
	}
	if until.Before(since) {
		writeError(w, http.StatusBadRequest, "until must be after since")
		return
	}
	if until.Sub(since) > 366*24*time.Hour {
		writeError(w, http.StatusBadRequest, "range is limited to one year")
		return
	}
	report, err := s.store.UsageReport(u.ID, isAdminRole(u.Role), since, until,
		strings.TrimSpace(r.URL.Query().Get("host")), strings.TrimSpace(r.URL.Query().Get("agent")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}
