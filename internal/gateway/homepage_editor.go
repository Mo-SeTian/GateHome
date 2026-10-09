package gateway

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

func (a *Admin) homepageEditorDocument(w http.ResponseWriter, r *http.Request) {
	id, _ := a.homepageUser(w, r)
	document, exists := a.store.pages.snapshot(id)
	if !exists {
		apiError(w, 404, "桌面不存在")
		return
	}
	jsonResponse(w, 200, struct {
		homepageDocument
		SpaceID string `json:"space_id"`
	}{document, id})
}

func readHomepageUpload(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImageUpload+(64<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, errors.New("请选择图片文件")
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" {
		return nil, errors.New("请选择图片文件")
	}
	content, err := io.ReadAll(io.LimitReader(part, maxImageUpload+1))
	part.Close()
	if err != nil || len(content) > maxImageUpload {
		return nil, errors.New("图片须小于等于 5 MiB")
	}
	if _, err := reader.NextPart(); err != io.EOF {
		return nil, errors.New("一次只能上传一张图片")
	}
	return content, nil
}

func homepagePublicURL(c Config, route Route) string {
	group, exists := c.Group(route.GroupID)
	if !exists {
		return ""
	}
	scheme, port := "http", group.HTTPPort
	if route.TLS {
		scheme, port = "https", group.HTTPSPort
	}
	if port == 0 {
		return ""
	}
	return (&url.URL{Scheme: scheme, Host: net.JoinHostPort(route.Host, strconv.Itoa(port)), Path: "/"}).String()
}

func (a *Admin) homepageEditorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/editor/config", a.requireHomepageEditor(a.homepageEditorDocument))
	mux.HandleFunc("PUT /api/editor/config", a.requireHomepageEditor(func(w http.ResponseWriter, r *http.Request) {
		var input homepageDocument
		if !decodeBody(w, r, &input) {
			return
		}
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		id, authenticated := a.homepageUser(w, r)
		if !authenticated || a.maintenance.Busy() {
			apiError(w, 409, "登录已失效或正在执行维护，请重新登录后重试")
			return
		}
		if _, err := a.store.pages.update(id, input.Homepage, input.Revision); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		a.homepageEditorDocument(w, r)
	}))
	for _, background := range []bool{false, true} {
		kind := "images"
		if background {
			kind = "backgrounds"
		}
		respond := func(w http.ResponseWriter, r *http.Request, content []byte) {
			a.updateMu.Lock()
			defer a.updateMu.Unlock()
			owner, authenticated := a.homepageUser(w, r)
			if !authenticated || a.maintenance.Busy() {
				apiError(w, 409, "登录已失效或正在执行维护")
				return
			}
			dir, err := homepageSpaceDirectory(a.store.paths.Data, owner)
			if err != nil {
				apiError(w, 400, err.Error())
				return
			}
			document, _ := a.store.pages.snapshot(owner)
			pruneRouteImages(dir, Config{Homepage: document.Homepage})
			pruneHomepageBackgrounds(dir, document.Homepage.Background)
			folder, limit := "route-images", 512
			if background {
				folder, limit = "homepage-backgrounds", 8
			}
			entries, _ := os.ReadDir(filepath.Join(dir, folder))
			if len(entries) >= limit {
				apiError(w, 400, "此桌面的图片缓存已满，请先保存已选图片并清理闲置图片")
				return
			}
			var id string
			if background {
				id, err = storeHomepageBackground(dir, content)
			} else {
				id, err = storeRouteImage(dir, content)
			}
			if err != nil {
				apiError(w, 400, err.Error())
				return
			}
			jsonResponse(w, 201, map[string]string{"id": id, "url": "/api/editor/" + kind + "/" + id})
		}
		mux.HandleFunc("POST /api/editor/"+kind+"/upload", a.requireHomepageEditor(func(w http.ResponseWriter, r *http.Request) {
			content, err := readHomepageUpload(w, r)
			if err != nil {
				apiError(w, 400, err.Error())
				return
			}
			respond(w, r, content)
		}))
		mux.HandleFunc("POST /api/editor/"+kind+"/import", a.requireHomepageEditor(func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				URL string `json:"url"`
			}
			if !decodeBody(w, r, &input) {
				return
			}
			content, err := downloadRouteImage(r.Context(), input.URL)
			if err != nil {
				apiError(w, 400, err.Error())
				return
			}
			respond(w, r, content)
		}))
		mux.HandleFunc("GET /api/editor/"+kind+"/{id}", a.requireHomepageEditor(func(w http.ResponseWriter, r *http.Request) {
			owner, _ := a.homepageUser(w, r)
			dir, err := homepageSpaceDirectory(a.store.paths.Data, owner)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			var content []byte
			if background {
				content, err = readHomepageBackground(dir, r.PathValue("id"))
			} else {
				content, err = readRouteImage(dir, r.PathValue("id"))
			}
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", http.DetectContentType(content))
			w.Write(content)
		}))
	}
	mux.HandleFunc("GET /api/editor/importable", a.requireHomepageEditor(func(w http.ResponseWriter, r *http.Request) {
		c := a.store.Snapshot().Config
		type service struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			LAN   string `json:"lan"`
			WAN   string `json:"wan"`
			Image string `json:"image"`
		}
		services := []service{}
		for _, route := range c.Routes {
			services = append(services, service{routeKey(route), route.Name, route.Upstream, homepagePublicURL(c, route), route.Image})
		}
		jsonResponse(w, 200, map[string]any{"services": services})
	}))
	mux.HandleFunc("GET /api/editor/source-images/{id}", a.requireHomepageEditor(func(w http.ResponseWriter, r *http.Request) {
		id, selected := r.PathValue("id"), false
		for _, route := range a.store.Snapshot().Config.Routes {
			selected = selected || (id != "" && route.Image == id)
		}
		if !selected {
			http.NotFound(w, r)
			return
		}
		content, err := readRouteImage(a.store.paths.Data, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", http.DetectContentType(content))
		w.Write(content)
	}))
	mux.HandleFunc("POST /api/editor/import", a.requireHomepageEditor(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			PageID   string   `json:"page_id"`
			Services []string `json:"services"`
			Revision int      `json:"revision"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		if len(input.Services) < 1 || len(input.Services) > 200 {
			apiError(w, 400, "请选择 1–200 个反代服务")
			return
		}
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		owner, authenticated := a.homepageUser(w, r)
		if !authenticated || a.maintenance.Busy() {
			apiError(w, 409, "登录已失效或正在执行维护")
			return
		}
		document, _ := a.store.pages.snapshot(owner)
		if document.Revision != input.Revision {
			apiError(w, 409, "桌面已变化，请刷新后重试")
			return
		}
		home := cloneHomepage(document.Homepage)
		var target *HomepagePage
		for i := range home.Groups {
			for j := range home.Groups[i].Pages {
				if home.Groups[i].Pages[j].ID == input.PageID {
					target = &home.Groups[i].Pages[j]
				}
			}
		}
		if target == nil {
			apiError(w, 400, "请选择自己桌面的导入页面")
			return
		}
		c := a.store.Snapshot().Config
		dir, err := homepageSpaceDirectory(a.store.paths.Data, owner)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		for _, key := range input.Services {
			var source *Route
			for i := range c.Routes {
				if routeKey(c.Routes[i]) == key {
					source = &c.Routes[i]
				}
			}
			if source == nil {
				apiError(w, 400, "反代服务已变化，请重新选择")
				return
			}
			wan, duplicate := homepagePublicURL(c, *source), false
			for _, link := range target.Links {
				duplicate = duplicate || (link.LAN == source.Upstream && link.WAN == wan)
			}
			if duplicate {
				continue
			}
			id, err := stageID()
			if err != nil {
				apiError(w, 500, "无法创建应用链接")
				return
			}
			if source.Image != "" {
				content, err := readRouteImage(a.store.paths.Data, source.Image)
				if err != nil || storeHomepageAsset(dir, "route-images", source.Image+".img", content) != nil {
					apiError(w, 400, "反代图标无法复制，请检查图片后重试")
					return
				}
			}
			target.Links = append(target.Links, HomepageLink{ID: id, Name: source.Name, LAN: source.Upstream, WAN: wan, Image: source.Image})
		}
		if _, err := a.store.pages.update(owner, home, input.Revision); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		a.homepageEditorDocument(w, r)
	}))
}
