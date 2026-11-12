package hub

import (
	"database/sql"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

type UsageTotals struct {
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	CostRuns         int     `json:"cost_runs"`
	Runs             int     `json:"runs"`
	RunsWithUsage    int     `json:"runs_with_usage"`
	ReportedRuns     int     `json:"reported_runs"`
	EstimatedRuns    int     `json:"estimated_runs"`
	UnavailableRuns  int     `json:"unavailable_runs"`
}

type UsageGroup struct {
	Name             string  `json:"name"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	Runs             int     `json:"runs"`
	RunsWithUsage    int     `json:"runs_with_usage"`
}

type UsageDay struct {
	Date         string  `json:"date"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	Runs         int     `json:"runs"`
}

type UsageRun struct {
	RunID        string    `json:"run_id"`
	SessionID    string    `json:"session_id"`
	SessionTitle string    `json:"session_title"`
	Agent        string    `json:"agent"`
	HostID       string    `json:"host_id"`
	HostName     string    `json:"host_name"`
	StartedAt    time.Time `json:"started_at"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CostUSD      *float64  `json:"cost_usd,omitempty"`
	Source       string    `json:"source"`
}

type UsageReport struct {
	Summary   UsageTotals  `json:"summary"`
	ByAgent   []UsageGroup `json:"by_agent"`
	ByHost    []UsageGroup `json:"by_host"`
	ByProject []UsageGroup `json:"by_project"`
	BySession []UsageGroup `json:"by_session"`
	Daily     []UsageDay   `json:"daily"`
	Runs      []UsageRun   `json:"runs"`
}

func (s *Store) UpsertRunUsage(runID string, usage protocol.Usage) error {
	source := usage.Source
	if source == "" {
		source = "agent"
	}
	var cost any
	if usage.CostUSD != nil {
		cost = *usage.CostUSD
	}
	_, err := s.db.Exec(`INSERT INTO run_usage(run_id,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,cost_usd,source,updated_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(run_id) DO UPDATE SET input_tokens=excluded.input_tokens, output_tokens=excluded.output_tokens,
		cache_read_tokens=excluded.cache_read_tokens, cache_write_tokens=excluded.cache_write_tokens,
		cost_usd=excluded.cost_usd, source=excluded.source, updated_at=excluded.updated_at`,
		runID, usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheWriteTokens,
		cost, source, ms(time.Now()))
	return err
}

// UsageReport aggregates reported usage only. Runs without measurements are
// counted as unavailable rather than estimated.
func (s *Store) UsageReport(userID string, all bool, since, until time.Time, hostID, agent string) (UsageReport, error) {
	report := UsageReport{ByAgent: []UsageGroup{}, ByHost: []UsageGroup{}, ByProject: []UsageGroup{}, BySession: []UsageGroup{}, Daily: []UsageDay{}, Runs: []UsageRun{}}
	where := ` WHERE h.revoked_at IS NULL AND r.created_at>=? AND r.created_at<=?`
	args := []any{ms(since), ms(until)}
	if !all {
		where += ` AND (h.owner_id=? OR EXISTS (SELECT 1 FROM host_grants g WHERE g.host_id=h.id AND g.user_id=?))`
		args = append(args, userID, userID)
	}
	if hostID != "" {
		where += ` AND r.host_id=?`
		args = append(args, hostID)
	}
	if agent != "" {
		where += ` AND COALESCE(s.agent,'')=?`
		args = append(args, agent)
	}
	base := ` FROM runs r JOIN hosts h ON h.id=r.host_id LEFT JOIN sessions s ON s.id=r.session_id
		LEFT JOIN run_usage u ON u.run_id=r.id` + where

	row := s.db.QueryRow(`SELECT
		COALESCE(SUM(u.input_tokens),0),COALESCE(SUM(u.output_tokens),0),
		COALESCE(SUM(u.cache_read_tokens),0),COALESCE(SUM(u.cache_write_tokens),0),
		COALESCE(SUM(CASE WHEN u.cost_usd IS NULL THEN 0 ELSE u.cost_usd END),0),
		COALESCE(SUM(CASE WHEN u.cost_usd IS NULL THEN 0 ELSE 1 END),0),
		COUNT(*),COUNT(u.run_id),
		COALESCE(SUM(CASE WHEN u.source='agent' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN u.source='estimated' THEN 1 ELSE 0 END),0)`+base, args...)
	if err := row.Scan(&report.Summary.InputTokens, &report.Summary.OutputTokens,
		&report.Summary.CacheReadTokens, &report.Summary.CacheWriteTokens,
		&report.Summary.CostUSD, &report.Summary.CostRuns, &report.Summary.Runs,
		&report.Summary.RunsWithUsage, &report.Summary.ReportedRuns, &report.Summary.EstimatedRuns); err != nil {
		return report, err
	}
	report.Summary.UnavailableRuns = report.Summary.Runs - report.Summary.RunsWithUsage

	group := func(expr string) ([]UsageGroup, error) {
		rows, err := s.db.Query(`SELECT `+expr+` AS group_name,
			COALESCE(SUM(u.input_tokens),0),COALESCE(SUM(u.output_tokens),0),
			COALESCE(SUM(u.cache_read_tokens),0),COALESCE(SUM(u.cache_write_tokens),0),
			COALESCE(SUM(CASE WHEN u.cost_usd IS NULL THEN 0 ELSE u.cost_usd END),0),
			COUNT(*),COUNT(u.run_id)`+base+` GROUP BY group_name ORDER BY (COALESCE(SUM(u.input_tokens),0)+COALESCE(SUM(u.output_tokens),0)) DESC LIMIT 50`, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []UsageGroup{}
		for rows.Next() {
			var g UsageGroup
			if err := rows.Scan(&g.Name, &g.InputTokens, &g.OutputTokens, &g.CacheReadTokens, &g.CacheWriteTokens, &g.CostUSD, &g.Runs, &g.RunsWithUsage); err != nil {
				return nil, err
			}
			out = append(out, g)
		}
		return out, rows.Err()
	}
	var err error
	if report.ByAgent, err = group(`COALESCE(NULLIF(s.agent,''),'unknown')`); err != nil {
		return report, err
	}
	if report.ByHost, err = group(`COALESCE(NULLIF(h.name,''),h.id)`); err != nil {
		return report, err
	}
	if report.ByProject, err = group(`COALESCE(NULLIF(s.repo_url,''),NULLIF(s.cwd,''),'unknown')`); err != nil {
		return report, err
	}
	if report.BySession, err = group(`COALESCE(NULLIF(s.title,''),r.session_id,'unknown')`); err != nil {
		return report, err
	}

	rows, err := s.db.Query(`SELECT strftime('%Y-%m-%d',r.created_at/1000,'unixepoch') AS day,
		COALESCE(SUM(u.input_tokens),0),COALESCE(SUM(u.output_tokens),0),
		COALESCE(SUM(CASE WHEN u.cost_usd IS NULL THEN 0 ELSE u.cost_usd END),0),COUNT(*)`+base+`
		GROUP BY day ORDER BY day`, args...)
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var day UsageDay
		if err := rows.Scan(&day.Date, &day.InputTokens, &day.OutputTokens, &day.CostUSD, &day.Runs); err != nil {
			rows.Close()
			return report, err
		}
		report.Daily = append(report.Daily, day)
	}
	rows.Close()

	runRows, err := s.db.Query(`SELECT r.id,COALESCE(r.session_id,''),COALESCE(s.title,''),COALESCE(s.agent,''),
		r.host_id,COALESCE(h.name,''),r.created_at,
		COALESCE(u.input_tokens,0),COALESCE(u.output_tokens,0),u.cost_usd,COALESCE(u.source,'')`+base+`
		ORDER BY r.created_at DESC LIMIT 100`, args...)
	if err != nil {
		return report, err
	}
	defer runRows.Close()
	for runRows.Next() {
		var item UsageRun
		var started int64
		var cost sql.NullFloat64
		if err := runRows.Scan(&item.RunID, &item.SessionID, &item.SessionTitle, &item.Agent,
			&item.HostID, &item.HostName, &started, &item.InputTokens, &item.OutputTokens, &cost, &item.Source); err != nil {
			return report, err
		}
		item.StartedAt = fromMS(started)
		if cost.Valid {
			value := cost.Float64
			item.CostUSD = &value
		}
		report.Runs = append(report.Runs, item)
	}
	return report, runRows.Err()
}
