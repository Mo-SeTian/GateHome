package panel

import (
	"encoding/json"
	"sun-panel/models"
	"testing"
)

func TestEditsCannotCreateAssociatedAccounts(t *testing.T) {
	db, group, _ := panelFixture(t)
	nested := map[string]any{"username": "TEST_ONLY_INJECTED_ADMIN", "password": "TEST_ONLY_PASSWORD", "role": 1, "status": 1}
	tests := []struct {
		name string
		call func() map[string]json.RawMessage
	}{
		{"group", func() map[string]json.RawMessage {
			return panelRequest(t, (&ItemIconGroup{}).Edit, map[string]any{"title": "New", "userId": 2, "user": nested})
		}},
		{"item", func() map[string]json.RawMessage {
			return panelRequest(t, (&ItemIcon{}).Edit, map[string]any{"title": "New", "itemIconGroupId": group.ID, "userId": 2, "user": nested})
		}},
		{"batch", func() map[string]json.RawMessage {
			return panelRequest(t, (&ItemIcon{}).AddMultiple, []map[string]any{{"id": 999, "title": "New", "itemIconGroupId": group.ID, "userId": 2, "user": nested}})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if string(test.call()["code"]) != "0" {
				t.Fatal("valid editable fields were rejected")
			}
			var users, foreign int64
			db.Model(&models.User{}).Count(&users)
			db.Model(&models.ItemIcon{}).Where("user_id<>1").Count(&foreign)
			if users != 2 || foreign != 0 {
				t.Fatal("client-supplied account or ownership was persisted")
			}
		})
	}
	var foreign int64
	db.Model(&models.ItemIconGroup{}).Where("user_id<>1").Count(&foreign)
	if foreign != 0 {
		t.Fatal("group ownership was assigned by client")
	}
}
func TestForeignGroupAndBatchEditsAreRejected(t *testing.T) {
	db, group, _ := panelFixture(t)
	other := models.ItemIconGroup{Title: "Other", UserId: 2}
	db.Create(&other)
	if string(panelRequest(t, (&ItemIconGroup{}).Edit, map[string]any{"id": other.ID, "title": "Changed", "userId": 1})["code"]) == "0" {
		t.Fatal("foreign group edit succeeded")
	}
	var stored models.ItemIconGroup
	db.First(&stored, other.ID)
	if stored.UserId != 2 || stored.Title != "Other" {
		t.Fatal("foreign group changed")
	}
	result := panelRequest(t, (&ItemIcon{}).AddMultiple, []map[string]any{{"title": "Valid", "itemIconGroupId": group.ID}, {"title": "Foreign", "itemIconGroupId": other.ID}})
	var count int64
	db.Model(&models.ItemIcon{}).Count(&count)
	if string(result["code"]) == "0" || count != 0 {
		t.Fatal("invalid batch was partially saved")
	}
	// Duplicate and foreign IDs must not bypass retention of the last owned group.
	result = panelRequest(t, (&ItemIconGroup{}).Deletes, map[string]any{"ids": []uint{group.ID, group.ID, other.ID}})
	if string(result["code"]) == "0" {
		t.Fatal("last owned group was deleted")
	}
}
func TestPreferencesKeepOwnerAndAllowClearing(t *testing.T) {
	db, _, _ := panelFixture(t)
	if db.AutoMigrate(&models.UserConfig{}) != nil {
		t.Fatal("fixture migration failed")
	}
	db.Create(&models.UserConfig{UserId: 1, PanelJson: `{"title":"Before"}`, SearchEngineJson: `{"engine":"Before"}`})
	db.Create(&models.UserConfig{UserId: 2, PanelJson: `{"title":"Other"}`, SearchEngineJson: `{}`})
	result := panelRequest(t, (&UserConfig{}).Set, map[string]any{"userId": 2, "panel": map[string]any{}, "searchEngine": map[string]any{}})
	if string(result["code"]) != "0" {
		t.Fatal("preference update failed")
	}
	var own, other models.UserConfig
	db.First(&own, "user_id=1")
	db.First(&other, "user_id=2")
	if own.UserId != 1 || own.PanelJson != "{}" || own.SearchEngineJson != "{}" || other.PanelJson != `{"title":"Other"}` {
		t.Fatal("preference ownership or empty values changed incorrectly")
	}
}
