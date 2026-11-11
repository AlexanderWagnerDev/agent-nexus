package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretBoxRoundTrip(t *testing.T) {
	dir := t.TempDir()
	box, err := OpenSecretBox(dir)
	if err != nil {
		t.Fatal(err)
	}
	sealed := box.Seal("JBSWY3DPEHPK3PXP")
	if !strings.HasPrefix(sealed, secretPrefix) {
		t.Fatalf("secret was not encrypted: %q", sealed)
	}
	plain, err := box.Open(sealed)
	if err != nil || plain != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("round trip failed: %q %v", plain, err)
	}
	if got, err := box.Open("legacy-plaintext"); err != nil || got != "legacy-plaintext" {
		t.Fatalf("legacy value not readable: %q %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.key"), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSecretBox(dir); err == nil {
		t.Fatal("damaged key file was accepted")
	}
}

func TestStoreEncryptsTOTPAtRest(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	box, err := OpenSecretBox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.SetSecretBox(box)
	user, err := store.CreateUser("owner", "hash", "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := store.db.QueryRow(`SELECT totp_secret FROM users WHERE id=?`, user.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, secretPrefix) || strings.Contains(stored, "JBSWY3DPEHPK3PXP") {
		t.Fatalf("TOTP secret was stored in plaintext: %q", stored)
	}
	loaded, err := store.UserByUsername("owner")
	if err != nil || loaded.TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("TOTP secret did not round trip: %q %v", loaded.TOTPSecret, err)
	}
	if err := store.SetTOTP(user.ID, "KRSXG5CTMVRXEZLU", true); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT totp_secret FROM users WHERE id=?`, user.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, secretPrefix) {
		t.Fatalf("SetTOTP stored plaintext: %q", stored)
	}
	loaded, err = store.UserByID(user.ID)
	if err != nil || !loaded.TOTPEnabled || loaded.TOTPSecret != "KRSXG5CTMVRXEZLU" {
		t.Fatalf("updated TOTP secret did not round trip: %+v %v", loaded, err)
	}
}

func TestMigrateSecretsEncryptsLegacyTOTP(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	user, err := store.CreateUser("legacy", "hash", "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	box, err := OpenSecretBox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.SetSecretBox(box)
	if err := store.MigrateSecrets(); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := store.db.QueryRow(`SELECT totp_secret FROM users WHERE id=?`, user.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, secretPrefix) {
		t.Fatalf("legacy secret was not encrypted: %q", stored)
	}
	loaded, err := store.UserByID(user.ID)
	if err != nil || loaded.TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("migrated secret did not decrypt: %q %v", loaded.TOTPSecret, err)
	}
	if err := store.MigrateSecrets(); err != nil {
		t.Fatalf("migration is not idempotent: %v", err)
	}
}
