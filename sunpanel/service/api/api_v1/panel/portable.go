package panel

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sun-panel/api/api_v1/common/apiReturn"
	"sun-panel/api/api_v1/common/base"
	"sun-panel/global"
	"sun-panel/models"
	"sun-panel/models/datatype"
	"time"

	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"
	"gorm.io/gorm"
)

const portableLimit = 64 << 20

type portableItem struct {
	Title       string                    `json:"title"`
	URL         string                    `json:"url"`
	LanURL      string                    `json:"lanUrl"`
	Description string                    `json:"description"`
	OpenMethod  int                       `json:"openMethod"`
	Sort        int                       `json:"sort"`
	Icon        datatype.ItemIconIconInfo `json:"icon"`
	GateHome    *models.GateHomeLink      `json:"gateHome,omitempty"`
}
type portableGroup struct {
	Title       string         `json:"title"`
	Icon        string         `json:"icon"`
	Description string         `json:"description"`
	Sort        int            `json:"sort"`
	Items       []portableItem `json:"items"`
}
type portableManifest struct {
	Format    string            `json:"format"`
	CreatedAt time.Time         `json:"createdAt"`
	Groups    []portableGroup   `json:"groups"`
	Images    map[string]string `json:"images"`
}

func portableImageValid(name string, data []byte) bool {
	if len(data) == 0 || len(data) > 10<<20 {
		return false
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		return err == nil && config.Width > 0 && config.Height > 0 && config.Width <= 16384 && config.Height <= 16384
	case ".ico":
		return len(data) >= 22 && bytes.Equal(data[:4], []byte{0, 0, 1, 0})
	case ".svg":
		decoder := xml.NewDecoder(bytes.NewReader(data))
		for {
			token, err := decoder.Token()
			if err != nil {
				return false
			}
			if start, ok := token.(xml.StartElement); ok {
				return start.Name.Local == "svg"
			}
		}
	}
	return false
}

func buildPortable(db *gorm.DB, userID uint, root string) ([]byte, error) {
	manifest := portableManifest{Format: "gatehome-sunpanel-v1", CreatedAt: time.Now().UTC(), Groups: []portableGroup{}, Images: map[string]string{}}
	var groups []models.ItemIconGroup
	if err := db.Where("user_id=?", userID).Order("sort, created_at").Find(&groups).Error; err != nil {
		return nil, err
	}
	if len(groups) > 100 {
		return nil, errors.New("首页 ZIP 最多支持 100 个分组")
	}
	images := map[string][]byte{}
	var total, count int
	for _, group := range groups {
		if strings.TrimSpace(group.Title) == "" || len([]rune(group.Title)) > 100 {
			return nil, errors.New("分组名称无效，请修改后导出")
		}
		out := portableGroup{Title: group.Title, Icon: group.Icon, Description: group.Description, Sort: group.Sort, Items: []portableItem{}}
		var items []models.ItemIcon
		if err := db.Where("user_id=? AND item_icon_group_id=?", userID, group.ID).Order("sort, created_at").Find(&items).Error; err != nil {
			return nil, err
		}
		for _, item := range items {
			count++
			if count > 1000 {
				return nil, errors.New("首页 ZIP 最多支持 1000 个网站")
			}
			if strings.TrimSpace(item.Title) == "" || len([]rune(item.Title)) > 100 || !validSite(item.Url, false) || !validSite(item.LanUrl, true) || item.OpenMethod < 1 || item.OpenMethod > 3 || (item.GateHome != nil && !validLink(item.GateHome)) {
				return nil, errors.New("网站名称、HTTP(S) 地址或打开方式无效，请修改后导出")
			}
			var icon datatype.ItemIconIconInfo
			if json.Unmarshal([]byte(item.IconJson), &icon) != nil {
				return nil, errors.New("项目图片信息无效")
			}
			if icon.ItemType == 2 && strings.HasPrefix(icon.Src, "/sunpanel/uploads/") {
				rel := strings.TrimPrefix(icon.Src, "/sunpanel/uploads/")
				if path.Clean(rel) != rel || strings.ContainsAny(rel, "\\\x00") || strings.HasPrefix(rel, "../") {
					return nil, errors.New("本地图片路径无效")
				}
				uploads, err := os.OpenRoot(root)
				if err != nil {
					return nil, errors.New("本地图片目录不可读")
				}
				file, err := uploads.Open(rel)
				uploads.Close()
				if err != nil {
					return nil, errors.New("本地图片缺失或不可读")
				}
				data, err := io.ReadAll(io.LimitReader(file, 10<<20+1))
				file.Close()
				if err != nil || !portableImageValid(rel, data) {
					return nil, errors.New("本地图片无效或超过 10 MiB")
				}
				sum := sha256.Sum256(data)
				digest := hex.EncodeToString(sum[:])
				name := "images/" + digest + strings.ToLower(path.Ext(rel))
				if images[name] == nil {
					total += len(data)
					if total > portableLimit {
						return nil, errors.New("首页导出超过 64 MiB")
					}
					images[name] = data
					manifest.Images[name] = digest
				}
				icon.Src = name
			}
			out.Items = append(out.Items, portableItem{item.Title, item.Url, item.LanUrl, item.Description, item.OpenMethod, item.Sort, icon, item.GateHome})
		}
		manifest.Groups = append(manifest.Groups, out)
	}
	metadata, err := json.Marshal(manifest)
	if err != nil || total+len(metadata) > portableLimit {
		return nil, errors.New("首页导出超过 64 MiB")
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("manifest.json")
	if err != nil {
		return nil, err
	}
	if _, err = entry.Write(metadata); err != nil {
		return nil, err
	}
	for name, data := range images {
		entry, err := writer.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = entry.Write(data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func readPortable(data []byte) (portableManifest, map[string][]byte, error) {
	var manifest portableManifest
	images := map[string][]byte{}
	invalid := errors.New("首页 ZIP 无效、损坏或超过容量限制")
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(data) > portableLimit+1<<20 || len(reader.File) > 1001 {
		return manifest, images, invalid
	}
	files := map[string][]byte{}
	var total uint64
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || file.Mode()&os.ModeSymlink != 0 || path.Clean(file.Name) != file.Name || strings.ContainsAny(file.Name, "\\\x00") || files[file.Name] != nil {
			return manifest, images, invalid
		}
		total += file.UncompressedSize64
		if total > portableLimit {
			return manifest, images, invalid
		}
		opened, err := file.Open()
		if err != nil {
			return manifest, images, invalid
		}
		contents, err := io.ReadAll(io.LimitReader(opened, portableLimit+1))
		opened.Close()
		if err != nil || uint64(len(contents)) != file.UncompressedSize64 {
			return manifest, images, invalid
		}
		files[file.Name] = contents
	}
	if json.Unmarshal(files["manifest.json"], &manifest) != nil || manifest.Format != "gatehome-sunpanel-v1" || len(manifest.Groups) > 100 || len(manifest.Images)+1 != len(files) {
		return manifest, images, invalid
	}
	used := map[string]bool{}
	count := 0
	for _, group := range manifest.Groups {
		if strings.TrimSpace(group.Title) == "" || len([]rune(group.Title)) > 100 {
			return manifest, images, invalid
		}
		for _, item := range group.Items {
			count++
			if count > 1000 || strings.TrimSpace(item.Title) == "" || len([]rune(item.Title)) > 100 || !validSite(item.URL, false) || !validSite(item.LanURL, true) || item.OpenMethod < 1 || item.OpenMethod > 3 || (item.GateHome != nil && !validLink(item.GateHome)) {
				return manifest, images, invalid
			}
			if item.Icon.ItemType == 2 && strings.HasPrefix(item.Icon.Src, "images/") {
				name := item.Icon.Src
				digest := manifest.Images[name]
				sum := sha256.Sum256(files[name])
				if len(digest) != 64 || name != "images/"+digest+path.Ext(name) || digest != hex.EncodeToString(sum[:]) || !portableImageValid(name, files[name]) {
					return manifest, images, invalid
				}
				used[name] = true
				images[name] = files[name]
			} else if item.Icon.ItemType == 2 && strings.HasPrefix(item.Icon.Src, "/sunpanel/uploads/") {
				return manifest, images, invalid
			}
		}
	}
	if len(used) != len(manifest.Images) {
		return manifest, images, invalid
	}
	return manifest, images, nil
}

func importPortable(db *gorm.DB, userID uint, root string, manifest portableManifest, images map[string][]byte) error {
	// A unique directory lets a failed transaction remove only this import's files.
	rel := "portable/" + rand.Text()
	uploads, err := os.OpenRoot(root)
	if err != nil {
		return errors.New("图片目录不可写")
	}
	defer uploads.Close()
	if err = uploads.MkdirAll(rel, 0700); err != nil {
		return errors.New("图片目录不可写")
	}
	committed := false
	defer func() {
		if !committed {
			uploads.RemoveAll(rel)
		}
	}()
	for name, data := range images {
		file, err := uploads.OpenFile(rel+"/"+path.Base(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return errors.New("图片保存失败")
		}
		_, err = file.Write(data)
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			return errors.New("图片保存失败")
		}
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		for name := range images {
			src := filepath.ToSlash(filepath.Join(root, rel, path.Base(name)))
			if !strings.HasPrefix(src, "/") && !strings.HasPrefix(src, "./") {
				src = "./" + src
			}
			if err := tx.Create(&models.File{UserId: userID, Src: src, Ext: path.Ext(name), FileName: path.Base(name)}).Error; err != nil {
				return err
			}
		}
		for _, group := range manifest.Groups {
			created := models.ItemIconGroup{Title: group.Title, Icon: group.Icon, Description: group.Description, Sort: group.Sort, UserId: userID}
			if err := tx.Create(&created).Error; err != nil {
				return err
			}
			for _, item := range group.Items {
				if images[item.Icon.Src] != nil {
					item.Icon.Src = "/sunpanel/uploads/" + rel + "/" + path.Base(item.Icon.Src)
				}
				icon, err := json.Marshal(item.Icon)
				if err != nil {
					return err
				}
				createdItem := models.ItemIcon{Title: item.Title, Url: item.URL, LanUrl: item.LanURL, Description: item.Description, OpenMethod: item.OpenMethod, Sort: item.Sort, IconJson: string(icon), GateHome: item.GateHome, ItemIconGroupId: int(created.ID), UserId: userID}
				if err := tx.Create(&createdItem).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return errors.New("首页导入失败，未保存本次分组和图片")
	}
	committed = true
	return nil
}

func (a *ItemIcon) PortableExport(c *gin.Context) {
	user, _ := base.GetCurrentUserInfo(c)
	data, err := buildPortable(global.Db, user.ID, global.Config.GetValueString("base", "source_path"))
	if err != nil {
		apiReturn.Error(c, err.Error())
		return
	}
	c.Header("Content-Disposition", `attachment; filename="SunPanel-`+time.Now().UTC().Format("20060102-150405")+`.zip"`)
	c.Data(http.StatusOK, "application/zip", data)
}

func portableUpload(c *gin.Context) (portableManifest, map[string][]byte, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, portableLimit+2<<20)
	file, err := c.FormFile("file")
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if err != nil {
		return portableManifest{}, nil, errors.New("请选择不超过 65 MiB 的首页 ZIP")
	}
	opened, err := file.Open()
	if err != nil {
		return portableManifest{}, nil, errors.New("ZIP 读取失败")
	}
	defer opened.Close()
	data, err := io.ReadAll(io.LimitReader(opened, portableLimit+1<<20+1))
	if err != nil {
		return portableManifest{}, nil, errors.New("ZIP 读取失败")
	}
	return readPortable(data)
}

func (a *ItemIcon) PortableInspect(c *gin.Context) {
	manifest, images, err := portableUpload(c)
	if err != nil {
		apiReturn.Error(c, err.Error())
		return
	}
	items, external := 0, 0
	for _, group := range manifest.Groups {
		items += len(group.Items)
		for _, item := range group.Items {
			if item.Icon.ItemType == 3 || (item.Icon.ItemType == 2 && images[item.Icon.Src] == nil && item.Icon.Src != "") {
				external++
			}
		}
	}
	apiReturn.SuccessData(c, gin.H{"groups": len(manifest.Groups), "items": items, "images": len(images), "externalImages": external, "createdAt": manifest.CreatedAt})
}

func (a *ItemIcon) PortableImport(c *gin.Context) {
	user, _ := base.GetCurrentUserInfo(c)
	manifest, images, err := portableUpload(c)
	if err == nil {
		err = importPortable(global.Db, user.ID, global.Config.GetValueString("base", "source_path"), manifest, images)
	}
	if err != nil {
		apiReturn.Error(c, err.Error())
		return
	}
	apiReturn.Success(c)
}
