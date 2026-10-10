package panel

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sun-panel/api/api_v1/common/apiReturn"
	"sun-panel/api/api_v1/common/base"
	"sun-panel/global"
	"sun-panel/models"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func canonicalSite(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443") {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	return u.String()
}

func validSite(raw string, optional bool) bool {
	if optional && raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 4096 && u.Hostname() != "" && u.User == nil && (u.Scheme == "http" || u.Scheme == "https")
}

func validLink(link *models.GateHomeLink) bool {
	return link != nil && len(link.RouteID) > 0 && len(link.RouteID) <= 64 && len([]rune(link.Title)) <= 20 && validSite(link.URL, false) && validSite(link.LanURL, true)
}

func ownedGroup(tx *gorm.DB, id int, userID uint) bool {
	var count int64
	return id > 0 && tx.Model(&models.ItemIconGroup{}).Where("id=? AND user_id=?", id, userID).Count(&count).Error == nil && count == 1
}

func (a *ItemIcon) GateHomeImport(c *gin.Context) {
	user, _ := base.GetCurrentUserInfo(c)
	var input struct {
		Items []models.ItemIcon `json:"items"`
	}
	if c.ShouldBindJSON(&input) != nil || len(input.Items) == 0 || len(input.Items) > 100 {
		apiReturn.Error(c, "请选择 1–100 个反代项")
		return
	}
	created, skipped := 0, 0
	err := global.Db.Transaction(func(tx *gorm.DB) error {
		var existing []models.ItemIcon
		if err := tx.Where("user_id=?", user.ID).Find(&existing).Error; err != nil {
			return err
		}
		for _, item := range input.Items {
			if !validLink(item.GateHome) || !ownedGroup(tx, item.ItemIconGroupId, user.ID) || !validSite(item.Url, false) || !validSite(item.LanUrl, true) {
				return errors.New("导入数据或目标分组无效")
			}
			duplicate := false
			for _, old := range existing {
				if (old.GateHome != nil && old.GateHome.RouteID == item.GateHome.RouteID) || canonicalSite(old.Url) == canonicalSite(item.Url) {
					duplicate = true
					break
				}
			}
			if duplicate {
				skipped++
				continue
			}
			item.BaseModel, item.User, item.UserId = models.BaseModel{}, models.User{}, user.ID
			encoded, err := json.Marshal(item.Icon)
			if err != nil {
				return err
			}
			item.IconJson, item.Sort = string(encoded), 9999
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			existing = append(existing, item)
			created++
		}
		return nil
	})
	if err != nil {
		apiReturn.Error(c, "批量导入未完成，请刷新分组和反代列表后重试")
		return
	}
	apiReturn.SuccessData(c, gin.H{"created": created, "skipped": skipped})
}

func (a *ItemIcon) GateHomeSync(c *gin.Context) {
	user, _ := base.GetCurrentUserInfo(c)
	var input struct {
		Items []struct {
			ID        uint                `json:"id"`
			UpdatedAt time.Time           `json:"updatedAt"`
			Route     models.GateHomeLink `json:"route"`
		} `json:"items"`
	}
	if c.ShouldBindJSON(&input) != nil || len(input.Items) == 0 || len(input.Items) > 100 {
		apiReturn.Error(c, "请选择需要更新的关联项目")
		return
	}
	err := global.Db.Transaction(func(tx *gorm.DB) error {
		for _, request := range input.Items {
			var item models.ItemIcon
			if !validLink(&request.Route) || tx.Where("id=? AND user_id=?", request.ID, user.ID).First(&item).Error != nil || item.GateHome == nil || item.GateHome.RouteID != request.Route.RouteID || !item.UpdatedAt.Equal(request.UpdatedAt) {
				return errors.New("项目已变化，请重新预览")
			}
			if item.Title == item.GateHome.Title {
				item.Title = request.Route.Title
			}
			if item.Url == item.GateHome.URL {
				item.Url = request.Route.URL
			}
			if item.LanUrl == item.GateHome.LanURL {
				item.LanUrl = request.Route.LanURL
			}
			item.GateHome = &request.Route
			if err := tx.Model(&item).Select("Title", "Url", "LanUrl", "GateHome").Updates(&item).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		apiReturn.Error(c, "关联更新未完成，项目可能已变化，请刷新后重新预览")
		return
	}
	apiReturn.SuccessData(c, gin.H{"updated": len(input.Items)})
}
