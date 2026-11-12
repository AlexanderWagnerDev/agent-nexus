package hub

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestNotificationPreferencesPerHost(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, err := hub.store.CreateHost(owner.ID, "notify-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	otherHostID, err := hub.store.CreateHost(owner.ID, "other-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}

	// Global defaults: completed off.
	status, body := hub.do(nil, http.MethodPut, "/api/v1/me/settings", map[string]any{"notifications": map[string]bool{"completed": false}}, nil)
	expectStatus(t, status, http.StatusOK, body, "global notification prefs")

	prefs, err := hub.store.EffectiveNotificationPrefs(owner.ID, hostID)
	if err != nil {
		t.Fatal(err)
	}
	if prefs["completed"] || !prefs["failed"] || !prefs["approval"] {
		t.Fatalf("global effective prefs: %+v", prefs)
	}

	// Host override: completed on, failed off, unknown key ignored.
	status, body = hub.do(nil, http.MethodPut, "/api/v1/me/settings/notifications/hosts/"+hostID,
		map[string]any{"notifications": map[string]bool{"completed": true, "failed": false, "bogus": true}}, nil)
	expectStatus(t, status, http.StatusOK, body, "host notification prefs")
	prefs, err = hub.store.EffectiveNotificationPrefs(owner.ID, hostID)
	if err != nil {
		t.Fatal(err)
	}
	if !prefs["completed"] || prefs["failed"] || prefs["disconnected"] != true {
		t.Fatalf("host effective prefs: %+v", prefs)
	}
	if _, ok := prefs["bogus"]; ok {
		t.Fatalf("unknown notification key was stored: %+v", prefs)
	}
	other, err := hub.store.EffectiveNotificationPrefs(owner.ID, otherHostID)
	if err != nil {
		t.Fatal(err)
	}
	if other["completed"] || !other["failed"] {
		t.Fatalf("host override leaked to another host: %+v", other)
	}

	// The settings response exposes stored overrides for the UI.
	status, body = hub.do(nil, http.MethodGet, "/api/v1/me/settings", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "settings with host overrides")
	var settings struct {
		Notifications       map[string]bool            `json:"notifications"`
		NotificationsByHost map[string]map[string]bool `json:"notifications_by_host"`
	}
	if err := json.Unmarshal(body, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Notifications["completed"] {
		t.Fatalf("global completed pref: %+v", settings.Notifications)
	}
	if override := settings.NotificationsByHost[hostID]; !override["completed"] || override["failed"] {
		t.Fatalf("stored host override: %+v", settings.NotificationsByHost)
	}

	// Reset returns to the global default.
	status, body = hub.do(nil, http.MethodDelete, "/api/v1/me/settings/notifications/hosts/"+hostID, nil, nil)
	expectStatus(t, status, http.StatusOK, body, "reset host notification prefs")
	prefs, err = hub.store.EffectiveNotificationPrefs(owner.ID, hostID)
	if err != nil {
		t.Fatal(err)
	}
	if prefs["completed"] {
		t.Fatalf("reset did not restore the global value: %+v", prefs)
	}

	// A member without a grant cannot address another host.
	member, token := hub.sessionFor("member", "notify-member")
	_ = member
	status, body = hub.do(hub.clientWithToken(token), http.MethodPut, "/api/v1/me/settings/notifications/hosts/"+otherHostID,
		map[string]any{"notifications": map[string]bool{"approval": false}}, nil)
	expectStatus(t, status, http.StatusNotFound, body, "unshared host")
	status, body = hub.do(nil, http.MethodPut, "/api/v1/me/settings/notifications/hosts/missing",
		map[string]any{"notifications": map[string]bool{"approval": false}}, nil)
	expectStatus(t, status, http.StatusNotFound, body, "unknown host")
}

func TestEffectiveNotificationPreferencesOrder(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	hostID, _ := hub.store.CreateHost(owner.ID, "order-host", make([]byte, 32), "linux", "amd64", "alpha")
	now := time.Now()
	_ = now
	if err := hub.store.SetNotificationPrefs(owner.ID, map[string]bool{"approval": false, "failed": false}); err != nil {
		t.Fatal(err)
	}
	if err := hub.store.SetNotificationHostPrefs(owner.ID, hostID, map[string]bool{"approval": true}); err != nil {
		t.Fatal(err)
	}
	prefs, err := hub.store.EffectiveNotificationPrefs(owner.ID, hostID)
	if err != nil {
		t.Fatal(err)
	}
	if !prefs["approval"] || prefs["failed"] || !prefs["completed"] {
		t.Fatalf("priority order: %+v", prefs)
	}
}
