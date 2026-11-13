package hub

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRateLimiterMapIsBounded(t *testing.T) {
	limiter := newRateLimiter(5, time.Minute)
	limiter.maxEntries = 4
	for i := 0; i < 100; i++ {
		limiter.allow(fmt.Sprintf("attacker-%d", i))
	}
	limiter.mu.Lock()
	size := len(limiter.entries)
	limiter.mu.Unlock()
	if size > limiter.maxEntries {
		t.Fatalf("rate limiter map grew to %d entries, max %d", size, limiter.maxEntries)
	}

	// The window is still enforced for a key that is present.
	for i := 0; i < 5; i++ {
		if !limiter.allow("stable") {
			t.Fatalf("attempt %d was rejected before the limit", i)
		}
	}
	if limiter.allow("stable") {
		t.Fatal("the limiter did not enforce its limit")
	}
}

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "test-password-123") {
		t.Fatal("valid password was rejected")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Fatal("invalid password was accepted")
	}
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short password was accepted")
	}
}

func TestPairingCodeFormat(t *testing.T) {
	for i := 0; i < 50; i++ {
		code := newPairingCode()
		if !ValidatePairingCode(code) {
			t.Fatalf("generated code %q is invalid", code)
		}
		if strings.ContainsRune(code, 0) {
			t.Fatalf("generated code %q contains a NUL byte", code)
		}
	}
}

func TestTOTP(t *testing.T) {
	secret := NewTOTPSecret()
	now := time.Now().UTC().Unix() / 30
	code := totpAt(secret, now)
	if !ValidateTOTP(secret, code) {
		t.Fatal("current TOTP code was rejected")
	}
	if ValidateTOTP(secret, "000000") {
		t.Fatal("obviously wrong TOTP code was accepted")
	}
}
