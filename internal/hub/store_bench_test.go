package hub

import (
	"fmt"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func benchmarkSessions(count int) []protocol.Session {
	now := time.Now().UTC()
	out := make([]protocol.Session, count)
	for i := range out {
		out[i] = protocol.Session{
			Agent: "opencode", NativeID: fmt.Sprintf("ses_bench_%05d", i),
			Title: fmt.Sprintf("Session %d", i), Cwd: fmt.Sprintf("/work/project-%d", i%100),
			GitBranch: "main", CreatedAt: now, UpdatedAt: now.Add(-time.Duration(i) * time.Second),
			MessageCount: i % 500, State: protocol.StateIdle,
		}
	}
	return out
}

func BenchmarkReplaceHostSessions5000(b *testing.B) {
	store, err := OpenStore(b.TempDir() + "/hub.db")
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	user, _ := store.CreateUser("owner", "hash", "secret")
	hostID, _ := store.CreateHost(user.ID, "bench", make([]byte, 32), "linux", "amd64", "0.3.1")
	sessions := benchmarkSessions(5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := store.ReplaceHostSessions(hostID, sessions); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListSessions5000(b *testing.B) {
	store, err := OpenStore(b.TempDir() + "/hub.db")
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	user, _ := store.CreateUser("owner", "hash", "secret")
	hostID, _ := store.CreateHost(user.ID, "bench", make([]byte, 32), "linux", "amd64", "0.3.1")
	if _, _, err := store.ReplaceHostSessions(hostID, benchmarkSessions(5000)); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.ListSessions(user.ID, "", "", "", "", "", 500, 0, true); err != nil {
			b.Fatal(err)
		}
	}
}
