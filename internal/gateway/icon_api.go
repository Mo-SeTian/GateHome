package gateway

import (
	"bytes"
	"errors"
	"image"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxIconBatch = 16

func (a *Admin) iconRoutes(mux *http.ServeMux) {
	library := &a.icons
	data := a.store.paths.Data
	library.once.Do(func() {
		library.mu.Lock()
		defer library.mu.Unlock()
		_ = library.reindex(data)
	})

	mux.HandleFunc("GET /api/icons", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		keyword := query.Get("keyword")
		if len(keyword) > 256 {
			apiError(w, 400, "搜索词过长")
			return
		}
		page, _ := strconv.Atoi(query.Get("page"))
		limit, err := strconv.Atoi(query.Get("limit"))
		if err != nil || limit <= 0 {
			limit = 72
		}
		page, limit = min(max(page, 1), 1_000_000), min(limit, 120)
		library.mu.RLock()
		index := library.index
		library.mu.RUnlock()
		if index == nil {
			apiError(w, 503, "图标库暂不可用，请检查数据目录后重建索引")
			return
		}
		items, total := index.search(keyword, query.Get("category"), query.Get("source"), query.Get("style"), page, limit)
		jsonResponse(w, 200, map[string]any{"items": items, "total": total, "page": page, "limit": limit})
	}))
	mux.HandleFunc("GET /api/icons/stats", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		library.mu.RLock()
		index := library.index
		library.mu.RUnlock()
		if index == nil {
			apiError(w, 503, "图标库暂不可用，请检查数据目录后重建索引")
			return
		}
		jsonResponse(w, 200, index.Stats)
	}))
	mux.HandleFunc("POST /api/icons/reindex", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if !a.lockIconWrite(w) {
			return
		}
		defer a.unlockIconWrite()
		if err := library.reindex(data); err != nil {
			apiError(w, 500, "重建图标索引失败，请检查数据目录")
			return
		}
		jsonResponse(w, 200, library.index.Stats)
	}))
	mux.HandleFunc("POST /api/icons/upload", a.requireAuth(a.uploadIcons))
	mux.HandleFunc("DELETE /api/icons/{id...}", a.requireAuth(a.deleteIcon))
	mux.HandleFunc("GET /api/icons/file/{path...}", a.requireAuth(a.serveLibraryIcon))
	mux.HandleFunc("POST /api/icons/import-github", a.requireAuth(a.startIconImport))
	mux.HandleFunc("GET /api/icons/import-status", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		library.jobMu.Lock()
		status := library.job
		library.jobMu.Unlock()
		if status.State == "" {
			status.State = "idle"
		}
		jsonResponse(w, 200, status)
	}))
}

func (a *Admin) lockIconWrite(w http.ResponseWriter) bool {
	a.updateMu.Lock()
	if a.maintenance.Busy() {
		a.updateMu.Unlock()
		apiError(w, 409, "正在执行维护操作")
		return false
	}
	a.icons.mu.Lock()
	return true
}

func (a *Admin) unlockIconWrite() {
	a.icons.mu.Unlock()
	a.updateMu.Unlock()
}

func (a *Admin) serveLibraryIcon(w http.ResponseWriter, r *http.Request) {
	rel := r.PathValue("path")
	a.icons.mu.RLock()
	file, info, err := openIconFile(a.store.paths.Data, rel)
	a.icons.mu.RUnlock()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	format := iconFormat(rel)
	if _, _, err := iconDimensions(file, format); err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", iconMIMEs[format])
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; sandbox")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.Header().Add("Vary", "Cookie, Origin, Sec-Fetch-Site")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (a *Admin) uploadIcons(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxIconBatch*maxImageUpload+(64<<10))
	if c := http.NewResponseController(w); c.SetReadDeadline(time.Now().Add(60*time.Second)) == nil {
		defer c.SetReadDeadline(time.Time{})
	}
	reader, err := r.MultipartReader()
	if err != nil {
		apiError(w, 400, "请选择图标文件")
		return
	}
	data := a.store.paths.Data
	stage, err := stageIcons(data, ".upload-")
	if err != nil {
		apiError(w, 500, "无法暂存图标")
		return
	}
	defer os.RemoveAll(stage)

	var names []string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil || len(names) == maxIconBatch {
			apiError(w, 400, "一次最多上传 16 张图标")
			return
		}
		name := part.FileName()
		format := iconFormat(name)
		if part.FormName() != "file" || len(name) > 240 || !safeIconPath("uploads/"+name) || strings.Contains(name, "/") {
			apiError(w, 400, "图标文件名或格式无效")
			return
		}
		content, err := io.ReadAll(io.LimitReader(part, maxImageUpload+1))
		if err != nil || len(content) == 0 || len(content) > maxImageUpload {
			apiError(w, 400, "每张图标须小于等于 5 MiB")
			return
		}
		part.Close()
		if _, _, err := iconDimensions(bytes.NewReader(content), format); err != nil {
			apiError(w, 400, "图标内容无效，或图片尺寸超过 4096 × 4096")
			return
		}
		if format != "svg" && format != "ico" {
			if _, _, err := image.Decode(bytes.NewReader(content)); err != nil {
				apiError(w, 400, "图标无法解码")
				return
			}
		}
		subdir := strconv.Itoa(len(names))
		if err := os.Mkdir(filepath.Join(stage, subdir), 0700); err != nil {
			apiError(w, 500, "无法暂存图标")
			return
		}
		if err := os.WriteFile(filepath.Join(stage, subdir, name), content, 0600); err != nil {
			apiError(w, 500, "无法暂存图标")
			return
		}
		names = append(names, subdir+"/"+name)
	}
	if len(names) == 0 {
		apiError(w, 400, "请选择图标文件")
		return
	}
	if !a.lockIconWrite(w) {
		return
	}
	defer a.unlockIconWrite()
	root, err := os.OpenRoot(data)
	if err != nil {
		apiError(w, 500, "图标数据目录不可用")
		return
	}
	defer root.Close()
	parent := "icons/uploads/" + time.Now().Format("200601")
	if _, err := plainIconPath(root, parent, true, true); err != nil {
		apiError(w, 500, "图标上传目录不可用")
		return
	}
	group := parent + "/" + strings.TrimPrefix(filepath.Base(stage), ".upload-")
	if err := root.Rename("icons/"+filepath.Base(stage), group); err != nil {
		apiError(w, 500, "图标保存失败")
		return
	}
	if err := a.icons.reindex(data); err != nil {
		_ = root.RemoveAll(group)
		apiError(w, 500, "图标索引保存失败")
		return
	}
	items := make([]libraryIcon, 0, len(names))
	for _, icon := range a.icons.index.Items {
		if strings.HasPrefix(icon.RelPath, strings.TrimPrefix(group, "icons/")+"/") {
			items = append(items, icon)
		}
	}
	jsonResponse(w, 201, map[string]any{"items": items})
}

func (a *Admin) deleteIcon(w http.ResponseWriter, r *http.Request) {
	rel := r.PathValue("id")
	if !safeIconPath(rel) {
		apiError(w, 400, "图标路径无效")
		return
	}
	if !strings.HasPrefix(rel, "uploads/") {
		apiError(w, 403, "只能删除本地上传的图标")
		return
	}
	if !a.lockIconWrite(w) {
		return
	}
	defer a.unlockIconWrite()
	for _, route := range a.store.Snapshot().Config.Routes {
		if route.Image == iconRefPrefix+rel {
			apiError(w, 409, "图标正在被反代服务使用，请先更换服务图标")
			return
		}
	}
	data := a.store.paths.Data
	root, err := os.OpenRoot(data)
	if err != nil {
		apiError(w, 500, "图标数据目录不可用")
		return
	}
	defer root.Close()
	name := "icons/" + rel
	if _, err := plainIconPath(root, name, false, false); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			apiError(w, 404, "图标不存在")
		} else {
			apiError(w, 400, "图标路径无效")
		}
		return
	}
	stage, err := stageIcons(data, ".delete-")
	if err != nil {
		apiError(w, 500, "图标删除失败")
		return
	}
	defer os.RemoveAll(stage)
	pending := "icons/" + filepath.Base(stage) + "/icon"
	if err := root.Rename(name, pending); err != nil {
		apiError(w, 500, "图标删除失败")
		return
	}
	if err := a.icons.reindex(data); err != nil {
		_ = root.Rename(pending, name)
		apiError(w, 500, "图标索引保存失败")
		return
	}
	for dir := path.Dir(name); dir != "icons/uploads"; dir = path.Dir(dir) {
		if err := root.Remove(dir); err != nil {
			break
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
