package cmn

import (
	"strings"
	"testing"
)

func TestPasswordHashAndLegacyVerification(t *testing.T) {
	password := "TEST_ONLY_PASSWORD"
	first, err := HashPassword(password)
	if err != nil {
		t.Fatal("hash failed")
	}
	second, err := HashPassword(password)
	if err != nil {
		t.Fatal("hash failed")
	}
	if first == second || !strings.HasPrefix(first, "$2") || !VerifyPassword(first, password) || VerifyPassword(first, "TEST_ONLY_WRONG") {
		t.Fatal("salt or verification failed")
	}
	legacy := PasswordEncryption(password)
	if !VerifyPassword(legacy, password) || VerifyPassword(legacy, "TEST_ONLY_WRONG") || VerifyPassword("invalid", password) {
		t.Fatal("legacy verification failed")
	}
	for _, invalid := range []string{"", "short", strings.Repeat("界", 25)} {
		if _, err := HashPassword(invalid); err == nil {
			t.Fatal("invalid password length accepted")
		}
	}
	if hash, err := HashPassword(strings.Repeat("a", 72)); err != nil || !VerifyPassword(hash, strings.Repeat("a", 72)) {
		t.Fatal("72 byte password rejected")
	}
}

func TestLegacyUnicodeAndShortPasswordMigration(t *testing.T) {
	for _, password := range []string{"old", strings.Repeat("界", 50)} {
		hash, err := UpgradeLegacyPassword(password)
		if err != nil || !VerifyPassword(hash, password) || VerifyPassword(hash, password+"x") {
			t.Fatal("legacy password migration changed the accepted password")
		}
	}
}
