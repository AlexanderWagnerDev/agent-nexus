package hub

import "testing"

func TestReportedHostNameFollowsChangesWithoutRevertingRenames(t *testing.T) {
	hub := newSetupHub(t)
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "nexus.exe", make([]byte, 32), "windows", "amd64", "0.1.3")
	if err != nil {
		t.Fatal(err)
	}

	changed, err := hub.store.SyncReportedHostName(hostID, "nexus.exe")
	if err != nil || changed {
		t.Fatalf("unchanged reported name: changed=%v err=%v", changed, err)
	}

	if err := hub.store.RenameHost(hostID, "Desk"); err != nil {
		t.Fatal(err)
	}
	changed, err = hub.store.SyncReportedHostName(hostID, "nexus.exe")
	if err != nil || changed {
		t.Fatalf("reconnect reverted the admin rename: changed=%v err=%v", changed, err)
	}
	host, err := hub.store.HostByID(hostID)
	if err != nil || host.Name != "Desk" {
		t.Fatalf("display name after reconnect: %q %v", host.Name, err)
	}

	changed, err = hub.store.SyncReportedHostName(hostID, "NewPC")
	if err != nil || !changed {
		t.Fatalf("changed reported name: changed=%v err=%v", changed, err)
	}
	host, err = hub.store.HostByID(hostID)
	if err != nil || host.Name != "NewPC" {
		t.Fatalf("display name after rename: %q %v", host.Name, err)
	}
}
