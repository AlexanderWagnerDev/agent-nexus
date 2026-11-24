package hub

import (
	"context"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// Terminal resize is host input. A revoked terminal_input grant must stop it
// even while the device still holds the controller lease of a live run.
func TestResizeRequiresTerminalInputPermission(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	host := startFakeHost(t, hub, owner, "resize-host")

	member, err := hub.store.CreateUserWithRole("member", "hash", "secret", "member")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.SetHostGrant(host.hostID, member.ID, "view"); err != nil {
		t.Fatal(err)
	}
	_, device, err := hub.store.CreateSession(member.ID, "test browser")
	if err != nil {
		t.Fatal(err)
	}
	const runID = "run-resize-authz"
	if err := hub.store.CreateRun(runID, "", host.hostID, protocol.ModeTerminal, member.ID, device.ID); err != nil {
		t.Fatal(err)
	}

	conn := &clientConn{
		user:   member,
		device: device,
		send:   make(chan protocol.ServerEnvelope, 8),
	}
	resize := protocol.ClientEnvelope{Type: "resize", RunID: runID, Cols: 200, Rows: 60}

	hub.server.handleClientMessage(context.Background(), conn, resize)
	select {
	case msg := <-host.gotCh:
		t.Fatalf("resize reached the host without terminal_input: %+v", msg)
	case <-time.After(300 * time.Millisecond):
	}

	if err := hub.store.SetHostGrant(host.hostID, member.ID, "view,terminal_input"); err != nil {
		t.Fatal(err)
	}
	hub.server.clearPermissionCache()
	hub.server.handleClientMessage(context.Background(), conn, resize)
	msg := host.waitCommand(t, "run.resize")
	if msg.RunID != runID {
		t.Fatalf("resize for the wrong run: %+v", msg)
	}
}
