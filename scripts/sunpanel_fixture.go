//go:build ignore

// Seeds only the disposable SunPanel database created by browser checks.
package main

import (
	"os"
	"path/filepath"
	"sun-panel/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func main() {
	if len(os.Args) != 2 {
		os.Exit(1)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(os.Args[1], "sunpanel/database/database.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), NamingStrategy: schema.NamingStrategy{SingularTable: true}})
	if err != nil {
		os.Exit(1)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.First(&user).Error; err != nil {
			return err
		}
		if err := tx.Model(&user).Updates(map[string]any{"username": "browser-fixture@example.test", "name": "Browser fixture", "token": "", "status": 1, "role": 1}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&models.ItemIcon{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&models.ItemIconGroup{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.UserConfig{UserId: user.ID, SearchEngineJson: "{}", PanelJson: `{"footerHtml":"<a href='https://example.test/' data-original='yes'>Home</a> <a href='urn:fixture'>Plain label</a><img alt='unused'>"}`}).Error; err != nil {
			return err
		}
		return tx.Create(&models.ItemIconGroup{Title: "Browser fixture", UserId: user.ID}).Error
	})
	if err != nil {
		os.Exit(1)
	}
}
