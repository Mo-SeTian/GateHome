package system

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"sun-panel/api/api_v1/common/apiReturn"
	"sun-panel/api/api_v1/common/base"
	"sun-panel/global"
	"sun-panel/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type assetReference struct {
	Title string `json:"title"`
	Kind  string `json:"kind"`
}

func assetKey(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	source := strings.TrimPrefix(parsed.Path, "/sunpanel")
	source = strings.TrimPrefix(source, ".")
	if !strings.HasPrefix(source, "/uploads/") {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(source))
}

// Check every account because local image URLs can be reused by another account.
// Other accounts' names and website data are never included in responses.
func assetReferences(db *gorm.DB, owner uint) (map[string][]assetReference, error) {
	refs := map[string][]assetReference{}
	var items []models.ItemIcon
	if err := db.Find(&items).Error; err != nil {
		return nil, err
	}
	for _, item := range items {
		var icon struct {
			Src      string `json:"src"`
			Text     string `json:"text"`
			ItemType int    `json:"itemType"`
		}
		if item.IconJson == "" {
			continue
		}
		if json.Unmarshal([]byte(item.IconJson), &icon) != nil {
			return nil, errors.New("icon data cannot be checked")
		}
		source := icon.Src
		if icon.ItemType == 3 {
			source = icon.Text
		}
		if icon.ItemType == 1 {
			continue
		}
		key := assetKey(source)
		if key == "" {
			continue
		}
		ref := assetReference{Title: item.Title, Kind: "网站图标"}
		if item.UserId != owner {
			ref = assetReference{Title: "其他账号", Kind: "图片引用"}
		}
		refs[key] = append(refs[key], ref)
	}
	var configs []models.UserConfig
	if err := db.Find(&configs).Error; err != nil {
		return nil, err
	}
	for _, config := range configs {
		if config.PanelJson == "" {
			continue
		}
		var panel any
		if json.Unmarshal([]byte(config.PanelJson), &panel) != nil {
			return nil, errors.New("panel data cannot be checked")
		}
		seen := map[string]bool{}
		var visit func(any)
		visit = func(value any) {
			switch value := value.(type) {
			case string:
				key := assetKey(value)
				if key != "" && !seen[key] {
					seen[key] = true
					ref := assetReference{Title: "桌面设置", Kind: "壁纸/图片"}
					if config.UserId != owner {
						ref = assetReference{Title: "其他账号", Kind: "图片引用"}
					}
					refs[key] = append(refs[key], ref)
				}
			case map[string]any:
				for _, v := range value {
					visit(v)
				}
			case []any:
				for _, v := range value {
					visit(v)
				}
			}
		}
		visit(panel)
	}
	return refs, nil
}

func checkedAssetPath(source string) (string, error) {
	root, err := filepath.Abs(global.Config.GetValueString("base", "source_path"))
	if err != nil {
		return "", err
	}
	file, err := filepath.Abs(source)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, file)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", errors.New("invalid asset path")
	}
	// Do not follow a symbolic link outside the storage tree.
	current := root
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return file, nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("invalid asset link")
		}
	}
	return file, nil
}

func (a *FileApi) DeletePreview(c *gin.Context) {
	user, _ := base.GetCurrentUserInfo(c)
	refs, err := assetReferences(global.Db, user.ID)
	if err != nil {
		apiReturn.Error(c, "无法检查图片引用，请稍后重试")
		return
	}
	var files []models.File
	if global.Db.Where("user_id=?", user.ID).Find(&files).Error != nil {
		apiReturn.Error(c, "无法读取图片文件")
		return
	}
	list := []map[string]any{}
	var total int64
	for _, file := range files {
		if len(refs[assetKey(file.Src)]) != 0 {
			continue
		}
		source, err := checkedAssetPath(file.Src)
		if err != nil {
			continue
		}
		var size int64
		if info, err := os.Stat(source); err == nil {
			size = info.Size()
		}
		total += size
		list = append(list, map[string]any{"id": file.ID, "fileName": file.FileName, "src": "/sunpanel" + strings.TrimPrefix(file.Src, "."), "size": size})
	}
	apiReturn.SuccessData(c, gin.H{"list": list, "bytes": total})
}
