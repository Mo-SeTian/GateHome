package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sun-panel/api/api_v1/common/apiData/commonApiStructs"
	"sun-panel/api/api_v1/common/apiData/panelApiStructs"
	"sun-panel/api/api_v1/common/apiReturn"
	"sun-panel/api/api_v1/common/base"
	"sun-panel/global"
	"sun-panel/lib/cmn"
	"sun-panel/lib/siteFavicon"
	"sun-panel/models"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sun-panel/models/datatype"
)

type ItemIcon struct {
}

// Only editable website fields are accepted; ownership and ORM associations are server controlled.
type itemIconInput struct {
	ID              uint                      `json:"id"`
	Title           string                    `json:"title"`
	Url             string                    `json:"url"`
	LanUrl          string                    `json:"lanUrl"`
	Description     string                    `json:"description"`
	OpenMethod      int                       `json:"openMethod"`
	Sort            int                       `json:"sort"`
	ItemIconGroupId int                       `json:"itemIconGroupId"`
	Icon            datatype.ItemIconIconInfo `json:"icon"`
	GateHome        *models.GateHomeLink      `json:"gateHome,omitempty"`
}

func (v itemIconInput) model(userID uint) models.ItemIcon {
	return models.ItemIcon{BaseModel: models.BaseModel{ID: v.ID}, Title: v.Title, Url: v.Url, LanUrl: v.LanUrl, Description: v.Description, OpenMethod: v.OpenMethod, Sort: v.Sort, ItemIconGroupId: v.ItemIconGroupId, Icon: v.Icon, GateHome: v.GateHome, UserId: userID}
}

func (a *ItemIcon) Edit(c *gin.Context) {
	userInfo, _ := base.GetCurrentUserInfo(c)
	input := itemIconInput{}

	if err := c.ShouldBindBodyWith(&input, binding.JSON); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}

	req := input.model(userInfo.ID)
	if req.ItemIconGroupId == 0 {
		// apiReturn.Error(c, "Group is mandatory")
		apiReturn.ErrorParamFomat(c, "Group is mandatory")
		return
	}

	req.UserId = userInfo.ID
	if !ownedGroup(global.Db, req.ItemIconGroupId, userInfo.ID) || (req.GateHome != nil && !validLink(req.GateHome)) {
		apiReturn.Error(c, "分组或关联信息无效")
		return
	}

	// json转字符串
	if j, err := json.Marshal(req.Icon); err == nil {
		req.IconJson = string(j)
	}

	if req.ID != 0 {
		// 修改
		updateField := []string{"IconJson", "Icon", "Title", "Url", "LanUrl", "Description", "OpenMethod", "ItemIconGroupId", "GateHome"}
		if req.Sort != 0 {
			updateField = append(updateField, "Sort")
		}
		result := global.Db.Model(&models.ItemIcon{}).
			Omit(clause.Associations).Select(updateField).
			Where("id=? AND user_id=?", req.ID, userInfo.ID).Updates(&req)
		if result.Error != nil || result.RowsAffected != 1 {
			apiReturn.Error(c, "项目保存失败，请刷新后重试")
			return
		}
	} else {
		req.Sort = 9999
		// 创建
		if err := global.Db.Omit(clause.Associations).Create(&req).Error; err != nil {
			apiReturn.Error(c, "项目保存失败")
			return
		}
	}

	apiReturn.SuccessData(c, req)
}

// 添加多个图标
func (a *ItemIcon) AddMultiple(c *gin.Context) {
	userInfo, _ := base.GetCurrentUserInfo(c)
	var input []itemIconInput
	if err := c.ShouldBindBodyWith(&input, binding.JSON); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}
	if len(input) == 0 || len(input) > 100 {
		apiReturn.Error(c, "请选择 1–100 个网站")
		return
	}
	req := make([]models.ItemIcon, len(input))
	for i, v := range input {
		if !ownedGroup(global.Db, v.ItemIconGroupId, userInfo.ID) || (v.GateHome != nil && !validLink(v.GateHome)) {
			apiReturn.Error(c, "分组或关联信息无效")
			return
		}
		req[i] = v.model(userInfo.ID)
		req[i].ID = 0
		data, err := json.Marshal(req[i].Icon)
		if err != nil {
			apiReturn.ErrorParamFomat(c, "图标信息无效")
			return
		}
		req[i].IconJson = string(data)
	}
	if global.Db.Omit(clause.Associations).Create(&req).Error != nil {
		apiReturn.Error(c, "项目保存失败")
		return
	}

	apiReturn.SuccessData(c, req)
}

// // 获取详情
// func (a *ItemIcon) GetInfo(c *gin.Context) {
// 	req := systemApiStructs.AiDrawGetInfoReq{}

// 	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
// 		apiReturn.ErrorParamFomat(c, err.Error())
// 		return
// 	}

// 	userInfo, _ := base.GetCurrentUserInfo(c)

// 	aiDraw := models.AiDraw{}
// 	aiDraw.ID = req.ID
// 	if err := aiDraw.GetInfo(global.Db); err != nil {
// 		if err == gorm.ErrRecordNotFound {
// 			apiReturn.Error(c, "不存在记录")
// 			return
// 		}
// 		apiReturn.ErrorDatabase(c, err.Error())
// 		return
// 	}

// 	if userInfo.ID != aiDraw.UserID {
// 		apiReturn.ErrorNoAccess(c)
// 		return
// 	}

// 	apiReturn.SuccessData(c, aiDraw)
// }

func (a *ItemIcon) GetListByGroupId(c *gin.Context) {
	req := models.ItemIcon{}

	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}

	userInfo, _ := base.GetCurrentUserInfo(c)
	itemIcons := []models.ItemIcon{}

	if err := global.Db.Order("sort ,created_at").Find(&itemIcons, "item_icon_group_id = ? AND user_id=?", req.ItemIconGroupId, userInfo.ID).Error; err != nil {
		apiReturn.ErrorDatabase(c, err.Error())
		return
	}

	for k, v := range itemIcons {
		json.Unmarshal([]byte(v.IconJson), &itemIcons[k].Icon)
	}

	apiReturn.SuccessListData(c, itemIcons, 0)
}

func (a *ItemIcon) Deletes(c *gin.Context) {
	req := commonApiStructs.RequestDeleteIds[uint]{}

	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}

	userInfo, _ := base.GetCurrentUserInfo(c)
	if err := global.Db.Delete(&models.ItemIcon{}, "id in ? AND user_id=?", req.Ids, userInfo.ID).Error; err != nil {
		apiReturn.ErrorDatabase(c, err.Error())
		return
	}

	apiReturn.Success(c)
}

// 保存排序
func (a *ItemIcon) SaveSort(c *gin.Context) {
	req := panelApiStructs.ItemIconSaveSortRequest{}

	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}

	userInfo, _ := base.GetCurrentUserInfo(c)

	transactionErr := global.Db.Transaction(func(tx *gorm.DB) error {
		// 在事务中执行一些 db 操作（从这里开始，您应该使用 'tx' 而不是 'db'）
		for _, v := range req.SortItems {
			if err := tx.Model(&models.ItemIcon{}).Where("user_id=? AND id=? AND item_icon_group_id=?", userInfo.ID, v.Id, req.ItemIconGroupId).Update("sort", v.Sort).Error; err != nil {
				// 返回任何错误都会回滚事务
				return err
			}
		}

		// 返回 nil 提交事务
		return nil
	})

	if transactionErr != nil {
		apiReturn.ErrorDatabase(c, transactionErr.Error())
		return
	}

	apiReturn.Success(c)
}

// 支持获取并直接下载对方网站图标到服务器
func (a *ItemIcon) GetSiteFavicon(c *gin.Context) {
	userInfo, _ := base.GetCurrentUserInfo(c)
	req := panelApiStructs.ItemIconGetSiteFaviconReq{}

	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}
	resp := panelApiStructs.ItemIconGetSiteFaviconResp{}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	fullUrl, err := siteFavicon.GetOneFaviconURLContext(ctx, req.Url)
	if err != nil {
		apiReturn.Error(c, "图标获取失败："+err.Error())
		return
	}
	parsedURL, err := url.Parse(req.Url)
	if err != nil {
		apiReturn.Error(c, "网站地址无效")
		return
	}

	// 生成保存目录
	configUpload := global.Config.GetValueString("base", "source_path")
	savePath := fmt.Sprintf("%s/%d/%d/%d/", configUpload, time.Now().Year(), time.Now().Month(), time.Now().Day())
	isExist, _ := cmn.PathExists(savePath)
	if !isExist {
		if err := os.MkdirAll(savePath, 0755); err != nil {
			apiReturn.Error(c, "图标目录不可写")
			return
		}
	}

	// 下载
	var imgInfo *os.File
	{
		var err error
		if imgInfo, err = siteFavicon.DownloadImageContext(ctx, fullUrl, savePath, 1024*1024); err != nil {
			apiReturn.Error(c, "acquisition failed: download"+err.Error())
			return
		}
	}

	// 保存到数据库
	imagePath := imgInfo.Name()
	if !filepath.IsAbs(imagePath) {
		imagePath = "./" + filepath.ToSlash(imagePath)
	}
	ext := path.Ext(imagePath)
	mFile := models.File{}
	if _, err := mFile.AddFile(userInfo.ID, parsedURL.Host, ext, imagePath); err != nil {
		os.Remove(imgInfo.Name())
		apiReturn.Error(c, "图标记录保存失败")
		return
	}
	resp.IconUrl = "/sunpanel" + strings.TrimPrefix(imagePath, ".")
	apiReturn.SuccessData(c, resp)
}
