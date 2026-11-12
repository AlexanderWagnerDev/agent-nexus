package hub

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestSessionLocalMetadataAndFiltering(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "meta-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, _, err := hub.store.ReplaceHostSessions(hostID, []protocol.Session{
		{NativeID: "native-a", Agent: "opencode", Title: "Alpha session", Cwd: "/work/alpha", CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour), MessageCount: 2},
		{NativeID: "native-b", Agent: "codex", Title: "Beta session", Cwd: "/work/beta", CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now, MessageCount: 9},
	}); err != nil {
		t.Fatal(err)
	}
	sessions, err := hub.store.ListSessions(owner.ID, hostID, "", "", "", "", 10, 0, true)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("sessions missing: %v %v", sessions, err)
	}
	alphaID, betaID := "", ""
	for _, sess := range sessions {
		switch sess.Title {
		case "Alpha session":
			alphaID = sess.ID
		case "Beta session":
			betaID = sess.ID
		}
	}

	status, body := hub.do(nil, http.MethodPatch, "/api/v1/sessions/"+alphaID, map[string]any{"local_title": "My local alpha"}, nil)
	expectStatus(t, status, http.StatusOK, body, "local title")
	status, body = hub.do(nil, http.MethodPost, "/api/v1/sessions/"+alphaID+"/tags", map[string]any{"tag": "frontend"}, nil)
	expectStatus(t, status, http.StatusOK, body, "add tag")
	status, body = hub.do(nil, http.MethodPost, "/api/v1/sessions/"+betaID+"/tags", map[string]any{"tag": "backend"}, nil)
	expectStatus(t, status, http.StatusOK, body, "add tag")

	// The native title must survive the local override.
	raw, err := hub.store.SessionByID(owner.ID, alphaID, true)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Title != "Alpha session" || raw.LocalTitle != "My local alpha" {
		t.Fatalf("native title was modified: %+v", raw)
	}

	// Tag filter and message-count ordering are server-side.
	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions?tag=frontend", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "tag filter")
	var list struct {
		Sessions []protocol.SessionView `json:"sessions"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Sessions) != 1 || list.Sessions[0].LocalTitle != "My local alpha" {
		t.Fatalf("tag filter returned %+v", list.Sessions)
	}

	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions?sort=messages&order=desc", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "sort by messages")
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Sessions) < 2 || list.Sessions[0].Title != "Beta session" {
		t.Fatalf("message ordering is wrong: %+v", list.Sessions)
	}

	// RFC 3339 bounds are validated, not silently ignored.
	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions?since=yesterday", nil, nil)
	expectStatus(t, status, http.StatusBadRequest, body, "invalid since")
	since := url.QueryEscape(now.Add(-30 * time.Minute).Format(time.RFC3339))
	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions?since="+since, nil, nil)
	expectStatus(t, status, http.StatusOK, body, "valid since")
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Sessions) != 1 || list.Sessions[0].Title != "Beta session" {
		t.Fatalf("time filter returned %+v", list.Sessions)
	}

	status, body = hub.do(nil, http.MethodDelete, "/api/v1/sessions/"+alphaID+"/tags/frontend", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "remove tag")
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	status, body = hub.do(nil, http.MethodPost, "/api/v1/sessions/"+alphaID+"/tags", map[string]any{"tag": "not valid!"}, nil)
	expectStatus(t, status, http.StatusBadRequest, body, "invalid tag")
}
