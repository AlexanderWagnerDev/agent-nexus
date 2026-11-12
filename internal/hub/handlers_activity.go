package hub

import (
	"net/http"
	"strings"
	"time"
)

type ActivityDay struct {
	Date     string   `json:"date"`
	Sessions int      `json:"sessions"`
	Runs     int      `json:"runs"`
	Agents   []string `json:"agents"`
	Hosts    []string `json:"hosts"`
}

type ActivityView struct {
	Days     []ActivityDay `json:"days"`
	Sessions int           `json:"sessions"`
	Runs     int           `json:"runs"`
	From     time.Time     `json:"from"`
	To       time.Time     `json:"to"`
}

func splitList(value string) []string {
	if value == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	out := []string{}
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && !seen[part] {
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}

func (s *Server) handleProjectActivity(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	hostID := strings.TrimSpace(r.URL.Query().Get("host"))
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if repo == "" && (hostID == "" || project == "") {
		writeError(w, http.StatusBadRequest, "host and project are required")
		return
	}
	now := time.Now().UTC()
	from := now.Add(-90 * 24 * time.Hour)
	to := now
	if raw := strings.TrimSpace(r.URL.Query().Get("from")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "from must be an RFC 3339 timestamp")
			return
		}
		from = parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("to")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "to must be an RFC 3339 timestamp")
			return
		}
		to = parsed
	}
	if to.Before(from) || to.Sub(from) > 366*24*time.Hour {
		writeError(w, http.StatusBadRequest, "invalid activity range")
		return
	}
	where := ` WHERE h.revoked_at IS NULL`
	args := []any{}
	if !isAdminRole(u.Role) {
		where += ` AND (h.owner_id=? OR EXISTS (SELECT 1 FROM host_grants g WHERE g.host_id=h.id AND g.user_id=?))`
		args = append(args, u.ID, u.ID)
	}
	if repo != "" {
		where += ` AND s.repo_url=?`
		args = append(args, repo)
	} else {
		where += ` AND s.host_id=? AND s.cwd=?`
		args = append(args, hostID, project)
	}
	base := ` FROM sessions s JOIN hosts h ON h.id=s.host_id` + where

	view := ActivityView{Days: []ActivityDay{}, From: from, To: to}
	type dayRow struct {
		date     string
		sessions int
		agents   string
		hosts    string
	}
	dayMap := map[string]*ActivityDay{}
	rows, err := s.store.db.Query(`SELECT strftime('%Y-%m-%d',s.created_at/1000,'unixepoch') AS day,COUNT(*),
		GROUP_CONCAT(DISTINCT s.agent),GROUP_CONCAT(DISTINCT h.name)`+base+`
		AND s.created_at>=? AND s.created_at<=? GROUP BY day ORDER BY day`, append(args, ms(from), ms(to))...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for rows.Next() {
		var day dayRow
		if err := rows.Scan(&day.date, &day.sessions, &day.agents, &day.hosts); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		entry := &ActivityDay{Date: day.date, Sessions: day.sessions, Agents: splitList(day.agents), Hosts: splitList(day.hosts)}
		dayMap[day.date] = entry
		view.Sessions += day.sessions
	}
	rows.Close()

	runBase := ` FROM sessions s JOIN hosts h ON h.id=s.host_id JOIN runs r ON r.session_id=s.id` + where
	runRows, err := s.store.db.Query(`SELECT strftime('%Y-%m-%d',r.created_at/1000,'unixepoch') AS day,COUNT(*)`+runBase+`
		AND r.created_at>=? AND r.created_at<=? GROUP BY day ORDER BY day`, append(args, ms(from), ms(to))...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for runRows.Next() {
		var date string
		var runs int
		if err := runRows.Scan(&date, &runs); err != nil {
			runRows.Close()
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		entry := dayMap[date]
		if entry == nil {
			entry = &ActivityDay{Date: date, Agents: []string{}, Hosts: []string{}}
			dayMap[date] = entry
		}
		entry.Runs += runs
		view.Runs += runs
	}
	runRows.Close()

	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		if entry := dayMap[key]; entry != nil {
			view.Days = append(view.Days, *entry)
		} else {
			view.Days = append(view.Days, ActivityDay{Date: key, Agents: []string{}, Hosts: []string{}})
		}
	}
	writeJSON(w, http.StatusOK, view)
}
