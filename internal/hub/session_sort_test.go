package hub

import (
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
