package hub

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestCreateFirstUserIsAtomic(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	const workers = 8
	var wg sync.WaitGroup
	var successes atomic.Int32
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.CreateFirstUser(fmt.Sprintf("owner-%d", i), "hash", "secret")
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrSetupComplete):
			default:
				t.Errorf("unexpected setup error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if got := successes.Load(); got != 1 {
		t.Fatalf("concurrent setup created %d owners, want 1", got)
	}
	if n, err := s.UserCount(); err != nil || n != 1 {
		t.Fatalf("user count = %d, %v", n, err)
	}
}

func TestHubRestartClearsStaleSessionState(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "restart-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, _, err := hub.store.ReplaceHostSessions(hostID, []protocol.Session{{
		Agent: "codex", NativeID: "restart-session", Title: "Restart", Cwd: "/work/restart",
		State: protocol.StateRunning, ActiveElsewhere: true, CreatedAt: now, UpdatedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}

	// A second Server on the same store simulates a hub restart before any
	// host reconnects.
	restarted := NewServer(Config{}, hub.store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	list, err := restarted.store.ListSessions(owner.ID, hostID, "", "", "", "", 10, 0, true)
	if err != nil || len(list) != 1 {
		t.Fatalf("sessions after restart: %+v %v", list, err)
	}
	if list[0].State != protocol.StateIdle || list[0].ActiveElsewhere {
		t.Fatalf("stale live state survived the restart: %+v", list[0])
	}
}

func TestHubRestartClearsStaleRunsAndSessionsCanBeResumed(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "restart-runs", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, _, err := hub.store.ReplaceHostSessions(hostID, []protocol.Session{{
		Agent: "codex", NativeID: "restart-run-session", Title: "Restart run", Cwd: "/work/restart",
		CreatedAt: now, UpdatedAt: now, MessageCount: 1, State: protocol.StateIdle,
	}}); err != nil {
		t.Fatal(err)
	}
	sess, err := hub.store.SessionByNativeID(owner.ID, hostID, "codex", "restart-run-session", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateRunAt("stale-run", sess.ID, hostID, "/work/restart", protocol.ModeTerminal, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	hub.server.setRunState("stale-run", protocol.StateRunning)

	// A second Server on the same store simulates a hub restart while the
	// host stays offline.
	restarted := NewServer(Config{}, hub.store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	run, _, err := restarted.store.RunByID("stale-run")
	if err != nil {
		t.Fatal(err)
	}
	if run.State != protocol.StateFailed || run.EndedAt == nil || run.ErrorCode != ErrCodeNetwork {
		t.Fatalf("stale run was not closed: %+v", run)
	}
	if id, active := restarted.store.ActiveRunForSession(sess.ID); active {
		t.Fatalf("session is still blocked by run %s", id)
	}
}

func TestHostReconnectReactivatesAdoptedRun(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "adopt-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, _, err := hub.store.ReplaceHostSessions(hostID, []protocol.Session{{
		Agent: "codex", NativeID: "adopt-session", Title: "Adopt", Cwd: "/work/adopt",
		CreatedAt: now, UpdatedAt: now, MessageCount: 1, State: protocol.StateIdle,
	}}); err != nil {
		t.Fatal(err)
	}
	sess, err := hub.store.SessionByNativeID(owner.ID, hostID, "codex", "adopt-session", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateRunAt("adopt-run", sess.ID, hostID, "/work/adopt", protocol.ModeStructured, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	// A disconnect closes the run; the host then proves the process survived.
	hub.server.setRunState("adopt-run", protocol.StateFailed)
	hub.server.adoptHostRuns(hostID, []protocol.RunInfo{{RunID: "adopt-run", SessionID: sess.ID, State: protocol.StateRunning}})

	run, _, err := hub.store.RunByID("adopt-run")
	if err != nil {
		t.Fatal(err)
	}
	if run.State != protocol.StateRunning || run.EndedAt != nil {
		t.Fatalf("adopted run is not active again: %+v", run)
	}
	if id, active := hub.store.ActiveRunForSession(sess.ID); !active || id != "adopt-run" {
		t.Fatalf("adopted run does not block a second writer: %q %v", id, active)
	}
}

func TestFinishedRunStateIsReleased(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "memory-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateRun("memory-run", "", hostID, protocol.ModeTerminal, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	hub.server.appendRunOutput("memory-run", 1, []byte("buffered terminal output"))
	hub.server.setRunState("memory-run", protocol.StateIdle)

	// A live run must keep its replay buffer.
	hub.server.cleanupRunState("memory-run")
	hub.server.mu.Lock()
	_, kept := hub.server.runs["memory-run"]
	hub.server.mu.Unlock()
	if !kept {
		t.Fatal("cleanup dropped the state of a live run")
	}

	hub.server.setRunState("memory-run", protocol.StateExited)
	hub.server.cleanupRunState("memory-run")
	hub.server.mu.Lock()
	_, keptRun := hub.server.runs["memory-run"]
	_, keptHost := hub.server.runHosts["memory-run"]
	hub.server.mu.Unlock()
	if keptRun || keptHost {
		t.Fatalf("finished run was not released: runs=%v runHosts=%v", keptRun, keptHost)
	}
}

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

func TestStoreFiltersSessionsByNormalizedRepoURL(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("owner", "hash", "secret")
	now := time.Now().UTC().Truncate(time.Second)
	for _, hostName := range []string{"linux-builder", "windows-dev"} {
		hostID, err := s.CreateHost(u.ID, hostName, make([]byte, 32), "linux", "amd64", "0.1.0")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.ReplaceHostSessions(hostID, []protocol.Session{
			{NativeID: "same-" + hostName, Agent: "codex", Title: "shared repo", Cwd: "/work/" + hostName, RepoURL: "https://github.com/example/sample-api", CreatedAt: now, UpdatedAt: now, State: protocol.StateIdle},
			{NativeID: "other-" + hostName, Agent: "codex", Title: "other repo", Cwd: "/work/other-" + hostName, RepoURL: "https://github.com/example/other", CreatedAt: now, UpdatedAt: now, State: protocol.StateIdle},
		}); err != nil {
			t.Fatal(err)
		}
	}
	filtered, err := s.ListSessionsFiltered(u.ID, SessionFilter{RepoURL: "https://github.com/example/sample-api/", Limit: 20}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected both hosts for the same remote, got %d", len(filtered))
	}
	total, err := s.CountSessionsFiltered(u.ID, SessionFilter{RepoURL: "https://github.com/example/sample-api"}, true)
	if err != nil || total != 2 {
		t.Fatalf("repo count: %d %v", total, err)
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

func TestAuditChainSurvivesConcurrentAppends(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	const writers = 16
	const perWriter = 12
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				if err := s.AppendAudit(fmt.Sprintf("user-%d", writer), "test.event", map[string]int{"i": j}); err != nil {
					t.Errorf("append: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	broken, err := s.VerifyAudit()
	if err != nil {
		t.Fatal(err)
	}
	if broken != 0 {
		t.Fatalf("audit chain broke at sequence %d", broken)
	}
	events, err := s.ListAudit(1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != writers*perWriter {
		t.Fatalf("expected %d events, got %d", writers*perWriter, len(events))
	}
}

func TestApplyHostSessionDelta(t *testing.T) {
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
		{NativeID: "a", Agent: "codex", Title: "one", Cwd: "/work/a", CreatedAt: now, UpdatedAt: now},
		{NativeID: "b", Agent: "opencode", Title: "two", Cwd: "/work/b", CreatedAt: now, UpdatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	upserted, deleted, err := s.ApplyHostSessionDelta(hostID,
		[]protocol.Session{{NativeID: "a", Agent: "codex", Title: "one changed", Cwd: "/work/a", CreatedAt: now, UpdatedAt: now.Add(time.Minute)}},
		[]protocol.SessionKey{{Agent: "opencode", NativeID: "b"}})
	if err != nil || upserted != 1 || deleted != 1 {
		t.Fatalf("delta: %d %d %v", upserted, deleted, err)
	}
	sessions, err := s.ListSessions(u.ID, hostID, "", "", "", "", 10, 0, true)
	if err != nil || len(sessions) != 1 || sessions[0].Title != "one changed" {
		t.Fatalf("delta result: %+v %v", sessions, err)
	}
	if _, _, err := s.ApplyHostSessionDelta(hostID, nil, []protocol.SessionKey{{Agent: "codex", NativeID: "a"}}); err != nil {
		t.Fatal(err)
	}
	sessions, _ = s.ListSessions(u.ID, hostID, "", "", "", "", 10, 0, true)
	if len(sessions) != 0 {
		t.Fatalf("explicit removal failed: %+v", sessions)
	}
}

func TestBackupSnapshot(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.CreateUser("owner", "hash", "secret")
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := s.CreateHost(u.ID, "demo", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReplaceHostSessions(hostID, benchmarkSessions(50)); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/backup.db"
	if err := s.Backup(path); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(path); err == nil {
		t.Fatal("overwriting an existing backup was allowed")
	}
	copyStore, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer copyStore.Close()
	if n, err := copyStore.UserCount(); err != nil || n != 1 {
		t.Fatalf("backup content: %d %v", n, err)
	}
	sessions, err := copyStore.ListSessions(u.ID, hostID, "", "", "", "", 100, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 50 {
		t.Fatalf("backup lost sessions: got %d want 50", len(sessions))
	}
}
