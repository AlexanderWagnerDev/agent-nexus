package hub

import (
	"encoding/json"
	"errors"
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

func TestTranscriptDetailPersistsForUser(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	var settings struct {
		TranscriptDetail string `json:"transcript_detail"`
	}
	status, body := hub.do(nil, http.MethodGet, "/api/v1/me/settings", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "settings")
	if err := json.Unmarshal(body, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.TranscriptDetail != "" {
		t.Fatalf("an unset detail must stay empty for the device fallback: %s", body)
	}
	status, body = hub.do(nil, http.MethodPut, "/api/v1/me/settings", map[string]any{"transcript_detail": "full"}, nil)
	expectStatus(t, status, http.StatusOK, body, "update detail")
	status, body = hub.do(nil, http.MethodGet, "/api/v1/me/settings", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "settings after update")
	if err := json.Unmarshal(body, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.TranscriptDetail != "full" {
		t.Fatalf("detail not persisted: %s", body)
	}
	status, body = hub.do(nil, http.MethodPut, "/api/v1/me/settings", map[string]any{"transcript_detail": "neon"}, nil)
	expectStatus(t, status, http.StatusBadRequest, body, "invalid detail")
}

func TestSavePushSubscriptionDoesNotRebindAnotherAccount(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	second, err := hub.store.CreateUserWithRole("second", "hash", "", "member")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "https://push.example.test/sub/1"
	_, deviceA, err := hub.store.CreateSession(owner.ID, "browser-a")
	if err != nil {
		t.Fatal(err)
	}
	_, deviceB, err := hub.store.CreateSession(second.ID, "browser-b")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.SavePushSubscription(owner.ID, deviceA.ID, endpoint, "p256", "auth"); err != nil {
		t.Fatal(err)
	}
	// The same user re-subscribing from the same browser is a normal upsert.
	if err := hub.store.SavePushSubscription(owner.ID, deviceA.ID, endpoint, "p256b", "authb"); err != nil {
		t.Fatalf("re-subscribe by the same user: %v", err)
	}
	if err := hub.store.SavePushSubscription(second.ID, deviceB.ID, endpoint, "p256", "auth"); !errors.Is(err, errPushSubscriptionOwned) {
		t.Fatalf("another account took over the subscription: %v", err)
	}
	subs, err := hub.store.PushSubscriptions(owner.ID)
	if err != nil || len(subs) != 1 {
		t.Fatalf("owner subscriptions: %+v %v", subs, err)
	}
}
