package panel

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sun-panel/global"
	"sun-panel/lib/iniConfig"
	"sun-panel/models"
	"sun-panel/models/datatype"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gopkg.in/ini.v1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func panelFixture(t *testing.T) (*gorm.DB, models.ItemIconGroup, string) {
	t.Helper()
	root := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "panel.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil || db.AutoMigrate(&models.User{}, &models.ItemIconGroup{}, &models.ItemIcon{}, &models.File{}) != nil {
		t.Fatal("fixture database initialization failed")
	}
	old := global.Db
	global.Db = db
	t.Cleanup(func() { global.Db = old; sql, _ := db.DB(); sql.Close() })
	for _, id := range []uint{1, 2} {
		if db.Create(&models.User{BaseModel: models.BaseModel{ID: id}, Username: "TEST_ONLY_USER"}).Error != nil {
			t.Fatal("fixture user failed")
		}
	}
	group := models.ItemIconGroup{Title: "TEST_ONLY_GROUP", UserId: 1}
	if db.Create(&group).Error != nil {
		t.Fatal("fixture group failed")
	}
	uploads := filepath.Join(root, "uploads")
	if os.Mkdir(uploads, 0700) != nil {
		t.Fatal("fixture uploads failed")
	}
	oldConfig := global.Config
	global.Config = &iniConfig.IniConfig{Config: ini.Empty()}
	global.Config.Config.Section("base").Key("source_path").SetValue(uploads)
	t.Cleanup(func() { global.Config = oldConfig })
	return db, group, uploads
}

func portableRequest(t *testing.T, handler gin.HandlerFunc, data []byte) map[string]json.RawMessage {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "TEST_ONLY_HOME.zip")
	if err != nil {
		t.Fatal("multipart fixture failed")
	}
	file.Write(data)
	writer.Close()
	router := gin.New()
	router.POST("/test", func(c *gin.Context) { c.Set("userInfo", models.User{BaseModel: models.BaseModel{ID: 1}}); handler(c) })
	r := httptest.NewRequest("POST", "/test", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	var result map[string]json.RawMessage
	if json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal("invalid ZIP API response")
	}
	return result
}

func panelRequest(t *testing.T, handler gin.HandlerFunc, payload any) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal("fixture JSON failed")
	}
	router := gin.New()
	router.POST("/test", func(c *gin.Context) { c.Set("userInfo", models.User{BaseModel: models.BaseModel{ID: 1}}); handler(c) })
	r := httptest.NewRequest("POST", "/test", bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	var result map[string]json.RawMessage
	if json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal("invalid API response")
	}
	return result
}

func TestGateHomeBatchImportAndSynchronization(t *testing.T) {
	db, group, _ := panelFixture(t)
	api := &ItemIcon{}
	link := &models.GateHomeLink{RouteID: "TEST_ONLY_SOURCE", Title: "NAS", URL: "https://nas.example.test/", LanURL: "http://192.168.1.2:5000/"}
	item := models.ItemIcon{Title: link.Title, Url: link.URL, LanUrl: link.LanURL, OpenMethod: 2, ItemIconGroupId: int(group.ID), GateHome: link}
	for i := 0; i < 2; i++ {
		result := panelRequest(t, api.GateHomeImport, map[string]any{"items": []models.ItemIcon{item, item}})
		if string(result["code"]) != "0" {
			t.Fatal("batch import failed")
		}
	}
	var count int64
	db.Model(&models.ItemIcon{}).Count(&count)
	if count != 1 {
		t.Fatal("duplicate imports created multiple items")
	}
	var stored models.ItemIcon
	db.First(&stored)
	if stored.GateHome == nil || stored.GateHome.RouteID != link.RouteID {
		t.Fatal("source association was not persisted")
	}
	db.Model(&stored).Update("title", "Custom NAS")
	db.First(&stored, stored.ID)
	updated := models.GateHomeLink{RouteID: link.RouteID, Title: "Renamed NAS", URL: "https://renamed.example.test/", LanURL: "http://192.168.1.3:5000/"}
	request := map[string]any{"items": []map[string]any{{"id": stored.ID, "updatedAt": stored.UpdatedAt, "route": updated}}}
	if result := panelRequest(t, api.GateHomeSync, request); string(result["code"]) != "0" {
		t.Fatal("explicit sync failed")
	}
	db.First(&stored, stored.ID)
	if stored.Title != "Custom NAS" || stored.Url != updated.URL || stored.LanUrl != updated.LanURL || stored.GateHome.URL != updated.URL || stored.ItemIconGroupId != int(group.ID) {
		t.Fatal("sync lost custom values or did not apply source changes")
	}
	if result := panelRequest(t, api.GateHomeSync, request); string(result["code"]) == "0" {
		t.Fatal("stale synchronization preview was accepted")
	}
	// A failed batch must not leave its earlier valid row behind.
	valid := item
	valid.Url = "https://second.example.test/"
	valid.GateHome = &models.GateHomeLink{RouteID: "TEST_ONLY_SECOND", Title: "Second", URL: valid.Url, LanURL: valid.LanUrl}
	invalid := valid
	invalid.ItemIconGroupId = 999
	if result := panelRequest(t, api.GateHomeImport, map[string]any{"items": []models.ItemIcon{valid, invalid}}); string(result["code"]) == "0" {
		t.Fatal("invalid target group accepted")
	}
	db.Model(&models.ItemIcon{}).Count(&count)
	if count != 1 {
		t.Fatal("failed batch was not rolled back")
	}
}

func portableZIP(t *testing.T, manifest portableManifest, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	metadata, _ := json.Marshal(manifest)
	entry, _ := writer.Create("manifest.json")
	entry.Write(metadata)
	for name, data := range files {
		entry, _ := writer.Create(name)
		entry.Write(data)
	}
	writer.Close()
	return buffer.Bytes()
}

func TestPortableRoundTripAndRollback(t *testing.T) {
	db, group, root := panelFixture(t)
	var pngData bytes.Buffer
	png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	if os.WriteFile(filepath.Join(root, "test.png"), pngData.Bytes(), 0600) != nil {
		t.Fatal("image fixture failed")
	}
	icon, _ := json.Marshal(datatype.ItemIconIconInfo{ItemType: 2, Src: "/sunpanel/uploads/test.png", BackgroundColor: "#ffffff"})
	item := models.ItemIcon{Title: "TEST_ONLY_BOOKMARK", Url: "https://site.example.test/", LanUrl: "http://192.168.1.2:5000/", IconJson: string(icon), OpenMethod: 3, ItemIconGroupId: int(group.ID), UserId: 1, Sort: 2}
	if db.Create(&item).Error != nil {
		t.Fatal("bookmark fixture failed")
	}
	other := item
	other.BaseModel = models.BaseModel{}
	other.UserId = 2
	other.Title = "TEST_ONLY_OTHER_USER"
	db.Create(&other)
	data, err := buildPortable(db, 1, root)
	if err != nil {
		t.Fatal("portable export failed")
	}
	manifest, images, err := readPortable(data)
	if err != nil || len(manifest.Groups) != 1 || len(manifest.Groups[0].Items) != 1 || len(images) != 1 || bytes.Contains(data, []byte("TEST_ONLY_OTHER_USER")) {
		t.Fatal("portable export included another user's data or omitted images")
	}
	for name, original := range images {
		corrupted := append([]byte(nil), original...)
		corrupted[len(corrupted)-1] ^= 1
		if _, _, err := readPortable(portableZIP(t, manifest, map[string][]byte{name: corrupted})); err == nil {
			t.Fatal("image checksum mismatch was accepted")
		}
	}
	api := &ItemIcon{}
	if result := portableRequest(t, api.PortableInspect, data); string(result["code"]) != "0" {
		t.Fatal("ZIP preview API rejected a valid export")
	}
	var groupCount int64
	db.Model(&models.ItemIconGroup{}).Count(&groupCount)
	if _, err := os.Stat(filepath.Join(root, "portable")); !os.IsNotExist(err) || groupCount != 1 {
		t.Fatal("ZIP preview wrote groups or images")
	}
	if result := portableRequest(t, api.PortableImport, data); string(result["code"]) != "0" {
		t.Fatal("portable import failed")
	}
	var imported models.ItemIcon
	db.Where("id > ? AND user_id=?", other.ID, 1).First(&imported)
	var restoredIcon datatype.ItemIconIconInfo
	json.Unmarshal([]byte(imported.IconJson), &restoredIcon)
	if imported.OpenMethod != 3 || imported.LanUrl != item.LanUrl || restoredIcon.BackgroundColor != "#ffffff" || !strings.HasPrefix(restoredIcon.Src, "/sunpanel/uploads/portable/") {
		t.Fatal("round trip did not preserve item settings or rewrite local image URLs")
	}
	rel := restoredIcon.Src[len("/sunpanel/uploads/"):]
	stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil || !bytes.Equal(stored, pngData.Bytes()) {
		t.Fatal("round trip image differs")
	}
	before, _ := os.ReadDir(filepath.Join(root, "portable"))
	var groupsBefore int64
	db.Model(&models.ItemIconGroup{}).Count(&groupsBefore)
	db.Callback().Create().Before("gorm:create").Register("test:reject-portable", func(tx *gorm.DB) { tx.AddError(errors.New("TEST_ONLY_DB_FAILURE")) })
	if importPortable(db, 1, root, manifest, images) == nil {
		t.Fatal("database failure reported import success")
	}
	after, _ := os.ReadDir(filepath.Join(root, "portable"))
	var groupsAfter int64
	db.Model(&models.ItemIconGroup{}).Count(&groupsAfter)
	if len(after) != len(before) || groupsAfter != groupsBefore {
		t.Fatal("failed import left image files or groups")
	}
}

func TestPortableExportRejectsUnimportableItems(t *testing.T) {
	db, group, root := panelFixture(t)
	item := models.ItemIcon{Title: "Site", Url: "https://site.example.test/", OpenMethod: 2, IconJson: `{ "itemType": 1 }`, UserId: 1, ItemIconGroupId: int(group.ID)}
	if db.Create(&item).Error != nil {
		t.Fatal("bookmark fixture failed")
	}
	db.Model(&item).Update("url", "javascript:TEST_ONLY_INVALID")
	if _, err := buildPortable(db, 1, root); err == nil {
		t.Fatal("export generated a ZIP that could not be imported")
	}
	db.Model(&item).Update("url", "https://site.example.test/")
	items := make([]models.ItemIcon, 1000)
	for i := range items {
		items[i] = item
		items[i].BaseModel = models.BaseModel{}
	}
	if db.CreateInBatches(items, 100).Error != nil {
		t.Fatal("bookmark capacity fixture failed")
	}
	if _, err := buildPortable(db, 1, root); err == nil {
		t.Fatal("export exceeded the import item limit")
	}
}

func TestPortableRejectsCorruptionAndUnsafePaths(t *testing.T) {
	db, group, root := panelFixture(t)
	manifest := portableManifest{Format: "gatehome-sunpanel-v1", CreatedAt: time.Now(), Groups: []portableGroup{{Title: "Test", Items: []portableItem{{Title: "Site", URL: "https://site.example.test/", OpenMethod: 2}}}}, Images: map[string]string{}}
	for _, data := range [][]byte{[]byte("not zip"), portableZIP(t, manifest, map[string][]byte{"../outside": []byte("TEST_ONLY_PATH")}), portableZIP(t, manifest, map[string][]byte{"images/unlisted.png": []byte("TEST_ONLY_IMAGE")})} {
		if _, _, err := readPortable(data); err == nil {
			t.Fatal("unsafe ZIP accepted")
		}
	}
	manifest.Groups[0].Items[0].URL = "http://user:TEST_ONLY_PASSWORD@site.example.test/"
	if _, _, err := readPortable(portableZIP(t, manifest, nil)); err == nil {
		t.Fatal("URL containing credentials accepted")
	}
	// Symlinks cannot export content outside the upload directory.
	outside := filepath.Join(t.TempDir(), "private.png")
	os.WriteFile(outside, []byte("TEST_ONLY_PRIVATE_FILE"), 0600)
	os.Symlink(outside, filepath.Join(root, "escape.png"))
	icon, _ := json.Marshal(datatype.ItemIconIconInfo{ItemType: 2, Src: "/sunpanel/uploads/escape.png"})
	db.Create(&models.ItemIcon{Title: "Site", Url: "https://site.example.test/", OpenMethod: 2, IconJson: string(icon), UserId: 1, ItemIconGroupId: int(group.ID)})
	if _, err := buildPortable(db, 1, root); err == nil {
		t.Fatal("export followed a symlink outside uploads")
	}
}
