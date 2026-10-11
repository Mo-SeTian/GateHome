package system

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sun-panel/api/api_v1/common/apiData/commonApiStructs"
	"sun-panel/api/api_v1/common/apiReturn"
	"sun-panel/api/api_v1/common/base"
	"sun-panel/global"
	"sun-panel/lib/imageasset"
	"sun-panel/models"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type FileApi struct{}

var uploadWorkers = make(chan struct{}, 4)

const uploadBodyLimit = 64 << 20

func beginUpload(c *gin.Context) bool {
	select {
	case uploadWorkers <- struct{}{}:
	default:
		c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "msg": "上传任务过多，请稍后重试"})
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, uploadBodyLimit)
	if c.Request.ParseMultipartForm(2<<20) != nil {
		<-uploadWorkers
		if c.Request.MultipartForm != nil {
			c.Request.MultipartForm.RemoveAll()
		}
		apiReturn.ErrorByCode(c, 1300)
		return false
	}
	return true
}
func finishUpload(c *gin.Context) { c.Request.MultipartForm.RemoveAll(); <-uploadWorkers }
func saveUpload(f *multipart.FileHeader, userID uint, imageOnly bool) (string, error) {
	if f.Size > imageasset.Limit {
		return "", errors.New("file too large")
	}
	source, err := f.Open()
	if err != nil {
		return "", err
	}
	defer source.Close()
	data, err := io.ReadAll(io.LimitReader(source, imageasset.Limit+1))
	if err != nil || len(data) > imageasset.Limit {
		return "", errors.New("invalid file")
	}
	ext := strings.ToLower(filepath.Ext(f.Filename))
	if imageOnly && !imageasset.Validate(f.Filename, data) {
		return "", errors.New("invalid image")
	}
	now := time.Now()
	dir := filepath.Join(global.Config.GetValueString("base", "source_path"), fmt.Sprint(now.Year()), fmt.Sprint(int(now.Month())), fmt.Sprint(now.Day()))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, "upload-*"+ext)
	if err != nil {
		return "", err
	}
	name := file.Name()
	if !filepath.IsAbs(name) {
		name = "./" + filepath.ToSlash(name)
	}
	_, err = file.Write(data)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		os.Remove(name)
		return "", errors.New("file write failed")
	}
	if _, err := (&models.File{}).AddFile(userID, f.Filename, ext, name); err != nil {
		os.Remove(name)
		return "", err
	}
	return "/sunpanel" + strings.TrimPrefix(filepath.ToSlash(name), "."), nil
}
func (a *FileApi) UploadImg(c *gin.Context) {
	if !beginUpload(c) {
		return
	}
	defer finishUpload(c)
	user, _ := base.GetCurrentUserInfo(c)
	f, err := c.FormFile("imgfile")
	if err != nil {
		apiReturn.ErrorByCode(c, 1300)
		return
	}
	source, err := saveUpload(f, user.ID, true)
	if err != nil {
		apiReturn.Error(c, "图片上传失败：请检查图片格式、大小和存储权限")
		return
	}
	apiReturn.SuccessData(c, gin.H{"imageUrl": source})
}
func (a *FileApi) UploadFiles(c *gin.Context) {
	if !beginUpload(c) {
		return
	}
	defer finishUpload(c)
	user, _ := base.GetCurrentUserInfo(c)
	files := c.Request.MultipartForm.File["files[]"]
	if len(files) == 0 || len(files) > 100 {
		apiReturn.Error(c, "请选择 1–100 个文件")
		return
	}
	errFiles := []string{}
	succMap := map[string]string{}
	for _, f := range files {
		source, err := saveUpload(f, user.ID, false)
		if err != nil {
			errFiles = append(errFiles, f.Filename)
		} else {
			succMap[f.Filename] = source
		}
	}
	apiReturn.SuccessData(c, gin.H{"succMap": succMap, "errFiles": errFiles})
}

func (a *FileApi) GetList(c *gin.Context) {
	list := []models.File{}
	userInfo, _ := base.GetCurrentUserInfo(c)
	var count int64
	if err := global.Db.Order("created_at desc").Find(&list, "user_id=?", userInfo.ID).Count(&count).Error; err != nil {
		apiReturn.ErrorDatabase(c, err.Error())
		return
	}

	refs, err := assetReferences(global.Db, userInfo.ID)
	if err != nil {
		apiReturn.Error(c, "无法检查图片引用，请稍后重试")
		return
	}
	data := []map[string]interface{}{}
	for _, v := range list {
		var size int64
		if info, err := os.Stat(v.Src); err == nil {
			size = info.Size()
		}
		data = append(data, map[string]interface{}{
			"src":        "/sunpanel" + v.Src[1:],
			"fileName":   v.FileName,
			"id":         v.ID,
			"createTime": v.CreatedAt,
			"updateTime": v.UpdatedAt,
			"path":       v.Src,
			"references": refs[assetKey(v.Src)],
			"size":       size,
		})
	}
	apiReturn.SuccessListData(c, data, count)
}

func (a *FileApi) Deletes(c *gin.Context) {
	req := commonApiStructs.RequestDeleteIds[uint]{}
	user, _ := base.GetCurrentUserInfo(c)
	if c.ShouldBindJSON(&req) != nil || len(req.Ids) == 0 || len(req.Ids) > 100 {
		apiReturn.Error(c, "请选择 1–100 个文件")
		return
	}
	type movedFile struct{ source, staged string }
	moved := []movedFile{}
	err := global.Db.Transaction(func(tx *gorm.DB) error {
		var files []models.File
		if err := tx.Where("user_id=? AND id in ?", user.ID, req.Ids).Find(&files).Error; err != nil {
			return err
		}
		if len(files) != len(req.Ids) {
			return errors.New("missing file")
		}
		// Acquire the SQLite write transaction before checking live references.
		if err := tx.Delete(&files).Error; err != nil {
			return err
		}
		refs, err := assetReferences(tx, user.ID)
		if err != nil {
			return err
		}
		for _, file := range files {
			if len(refs[assetKey(file.Src)]) != 0 {
				return errors.New("referenced file")
			}
			source, err := checkedAssetPath(file.Src)
			if err != nil {
				return err
			}
			if _, err := os.Lstat(source); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return err
			}
			stage, err := os.CreateTemp(filepath.Dir(source), ".asset-delete-")
			if err != nil {
				return err
			}
			stage.Close()
			if err := os.Rename(source, stage.Name()); err != nil {
				os.Remove(stage.Name())
				return err
			}
			moved = append(moved, movedFile{source, stage.Name()})
		}
		return nil
	})
	if err != nil {
		for i := len(moved) - 1; i >= 0; i-- {
			os.Rename(moved[i].staged, moved[i].source)
		}
		apiReturn.Error(c, "删除未完成：图片仍被使用、文件已变化或存储不可写，请刷新后重试")
		return
	}
	for _, file := range moved {
		if os.Remove(file.staged) != nil {
			apiReturn.Error(c, "文件引用已移除，但临时文件清理失败，请检查存储目录权限")
			return
		}
	}
	apiReturn.Success(c)
}
