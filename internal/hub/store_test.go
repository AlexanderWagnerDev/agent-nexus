package hub

import (
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestStoreSessionsAndAudit(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.CreateUser("owner", "hash", "secret")
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := s.CreateHost(u.ID, "host", []byte("01234567890123456789012345678901"), "linux", "amd64", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	n, del, err := s.ReplaceHostSessions(hostID, []protocol.Session{
		{NativeID: "a", Agent: "codex", Title: "one", Cwd: "/tmp/a", CreatedAt: now, UpdatedAt: now, MessageCount: 2, State: protocol.StateIdle},
		{NativeID: "b", Agent: "opencode", Title: "two", Cwd: "/tmp/b", CreatedAt: now, UpdatedAt: now, MessageCount: 4, State: protocol.StateIdle},
	})
	if err != nil || n != 2 || del != 0 {
		t.Fatalf("unexpected snapshot result: %d %d %v", n, del, err)
	}
	sessions, err := s.ListSessions(u.ID, "", "", "", "", "", 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	n, del, err = s.ReplaceHostSessions(hostID, []protocol.Session{
		{NativeID: "a", Agent: "codex", Title: "one updated", Cwd: "/tmp/a", CreatedAt: now, UpdatedAt: now.Add(time.Minute), MessageCount: 3, State: protocol.StateRunning},
	})
	if err != nil || n != 1 || del != 1 {
		t.Fatalf("unexpected second snapshot: %d %d %v", n, del, err)
	}
	if err := s.AppendAudit("owner", "test", map[string]string{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendAudit("owner", "test2", nil); err != nil {
		t.Fatal(err)
	}
	if broken, err := s.VerifyAudit(); err != nil || broken != 0 {
		t.Fatalf("audit chain broken at %d: %v", broken, err)
	}
}

func TestPairingCodeSingleUse(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("owner", "hash", "secret")
	if err := s.CreatePairingCode(u.ID, "ABCD-EFGH", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumePairingCode("ABCD-EFGH"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumePairingCode("ABCD-EFGH"); err == nil {
		t.Fatal("pairing code was accepted twice")
	}
}
