package panel

import (
	"encoding/json"
	"sun-panel/models"
	"sun-panel/models/datatype"
	"testing"
)

func TestIconManagementScopeAndUpdates(t *testing.T) {
	db, group, _ := panelFixture(t)
	api := &ItemIcon{}
	item := models.ItemIcon{Title: "TEST_ONLY_SITE", Url: "https://site.example.test/", LanUrl: "http://192.168.1.8/", Description: "Keep description", OpenMethod: 3, Sort: 7, UserId: 1, ItemIconGroupId: int(group.ID), IconJson: `{"itemType":1,"text":"Old"}`, GateHome: &models.GateHomeLink{RouteID: "TEST_ONLY_ROUTE", Title: "Source", URL: "https://source.example.test/"}}
	other := models.ItemIcon{Title: "TEST_ONLY_OTHER_USER", UserId: 2, ItemIconGroupId: int(group.ID), IconJson: `{"itemType":1,"text":"Private"}`}
	deleted := models.ItemIcon{Title: "TEST_ONLY_DELETED", UserId: 1, ItemIconGroupId: int(group.ID)}
	for _, row := range []*models.ItemIcon{&item, &other, &deleted} {
		if db.Create(row).Error != nil {
			t.Fatal("fixture creation failed")
		}
	}
	db.Delete(&deleted)
	list := panelRequest(t, api.GetIconList, map[string]any{})
	var data struct {
		List  []models.ItemIcon `json:"list"`
		Count int               `json:"count"`
	}
	if string(list["code"]) != "0" || json.Unmarshal(list["data"], &data) != nil || len(data.List) != 1 || data.Count != 1 || data.List[0].ID != item.ID || data.List[0].Icon.Text != "Old" {
		t.Fatal("list must include only the current user's active websites and decoded icons")
	}
	icon := datatype.ItemIconIconInfo{ItemType: 2, Src: "/sunpanel/uploads/TEST_ONLY_ICON.png", BackgroundColor: "#ffffff"}
	request := map[string]any{"id": item.ID, "updateTime": item.UpdatedAt, "icon": icon, "title": "Must not change", "url": "https://ignored.example.test/", "userId": 2, "itemIconGroupId": 999}
	result := panelRequest(t, api.EditIcon, request)
	if string(result["code"]) != "0" {
		t.Fatal("owned icon update failed")
	}
	var saved models.ItemIcon
	db.First(&saved, item.ID)
	var actual datatype.ItemIconIconInfo
	json.Unmarshal([]byte(saved.IconJson), &actual)
	if actual != icon || saved.Title != item.Title || saved.Url != item.Url || saved.LanUrl != item.LanUrl || saved.Description != item.Description || saved.OpenMethod != item.OpenMethod || saved.Sort != item.Sort || saved.UserId != item.UserId || saved.ItemIconGroupId != item.ItemIconGroupId || *saved.GateHome != *item.GateHome || !saved.CreatedAt.Equal(item.CreatedAt) || !saved.UpdatedAt.After(item.UpdatedAt) {
		t.Fatal("only icon and update timestamp may change")
	}
	var response models.ItemIcon
	if json.Unmarshal(result["data"], &response) != nil || response.Icon != icon || !response.UpdatedAt.Equal(saved.UpdatedAt) {
		t.Fatal("response must contain the persisted icon and new revision")
	}
	for name, payload := range map[string]map[string]any{
		"stale editor":     request,
		"other account":    {"id": other.ID, "updateTime": other.UpdatedAt, "icon": icon},
		"deleted item":     {"id": deleted.ID, "updateTime": deleted.UpdatedAt, "icon": icon},
		"missing revision": {"id": item.ID, "icon": icon},
		"missing icon":     {"id": item.ID, "updateTime": saved.UpdatedAt},
		"invalid type":     {"id": item.ID, "updateTime": saved.UpdatedAt, "icon": map[string]any{"itemType": 9}},
		"missing item":     {"id": 99999, "updateTime": saved.UpdatedAt, "icon": icon},
	} {
		t.Run(name, func(t *testing.T) {
			if string(panelRequest(t, api.EditIcon, payload)["code"]) == "0" {
				t.Fatal("invalid or unowned icon update accepted")
			}
		})
	}
	var unchanged models.ItemIcon
	db.First(&unchanged, item.ID)
	if unchanged.IconJson != saved.IconJson || !unchanged.UpdatedAt.Equal(saved.UpdatedAt) {
		t.Fatal("rejected requests must not mutate the website")
	}
	unchanged = models.ItemIcon{}
	db.First(&unchanged, other.ID)
	if unchanged.IconJson != other.IconJson {
		t.Fatal("another account's icon changed")
	}
}
