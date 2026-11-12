package hub

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestSessionTitleSortUsesPerUserLocalTitle(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "sort-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, _, err := hub.store.ReplaceHostSessions(hostID, []protocol.Session{
		{NativeID: "zulu", Agent: "opencode", Title: "Zulu", Cwd: "/work/zulu", CreatedAt: now, UpdatedAt: now},
		{NativeID: "alpha", Agent: "opencode", Title: "Alpha", Cwd: "/work/alpha", CreatedAt: now, UpdatedAt: now},
		{NativeID: "mike", Agent: "opencode", Title: "Mike", Cwd: "/work/mike", CreatedAt: now, UpdatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	list, err := hub.store.ListSessions(owner.ID, hostID, "", "", "", "", 10, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	byNative := map[string]protocol.SessionView{}
	for _, sess := range list {
		byNative[sess.NativeID] = sess
	}
	if len(byNative) != 3 {
		t.Fatalf("sessions: %+v", list)
	}
	// The local override sorts as "Aardvark" instead of "Zulu".
	if err := hub.store.SetSessionLocalTitle(owner.ID, byNative["zulu"].ID, "Aardvark"); err != nil {
		t.Fatal(err)
	}

	asc, err := hub.store.ListSessionsFiltered(owner.ID, SessionFilter{Sort: "title", Order: "asc", Limit: 10}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(asc) != 3 || asc[0].NativeID != "zulu" || asc[1].NativeID != "alpha" || asc[2].NativeID != "mike" {
		t.Fatalf("ascending title sort ignored local title: %+v", asc)
	}
	if asc[0].LocalTitle != "Aardvark" || asc[0].Title != "Zulu" {
		t.Fatalf("native title was not preserved: %+v", asc[0])
	}

	desc, err := hub.store.ListSessionsFiltered(owner.ID, SessionFilter{Sort: "title", Order: "desc", Limit: 10}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(desc) != 3 || desc[0].NativeID != "mike" || desc[1].NativeID != "alpha" || desc[2].NativeID != "zulu" {
		t.Fatalf("descending title sort ignored local title: %+v", desc)
	}

	// Pagination must follow the same ordering without duplicates.
	seen := map[string]bool{}
	for offset := 0; offset < 3; offset++ {
		page, err := hub.store.ListSessionsFiltered(owner.ID, SessionFilter{Sort: "title", Order: "asc", Limit: 1, Offset: offset}, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) != 1 || seen[page[0].NativeID] {
			t.Fatalf("pagination at offset %d: %+v", offset, page)
		}
		seen[page[0].NativeID] = true
	}
	if len(seen) != 3 {
		t.Fatalf("pagination skipped sessions: %v", seen)
	}

	// Another user's local title must not affect the owner's ordering.
	hash, err := HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	other, err := hub.store.CreateUserWithRole("other-sorter", hash, NewTOTPSecret(), "member")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.SetSessionLocalTitle(other.ID, byNative["zulu"].ID, "zzzz"); err != nil {
		t.Fatal(err)
	}
	if err := hub.store.SetHostGrant(hostID, other.ID, "view,read_transcript"); err != nil {
		t.Fatal(err)
	}
	otherAsc, err := hub.store.ListSessionsFiltered(other.ID, SessionFilter{Sort: "title", Order: "asc", Limit: 10}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherAsc) != 3 || otherAsc[0].NativeID != "alpha" || otherAsc[2].NativeID != "zulu" {
		t.Fatalf("another user's override leaked into the sort: %+v", otherAsc)
	}
}

func TestSessionsAPIReturnsTotalAndStablePages(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "total-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, _, err := hub.store.ReplaceHostSessions(hostID, []protocol.Session{
		{NativeID: "n1", Agent: "opencode", Title: "One", Cwd: "/work/one", CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour)},
		{NativeID: "n2", Agent: "opencode", Title: "Two", Cwd: "/work/two", CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)},
		{NativeID: "n3", Agent: "opencode", Title: "Three", Cwd: "/work/three", CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}
	status, body := hub.do(nil, http.MethodGet, "/api/v1/sessions?limit=1&sort=created&order=asc", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "first page")
	var page struct {
		Sessions []protocol.SessionView `json:"sessions"`
		HasMore  bool                   `json:"has_more"`
		Offset   int                    `json:"offset"`
		Total    int                    `json:"total"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Sessions) != 1 || !page.HasMore || page.Sessions[0].NativeID != "n1" {
		t.Fatalf("first page: %+v", page)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions?limit=1&offset=1&sort=created&order=asc", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "second page")
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.Sessions[0].NativeID != "n2" || !page.HasMore {
		t.Fatalf("second page: %+v", page)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/sessions?limit=1&offset=2&sort=created&order=asc", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "third page")
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.Sessions[0].NativeID != "n3" || page.HasMore {
		t.Fatalf("third page: %+v", page)
	}
}
