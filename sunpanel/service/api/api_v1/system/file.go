package system

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sun-panel/api/api_v1/common/apiData/commonApiStructs"
	"sun-panel/api/api_v1/common/apiReturn"
	"sun-panel/api/api_v1/common/base"
	"sun-panel/global"
	"sun-panel/lib/cmn"
	"sun-panel/models"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type FileApi struct{}

func (a *FileApi) UploadImg(c *gin.Context) {
	userInfo, _ := base.GetCurrentUserInfo(c)
	configUpload := global.Config.GetValueString("base", "source_path")
	f, err := c.FormFile("imgfile")
	if err != nil {
		apiReturn.ErrorByCode(c, 1300)
		return
	} else {
		fileExt := strings.ToLower(path.Ext(f.Filename))
		agreeExts := []string{
			".png",
			".jpg",
			".gif",
			".jpeg",
			".webp",
			".svg",
			".ico",
		}

		if !cmn.InArray(agreeExts, fileExt) {
			apiReturn.ErrorByCode(c, 1301)
			return
		}
		fileName := cmn.Md5(fmt.Sprintf("%s%s", f.Filename, time.Now().String()))
		fildDir := fmt.Sprintf("%s/%d/%d/%d/", configUpload, time.Now().Year(), time.Now().Month(), time.Now().Day())
		if err := os.MkdirAll(fildDir, os.ModePerm); err != nil {
			apiReturn.ErrorByCode(c, 1300)
			return
		}
		filepath := fmt.Sprintf("%s%s%s", fildDir, fileName, fileExt)
		if err := c.SaveUploadedFile(f, filepath); err != nil {
			os.Remove(filepath)
			apiReturn.ErrorByCode(c, 1300)
			return
		}

		// 像数据库添加记录
		mFile := models.File{}
		if _, err := mFile.AddFile(userInfo.ID, f.Filename, fileExt, filepath); err != nil {
			os.Remove(filepath)
			apiReturn.ErrorByCode(c, 1200)
			return
		}
		apiReturn.SuccessData(c, gin.H{
			"imageUrl": "/sunpanel" + filepath[1:],
		})
	}
}

func (a *FileApi) UploadFiles(c *gin.Context) {
	userInfo, _ := base.GetCurrentUserInfo(c)
	configUpload := global.Config.GetValueString("base", "source_path")

	form, err := c.MultipartForm()
	if err != nil {
		apiReturn.ErrorByCode(c, 1300)
		return
	}
	files := form.File["files[]"]
	errFiles := []string{}
	succMap := map[string]string{}
	for _, f := range files {
		fileExt := strings.ToLower(path.Ext(f.Filename))
		fileName := cmn.Md5(fmt.Sprintf("%s%s", f.Filename, time.Now().String()))
		fildDir := fmt.Sprintf("%s/%d/%d/%d/", configUpload, time.Now().Year(), time.Now().Month(), time.Now().Day())
		isExist, _ := cmn.PathExists(fildDir)
		if !isExist {
			os.MkdirAll(fildDir, os.ModePerm)
		}
		filepath := fmt.Sprintf("%s%s%s", fildDir, fileName, fileExt)
		if c.SaveUploadedFile(f, filepath) != nil {
			errFiles = append(errFiles, f.Filename)
		} else {
			// 成功
			// 像数据库添加记录
			mFile := models.File{}
			mFile.AddFile(userInfo.ID, f.Filename, fileExt, filepath)
			succMap[f.Filename] = "/sunpanel" + filepath[1:]
		}
	}

	apiReturn.SuccessData(c, gin.H{
		"succMap":  succMap,
		"errFiles": errFiles,
	})
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
