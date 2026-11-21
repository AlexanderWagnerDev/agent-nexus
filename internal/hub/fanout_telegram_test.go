package hub

import (
	"context"
	"strings"
	"testing"
)

func TestFanoutWorktreeKeyNormalizesHostAliases(t *testing.T) {
	windows := []string{`C:\Work\Repo`, `c:/work/repo/`, `C:/Work/Repo/../Repo`}
	for _, cwd := range windows[1:] {
		if got, want := fanoutWorktreeKey("windows", cwd), fanoutWorktreeKey("windows", windows[0]); got != want {
			t.Fatalf("windows alias %q -> %q, want %q", cwd, got, want)
		}
	}
	if fanoutWorktreeKey("linux", "/work/Repo") == fanoutWorktreeKey("linux", "/work/repo") {
		t.Fatal("linux paths are case-sensitive and must not collapse")
	}
	if fanoutWorktreeKey("linux", "/work/x/../x") != fanoutWorktreeKey("linux", "/work/x") {
		t.Fatal("dot segments must collapse")
	}
	if fanoutWorktreeKey("linux", `/work/a\b`) == fanoutWorktreeKey("linux", "/work/a/b") {
		t.Fatal("a literal backslash in a Unix directory must not become a separator")
	}
}

func TestTelegramTokenIsNotLeakedInErrors(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	config := map[string]string{"token": "123456:\nSECRET-TOKEN", "chat_id": "42"}
	if err := validateNotificationConfig("telegram", "tg", config, false); err == nil {
		t.Fatal("malformed telegram token was accepted")
	}
	if _, err := hub.server.channelRequest(context.Background(), "telegram", config, notificationPayload{Title: "t"}); err == nil {
		t.Fatal("malformed telegram token produced no error")
	} else if strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Fatalf("telegram token leaked in error: %v", err)
	}
}
