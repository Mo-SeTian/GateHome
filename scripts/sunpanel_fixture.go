//go:build ignore

// Seeds only the disposable SunPanel database created by browser checks.
package main

import (
	"io"
	"os"
	"path/filepath"
	"sun-panel/lib/cmn"
	"sun-panel/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func main() {
	password, err := io.ReadAll(os.Stdin)
	if err != nil || len(os.Args) != 2 {
		os.Exit(1)
	}
	defer clear(password)
	db, err := gorm.Open(sqlite.Open(filepath.Join(os.Args[1], "sunpanel/database/database.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), NamingStrategy: schema.NamingStrategy{SingularTable: true}})
	if err != nil {
		os.Exit(1)
	}
	hash, err := cmn.HashPassword(string(password))
	if err != nil {
		os.Exit(1)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.First(&user).Error; err != nil {
			return err
		}
		if err := tx.Model(&user).Updates(map[string]any{"username": "browser-fixture@example.test", "password": hash, "name": "Browser fixture", "token": "", "status": 1, "role": 1}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&models.ItemIcon{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&models.ItemIconGroup{}).Error; err != nil {
			return err
		}
		return tx.Create(&models.ItemIconGroup{Title: "Browser fixture", UserId: user.ID}).Error
	})
	if err != nil {
		os.Exit(1)
	}
}
