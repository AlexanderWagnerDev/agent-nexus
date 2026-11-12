package hub

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestUsageReportAggregatesReportedMetadata(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "usage-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, _, err := hub.store.ReplaceHostSessions(hostID, []protocol.Session{
		{Agent: "codex", NativeID: "usage-a", Title: "Alpha", Cwd: "/work/alpha", CreatedAt: now, UpdatedAt: now},
		{Agent: "opencode", NativeID: "usage-b", Title: "Beta", Cwd: "/work/beta", CreatedAt: now, UpdatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	alpha := sessionViewByNative(t, hub, owner, hostID, "usage-a")
	beta := sessionViewByNative(t, hub, owner, hostID, "usage-b")
	cost := 0.125
	runA := newID()
	runB := newID()
	if err := hub.store.CreateRun(runA, alpha.ID, hostID, protocol.ModeStructured, owner.ID, "device-a"); err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateRun(runB, beta.ID, hostID, protocol.ModeStructured, owner.ID, "device-a"); err != nil {
		t.Fatal(err)
	}
	if err := hub.store.UpsertRunUsage(runA, protocol.Usage{
		InputTokens: 100, OutputTokens: 40, CacheReadTokens: 20, CacheWriteTokens: 5, CostUSD: &cost, Source: "agent",
	}); err != nil {
		t.Fatal(err)
	}
	if err := hub.store.UpsertRunUsage(runB, protocol.Usage{InputTokens: 10, OutputTokens: 5, Source: "agent"}); err != nil {
		t.Fatal(err)
	}
	// A third run without usage must count as unavailable, not estimated.
	runC := newID()
	if err := hub.store.CreateRun(runC, alpha.ID, hostID, protocol.ModeStructured, owner.ID, "device-a"); err != nil {
		t.Fatal(err)
	}

	status, body := hub.do(nil, http.MethodGet,
		"/api/v1/usage?since="+now.Add(-time.Hour).Format(time.RFC3339)+"&until="+now.Add(time.Hour).Format(time.RFC3339), nil, nil)
	expectStatus(t, status, http.StatusOK, body, "usage report")
	var report UsageReport
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.InputTokens != 110 || report.Summary.OutputTokens != 45 ||
		report.Summary.CacheReadTokens != 20 || report.Summary.CacheWriteTokens != 5 {
		t.Fatalf("summary tokens: %+v", report.Summary)
	}
	if report.Summary.Runs != 3 || report.Summary.RunsWithUsage != 2 || report.Summary.UnavailableRuns != 1 {
		t.Fatalf("summary run counts: %+v", report.Summary)
	}
	if report.Summary.CostRuns != 1 || report.Summary.CostUSD != 0.125 {
		t.Fatalf("summary cost: %+v", report.Summary)
	}
	if len(report.ByAgent) != 2 || report.ByAgent[0].Name != "codex" {
		t.Fatalf("by agent: %+v", report.ByAgent)
	}
	if len(report.ByHost) != 1 || report.ByHost[0].Runs != 3 || report.ByHost[0].RunsWithUsage != 2 {
		t.Fatalf("by host: %+v", report.ByHost)
	}
	if len(report.ByProject) != 2 {
		t.Fatalf("by project: %+v", report.ByProject)
	}
	if len(report.Daily) != 1 || report.Daily[0].Runs != 3 {
		t.Fatalf("daily: %+v", report.Daily)
	}
	if len(report.Runs) != 3 {
		t.Fatalf("runs: %+v", report.Runs)
	}
	foundCost := false
	for _, run := range report.Runs {
		if run.RunID == runA && run.CostUSD != nil && *run.CostUSD == 0.125 {
			foundCost = true
		}
	}
	if !foundCost {
		t.Fatalf("reported cost missing from runs: %+v", report.Runs)
	}

	status, body = hub.do(nil, http.MethodGet,
		"/api/v1/usage?since="+now.Add(time.Hour).Format(time.RFC3339)+"&until="+now.Format(time.RFC3339), nil, nil)
	expectStatus(t, status, http.StatusBadRequest, body, "inverted range")
	status, body = hub.do(nil, http.MethodGet,
		"/api/v1/usage?since="+now.Add(-500*24*time.Hour).Format(time.RFC3339), nil, nil)
	expectStatus(t, status, http.StatusBadRequest, body, "oversized range")
}
