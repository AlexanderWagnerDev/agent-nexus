package hub

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestSessionRelationsAPI(t *testing.T) {
	hub := newSetupHub(t)
	owner, _ := hub.store.UserByUsername("owner")
	hostID, err := hub.store.CreateHost(owner.ID, "relations-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	sessions := []protocol.Session{
		{Agent: "opencode", NativeID: "ses-src", Title: "Source", Cwd: "/work", State: protocol.StateIdle, CreatedAt: now, UpdatedAt: now},
		{Agent: "opencode", NativeID: "ses-fork", Title: "Fork", Cwd: "/work", State: protocol.StateIdle, CreatedAt: now, UpdatedAt: now, ForkedFromNativeID: "ses-src"},
		{Agent: "opencode", NativeID: "ses-child", Title: "Child", Cwd: "/work", State: protocol.StateIdle, CreatedAt: now, UpdatedAt: now, ParentNativeID: "ses-src"},
	}
	if _, _, err := hub.store.ReplaceHostSessions(hostID, sessions); err != nil {
		t.Fatal(err)
	}
	list, err := hub.store.ListSessions(owner.ID, hostID, "", "", "", "", 10, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	byNative := map[string]protocol.SessionView{}
	for _, s := range list {
		byNative[s.NativeID] = s
	}
	src := byNative["ses-src"]
	if src.ID == "" {
		t.Fatalf("source session missing: %+v", list)
	}
	status, body := hub.do(nil, http.MethodGet, "/api/v1/sessions/"+src.ID+"/related", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "session relations")
	var payload struct {
		Forks    []protocol.SessionView `json:"forks"`
		Children []protocol.SessionView `json:"children"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Forks) != 1 || payload.Forks[0].NativeID != "ses-fork" {
		t.Fatalf("forks: %+v", payload.Forks)
	}
	if len(payload.Children) != 1 || payload.Children[0].NativeID != "ses-child" {
		t.Fatalf("children: %+v", payload.Children)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions/"+byNative["ses-fork"].ID+"/related", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "fork relations")
	var forkPayload struct {
		Parent *protocol.SessionView `json:"parent"`
	}
	if err := json.Unmarshal(body, &forkPayload); err != nil {
		t.Fatal(err)
	}
	if forkPayload.Parent == nil || forkPayload.Parent.NativeID != "ses-src" {
		t.Fatalf("fork parent: %+v", forkPayload.Parent)
	}
	if err := hub.store.CreateRun("run-usage", src.ID, hostID, protocol.ModeStructured, owner.ID, "device"); err != nil {
		t.Fatal(err)
	}
	cost := 0.25
	if err := hub.store.UpsertRunUsage("run-usage", protocol.Usage{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 5, CostUSD: &cost}); err != nil {
		t.Fatal(err)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions/"+src.ID+"/usage", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "session usage")
	var usage SessionUsage
	if err := json.Unmarshal(body, &usage); err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 100 || usage.OutputTokens != 20 || usage.RunsWithUsage != 1 || usage.CostUSD != 0.25 {
		t.Fatalf("session usage: %+v", usage)
	}
}
