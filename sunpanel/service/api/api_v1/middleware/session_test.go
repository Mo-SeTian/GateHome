package middleware_test

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gopkg.in/ini.v1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http/httptest"
	"path/filepath"
	"sun-panel/api/api_v1/middleware"
	"sun-panel/api/api_v1/panel"
	"sun-panel/global"
	"sun-panel/lib/cache"
	"sun-panel/lib/cmn"
	"sun-panel/lib/cmn/systemSetting"
	"sun-panel/lib/iniConfig"
	"sun-panel/lib/language"
	"sun-panel/models"
	"testing"
	"time"
)

func TestSessionsFollowAccountChanges(t *testing.T) {
	for _, change := range []string{"delete", "rename-and-password", "disable", "demote", "public-disabled", "last-enabled-admin"} {
		t.Run(change, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "session.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil || db.AutoMigrate(&models.User{}, &models.ItemIcon{}, &models.ItemIconGroup{}, &models.UserConfig{}, &models.ModuleConfig{}, &models.SystemSetting{}) != nil {
				t.Fatal("fixture database failed")
			}
			oldLang, oldLogger := global.Lang, global.Logger
			global.Lang = &language.LangStructObj{LangContet: &iniConfig.IniConfig{Config: ini.Empty()}}
			global.Logger = zap.NewNop().Sugar()
			oldDB, oldModels, oldUser, oldClient, oldSettings := global.Db, models.Db, global.UserToken, global.CUserToken, global.SystemSetting
			global.Db, models.Db = db, db
			global.UserToken = cache.NewGoCache[models.User](time.Minute, 0)
			global.CUserToken = cache.NewGoCache[string](time.Hour, 0)
			global.SystemSetting = &systemSetting.SystemSettingCache{Cache: cache.NewGoCache[interface{}](time.Minute, 0)}
			t.Cleanup(func() {
				global.Lang, global.Logger = oldLang, oldLogger
				global.Db, models.Db, global.UserToken, global.CUserToken, global.SystemSetting = oldDB, oldModels, oldUser, oldClient, oldSettings
				sql, _ := db.DB()
				sql.Close()
			})
			user := models.User{Username: "TEST_ONLY_ADMIN", Password: cmn.PasswordEncryption("TEST_ONLY_PASSWORD"), Status: 1, Role: 1, Token: "TEST_ONLY_INTERNAL"}
			db.Create(&user)
			db.Create(&models.User{Username: "TEST_ONLY_SECOND", Role: 1, Status: 1})
			global.UserToken.SetDefault(user.Token, user)
			global.CUserToken.SetDefault("TEST_ONLY_SESSION", user.Token)
			router := gin.New()
			router.GET("/private", middleware.LoginInterceptor, func(c *gin.Context) { c.Status(204) })
			router.GET("/admin", middleware.LoginInterceptor, middleware.AdminInterceptor, func(c *gin.Context) { c.Status(204) })
			router.GET("/public", middleware.PublicModeInterceptor, func(c *gin.Context) { c.Status(204) })
			router.POST("/delete", (&panel.UsersApi{}).Deletes)
			router.POST("/update", (&panel.UsersApi{}).Update)
			request := func(method, path string, payload any) *httptest.ResponseRecorder {
				data, _ := json.Marshal(payload)
				r := httptest.NewRequest(method, path, bytes.NewReader(data))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("token", "TEST_ONLY_SESSION")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				return w
			}
			if request("GET", "/private", nil).Code != 204 {
				t.Fatal("valid session rejected")
			}
			switch change {
			case "last-enabled-admin":
				db.Model(&models.User{}).Where("id<>?", user.ID).Update("status", 2)
				w := request("POST", "/delete", map[string]any{"userIds": []uint{user.ID}})
				var result struct{ Code int }
				if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Code == 0 {
					t.Fatal("last enabled administrator was deleted")
				}
				if request("GET", "/private", nil).Code != 204 {
					t.Fatal("failed deletion revoked account")
				}
				return
			case "delete":
				w := request("POST", "/delete", map[string]any{"userIds": []uint{user.ID}})
				var result struct{ Code int }
				if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Code != 0 {
					t.Fatal("account deletion failed")
				}
			case "rename-and-password":
				w := request("POST", "/update", map[string]any{"id": user.ID, "username": "TEST_ONLY_RENAMED", "password": "TEST_ONLY_NEW_PASSWORD", "role": 1})
				var result struct{ Code int }
				if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Code != 0 {
					t.Fatal("account reset failed")
				}
				var updated models.User
				db.First(&updated, user.ID)
				if updated.Token != "" || !cmn.VerifyPassword(updated.Password, "TEST_ONLY_NEW_PASSWORD") {
					t.Fatal("account reset not persisted")
				}
			case "disable", "public-disabled":
				db.Model(&user).Update("status", 2)
			case "demote":
				db.Model(&user).Update("role", 2)
			}
			if change == "demote" {
				if request("GET", "/private", nil).Code != 204 || request("GET", "/admin", nil).Code == 204 {
					t.Fatal("live role was not applied")
				}
			} else if request("GET", "/private", nil).Code == 204 {
				t.Fatal("revoked account remained authenticated")
			}
			if change == "public-disabled" {
				if global.SystemSetting.Set(systemSetting.PANEL_PUBLIC_USER_ID, user.ID) != nil {
					t.Fatal("public fixture failed")
				}
				if request("GET", "/public", nil).Code == 204 {
					t.Fatal("disabled public account was accepted")
				}
			}
		})
	}
}
