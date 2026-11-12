package hub

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestStructuredForkAdoptsNewSessionWithoutTouchingSource(t *testing.T) {
	hub := newSetupHub(t)
	signer, err := OpenHubSigner(filepath.Join(t.TempDir(), "signing"))
	if err != nil {
		t.Fatal(err)
	}
	hub.server.SetSigner(signer)
	owner := hub.testOwner()
	host := startFakeHost(t, hub, owner, "fork-host")
	host.setAnswer(func(method string, params json.RawMessage) (any, string) {
		return map[string]any{}, ""
	})
	now := time.Now()
	host.send(protocol.Envelope{Type: "sessions", Full: true, Sessions: []protocol.Session{{
		Agent: "codex", NativeID: "source-thread", Title: "Source conversation",
		Cwd: "/work/source", State: protocol.StateIdle, CreatedAt: now, UpdatedAt: now,
	}}})
	waitFor(t, "source session", func() bool {
		list, err := hub.store.ListSessions(owner.ID, host.hostID, "", "", "", "", 10, 0, true)
		return err == nil && len(list) == 1
	})
	source := sessionViewByNative(t, hub, owner, host.hostID, "source-thread")

	status, body := hub.do(nil, http.MethodPost, "/api/v1/sessions/"+source.ID+"/runs",
		map[string]any{"mode": "structured", "fork": true}, nil)
	expectStatus(t, status, http.StatusAccepted, body, "fork run")
	var run protocol.RunView
	if err := json.Unmarshal(body, &run); err != nil {
		t.Fatal(err)
	}
	host.waitCommand(t, "run.start")

	// The host reports the new native thread after thread/fork.
	host.send(protocol.Envelope{Type: "run.started", RunID: run.ID, Reason: "structured", NativeID: "forked-thread"})
	waitFor(t, "run rebound", func() bool {
		got, _, err := hub.store.RunByID(run.ID)
		return err == nil && got.SessionID != source.ID
	})
	rebound, _, err := hub.store.RunByID(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rebound.SessionID == source.ID {
		t.Fatal("run still points at the source session")
	}
	forked, err := hub.store.SessionByID(owner.ID, rebound.SessionID, true)
	if err != nil {
		t.Fatal(err)
	}
	if forked.NativeID != "forked-thread" || forked.Agent != "codex" || forked.HostID != host.hostID {
		t.Fatalf("forked session: %+v", forked)
	}
	if forked.Title != "Fork of Source conversation" {
		t.Fatalf("forked title: %q", forked.Title)
	}
	if forked.Cwd != "/work/source" || forked.ID == source.ID {
		t.Fatalf("forked session fields: %+v", forked)
	}

	unchanged, err := hub.store.SessionByID(owner.ID, source.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Title != "Source conversation" || unchanged.NativeID != "source-thread" || unchanged.Cwd != "/work/source" {
		t.Fatalf("source session was modified: %+v", unchanged)
	}

	// A duplicate run.started is idempotent.
	host.send(protocol.Envelope{Type: "run.started", RunID: run.ID, Reason: "structured", NativeID: "forked-thread"})
	time.Sleep(50 * time.Millisecond)
	again, _, err := hub.store.RunByID(run.ID)
	if err != nil || again.SessionID != rebound.SessionID {
		t.Fatalf("duplicate adoption changed the run: %+v %v", again, err)
	}
}

func TestAdoptForkedRunRejectsInvalidNativeID(t *testing.T) {
	for _, id := range []string{"", "bad\x00id", string(make([]byte, 201))} {
		if validForkNativeID(id) {
			t.Fatalf("invalid native id accepted: %q", id)
		}
	}
	if !validForkNativeID("thread-123") {
		t.Fatal("valid native id rejected")
	}
}
