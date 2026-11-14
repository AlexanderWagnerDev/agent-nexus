package hub

import (
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// A host update carries agent inventory, policy roots and update state. It
// must only reach clients that are allowed to view that host.
func TestHostUpdateBroadcastRespectsHostGrants(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	member, err := hub.store.CreateUserWithRole("member", "hash", "secret", "member")
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := hub.store.CreateHost(owner.ID, "grant-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.SetHostGrant(hostID, member.ID, "view"); err != nil {
		t.Fatal(err)
	}

	ownerConn := &clientConn{user: owner, send: make(chan protocol.ServerEnvelope, 8)}
	memberConn := &clientConn{user: member, send: make(chan protocol.ServerEnvelope, 8)}
	hub.server.mu.Lock()
	hub.server.clients[ownerConn] = struct{}{}
	hub.server.clients[memberConn] = struct{}{}
	hub.server.mu.Unlock()
	defer func() {
		hub.server.mu.Lock()
		delete(hub.server.clients, ownerConn)
		delete(hub.server.clients, memberConn)
		hub.server.mu.Unlock()
	}()

	hub.server.broadcastHostUpdate(hostID)
	select {
	case msg := <-ownerConn.send:
		if msg.Type != "host.updated" || msg.Host == nil || msg.Host.ID != hostID {
			t.Fatalf("unexpected owner message: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("host owner did not receive the update")
	}
	select {
	case msg := <-memberConn.send:
		if msg.Type != "host.updated" || msg.Host == nil || msg.Host.ID != hostID {
			t.Fatalf("member with a view grant received %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("member with a view grant did not receive the update")
	}

	// Without a grant the same client must not see the host projection.
	if err := hub.store.DeleteHostGrant(hostID, member.ID); err != nil {
		t.Fatal(err)
	}
	hub.server.broadcastHostUpdate(hostID)
	<-ownerConn.send
	select {
	case msg := <-memberConn.send:
		if msg.Type == "host.updated" {
			t.Fatalf("host update leaked to a user without a grant: %+v", msg.Host)
		}
	case <-time.After(200 * time.Millisecond):
	}
}

func TestGrantSemanticsAndViewerCeiling(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	viewer, err := hub.store.CreateUserWithRole("viewer", "hash", "secret", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	member, err := hub.store.CreateUserWithRole("member", "hash", "secret", "member")
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := hub.store.CreateHost(owner.ID, "grant-rules", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	// An explicit write grant must not lift the viewer read-only ceiling.
	if err := hub.store.SetHostGrant(hostID, viewer.ID, "manage"); err != nil {
		t.Fatal(err)
	}
	if hub.server.hostPermission(viewer, hostID, "manage") {
		t.Fatal("viewer gained write access through a manage grant")
	}
	if !hub.server.hostPermission(viewer, hostID, "view") {
		t.Fatal("viewer lost read access")
	}
	// A narrow grant still grants visibility of the host.
	if err := hub.store.SetHostGrant(hostID, member.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	if !hub.server.hostPermission(member, hostID, "view") {
		t.Fatal("a grant row did not grant visibility")
	}
	if !hub.server.hostPermission(member, hostID, "resume") {
		t.Fatal("explicit resume grant rejected")
	}
}

func TestRunScopedBroadcastHidesPromptTextFromOtherUsers(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	stranger, err := hub.store.CreateUserWithRole("stranger", "hash", "secret", "member")
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := hub.store.CreateHost(owner.ID, "broadcast-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateRun("run-broadcast", "", hostID, protocol.ModeStructured, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	ownerConn := &clientConn{user: owner, send: make(chan protocol.ServerEnvelope, 8), subs: map[string]bool{"run-broadcast": true}}
	strangerConn := &clientConn{user: stranger, send: make(chan protocol.ServerEnvelope, 8), subs: map[string]bool{}}
	hub.server.mu.Lock()
	hub.server.clients[ownerConn] = struct{}{}
	hub.server.clients[strangerConn] = struct{}{}
	hub.server.mu.Unlock()
	defer func() {
		hub.server.mu.Lock()
		delete(hub.server.clients, ownerConn)
		delete(hub.server.clients, strangerConn)
		hub.server.mu.Unlock()
	}()

	hub.server.broadcast(protocol.ServerEnvelope{Type: "run.prompt.sent", RunID: "run-broadcast", Message: "secret prompt text"})
	select {
	case msg := <-ownerConn.send:
		if msg.Message != "secret prompt text" {
			t.Fatalf("owner received %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive the queued prompt event")
	}
	select {
	case msg := <-strangerConn.send:
		t.Fatalf("prompt text leaked to another user: %+v", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestStopOnFinishedRunDoesNotResurrectIt(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "stop-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateRun("finished-run", "", hostID, protocol.ModeTerminal, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	hub.server.setRunState("finished-run", protocol.StateExited)
	status, body := hub.do(nil, "POST", "/api/v1/runs/finished-run/stop", nil, nil)
	if status != 409 {
		t.Fatalf("stop of a finished run returned %d: %s", status, body)
	}
	run, _, err := hub.store.RunByID("finished-run")
	if err != nil {
		t.Fatal(err)
	}
	if run.State != protocol.StateExited || run.EndedAt == nil {
		t.Fatalf("finished run was rewritten: %+v", run)
	}
}

func TestViewerGrantIsReducedToReadOnly(t *testing.T) {
	if got := permissionsForRole("viewer", "manage,resume,view"); got != "view" {
		t.Fatalf("viewer grant kept write permissions: %q", got)
	}
	if got := permissionsForRole("member", "manage,resume,view"); got != "manage,resume,view" {
		t.Fatalf("member grant was reduced: %q", got)
	}
	if got := permissionsForRole("viewer", "manage"); got != "view,read_transcript" {
		t.Fatalf("viewer grant without read permissions: %q", got)
	}
}

func TestPermissionCacheClearsOnGrantChange(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	member, err := hub.store.CreateUserWithRole("cache-member", "hash", "secret", "member")
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := hub.store.CreateHost(owner.ID, "cache-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.SetHostGrant(hostID, member.ID, "view"); err != nil {
		t.Fatal(err)
	}
	if !hub.server.hostPermission(member, hostID, "view") {
		t.Fatal("grant was not applied")
	}
	if err := hub.store.DeleteHostGrant(hostID, member.ID); err != nil {
		t.Fatal(err)
	}
	hub.server.clearPermissionCache()
	if hub.server.hostPermission(member, hostID, "view") {
		t.Fatal("revoked grant survived the cache clear")
	}
}

func TestLateEventDoesNotReopenAFinishedRun(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "late-event-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateRun("late-event-run", "", hostID, protocol.ModeStructured, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	hub.server.appendRunEvent("late-event-run", protocol.RunEvent{Type: "run.state", State: protocol.StateIdle})
	hub.server.appendRunEvent("late-event-run", protocol.RunEvent{Type: "run.state", State: protocol.StateExited})
	hub.server.appendRunEvent("late-event-run", protocol.RunEvent{Type: "run.state", State: protocol.StateRunning})
	run, _, err := hub.store.RunByID("late-event-run")
	if err != nil {
		t.Fatal(err)
	}
	if run.State != protocol.StateExited || run.EndedAt == nil {
		t.Fatalf("late event reopened a finished run: %+v", run)
	}
	rs := hub.server.runState("late-event-run")
	rs.mu.Lock()
	state := rs.state
	rs.mu.Unlock()
	if state != protocol.StateExited {
		t.Fatalf("in-memory run state = %q", state)
	}
}
