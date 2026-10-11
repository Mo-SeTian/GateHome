package system

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gopkg.in/ini.v1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http/httptest"
	"path/filepath"
	"sun-panel/global"
	"sun-panel/lib/cache"
	"sun-panel/lib/cmn"
	"sun-panel/lib/iniConfig"
	"sun-panel/lib/language"
	"sun-panel/models"
	"testing"
	"time"
)

func TestLoginMigratesLegacyPasswordAndThrottles(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "login.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil || db.AutoMigrate(&models.User{}) != nil {
		t.Fatal("fixture failed")
	}
	previousDB, previousCache, previousLimiter := global.Db, global.CUserToken, loginAttempts
	global.Db = db
	global.CUserToken = cache.NewGoCache[string](time.Hour, 0)
	loginAttempts = &loginLimiter{buckets: map[string]loginBucket{}}
	t.Cleanup(func() {
		global.Db, global.CUserToken, loginAttempts = previousDB, previousCache, previousLimiter
		sql, _ := db.DB()
		sql.Close()
	})
	legacy := cmn.PasswordEncryption("TEST_ONLY_PASSWORD")
	user := models.User{Username: "TEST_ONLY_USER", Password: legacy, Role: 2, Status: 1}
	db.Create(&user)
	router := gin.New()
	router.SetTrustedProxies([]string{"127.0.0.1", "::1"})
	router.POST("/login", (LoginApi{}).Login)
	call := func(password string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(map[string]string{"username": user.Username, "password": password})
		r := httptest.NewRequest("POST", "/login", bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		r.RemoteAddr = "192.0.2.1:12345"
		r.Header.Set("X-Forwarded-For", "192.0.2.99")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	call("TEST_ONLY_WRONG")
	var current models.User
	db.First(&current, user.ID)
	if current.Password != legacy || current.Token != "" {
		t.Fatal("failed login migrated credentials or issued session")
	}
	for i := 0; i < 2; i++ {
		var result struct {
			Code int
			Data struct {
				Token    string
				Password string
			}
		}
		w := call("TEST_ONLY_PASSWORD")
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Code != 0 || result.Data.Token == "" || result.Data.Password != "" {
			t.Fatal("login failed or exposed password")
		}
		db.First(&current, user.ID)
		if current.Password == legacy || !cmn.VerifyPassword(current.Password, "TEST_ONLY_PASSWORD") || len(current.Token) != 32 {
			t.Fatal("legacy storage or internal token not upgraded")
		}
		if internal, ok := global.CUserToken.Get(result.Data.Token); !ok || internal != current.Token {
			t.Fatal("session mapping failed")
		}
	}
	for i := 0; i < 7; i++ {
		call("TEST_ONLY_WRONG")
	}
	if w := call("TEST_ONLY_WRONG"); w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("independent login endpoint was not throttled")
	}
}
func TestLoginLimiterBoundsAndTrustedAddress(t *testing.T) {
	now := time.Now()
	limiter := &loginLimiter{buckets: map[string]loginBucket{}}
	for i := 0; i < 10; i++ {
		if !limiter.allow("192.0.2.1", "TEST_ONLY_ACCOUNT", now) {
			t.Fatal("premature account limit")
		}
	}
	if limiter.allow("192.0.2.2", "TEST_ONLY_ACCOUNT", now) {
		t.Fatal("account limit bypassed from another address")
	}
	if !limiter.allow("192.0.2.1", "TEST_ONLY_ACCOUNT", now.Add(time.Minute)) {
		t.Fatal("expired bucket retained")
	}
	for i := 0; i < 4096; i++ {
		limiter.buckets["TEST_ONLY_"+time.Unix(int64(i), 0).String()] = loginBucket{start: now, count: 1}
	}
	if limiter.allow("192.0.2.3", "TEST_ONLY_NEW", now) {
		t.Fatal("limiter storage limit exceeded")
	}
}

func TestOwnPasswordResetAcceptsLegacyAndBcrypt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "password.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil || db.AutoMigrate(&models.User{}) != nil {
		t.Fatal("fixture failed")
	}
	oldDB, oldModel, oldUser, oldLang := global.Db, models.Db, global.UserToken, global.Lang
	global.Db, models.Db = db, db
	global.UserToken = cache.NewGoCache[models.User](time.Minute, 0)
	global.Lang = &language.LangStructObj{LangContet: &iniConfig.IniConfig{Config: ini.Empty()}}
	t.Cleanup(func() {
		global.Db, models.Db, global.UserToken, global.Lang = oldDB, oldModel, oldUser, oldLang
		sql, _ := db.DB()
		sql.Close()
	})
	user := models.User{Username: "TEST_ONLY_USER", Password: cmn.PasswordEncryption("TEST_ONLY_PASSWORD"), Status: 1, Role: 2, Token: "TEST_ONLY_INTERNAL"}
	db.Create(&user)
	router := gin.New()
	router.POST("/password", func(c *gin.Context) { c.Set("userInfo", user); (&UserApi{}).UpdatePasssword(c) })
	call := func(oldPassword, newPassword string) int {
		data, _ := json.Marshal(map[string]string{"oldPassword": oldPassword, "newPassword": newPassword})
		r := httptest.NewRequest("POST", "/password", bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		var result struct{ Code int }
		if json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatal("invalid reset response")
		}
		return result.Code
	}
	if call("TEST_ONLY_WRONG", "TEST_ONLY_NEW_PASSWORD") == 0 || call("TEST_ONLY_PASSWORD", "short") == 0 {
		t.Fatal("invalid reset succeeded")
	}
	var current models.User
	db.First(&current, user.ID)
	if current.Token != user.Token || current.Password != user.Password {
		t.Fatal("failed reset changed credentials")
	}
	if call("TEST_ONLY_PASSWORD", "TEST_ONLY_NEW_PASSWORD") != 0 {
		t.Fatal("legacy password reset failed")
	}
	db.First(&current, user.ID)
	if current.Token != "" || !cmn.VerifyPassword(current.Password, "TEST_ONLY_NEW_PASSWORD") {
		t.Fatal("new password was not hashed or sessions remained valid")
	}
	if call("TEST_ONLY_NEW_PASSWORD", "TEST_ONLY_NEXT_PASSWORD") != 0 {
		t.Fatal("bcrypt password reset failed")
	}
}
