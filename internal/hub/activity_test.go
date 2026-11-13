package hub

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestProjectActivityAggregatesSessionsAndRuns(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "activity-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, _, err := hub.store.ReplaceHostSessions(hostID, []protocol.Session{{
		Agent: "codex", NativeID: "activity-1", Title: "Activity", Cwd: "/work/activity",
		RepoURL: "https://github.com/example/activity", CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}
	sess := sessionViewByNative(t, hub, owner, hostID, "activity-1")
	if err := hub.store.CreateRun(newID(), sess.ID, hostID, protocol.ModeStructured, owner.ID, "d"); err != nil {
		t.Fatal(err)
	}
	status, body := hub.do(nil, http.MethodGet,
		"/api/v1/projects/activity?host="+hostID+"&project=/work/activity", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "activity")
	var view ActivityView
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatal(err)
	}
	if view.Sessions != 1 || view.Runs != 1 {
		t.Fatalf("activity totals: %+v", view)
	}
	if len(view.Days) != 91 && len(view.Days) != 92 {
		t.Fatalf("unexpected day count: %d", len(view.Days))
	}
	found := false
	for _, day := range view.Days {
		if day.Date == now.Format("2006-01-02") {
			found = day.Sessions == 1 && day.Runs == 1 && len(day.Agents) == 1 && day.Hosts[0] == "activity-host"
		}
	}
	if !found {
		t.Fatalf("today missing from activity: %+v", view.Days)
	}
	createdDay := now.Add(-72 * time.Hour).Format("2006-01-02")
	for _, day := range view.Days {
		if day.Date == createdDay && day.Sessions != 0 {
			t.Fatalf("heatmap counted the creation day instead of the activity day: %+v", day)
		}
	}
	// The same data is reachable by repository identity across hosts.
	status, body = hub.do(nil, http.MethodGet,
		"/api/v1/projects/activity?repo=https://github.com/example/activity", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "activity by repo")
}
