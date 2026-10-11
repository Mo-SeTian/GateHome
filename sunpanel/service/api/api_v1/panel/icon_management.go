package panel

import (
	"encoding/json"
	"errors"
	"sun-panel/api/api_v1/common/apiReturn"
	"sun-panel/api/api_v1/common/base"
	"sun-panel/global"
	"sun-panel/models"
	"sun-panel/models/datatype"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (a *ItemIcon) GetIconList(c *gin.Context) {
	user, _ := base.GetCurrentUserInfo(c)
	items := []models.ItemIcon{}
	if err := global.Db.Where("user_id=?", user.ID).Order("item_icon_group_id, sort, created_at").Find(&items).Error; err != nil {
		apiReturn.Error(c, "图标列表读取失败，请重试")
		return
	}
	for i := range items {
		json.Unmarshal([]byte(items[i].IconJson), &items[i].Icon)
	}
	apiReturn.SuccessListData(c, items, int64(len(items)))
}

// Only the icon changes. A stale editor cannot overwrite a newer website edit.
func (a *ItemIcon) EditIcon(c *gin.Context) {
	user, _ := base.GetCurrentUserInfo(c)
	var req struct {
		ID         uint                       `json:"id"`
		Icon       *datatype.ItemIconIconInfo `json:"icon"`
		UpdateTime time.Time                  `json:"updateTime"`
	}
	if c.ShouldBindJSON(&req) != nil || req.ID == 0 || req.Icon == nil || req.Icon.ItemType < 1 || req.Icon.ItemType > 3 || req.UpdateTime.IsZero() {
		apiReturn.Error(c, "图标参数无效，请刷新后重试")
		return
	}
	data, err := json.Marshal(req.Icon)
	if err != nil {
		apiReturn.Error(c, "图标参数无效")
		return
	}
	var item models.ItemIcon
	err = global.Db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.ItemIcon{}).Where("id=? AND user_id=? AND updated_at=?", req.ID, user.ID, req.UpdateTime).
			Updates(map[string]any{"icon_json": string(data)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("stale or unowned icon")
		}
		return tx.Where("id=? AND user_id=?", req.ID, user.ID).First(&item).Error
	})
	if err != nil {
		apiReturn.Error(c, "图标未保存，项目可能已变更或删除，请刷新列表后重试")
		return
	}
	item.Icon = *req.Icon
	apiReturn.SuccessData(c, item)
}
