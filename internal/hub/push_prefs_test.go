package hub

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestNotificationPreferencesPersistAndDefault(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	status, body := hub.do(nil, http.MethodGet, "/api/v1/me/settings", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "settings")
	var settings struct {
		Theme         string          `json:"theme"`
		Notifications map[string]bool `json:"notifications"`
	}
	if err := json.Unmarshal(body, &settings); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"approval", "completed", "failed", "disconnected"} {
		if !settings.Notifications[kind] {
			t.Fatalf("%s should default to enabled: %s", kind, body)
		}
	}

	status, body = hub.do(nil, http.MethodPut, "/api/v1/me/settings", map[string]any{
		"notifications": map[string]bool{"completed": false, "disconnected": false},
	}, nil)
	expectStatus(t, status, http.StatusOK, body, "update prefs")
	if err := json.Unmarshal(body, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Notifications["completed"] || settings.Notifications["disconnected"] {
		t.Fatalf("prefs not applied: %s", body)
	}
	if !settings.Notifications["approval"] {
		t.Fatalf("unmentioned pref should stay enabled: %s", body)
	}

	// Theme-only updates (the appearance control) must not erase the push
	// preferences.
	status, body = hub.do(nil, http.MethodPut, "/api/v1/me/settings", map[string]any{"theme": "dark"}, nil)
	expectStatus(t, status, http.StatusOK, body, "theme update")
	if err := json.Unmarshal(body, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Theme != "dark" || settings.Notifications["completed"] {
		t.Fatalf("theme update lost notification prefs: %s", body)
	}
}
