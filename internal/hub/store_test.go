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
	sessions, err := s.ListSessions(u.ID, "", "", "", "", "", 10, 0, true)
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

func TestStoreProjectsFavoritesAndNotifications(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("owner", "hash", "secret")
	hostID, err := s.CreateHost(u.ID, "alpha", make([]byte, 32), "linux", "amd64", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, _, err := s.ReplaceHostSessions(hostID, []protocol.Session{
		{NativeID: "a", Agent: "codex", Title: "one", Cwd: "/work/app", GitBranch: "main", CreatedAt: now, UpdatedAt: now, State: protocol.StateRunning},
		{NativeID: "b", Agent: "opencode", Title: "two", Cwd: "/work/app", CreatedAt: now, UpdatedAt: now, State: protocol.StateIdle},
		{NativeID: "c", Agent: "codex", Title: "three", Cwd: "/work/other", CreatedAt: now, UpdatedAt: now.Add(-time.Hour), State: protocol.StateIdle},
	}); err != nil {
		t.Fatal(err)
	}
	projects, err := s.ListProjects(u.ID, true, 10)
	if err != nil || len(projects) != 2 {
		t.Fatalf("projects: %+v %v", projects, err)
	}
	if projects[0].Sessions != 2 || projects[0].Active != 1 || projects[0].Branch != "main" {
		t.Fatalf("unexpected project aggregate: %+v", projects[0])
	}
	sessions, _ := s.ListSessions(u.ID, "", "", "", "", "", 10, 0, true)
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}
	if err := s.SetSessionFavorite(u.ID, sessions[0].ID, true); err != nil {
		t.Fatal(err)
	}
	favorites, err := s.FavoriteSessions(u.ID, true)
	if err != nil || len(favorites) != 1 || favorites[0].ID != sessions[0].ID {
		t.Fatalf("favorites: %+v %v", favorites, err)
	}
	if err := s.SetSessionFavorite(u.ID, sessions[0].ID, false); err != nil {
		t.Fatal(err)
	}
	if favorites, _ := s.FavoriteSessions(u.ID, true); len(favorites) != 0 {
		t.Fatalf("favorite was not removed: %+v", favorites)
	}
	if err := s.AddNotification(u.ID, "completed", "Run finished", "A run finished.", "run-1", "/#/runs/run-1"); err != nil {
		t.Fatal(err)
	}
	if seen, err := s.NotificationExists(u.ID, "run-1", "completed"); err != nil || !seen {
		t.Fatalf("notification dedupe lookup failed: %v %v", seen, err)
	}
	items, unread, err := s.ListNotifications(u.ID, 10)
	if err != nil || len(items) != 1 || unread != 1 || items[0].Read {
		t.Fatalf("notifications: %+v %d %v", items, unread, err)
	}
	remaining, err := s.MarkNotificationsRead(u.ID, "")
	if err != nil || remaining != 0 {
		t.Fatalf("mark read: %d %v", remaining, err)
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

func TestStepUpWindow(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("owner", "hash", "secret")
	token, _, err := s.CreateSession(u.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	si, err := s.SessionByToken(token)
	if err != nil || !si.StepUpUntil.IsZero() {
		t.Fatalf("unexpected initial step-up: %v %v", si.StepUpUntil, err)
	}
	if err := s.MarkStepUp(token, time.Minute); err != nil {
		t.Fatal(err)
	}
	si, err = s.SessionByToken(token)
	if err != nil || !si.StepUpUntil.After(time.Now()) {
		t.Fatalf("step-up not recorded: %v %v", si.StepUpUntil, err)
	}
}

func TestHostConfirmation(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("owner", "hash", "secret")
	hostID, err := s.CreateHost(u.ID, "host", make([]byte, 32), "linux", "amd64", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	sas := protocol.HostSAS(hostID, make([]byte, 32))
	if err := s.CreateHostConfirmation(hostID, sas); err != nil {
		t.Fatal(err)
	}
	got, confirmed, exists, err := s.HostConfirmation(hostID)
	if err != nil || !exists || confirmed || got != sas {
		t.Fatalf("unexpected pending confirmation: %q %v %v %v", got, confirmed, exists, err)
	}
	hosts, err := s.ListHosts(u.ID, true)
	if err != nil || len(hosts) != 1 || !hosts[0].Pending || hosts[0].SAS != sas {
		t.Fatalf("pending host not exposed: %+v %v", hosts, err)
	}
	if err := s.ConfirmHost(u.ID, hostID); err != nil {
		t.Fatal(err)
	}
	hosts, err = s.ListHosts(u.ID, true)
	if err != nil || len(hosts) != 1 || hosts[0].Pending {
		t.Fatalf("host still pending after confirm: %+v %v", hosts, err)
	}
}

func TestHostGrant(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	owner, _ := s.CreateUser("owner", "hash", "secret")
	viewer, err := s.CreateUserWithRole("viewer", "hash2", "secret2", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := s.CreateHost(owner.ID, "host", make([]byte, 32), "linux", "amd64", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetHostGrant(hostID, viewer.ID, "view,resume"); err != nil {
		t.Fatal(err)
	}
	perms, err := s.HostGrantPermissions(hostID, viewer.ID)
	if err != nil || perms != "view,resume" {
		t.Fatalf("unexpected grant: %q %v", perms, err)
	}
	hosts, err := s.ListHosts(viewer.ID, false)
	if err != nil || len(hosts) != 1 {
		t.Fatalf("granted host not listed: %v %v", hosts, err)
	}
	hosts, err = s.ListHosts(viewer.ID, true)
	if err != nil || len(hosts) != 1 {
		t.Fatalf("admin host list failed: %v %v", hosts, err)
	}
}
