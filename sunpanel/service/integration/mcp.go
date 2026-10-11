package integration

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	panelapi "sun-panel/api/api_v1/panel"
	"sun-panel/global"
	"sun-panel/models"
	"sun-panel/models/datatype"
	"time"

	"gorm.io/gorm"
)

// MCP DTOs intentionally have no account, password, session or database fields.
type MCPGroup struct {
	ID          uint      `json:"id,omitempty"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Icon        string    `json:"icon"`
	Sort        int       `json:"sort"`
	UpdatedAt   time.Time `json:"updatedAt,omitempty"`
}

type MCPWebsite struct {
	ID          uint                       `json:"id,omitempty"`
	GroupID     int                        `json:"groupId"`
	Title       string                     `json:"title"`
	URL         string                     `json:"url"`
	LanURL      string                     `json:"lanUrl"`
	Description string                     `json:"description"`
	OpenMethod  int                        `json:"openMethod"`
	Sort        int                        `json:"sort"`
	Icon        *datatype.ItemIconIconInfo `json:"icon,omitempty"`
	GateHome    *models.GateHomeLink       `json:"gateHome,omitempty"`
	UpdatedAt   time.Time                  `json:"updatedAt,omitempty"`
}

type MCPDelete struct {
	ID        uint      `json:"id"`
	UpdatedAt time.Time `json:"updatedAt"`
	Confirm   bool      `json:"confirm"`
}

type MCPPreferences struct {
	Panel        map[string]any `json:"panel"`
	SearchEngine map[string]any `json:"searchEngine"`
	Revision     string         `json:"preferencesRevision"`
}

func preferencesRevision(config models.UserConfig) string {
	sum := sha256.Sum256([]byte(config.PanelJson + "\x00" + config.SearchEngineJson))
	return hex.EncodeToString(sum[:])
}

func mcpDecode(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("管理请求格式无效")
	}
	return nil
}

func mcpSite(value string, optional bool) bool {
	if optional && value == "" {
		return true
	}
	u, err := url.Parse(value)
	decoded, _ := url.PathUnescape(value)
	return err == nil && !strings.Contains(decoded, "<REDACTED>") && !strings.Contains(strings.ToLower(decoded), "%3credacted%3e") && len(value) <= 4096 && u.Hostname() != "" && u.User == nil && (u.Scheme == "http" || u.Scheme == "https")
}

func mcpGroup(group models.ItemIconGroup) MCPGroup {
	return MCPGroup{group.ID, group.Title, group.Description, group.Icon, group.Sort, group.UpdatedAt}
}

func mcpWebsite(item models.ItemIcon) MCPWebsite {
	var icon *datatype.ItemIconIconInfo
	if json.Unmarshal([]byte(item.IconJson), &icon) != nil || (icon != nil && (icon.ItemType < 1 || icon.ItemType > 3)) {
		icon = nil
	}
	return MCPWebsite{item.ID, item.ItemIconGroupId, item.Title, item.Url, item.LanUrl, item.Description, item.OpenMethod, item.Sort, icon, item.GateHome, item.UpdatedAt}
}

// The parent's ephemeral key is inherited through a pipe, never argv or disk.
func MCPBridge(key []byte) http.Handler {
	expected := "Bearer " + hex.EncodeToString(key)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fail := func(code int, message string) {
			w.WriteHeader(code)
			json.NewEncoder(w).Encode(map[string]string{"error": message})
		}
		if len(key) != 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expected)) != 1 {
			fail(403, "管理连接未授权")
			return
		}
		if r.Method != "POST" {
			fail(405, "只接受管理 POST 请求")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		data, err := io.ReadAll(r.Body)
		var input struct {
			Action string          `json:"action"`
			UserID uint            `json:"userId"`
			Data   json.RawMessage `json:"data"`
		}
		if err != nil || mcpDecode(data, &input) != nil {
			fail(400, "管理请求格式无效或过大")
			return
		}
		if input.Action == "users" {
			var users []struct {
				ID       uint   `json:"id"`
				Username string `json:"username"`
				Name     string `json:"name"`
			}
			if global.Db.Model(&models.User{}).Select("id, username, name").Where("status=1").Order("id").Find(&users).Error != nil {
				fail(400, "读取账号失败")
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"data": users})
			return
		}
		var user models.User
		if input.UserID == 0 || global.Db.Select("id, status").Where("id=? AND status=1", input.UserID).First(&user).Error != nil {
			fail(403, "绑定的 Sun-Panel 账号不可用")
			return
		}
		result, err := mcpOperation(global.Db, input.Action, user.ID, input.Data)
		if err != nil {
			fail(400, err.Error())
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": result})
	})
}

func mcpOperation(db *gorm.DB, action string, userID uint, data []byte) (any, error) {
	invalid := errors.New("参数无效，或项目已变化，请重新读取后再操作")
	switch action {
	case "desktop":
		var groups []models.ItemIconGroup
		var items []models.ItemIcon
		if db.Where("user_id=?", userID).Order("sort, created_at").Find(&groups).Error != nil || db.Where("user_id=?", userID).Order("sort, created_at").Find(&items).Error != nil {
			return nil, errors.New("读取首页失败")
		}
		outGroups, outItems := []MCPGroup{}, []MCPWebsite{}
		for _, group := range groups {
			outGroups = append(outGroups, mcpGroup(group))
		}
		for _, item := range items {
			outItems = append(outItems, mcpWebsite(item))
		}
		config := models.UserConfig{}
		if err := db.Where("user_id=?", userID).First(&config).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("读取首页偏好失败")
		}
		panel, search := map[string]any{}, map[string]any{}
		json.Unmarshal([]byte(config.PanelJson), &panel)
		json.Unmarshal([]byte(config.SearchEngineJson), &search)
		return map[string]any{"groups": outGroups, "items": outItems, "panel": panel, "searchEngine": search, "preferencesRevision": preferencesRevision(config)}, nil
	case "set_preferences":
		var input MCPPreferences
		if mcpDecode(data, &input) != nil || input.Panel == nil || input.SearchEngine == nil {
			return nil, invalid
		}
		var revision string
		err := db.Transaction(func(tx *gorm.DB) error {
			old := models.UserConfig{}
			err := tx.Where("user_id=?", userID).First(&old).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return invalid
			}
			if preferencesRevision(old) != input.Revision {
				return invalid
			}
			p, _ := json.Marshal(input.Panel)
			s, _ := json.Marshal(input.SearchEngine)
			next := models.UserConfig{UserId: userID, PanelJson: string(p), SearchEngineJson: string(s)}
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if tx.Create(&next).Error != nil {
					return invalid
				}
			} else {
				result := tx.Model(&models.UserConfig{}).Where("user_id=? AND panel_json=? AND search_engine_json=?", userID, old.PanelJson, old.SearchEngineJson).Updates(map[string]any{"panel_json": next.PanelJson, "search_engine_json": next.SearchEngineJson})
				if result.Error != nil || result.RowsAffected != 1 {
					return invalid
				}
			}
			revision = preferencesRevision(next)
			return nil
		})
		return map[string]string{"preferencesRevision": revision}, err
	case "import_routes":
		var input struct {
			Items []models.ItemIcon `json:"items"`
		}
		if mcpDecode(data, &input) != nil {
			return nil, invalid
		}
		created, skipped, err := panelapi.ImportGateHome(db, userID, input.Items)
		if err != nil {
			return nil, invalid
		}
		return map[string]int{"created": created, "skipped": skipped}, nil
	case "sync_routes":
		var input struct {
			Items []panelapi.GateHomeSyncItem `json:"items"`
		}
		if mcpDecode(data, &input) != nil || panelapi.SyncGateHome(db, userID, input.Items) != nil {
			return nil, invalid
		}
		return map[string]int{"updated": len(input.Items)}, nil
	case "save_group":
		var input MCPGroup
		if mcpDecode(data, &input) != nil || strings.TrimSpace(input.Title) == "" || len([]rune(input.Title)) > 100 || len(input.Description) > 1000 || len(input.Icon) > 1000 {
			return nil, invalid
		}
		group := models.ItemIconGroup{Title: input.Title, Description: input.Description, Icon: input.Icon, Sort: input.Sort, UserId: userID}
		if input.ID == 0 {
			if db.Create(&group).Error != nil {
				return nil, errors.New("分组保存失败")
			}
		} else {
			group.ID = input.ID
			result := db.Model(&group).Where("id=? AND user_id=? AND updated_at=?", input.ID, userID, input.UpdatedAt).Select("Title", "Description", "Icon", "Sort").Updates(&group)
			if result.Error != nil || result.RowsAffected != 1 {
				return nil, invalid
			}
		}
		return mcpGroup(group), nil
	case "save_website":
		var input MCPWebsite
		if mcpDecode(data, &input) != nil || strings.TrimSpace(input.Title) == "" || len([]rune(input.Title)) > 100 || len(input.Description) > 2000 || !mcpSite(input.URL, false) || !mcpSite(input.LanURL, true) || input.OpenMethod < 1 || input.OpenMethod > 3 {
			return nil, invalid
		}
		var item models.ItemIcon
		err := db.Transaction(func(tx *gorm.DB) error {
			var group models.ItemIconGroup
			if tx.Where("id=? AND user_id=?", input.GroupID, userID).First(&group).Error != nil {
				return invalid
			}
			if input.ID != 0 {
				if tx.Where("id=? AND user_id=?", input.ID, userID).First(&item).Error != nil || !item.UpdatedAt.Equal(input.UpdatedAt) {
					return invalid
				}
			}
			item.Title, item.Url, item.LanUrl, item.Description = input.Title, input.URL, input.LanURL, input.Description
			item.ItemIconGroupId, item.OpenMethod, item.Sort, item.UserId = input.GroupID, input.OpenMethod, input.Sort, userID
			if input.Icon != nil {
				if input.Icon.ItemType < 1 || input.Icon.ItemType > 3 || len(input.Icon.Src) > 4096 {
					return invalid
				}
				encoded, err := json.Marshal(input.Icon)
				if err != nil {
					return invalid
				}
				item.IconJson = string(encoded)
			} else if input.ID == 0 {
				item.IconJson = "null"
			}
			// Association is preserved on edits. Only parent-built imports set it.
			if input.ID == 0 {
				item.GateHome = nil
			}
			if tx.Omit("User", "Icon").Save(&item).Error != nil {
				return errors.New("网站保存失败")
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return mcpWebsite(item), nil
	case "delete_group", "delete_website":
		var input MCPDelete
		if mcpDecode(data, &input) != nil || !input.Confirm || input.ID == 0 {
			return nil, invalid
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			if action == "delete_group" {
				var group models.ItemIconGroup
				var count int64
				if tx.Where("id=? AND user_id=?", input.ID, userID).First(&group).Error != nil || !group.UpdatedAt.Equal(input.UpdatedAt) || tx.Model(&models.ItemIconGroup{}).Where("user_id=?", userID).Count(&count).Error != nil || count <= 1 {
					return invalid
				}
				if tx.Where("item_icon_group_id=? AND user_id=?", input.ID, userID).Delete(&models.ItemIcon{}).Error != nil || tx.Delete(&group).Error != nil {
					return errors.New("分组删除失败")
				}
			} else {
				result := tx.Where("id=? AND user_id=? AND updated_at=?", input.ID, userID, input.UpdatedAt).Delete(&models.ItemIcon{})
				if result.Error != nil || result.RowsAffected != 1 {
					return invalid
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return map[string]bool{"deleted": true}, nil
	}
	return nil, errors.New("不支持的首页管理操作")
}
