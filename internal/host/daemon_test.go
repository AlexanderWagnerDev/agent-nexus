package host

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestDiffSessionSnapshot(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	base := []protocol.Session{
		{Agent: "codex", NativeID: "a", Title: "one", UpdatedAt: now},
		{Agent: "opencode", NativeID: "b", Title: "two", UpdatedAt: now},
	}
	changed, removed, next := diffSessionSnapshot(nil, base, false)
	if len(changed) != 2 || len(removed) != 0 || len(next) != 2 {
		t.Fatalf("first snapshot: changed=%d removed=%d next=%d", len(changed), len(removed), len(next))
	}
	changed, removed, _ = diffSessionSnapshot(next, base, false)
	if len(changed) != 0 || len(removed) != 0 {
		t.Fatalf("unchanged snapshot produced work: changed=%d removed=%d", len(changed), len(removed))
	}
	updated := []protocol.Session{
		{Agent: "codex", NativeID: "a", Title: "one changed", UpdatedAt: now.Add(time.Minute)},
		{Agent: "opencode", NativeID: "b", Title: "two", UpdatedAt: now},
	}
	changed, removed, next2 := diffSessionSnapshot(next, updated, false)
	if len(changed) != 1 || changed[0].Title != "one changed" || len(removed) != 0 {
		t.Fatalf("update diff: changed=%+v removed=%+v", changed, removed)
	}
	changed, removed, _ = diffSessionSnapshot(next2, updated[:1], false)
	if len(changed) != 0 || len(removed) != 1 || removed[0].Agent != "opencode" || removed[0].NativeID != "b" {
		t.Fatalf("removal diff: changed=%+v removed=%+v", changed, removed)
	}
	changed, removed, _ = diffSessionSnapshot(next2, updated[:1], true)
	if len(changed) != 1 || len(removed) != 0 {
		t.Fatalf("forced snapshot should be full: changed=%d removed=%d", len(changed), len(removed))
	}
}

func TestConfigPathOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "isolated.json")
	t.Setenv("NEXUSD_CONFIG", want)
	got, err := ConfigPath()
	if err != nil || got != want {
		t.Fatalf("ConfigPath() = %q, %v; want %q", got, err, want)
	}
}
