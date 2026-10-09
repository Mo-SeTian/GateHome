package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/draw"
)

const maxHomepageBackground = 2 << 20

func normalizeHomepageBackground(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > maxImageUpload {
		return nil, errors.New("背景图片须小于等于 5 MiB")
	}
	c, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || c.Width < 1 || c.Height < 1 || c.Width > 4096 || c.Height > 4096 {
		return nil, errors.New("背景支持 PNG、JPEG、GIF、WebP，尺寸不超过 4096 × 4096")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("背景图片无法解码")
	}
	w, h := c.Width, c.Height
	if max(w, h) > 2560 {
		w, h = max(1, w*2560/max(c.Width, c.Height)), max(1, h*2560/max(c.Width, c.Height))
	}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(canvas, canvas.Bounds(), decoded, decoded.Bounds(), draw.Src, nil)
	var out bytes.Buffer
	if jpeg.Encode(&out, canvas, &jpeg.Options{Quality: 85}) != nil || out.Len() > maxHomepageBackground {
		return nil, errors.New("背景处理后超过 2 MiB，请缩小图片")
	}
	return out.Bytes(), nil
}

func validHomepageBackground(id string, data []byte) bool {
	if !routeImageID.MatchString(id) || len(data) == 0 || len(data) > maxHomepageBackground {
		return false
	}
	sum := sha256.Sum256(data)
	c, format, err := image.DecodeConfig(bytes.NewReader(data))
	return hex.EncodeToString(sum[:]) == id && err == nil && format == "jpeg" && c.Width > 0 && c.Height > 0 && c.Width <= 2560 && c.Height <= 2560
}

func readHomepageBackground(dir, id string) ([]byte, error) {
	if !routeImageID.MatchString(id) {
		return nil, errors.New("背景引用无效")
	}
	parent := filepath.Join(dir, "homepage-backgrounds")
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("背景目录无效")
	}
	path := filepath.Join(parent, id+".jpg")
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxHomepageBackground {
		return nil, errors.New("背景图片不存在")
	}
	data, err := os.ReadFile(path)
	if err != nil || !validHomepageBackground(id, data) {
		return nil, errors.New("背景图片损坏")
	}
	return data, nil
}

func storeHomepageBackground(dir string, data []byte) (string, error) {
	data, err := normalizeHomepageBackground(data)
	if err != nil {
		return "", err
	}
	parent := filepath.Join(dir, "homepage-backgrounds")
	if os.MkdirAll(parent, 0700) != nil {
		return "", errors.New("背景目录无法创建")
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("背景目录无效")
	}
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])
	if atomicWrite(filepath.Join(parent, id+".jpg"), data) != nil {
		return "", errors.New("背景图片保存失败")
	}
	return id, nil
}

func pruneHomepageBackgrounds(dir, keep string) {
	parent := filepath.Join(dir, "homepage-backgrounds")
	entries, _ := os.ReadDir(parent)
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".jpg")
		if !strings.HasSuffix(e.Name(), ".jpg") || !routeImageID.MatchString(id) || id == keep {
			continue
		}
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() && info.ModTime().Before(time.Now().Add(-24*time.Hour)) {
			os.Remove(filepath.Join(parent, e.Name()))
		}
	}
}

func (a *Admin) homepageBackgroundRoutes(mux *http.ServeMux) {
	respond := func(w http.ResponseWriter, data []byte) {
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		if a.maintenance.Busy() {
			apiError(w, 409, "正在执行维护操作")
			return
		}
		id, err := storeHomepageBackground(a.store.paths.Data, data)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		pruneHomepageBackgrounds(a.store.paths.Data, a.store.Snapshot().Config.Homepage.Background)
		jsonResponse(w, 201, map[string]string{"id": id, "url": "/api/homepage-backgrounds/" + id})
	}
	mux.HandleFunc("POST /api/homepage-backgrounds/upload", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxImageUpload+(64<<10))
		reader, err := r.MultipartReader()
		if err != nil {
			apiError(w, 400, "请选择背景图片")
			return
		}
		part, err := reader.NextPart()
		if err != nil || part.FormName() != "file" {
			apiError(w, 400, "请选择背景图片")
			return
		}
		data, err := io.ReadAll(io.LimitReader(part, maxImageUpload+1))
		part.Close()
		if err != nil || len(data) > maxImageUpload {
			apiError(w, 400, "背景图片超过 5 MiB 或读取失败")
			return
		}
		if _, err = reader.NextPart(); err != io.EOF {
			apiError(w, 400, "一次只能上传一张背景")
			return
		}
		respond(w, data)
	}))
	mux.HandleFunc("POST /api/homepage-backgrounds/import", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			URL string `json:"url"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		data, err := downloadRouteImage(r.Context(), input.URL)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		respond(w, data)
	}))
	mux.HandleFunc("GET /api/homepage-backgrounds/{id}", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		data, err := readHomepageBackground(a.store.paths.Data, r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(data)
	}))
}
