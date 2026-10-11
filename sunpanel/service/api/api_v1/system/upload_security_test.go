package system

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gopkg.in/ini.v1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sun-panel/global"
	"sun-panel/lib/imageasset"
	"sun-panel/lib/iniConfig"
	"sun-panel/models"
	"testing"
)

type uploadZeros struct{}

func (uploadZeros) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
func TestUploadContentSizeAndConcurrency(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "uploads.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil || db.AutoMigrate(&models.File{}) != nil {
		t.Fatal("fixture failed")
	}
	oldDB, oldModels, oldConfig := global.Db, models.Db, global.Config
	global.Db, models.Db = db, db
	global.Config = &iniConfig.IniConfig{Config: ini.Empty()}
	global.Config.Config.Section("base").Key("source_path").SetValue("./uploads")
	t.Cleanup(func() {
		global.Db, models.Db, global.Config = oldDB, oldModels, oldConfig
		sql, _ := db.DB()
		sql.Close()
	})
	router := gin.New()
	router.POST("/upload", func(c *gin.Context) {
		c.Set("userInfo", models.User{BaseModel: models.BaseModel{ID: 1}})
		(&FileApi{}).UploadImg(c)
	})
	call := func(name string, content io.Reader) *httptest.ResponseRecorder {
		var framing bytes.Buffer
		writer := multipart.NewWriter(&framing)
		writer.CreateFormFile("imgfile", name)
		prefix := append([]byte(nil), framing.Bytes()...)
		framing.Reset()
		writer.Close()
		body := io.MultiReader(bytes.NewReader(prefix), content, bytes.NewReader(framing.Bytes()))
		r := httptest.NewRequest("POST", "/upload", body)
		r.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	for _, test := range []struct {
		name    string
		content io.Reader
	}{
		{"fake.png", strings.NewReader(`<html><script>alert(1)</script></html>`)},
		{"active.svg", strings.NewReader(`<svg onload="alert(1)"></svg>`)},
		{"big.png", io.LimitReader(uploadZeros{}, imageasset.Limit+1)},
		{"body.png", io.LimitReader(uploadZeros{}, uploadBodyLimit+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := call(test.name, test.content)
			var result struct{ Code int }
			if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Code == 0 {
				t.Fatal("invalid/oversized upload accepted")
			}
		})
	}
	var count int64
	db.Model(&models.File{}).Count(&count)
	if count != 0 {
		t.Fatal("rejected upload left database records")
	}
	for i := 0; i < cap(uploadWorkers); i++ {
		uploadWorkers <- struct{}{}
	}
	w := call("busy.png", strings.NewReader("TEST_ONLY_IMAGE"))
	for i := 0; i < cap(uploadWorkers); i++ {
		<-uploadWorkers
	}
	if w.Code != 429 {
		t.Fatal("upload concurrency cap missing")
	}
	entries, err := os.ReadDir(filepath.Join(root, "uploads"))
	if err == nil && len(entries) != 0 {
		t.Fatal("rejected upload left stored content")
	}

	w = call("safe.svg", strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`))
	var success struct {
		Code int
		Data struct{ ImageURL string }
	}
	if json.Unmarshal(w.Body.Bytes(), &success) != nil || success.Code != 0 || !strings.HasPrefix(success.Data.ImageURL, "/sunpanel/uploads/") {
		t.Fatal("valid image returned an invalid public URL")
	}
	var stored models.File
	db.First(&stored)
	if !strings.HasPrefix(stored.Src, "./uploads/") || assetKey(stored.Src) != assetKey(success.Data.ImageURL) {
		t.Fatal("stored image path is inconsistent with image references")
	}
}
