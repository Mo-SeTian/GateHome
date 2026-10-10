package system

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sun-panel/global"
	"sun-panel/lib/iniConfig"
	"sun-panel/models"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gopkg.in/ini.v1"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUploadImagePersistence(t *testing.T) {
	for _, failure := range []string{"", "directory", "write", "database"} {
		t.Run("failure="+failure, func(t *testing.T) {
			root := t.TempDir()
			db, err := gorm.Open(sqlite.Open(filepath.Join(root, "database.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil || db.AutoMigrate(&models.File{}) != nil {
				t.Fatal("could not initialize upload fixture")
			}
			previousConfig, previousGlobalDB, previousModelDB := global.Config, global.Db, models.Db
			global.Config = &iniConfig.IniConfig{Config: ini.Empty()}
			global.Db, models.Db = db, db
			uploads := filepath.Join(root, "uploads")
			global.Config.Config.Section("base").Key("source_path").SetValue(uploads)
			t.Cleanup(func() {
				global.Config, global.Db, models.Db = previousConfig, previousGlobalDB, previousModelDB
				sqlDB, _ := db.DB()
				sqlDB.Close()
			})
			if failure == "directory" {
				if err := os.WriteFile(uploads, []byte("TEST_ONLY_BLOCKED_DIRECTORY"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "database" {
				db.Callback().Create().Before("gorm:create").Register("test:reject-upload", func(tx *gorm.DB) {
					tx.AddError(errors.New("TEST_ONLY_DATABASE_FAILURE"))
				})
			}
			if failure == "write" {
				if os.Geteuid() == 0 {
					t.Skip("root bypasses directory write permissions")
				}
				now := time.Now()
				day := filepath.Join(uploads, fmt.Sprint(now.Year()), fmt.Sprint(int(now.Month())), fmt.Sprint(now.Day()))
				if os.MkdirAll(day, 0755) != nil || os.Chmod(day, 0555) != nil {
					t.Fatal("could not create read-only upload fixture")
				}
				t.Cleanup(func() { os.Chmod(day, 0755) })
			}
			var imageData, body bytes.Buffer
			png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 1, 1)))
			writer := multipart.NewWriter(&body)
			part, _ := writer.CreateFormFile("imgfile", "test-icon.png")
			part.Write(imageData.Bytes())
			writer.Close()
			router := gin.New()
			router.POST("/upload", func(c *gin.Context) {
				c.Set("userInfo", models.User{BaseModel: models.BaseModel{ID: 1}})
				(&FileApi{}).UploadImg(c)
			})
			request := httptest.NewRequest("POST", "/upload", &body)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			var result struct {
				Code int `json:"code"`
				Data struct {
					ImageURL string `json:"imageUrl"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal("invalid upload response")
			}
			var count int64
			db.Model(&models.File{}).Count(&count)
			files, _ := filepath.Glob(filepath.Join(uploads, "*", "*", "*", "*.png"))
			if failure != "" {
				if result.Code == 0 || result.Data.ImageURL != "" || count != 0 || len(files) != 0 {
					t.Fatal("failed upload reported success or left a file/database record")
				}
				return
			}
			if result.Code != 0 || result.Data.ImageURL == "" || count != 1 || len(files) != 1 {
				t.Fatal("successful upload was not persisted")
			}
			stored, err := os.ReadFile(files[0])
			if err != nil || !bytes.Equal(stored, imageData.Bytes()) {
				t.Fatal("persisted image differs from uploaded content")
			}
		})
	}
}
