package database

import (
	"path/filepath"
	"testing"

	"sun-panel/lib/cmn"
	"sun-panel/models"
)

func TestAdminBootstrapAndMigration(t *testing.T) {
	db, err := (&SQLiteConfig{Filename: filepath.Join(t.TempDir(), "panel.db")}).Connect()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	if err := NotFoundAndCreateUser(db); err != nil {
		t.Fatal(err)
	}
	var first models.User
	if db.First(&first).Error != nil || first.Password != PendingPassword || cmn.VerifyPassword(first.Password, PendingPassword) {
		t.Fatal("first administrator must require operator setup")
	}
	// Both the old database format and an already-upgraded bcrypt hash occur in installations.
	newHash, _ := cmn.HashPassword("TEST_ONLY_CHANGED")
	oldHash, err := cmn.UpgradeLegacyPassword("12345678")
	if err != nil {
		t.Fatal(err)
	}
	for i, hash := range []string{cmn.PasswordEncryption("12345678"), oldHash, newHash} {
		if err := db.Model(&first).Updates(map[string]any{"password": hash, "token": "TEST_ONLY_SESSION"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := NotFoundAndCreateUser(db); err != nil {
			t.Fatal(err)
		}
		var result models.User
		if err := db.First(&result, first.ID).Error; err != nil {
			t.Fatal(err)
		}
		if i < 2 && (result.Password != PendingPassword || result.Token != "") {
			t.Fatal("legacy credential remained active")
		}
		if i == 2 && (result.Password != newHash || result.Token == "") {
			t.Fatal("changed account was modified")
		}
	}
}
