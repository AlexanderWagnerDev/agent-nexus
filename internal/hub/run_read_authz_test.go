package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func newRunReadRequest(runID string, u User) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+runID, nil)
	req.SetPathValue("id", runID)
	return req.WithContext(context.WithValue(req.Context(), ctxUser, u))
}

func TestRunReadRequiresHostViewAndHidesApprovalText(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	member, err := hub.store.CreateUserWithRole("member", "hash", "secret", "member")
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := hub.store.CreateHost(owner.ID, "read-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.SetHostGrant(hostID, member.ID, "view"); err != nil {
		t.Fatal(err)
	}
	const runID = "run-read-authz"
	if err := hub.store.CreateRun(runID, "", hostID, protocol.ModeTerminal, member.ID, "device"); err != nil {
		t.Fatal(err)
	}
	hub.server.appendRunEvent(runID, protocol.RunEvent{
		Type: "approval.requested", ApprovalID: "a1", Text: "secret command",
	})

	rec := httptest.NewRecorder()
	hub.server.handleRun(rec, newRunReadRequest(runID, member))
	if rec.Code != http.StatusOK {
		t.Fatalf("view-only run read: %d %s", rec.Code, rec.Body.String())
	}
	var view protocol.RunView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.PendingApproval != nil {
		t.Fatalf("approval text leaked without read_transcript/approve: %+v", view.PendingApproval)
	}

	if err := hub.store.SetHostGrant(hostID, member.ID, "view,approve"); err != nil {
		t.Fatal(err)
	}
	hub.server.clearPermissionCache()
	rec = httptest.NewRecorder()
	hub.server.handleRun(rec, newRunReadRequest(runID, member))
	if rec.Code != http.StatusOK {
		t.Fatalf("approve run read: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.PendingApproval == nil {
		t.Fatal("approve permission did not expose the pending approval")
	}

	if _, err := hub.store.db.Exec(`DELETE FROM host_grants WHERE host_id=? AND user_id=?`, hostID, member.ID); err != nil {
		t.Fatal(err)
	}
	hub.server.clearPermissionCache()
	rec = httptest.NewRecorder()
	hub.server.handleRun(rec, newRunReadRequest(runID, member))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("revoked host grant still read the run: %d %s", rec.Code, rec.Body.String())
	}
}
