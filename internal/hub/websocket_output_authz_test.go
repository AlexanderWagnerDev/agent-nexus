package hub

import (
	"context"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

// Subscribing to a live run must not replay transcript-grade output or events
// to a principal that only holds a resume/terminal grant. The live broadcast
// path already enforces read_transcript; the replay path must enforce it too.
func TestSubscribeReplayRequiresReadTranscript(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	member, err := hub.store.CreateUserWithRole("member", "hash", "secret", "member")
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := hub.store.CreateHost(owner.ID, "authz-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.SetHostGrant(hostID, member.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	const runID = "run-replay-authz"
	if err := hub.store.CreateRun(runID, "", hostID, protocol.ModeTerminal, member.ID, "device"); err != nil {
		t.Fatal(err)
	}
	hub.server.appendRunOutput(runID, 1, []byte("secret terminal output"))
	hub.server.appendRunEvent(runID, protocol.RunEvent{Type: "approval.requested", ApprovalID: "a1", Text: "secret approval text"})

	conn := &clientConn{
		user: member,
		send: make(chan protocol.ServerEnvelope, 16),
		subs: map[string]bool{},
	}
	hub.server.handleClientMessage(context.Background(), conn, protocol.ClientEnvelope{Type: "subscribe", RunID: runID})

	var sawState bool
	for {
		select {
		case msg := <-conn.send:
			if msg.Type == "run.output" || msg.Type == "run.event" {
				t.Fatalf("replayed protected content without read_transcript: %+v", msg)
			}
			if msg.Type == "run.state" {
				sawState = true
			}
			continue
		default:
		}
		break
	}
	if !sawState {
		t.Fatal("run.state was not delivered to the run owner")
	}

	// Granting read_transcript must make the same replay available.
	if err := hub.store.SetHostGrant(hostID, member.ID, "resume,read_transcript"); err != nil {
		t.Fatal(err)
	}
	hub.server.clearPermissionCache()
	hub.server.handleClientMessage(context.Background(), conn, protocol.ClientEnvelope{Type: "subscribe", RunID: runID, LastSeq: 0})
	var sawOutput, sawEvent bool
	for {
		select {
		case msg := <-conn.send:
			sawOutput = sawOutput || msg.Type == "run.output"
			sawEvent = sawEvent || msg.Type == "run.event"
			continue
		default:
		}
		break
	}
	if !sawOutput || !sawEvent {
		t.Fatalf("read_transcript did not restore replay: output=%v event=%v", sawOutput, sawEvent)
	}
}
