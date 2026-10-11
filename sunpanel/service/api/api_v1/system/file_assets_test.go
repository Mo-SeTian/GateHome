package system

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gopkg.in/ini.v1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"sun-panel/global"
	"sun-panel/lib/iniConfig"
	"sun-panel/models"
)

func TestAssetReferencesAndSafeCleanup(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "database.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil || db.AutoMigrate(&models.ItemIcon{}, &models.UserConfig{}, &models.File{}) != nil {
		t.Fatal("fixture initialization failed")
	}
	previous, previousModels, previousConfig := global.Db, models.Db, global.Config
	global.Db, models.Db = db, db
	global.Config = &iniConfig.IniConfig{Config: ini.Empty()}
	global.Config.Config.Section("base").Key("source_path").SetValue("./uploads")
	t.Cleanup(func() {
		global.Db, models.Db, global.Config = previous, previousModels, previousConfig
		sqlDB, _ := db.DB()
		sqlDB.Close()
	})
	os.MkdirAll("uploads", 0700)
	for _, name := range []string{"used.png", "wallpaper.png", "other.png", "unused.png", "rollback.png"} {
		source := "./uploads/" + name
		os.WriteFile(source, []byte("TEST_ONLY_IMAGE"), 0600)
		if db.Create(&models.File{UserId: 1, Src: source, FileName: name}).Error != nil {
			t.Fatal("file fixture failed")
		}
	}
	for _, item := range []models.ItemIcon{{UserId: 1, Title: "Own website", IconJson: `{"itemType":2,"src":"/sunpanel/uploads/used.png"}`}, {UserId: 2, Title: "TEST_ONLY_PRIVATE_TITLE", IconJson: `{"itemType":3,"text":"/sunpanel/uploads/other.png"}`}} {
		if db.Create(&item).Error != nil {
			t.Fatal("website fixture failed")
		}
	}
	if db.Create(&models.UserConfig{UserId: 1, PanelJson: `{"backgroundImageSrc":"/sunpanel/uploads/wallpaper.png"}`}).Error != nil {
		t.Fatal("wallpaper fixture failed")
	}
	api := &FileApi{}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("userInfo", models.User{BaseModel: models.BaseModel{ID: 1}}) })
	router.POST("/list", api.GetList)
	router.POST("/preview", api.DeletePreview)
	router.POST("/delete", api.Deletes)
	call := func(path string, payload any) (int, string) {
		data, _ := json.Marshal(payload)
		request := httptest.NewRequest("POST", path, bytes.NewReader(data))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		var reply struct {
			Code int `json:"code"`
		}
		if json.Unmarshal(response.Body.Bytes(), &reply) != nil {
			t.Fatal("invalid response")
		}
		return reply.Code, response.Body.String()
	}
	code, list := call("/list", nil)
	if code != 0 || strings.Contains(list, "TEST_ONLY_PRIVATE_TITLE") || !strings.Contains(list, "其他账号") {
		t.Fatal("reference listing incorrect or disclosed another user's title")
	}
	code, preview := call("/preview", nil)
	var previewData struct {
		Data struct {
			List []struct {
				FileName string `json:"fileName"`
			} `json:"list"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(preview), &previewData) != nil || code != 0 || len(previewData.Data.List) != 2 {
		t.Fatal("cleanup preview incorrect")
	}
	for _, row := range previewData.Data.List {
		if row.FileName != "unused.png" && row.FileName != "rollback.png" {
			t.Fatal("cleanup preview includes referenced asset")
		}
	}

	var files []models.File
	db.Find(&files)
	ids := map[string]uint{}
	for _, file := range files {
		ids[file.FileName] = file.ID
	}
	for _, name := range []string{"used.png", "wallpaper.png", "other.png"} {
		code, _ := call("/delete", map[string]any{"ids": []uint{ids[name]}})
		if code == 0 {
			t.Fatal("referenced asset deleted")
		}
		if _, err := os.Stat("uploads/" + name); err != nil {
			t.Fatal("referenced file disappeared")
		}
	}
	code, _ = call("/delete", map[string]any{"ids": []uint{99999}})
	if code == 0 {
		t.Fatal("unowned/missing ID accepted")
	}
	// If a later file is unsafe, earlier filesystem changes and rows must roll back.
	unsafe := models.File{UserId: 1, Src: filepath.Join(root, "outside.png"), FileName: "unsafe"}
	db.Create(&unsafe)
	os.WriteFile(unsafe.Src, []byte("TEST_ONLY_OUTSIDE"), 0600)
	code, _ = call("/delete", map[string]any{"ids": []uint{ids["rollback.png"], unsafe.ID}})
	if code == 0 {
		t.Fatal("unsafe path accepted")
	}
	if _, err := os.Stat("uploads/rollback.png"); err != nil {
		t.Fatal("rollback lost original file")
	}
	code, _ = call("/delete", map[string]any{"ids": []uint{ids["unused.png"]}})
	if code != 0 {
		t.Fatal("unused deletion failed")
	}
	if _, err := os.Stat("uploads/unused.png"); !os.IsNotExist(err) {
		t.Fatal("unused file remains")
	}
	var count int64
	db.Model(&models.File{}).Where("id=?", ids["unused.png"]).Count(&count)
	if count != 0 {
		t.Fatal("deleted asset record remains")
	}
}
