package hub

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestApprovalMetricsAnsweredResolvedAndScopedByRun(t *testing.T) {
	m := newMetrics()
	m.approvalRequested("run-1", "a1")
	if len(m.approvalStart) != 1 {
		t.Fatalf("requested approval was not recorded: %d", len(m.approvalStart))
	}
	m.approvalAnswered("run-1", "a1")
	if len(m.approvalStart) != 0 || m.approvals != 1 {
		t.Fatalf("answered approval was not cleaned up: %+v", m.approvalStart)
	}

	// The same approval id in two runs must not collide.
	m.approvalRequested("run-1", "same")
	m.approvalRequested("run-2", "same")
	m.approvalAnswered("run-1", "same")
	if len(m.approvalStart) != 1 {
		t.Fatalf("run scoping failed: %+v", m.approvalStart)
	}
	m.approvalsEnded("run-2")
	if len(m.approvalStart) != 0 {
		t.Fatalf("approvalsEnded left entries: %+v", m.approvalStart)
	}
	if len(m.approvalByRun) != 0 {
		t.Fatalf("run index leaked: %+v", m.approvalByRun)
	}
}

func TestApprovalMetricsHardCapEvictsOldest(t *testing.T) {
	m := newMetrics()
	for i := 0; i < approvalMetricCap+50; i++ {
		m.approvalRequested("run", "approval-"+strconv.Itoa(i))
	}
	if len(m.approvalStart) > approvalMetricCap {
		t.Fatalf("hard cap exceeded: %d", len(m.approvalStart))
	}
	if _, ok := m.approvalStart[approvalKey("run", "approval-0")]; ok {
		t.Fatal("oldest approval was not evicted")
	}
	if _, ok := m.approvalStart[approvalKey("run", "approval-"+strconv.Itoa(approvalMetricCap+49))]; !ok {
		t.Fatal("newest approval was not kept")
	}
	if len(m.approvalByRun["run"]) > approvalMetricCap {
		t.Fatalf("run index exceeded the cap: %d", len(m.approvalByRun["run"]))
	}
}

func TestApprovalMetricsConcurrentUse(t *testing.T) {
	m := newMetrics()
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "a-" + strconv.Itoa(i)
			m.approvalRequested("run", id)
			m.approvalAnswered("run", id)
		}(i)
	}
	wg.Wait()
	if len(m.approvalStart) != 0 || len(m.approvalByRun) != 0 {
		t.Fatalf("concurrent approvals leaked: %+v", m.approvalStart)
	}
	if m.approvals != 200 {
		t.Fatalf("answered counter: %d", m.approvals)
	}
}

func TestApprovalMetricsClearedByRunLifecycle(t *testing.T) {
	hub := newSetupHub(t)
	signer, err := OpenHubSigner(filepath.Join(t.TempDir(), "signing"))
	if err != nil {
		t.Fatal(err)
	}
	hub.server.SetSigner(signer)
	owner, _ := hub.store.UserByUsername("owner")
	host := startFakeHost(t, hub, owner, "approval-host")
	host.setAnswer(func(method string, params json.RawMessage) (any, string) {
		return map[string]any{}, ""
	})
	host.send(protocol.Envelope{Type: "sessions", Full: true, Sessions: []protocol.Session{{
		Agent: "opencode", NativeID: "approval-session", Title: "Approvals", Cwd: "/work/demo",
		State: protocol.StateIdle, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}})
	waitFor(t, "approval session", func() bool {
		list, err := hub.store.ListSessions(owner.ID, host.hostID, "", "", "", "", 10, 0, true)
		return err == nil && len(list) == 1
	})
	sess := sessionViewByNative(t, hub, owner, host.hostID, "approval-session")
	status, body := hub.do(nil, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs", map[string]any{"mode": "structured"}, nil)
	expectStatus(t, status, http.StatusAccepted, body, "create approval run")
	var run protocol.RunView
	if err := json.Unmarshal(body, &run); err != nil {
		t.Fatal(err)
	}
	host.waitCommand(t, "run.start")

	pending := func() bool {
		hub.server.metrics.mu.Lock()
		defer hub.server.metrics.mu.Unlock()
		return len(hub.server.metrics.approvalStart) == 1
	}

	// A host-side timeout or auto-decision arrives as approval.resolved.
	host.send(protocol.Envelope{Type: "run.event", RunID: run.ID, Event: &protocol.RunEvent{Type: "approval.requested", ApprovalID: "a-timeout"}})
	waitFor(t, "approval requested", pending)
	host.send(protocol.Envelope{Type: "run.event", RunID: run.ID, Event: &protocol.RunEvent{Type: "approval.resolved", ApprovalID: "a-timeout", Decision: "reject"}})
	waitFor(t, "approval resolved", func() bool { return !pending() })

	// A normal host-side run end clears a pending approval.
	host.send(protocol.Envelope{Type: "run.event", RunID: run.ID, Event: &protocol.RunEvent{Type: "approval.requested", ApprovalID: "a-exit"}})
	waitFor(t, "second approval requested", pending)
	host.send(protocol.Envelope{Type: "run.exit", RunID: run.ID, Code: 0, Reason: "done"})
	waitFor(t, "approval cleared by exit", func() bool { return !pending() })

	// A host disconnect marks the run failed and clears its approvals.
	status, body = hub.do(nil, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/runs", map[string]any{"mode": "structured"}, nil)
	expectStatus(t, status, http.StatusAccepted, body, "create disconnect run")
	var second protocol.RunView
	if err := json.Unmarshal(body, &second); err != nil {
		t.Fatal(err)
	}
	host.waitCommand(t, "run.start")
	host.send(protocol.Envelope{Type: "run.started", RunID: second.ID})
	host.send(protocol.Envelope{Type: "run.event", RunID: second.ID, Event: &protocol.RunEvent{Type: "approval.requested", ApprovalID: "a-drop"}})
	waitFor(t, "disconnect approval requested", pending)
	host.cancel()
	waitFor(t, "run failed after disconnect", func() bool {
		got, _, err := hub.store.RunByID(second.ID)
		return err == nil && got.State == protocol.StateFailed
	})
	waitFor(t, "approval cleared by disconnect", func() bool { return !pending() })
}
