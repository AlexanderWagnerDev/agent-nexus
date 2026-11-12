package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotificationChannelSSRFValidation(t *testing.T) {
	for _, target := range []string{
		"http://127.0.0.1:8080/hook",
		"http://localhost/hook",
		"http://10.0.0.5/hook",
		"http://192.168.1.4/hook",
		"http://169.254.169.254/latest/meta-data",
		"file:///etc/passwd",
		"https://user:pass@example.com/hook",
	} {
		if err := validateNotificationURL(target, false); err == nil {
			t.Errorf("unsafe target accepted: %s", target)
		}
	}
	if err := validateNotificationURL("https://example.com/hook", false); err != nil {
		t.Fatalf("public target rejected: %v", err)
	}
	if err := validateNotificationURL("http://127.0.0.1:8080/hook", true); err != nil {
		t.Fatalf("explicit test override rejected: %v", err)
	}
}

func TestNotificationChannelDeliveryAndRedaction(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	hub.server.notificationAllowPrivate = true
	owner := hub.testOwner()

	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	status, body := hub.do(nil, http.MethodPost, "/api/v1/me/notification-channels", map[string]any{
		"kind": "webhook", "name": "test hook", "config": map[string]string{"url": server.URL + "/hook"},
	}, nil)
	expectStatus(t, status, http.StatusCreated, body, "create webhook")
	var created NotificationChannel
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	if created.Target != "test hook" || strings.Contains(string(body), server.URL) {
		t.Fatalf("channel response leaked its URL: %s", body)
	}
	var sealed string
	if err := hub.store.db.QueryRow(`SELECT config FROM notification_channels WHERE id=?`, created.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, server.URL) {
		t.Fatal("channel configuration was stored in plaintext")
	}

	status, body = hub.do(nil, http.MethodGet, "/api/v1/me/notification-channels", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "list channels")
	if strings.Contains(string(body), server.URL) {
		t.Fatalf("list leaked the webhook URL: %s", body)
	}

	channel, err := hub.store.NotificationChannelByID(owner.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.server.deliverNotificationChannel(channel, "Title", "Body", "/#/runs/x"); err != nil {
		t.Fatal(err)
	}
	select {
	case payload := <-received:
		if !strings.Contains(payload, "Title") || !strings.Contains(payload, "Body") {
			t.Fatalf("webhook payload: %s", payload)
		}
	default:
		t.Fatal("webhook was not called")
	}

	status, body = hub.do(nil, http.MethodPost, "/api/v1/me/notification-channels/"+created.ID+"/test", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "test channel")
	<-received
	status, body = hub.do(nil, http.MethodDelete, "/api/v1/me/notification-channels/"+created.ID, nil, nil)
	expectStatus(t, status, http.StatusOK, body, "delete channel")
}
