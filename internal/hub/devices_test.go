package hub

import (
	"net/http"
	"testing"
)

func TestRevokeOtherDevices(t *testing.T) {
	hub := newSetupHub(t)
	owner, _ := hub.store.UserByUsername("owner")
	_, extraDevice, err := hub.store.CreateSession(owner.ID, "second device")
	if err != nil {
		t.Fatal(err)
	}
	status, body := hub.do(nil, http.MethodPost, "/api/v1/devices/revoke-others", map[string]any{}, nil)
	expectStatus(t, status, http.StatusOK, body, "revoke other devices")
	devices, err := hub.store.ListDevices(owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	var extraRevoked bool
	for _, d := range devices {
		if d.ID == extraDevice.ID && d.RevokedAt != nil {
			extraRevoked = true
		}
	}
	if !extraRevoked {
		t.Fatalf("extra device was not revoked: %+v", devices)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/devices", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "current device still signed in")
}
