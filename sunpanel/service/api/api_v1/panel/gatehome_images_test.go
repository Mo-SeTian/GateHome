package panel

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sun-panel/models"
	"sun-panel/models/datatype"
	"testing"
)

func TestGateHomeImageSyncBackfillsAndPreservesOverrides(t *testing.T) {
	db, group, _ := panelFixture(t)
	link := models.GateHomeLink{RouteID: "TEST_ONLY_ROUTE", Title: "NAS", URL: "https://nas.example.test/"}
	item := models.ItemIcon{Title: link.Title, Url: link.URL, UserId: 1, ItemIconGroupId: int(group.ID), GateHome: &link}
	if _, _, err := ImportGateHome(db, 1, []models.ItemIcon{item}); err != nil {
		t.Fatal(err)
	}
	var stored models.ItemIcon
	db.First(&stored)
	hash := strings.Repeat("a", 64)
	icon := datatype.ItemIconIconInfo{ItemType: 2, Src: "/sunpanel/uploads/imported.png", BackgroundColor: "transparent"}
	next := link
	next.Image = &hash
	next.Icon = icon
	apply := func(route models.GateHomeLink) {
		t.Helper()
		if err := SyncGateHome(db, 1, []GateHomeSyncItem{{ID: stored.ID, UpdatedAt: stored.UpdatedAt, Route: route}}); err != nil {
			t.Fatal(err)
		}
		db.First(&stored, stored.ID)
	}
	apply(next)
	var actual datatype.ItemIconIconInfo
	json.Unmarshal([]byte(stored.IconJson), &actual)
	if actual != icon || stored.GateHome.Image == nil || *stored.GateHome.Image != hash {
		t.Fatal("legacy blank icon was not filled")
	}
	// Older clients and MCP requests that omit image fields must preserve the new baseline.
	legacy := link
	legacy.Title = "Renamed NAS"
	apply(legacy)
	if stored.GateHome.Image == nil || *stored.GateHome.Image != hash || stored.GateHome.Icon != icon {
		t.Fatal("legacy sync discarded image metadata")
	}
	custom := datatype.ItemIconIconInfo{ItemType: 1, Text: "My icon", BackgroundColor: "#123456"}
	customJSON, _ := json.Marshal(custom)
	db.Model(&stored).Update("icon_json", string(customJSON))
	db.First(&stored, stored.ID)
	newHash := strings.Repeat("b", 64)
	changed := next
	changed.Image = &newHash
	changed.Icon.Src = "/sunpanel/uploads/updated.png"
	apply(changed)
	json.Unmarshal([]byte(stored.IconJson), &actual)
	if actual != custom || *stored.GateHome.Image != newHash || stored.GateHome.Icon != icon {
		t.Fatal("custom icon overwritten")
	}
	// An explicit clear is also a user override after an image has been linked.
	db.Model(&stored).Update("icon_json", `{}`)
	db.First(&stored, stored.ID)
	third := strings.Repeat("c", 64)
	changed.Image = &third
	apply(changed)
	if stored.IconJson != "{}" {
		t.Fatal("explicitly cleared icon overwritten")
	}
	if validLink(&models.GateHomeLink{RouteID: link.RouteID, Title: link.Title, URL: link.URL, Image: func() *string { s := "../invalid"; return &s }()}) {
		t.Fatal("invalid source image marker accepted")
	}
}

func TestGateHomeImageBaselineSurvivesPortableRoundTrip(t *testing.T) {
	db, group, root := panelFixture(t)
	var data bytes.Buffer
	png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 16, 16)))
	if os.WriteFile(filepath.Join(root, "linked.png"), data.Bytes(), 0600) != nil {
		t.Fatal("image fixture failed")
	}
	icon := datatype.ItemIconIconInfo{ItemType: 2, Src: "/sunpanel/uploads/linked.png", BackgroundColor: "transparent"}
	hash := strings.Repeat("d", 64)
	link := &models.GateHomeLink{RouteID: "TEST_ONLY_ROUTE", Title: "NAS", URL: "https://nas.example.test/", Image: &hash, Icon: icon}
	encoded, _ := json.Marshal(icon)
	item := models.ItemIcon{Title: link.Title, Url: link.URL, OpenMethod: 2, IconJson: string(encoded), GateHome: link, ItemIconGroupId: int(group.ID), UserId: 1}
	if db.Create(&item).Error != nil {
		t.Fatal("item fixture failed")
	}
	archive, err := buildPortable(db, 1, root)
	if err != nil {
		t.Fatal(err)
	}
	manifest, images, err := readPortable(archive)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Groups[0].Items[0].GateHome.Icon != manifest.Groups[0].Items[0].Icon {
		t.Fatal("export lost baseline mapping")
	}
	if err := importPortable(db, 1, root, manifest, images); err != nil {
		t.Fatal(err)
	}
	var restored models.ItemIcon
	db.Order("id desc").First(&restored)
	var restoredIcon datatype.ItemIconIconInfo
	json.Unmarshal([]byte(restored.IconJson), &restoredIcon)
	if restoredIcon.Src == icon.Src || !reflect.DeepEqual(restored.GateHome.Icon, restoredIcon) || *restored.GateHome.Image != hash {
		t.Fatal("restored baseline no longer matches its relocated icon")
	}
}
