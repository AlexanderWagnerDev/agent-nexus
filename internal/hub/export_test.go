package hub

import (
	"strings"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestUserTheme(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("owner", "hash", "secret")
	theme, err := s.UserTheme(u.ID)
	if err != nil || theme != "system" {
		t.Fatalf("default theme = %q %v", theme, err)
	}
	if err := s.SetUserTheme(u.ID, "light"); err != nil {
		t.Fatal(err)
	}
	if theme, err = s.UserTheme(u.ID); err != nil || theme != "light" {
		t.Fatalf("stored theme = %q %v", theme, err)
	}
}

func TestActiveRunsForUser(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	owner, _ := s.CreateUser("owner", "hash", "secret")
	viewer, _ := s.CreateUserWithRole("viewer", "hash2", "secret2", "viewer")
	hostID, _ := s.CreateHost(owner.ID, "host", make([]byte, 32), "linux", "amd64", "alpha")
	if err := s.SetHostGrant(hostID, viewer.ID, "view"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun("run1", "", hostID, protocol.ModeTerminal, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	runs, err := s.ActiveRunsForUser(viewer.ID, false)
	if err != nil || len(runs) != 1 || runs[0].ID != "run1" {
		t.Fatalf("granted viewer did not see the run: %+v %v", runs, err)
	}
	stranger, _ := s.CreateUserWithRole("stranger", "hash3", "secret3", "viewer")
	runs, err = s.ActiveRunsForUser(stranger.ID, false)
	if err != nil || len(runs) != 0 {
		t.Fatalf("stranger saw runs: %+v %v", runs, err)
	}
	if err := s.CreateRun("run2", "sess-x", hostID, protocol.ModeStructured, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	if id, ok := s.ActiveRunForSession("sess-x"); !ok || id != "run2" {
		t.Fatalf("active run not found: %q %v", id, ok)
	}
	s.UpdateRunState("run2", protocol.StateExited)
	if _, ok := s.ActiveRunForSession("sess-x"); ok {
		t.Fatal("ended run still considered active")
	}
}

func TestHostOwnsRun(t *testing.T) {
	s, err := OpenStore(t.TempDir() + "/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	owner, _ := s.CreateUser("owner", "hash", "secret")
	hostA, _ := s.CreateHost(owner.ID, "a", make([]byte, 32), "linux", "amd64", "alpha")
	hostB, _ := s.CreateHost(owner.ID, "b", make([]byte, 32), "linux", "amd64", "alpha")
	if err := s.CreateRun("run1", "", hostA, protocol.ModeTerminal, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	srv := &Server{store: s, runHosts: map[string]string{}}
	if !srv.hostOwnsRun(hostA, "run1") {
		t.Fatal("owning host was rejected")
	}
	if srv.hostOwnsRun(hostB, "run1") {
		t.Fatal("foreign host was accepted")
	}
	if srv.hostOwnsRun(hostA, "missing") {
		t.Fatal("unknown run was accepted")
	}
}

func TestOriginAllowed(t *testing.T) {
	s := &Server{cfg: Config{PublicURL: "https://nexus.example.com"}}
	if !s.originAllowed("https://nexus.example.com", "hub:8080") {
		t.Fatal("public origin rejected behind a proxy")
	}
	if !s.originAllowed("http://hub:8080", "hub:8080") {
		t.Fatal("direct host origin rejected")
	}
	if s.originAllowed("https://evil.example", "hub:8080") {
		t.Fatal("foreign origin accepted")
	}
}

func TestMarkdownExport(t *testing.T) {
	md := markdownExport(transcriptExport{
		Session: protocol.SessionView{Title: "Fix auth", HostName: "laptop", Agent: "codex", Cwd: "/tmp/project"},
		Messages: []protocol.Message{
			{Role: "user", Kind: "text", Text: "Fix the login"},
			{Role: "assistant", Kind: "tool_call", ToolName: "shell", ToolInput: `{"cmd":"go test"}`},
		},
	})
	for _, want := range []string{"# Fix auth", "- Agent: codex", "Fix the login", "**Tool call:** `shell`", `{"cmd":"go test"}`} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown export is missing %q:\n%s", want, md)
		}
	}
}
