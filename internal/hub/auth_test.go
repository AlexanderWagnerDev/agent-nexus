package hub

import (
	"strings"
	"testing"
	"time"
)

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
