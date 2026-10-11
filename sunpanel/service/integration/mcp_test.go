package integration

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sun-panel/global"
	"sun-panel/models"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func mcpFixture(t *testing.T) (*gorm.DB, models.ItemIconGroup, models.ItemIconGroup) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "panel.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil || db.AutoMigrate(&models.User{}, &models.ItemIconGroup{}, &models.ItemIcon{}, &models.UserConfig{}) != nil {
		t.Fatal("MCP fixture database failed")
	}
	old := global.Db
	global.Db = db
	t.Cleanup(func() { global.Db = old; sql, _ := db.DB(); sql.Close() })
	for _, id := range []uint{1, 2} {
		if db.Create(&models.User{BaseModel: models.BaseModel{ID: id}, Username: "TEST_ONLY_USER", Password: "TEST_ONLY_PASSWORD", Status: 1}).Error != nil {
			t.Fatal("user fixture failed")
		}
	}
	one := models.ItemIconGroup{UserId: 1, Title: "Owned"}
	two := models.ItemIconGroup{UserId: 2, Title: "Other"}
	if db.Create(&one).Error != nil || db.Create(&two).Error != nil {
		t.Fatal("group fixture failed")
	}
	return db, one, two
}

func operation(t *testing.T, db *gorm.DB, name string, payload any, wantError bool) any {
	t.Helper()
	data, _ := json.Marshal(payload)
	result, err := mcpOperation(db, name, 1, data)
	if (err != nil) != wantError {
		t.Fatalf("unexpected result for %s", name)
	}
	return result
}

func TestMCPBridgeAccountBoundaryAndAuthentication(t *testing.T) {
	db, one, two := mcpFixture(t)
	key := bytes.Repeat([]byte{7}, 32)
	h := MCPBridge(key)
	for _, valid := range []bool{false, true} {
		payload, _ := json.Marshal(map[string]any{"action": "desktop", "userId": 1})
		r := httptest.NewRequest("POST", "/sunpanel/integration/mcp", bytes.NewReader(payload))
		if valid {
			r.Header.Set("Authorization", "Bearer "+hex.EncodeToString(key))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if !valid && w.Code != 403 || valid && w.Code != 200 {
			t.Fatal("bridge authorization failed")
		}
		if valid && (strings.Contains(w.Body.String(), "TEST_ONLY_PASSWORD") || strings.Contains(w.Body.String(), two.Title) || !strings.Contains(w.Body.String(), one.Title)) {
			t.Fatal("bridge exposed account data")
		}
	}
	operation(t, db, "save_group", MCPGroup{ID: two.ID, Title: "Unauthorized", UpdatedAt: two.UpdatedAt}, true)
	operation(t, db, "save_website", MCPWebsite{GroupID: int(two.ID), Title: "Unauthorized", URL: "https://example.test", OpenMethod: 2}, true)
	operation(t, db, "delete_group", MCPDelete{ID: one.ID, UpdatedAt: one.UpdatedAt, Confirm: true}, true)
}

func TestMCPWebsiteCRUDAndConcurrentEdits(t *testing.T) {
	db, one, _ := mcpFixture(t)
	input := MCPWebsite{GroupID: int(one.ID), Title: "Service", URL: "https://example.test", LanURL: "http://192.168.1.1/", OpenMethod: 2}
	item := operation(t, db, "save_website", input, false).(MCPWebsite)
	input = item
	input.Title = "Renamed"
	updated := operation(t, db, "save_website", input, false).(MCPWebsite)
	operation(t, db, "save_website", input, true)
	operation(t, db, "delete_website", MCPDelete{ID: item.ID, UpdatedAt: item.UpdatedAt, Confirm: true}, true)
	operation(t, db, "delete_website", MCPDelete{ID: item.ID, UpdatedAt: updated.UpdatedAt}, true)
	operation(t, db, "delete_website", MCPDelete{ID: item.ID, UpdatedAt: updated.UpdatedAt, Confirm: true}, false)
	var count int64
	db.Model(&models.ItemIcon{}).Count(&count)
	if count != 0 {
		t.Fatal("website not deleted")
	}
	input.ID = 0
	input.URL = "https://user:TEST_ONLY_PASSWORD@example.test/"
	operation(t, db, "save_website", input, true)
}

func TestMCPPreferencesAndLinkedImport(t *testing.T) {
	db, one, _ := mcpFixture(t)
	desktop := operation(t, db, "desktop", nil, false).(map[string]any)
	input := MCPPreferences{Panel: map[string]any{"networkMode": "auto"}, SearchEngine: map[string]any{}, Revision: desktop["preferencesRevision"].(string)}
	operation(t, db, "set_preferences", input, false)
	operation(t, db, "set_preferences", input, true)
	var cfg models.UserConfig
	db.Where("user_id=1").First(&cfg)
	if !strings.Contains(cfg.PanelJson, "auto") {
		t.Fatal("preferences not saved")
	}
	link := models.GateHomeLink{RouteID: "nas", Title: "NAS", URL: "https://nas.example.test/", LanURL: "http://192.168.1.3/"}
	items := map[string]any{"items": []models.ItemIcon{{Title: link.Title, Url: link.URL, LanUrl: link.LanURL, ItemIconGroupId: int(one.ID), OpenMethod: 2, GateHome: &link}}}
	result := operation(t, db, "import_routes", items, false).(map[string]int)
	if result["created"] != 1 {
		t.Fatal("linked import failed")
	}
	result = operation(t, db, "import_routes", items, false).(map[string]int)
	if result["skipped"] != 1 {
		t.Fatal("duplicate import was not skipped")
	}
	var item models.ItemIcon
	db.Where("user_id=1").First(&item)
	db.Model(&item).Update("Title", "Custom title")
	db.First(&item, item.ID)
	link.Title = "Updated NAS"
	link.URL = "https://new-nas.example.test/"
	rows := map[string]any{"items": []map[string]any{{"id": item.ID, "updatedAt": item.UpdatedAt, "route": link}}}
	operation(t, db, "sync_routes", rows, false)
	db.First(&item, item.ID)
	if item.Title != "Custom title" || item.Url != link.URL {
		t.Fatal("sync did not preserve custom title or update default URL")
	}
	operation(t, db, "sync_routes", rows, true)
	operation(t, db, "save_group", MCPGroup{ID: one.ID, Title: "Stale", UpdatedAt: time.Time{}}, true)
}
