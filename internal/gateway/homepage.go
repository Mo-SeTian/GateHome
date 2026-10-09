package gateway

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type HomepageLink struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	LAN         string `json:"lan"`
	WAN         string `json:"wan"`
	Image       string `json:"image"`
	Favorite    bool   `json:"favorite"`
}

type HomepagePage struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Rows          int            `json:"rows"`
	Columns       int            `json:"columns"`
	MobileColumns int            `json:"mobile_columns"`
	Links         []HomepageLink `json:"links"`
}

type HomepageGroup struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Pages []HomepagePage `json:"pages"`
}

type HomepageSearchEngine struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type HomepageConfig struct {
	Enabled       bool                   `json:"enabled"`
	Port          int                    `json:"port"`
	Public        bool                   `json:"public"`
	Title         string                 `json:"title"`
	Tone          string                 `json:"tone"`
	Background    string                 `json:"background"`
	Shade         int                    `json:"shade"`
	Compact       bool                   `json:"compact"`
	ShowAddresses bool                   `json:"show_addresses"`
	CustomCSS     string                 `json:"custom_css"`
	SearchEngines []HomepageSearchEngine `json:"search_engines"`
	Groups        []HomepageGroup        `json:"groups"`
}

func defaultHomepage() HomepageConfig {
	return HomepageConfig{Port: 16680, Public: true, Title: "我的数字空间", Tone: "forest", Shade: 50, Groups: []HomepageGroup{}, SearchEngines: []HomepageSearchEngine{
		{ID: "baidu", Name: "百度", URL: "https://www.baidu.com/s?wd={query}"},
		{ID: "google", Name: "Google", URL: "https://www.google.com/search?q={query}"},
	}}
}

func homepageImages(c HomepageConfig) map[string]bool {
	images := map[string]bool{}
	for _, g := range c.Groups {
		for _, p := range g.Pages {
			for _, l := range p.Links {
				if l.Image != "" {
					images[l.Image] = true
				}
			}
		}
	}
	return images
}

// Older launchers snapshot only proxy-referenced icons. Their rollback would
// remove homepage-only icons, and they cannot restore the new background path.
func (m *Maintenance) checkHomepageFiles(incoming *HomepageConfig) error {
	if m.homepageFiles {
		return nil
	}
	var current struct {
		Config struct {
			Homepage HomepageConfig `json:"homepage"`
		} `json:"config"`
	}
	data, err := os.ReadFile(m.paths.file("state.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("当前首页配置无法检查")
	}
	if err == nil && json.Unmarshal(data, &current) != nil {
		return errors.New("当前首页配置无效")
	}
	needsFiles := func(c HomepageConfig) bool { return c.Background != "" || len(homepageImages(c)) > 0 }
	if needsFiles(current.Config.Homepage) || (incoming != nil && needsFiles(*incoming)) {
		return errors.New("旧启动器不支持首页图片的完整恢复与回滚，请使用最新安装脚本更新启动器（保留 config、log、data），Docker 请保留目录后重建镜像")
	}
	return nil
}

func validateHomepage(c HomepageConfig, groups []ProxyGroup) error {
	// Zero-valued configurations from older backups remain compatible.
	if c.Port == 0 && !c.Enabled && len(c.Groups) == 0 && c.CustomCSS == "" && c.Background == "" && c.SearchEngines == nil {
		return nil
	}
	if c.Port < 1024 || c.Port > 65535 {
		return errors.New("首页端口须为 1024–65535")
	}
	for _, g := range groups {
		if c.Enabled && (c.Port == g.HTTPPort || c.Port == g.HTTPSPort) {
			return errors.New("首页端口不能与反代监听端口相同")
		}
	}
	if strings.TrimSpace(c.Title) == "" || len(c.Title) > 160 || c.Shade < 0 || c.Shade > 90 || (c.Tone != "forest" && c.Tone != "dusk" && c.Tone != "midnight") || len(c.CustomCSS) > 32768 {
		return errors.New("首页标题、背景设置或 CSS 无效（CSS 最多 32 KiB）")
	}
	if c.Background != "" && !routeImageID.MatchString(c.Background) {
		return errors.New("首页背景图片引用无效")
	}
	if len(c.SearchEngines) < 1 || len(c.SearchEngines) > 12 {
		return errors.New("首页须设置 1–12 个搜索引擎")
	}
	engineIDs := map[string]bool{}
	for _, e := range c.SearchEngines {
		u, err := imageURL(e.URL)
		if !idPattern.MatchString(e.ID) || engineIDs[e.ID] || strings.TrimSpace(e.Name) == "" || len(e.Name) > 64 || err != nil || strings.ContainsAny(e.URL, "\r\n\x00") || strings.Count(e.URL, "{query}") != 1 || (!strings.Contains(u.Path, "{query}") && !strings.Contains(u.RawQuery, "{query}")) {
			return errors.New("搜索引擎名称或地址无效：须使用不含账号密码的 HTTP / HTTPS 地址，并在路径或查询参数中填写一次 {query}")
		}
		engineIDs[e.ID] = true
	}
	if len(c.Groups) > 30 {
		return errors.New("首页最多支持 30 个分组")
	}
	ids, count := map[string]bool{}, 0
	validID := func(id string) bool {
		if !idPattern.MatchString(id) || ids[id] {
			return false
		}
		ids[id] = true
		return true
	}
	for _, g := range c.Groups {
		if !validID(g.ID) || strings.TrimSpace(g.Name) == "" || len(g.Name) > 100 || len(g.Pages) < 1 || len(g.Pages) > 30 {
			return errors.New("首页分组名称、ID 或页面数量无效，每组须有 1–30 个页面")
		}
		for _, p := range g.Pages {
			if !validID(p.ID) || strings.TrimSpace(p.Name) == "" || len(p.Name) > 100 || p.Rows < 1 || p.Rows > 8 || p.Columns < 1 || p.Columns > 8 || p.MobileColumns < 1 || p.MobileColumns > 3 {
				return errors.New("首页页面须设置 1–8 行、1–8 列，手机端 1–3 列")
			}
			for _, l := range p.Links {
				count++
				if count > 200 || !validID(l.ID) || strings.TrimSpace(l.Name) == "" || len(l.Name) > 100 || len(l.Description) > 240 || (l.LAN == "" && l.WAN == "") {
					return errors.New("首页链接名称、ID 或地址无效，最多 200 个链接")
				}
				for _, raw := range []string{l.LAN, l.WAN} {
					if raw != "" {
						if _, err := imageURL(raw); err != nil || strings.ContainsAny(raw, "\r\n\x00") {
							return errors.New("首页链接须为不含账号密码的 HTTP / HTTPS 地址")
						}
					}
				}
				if l.Image != "" && !routeImageID.MatchString(l.Image) {
					return errors.New("首页链接图片引用无效")
				}
			}
		}
	}
	return nil
}

func (a *Admin) homepageAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT /api/homepage", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Homepage HomepageConfig `json:"homepage"`
			Revision int            `json:"revision"`
		}
		controller := http.NewResponseController(w)
		if controller.SetReadDeadline(time.Now().Add(15*time.Second)) == nil {
			defer controller.SetReadDeadline(time.Time{})
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			apiError(w, 400, "首页配置无效或超过 1 MiB")
			return
		}
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		if a.maintenance.Busy() {
			apiError(w, 409, "正在执行维护操作")
			return
		}
		c := a.store.Snapshot().Config
		c.Homepage = input.Homepage
		if err := validateAdminPort(c, a.adminPort); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		if err := a.store.Update(c, nil, input.Revision); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		pruneRouteImages(a.store.paths.Data, c)
		pruneHomepageBackgrounds(a.store.paths.Data, c.Homepage.Background)
		a.getConfig(w, r)
	}))
	a.homepageBackgroundRoutes(mux)
}

func homepageOriginAllowed(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	value := r.Header.Get("Origin")
	if value == "" {
		return true
	}
	u, err := url.Parse(value)
	return err == nil && u.User == nil && strings.EqualFold(u.Host, r.Host) && (u.Scheme == "http" || u.Scheme == "https") && (u.Path == "" || u.Path == "/") && u.RawQuery == "" && !u.ForceQuery && u.Fragment == ""
}

// This listener exposes only the selected homepage and its referenced assets.
// Administrative configuration and mutations are available solely on the admin listener.
func (a *Admin) HomepageHandler() http.Handler {
	mux := http.NewServeMux()
	allowed := func(r *http.Request) bool {
		if a.store.Snapshot().Config.Homepage.Public {
			return true
		}
		cookie, err := r.Cookie("gatehomepage_session")
		if err != nil {
			return false
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		return time.Now().Before(a.homepageSessions[sha256.Sum256([]byte(cookie.Value))])
	}
	protect := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !a.store.Snapshot().Config.Homepage.Public && !homepageOriginAllowed(r) {
				apiError(w, 403, "首页请求来源未获允许")
				return
			}
			if !allowed(r) {
				apiError(w, 401, "请先登录")
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /api/homepage", protect(func(w http.ResponseWriter, r *http.Request) {
		c := a.store.Snapshot().Config.Homepage
		c.CustomCSS = "" // CSS is served separately and never interpolated into HTML.
		jsonResponse(w, 200, c)
	}))
	mux.HandleFunc("GET /custom.css", protect(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		io.WriteString(w, a.store.Snapshot().Config.Homepage.CustomCSS)
	}))
	mux.HandleFunc("GET /images/{id}", protect(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !homepageImages(a.store.Snapshot().Config.Homepage)[id] {
			http.NotFound(w, r)
			return
		}
		data, err := readRouteImage(a.store.paths.Data, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", http.DetectContentType(data))
		w.Write(data)
	}))
	mux.HandleFunc("GET /background/{id}", protect(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" || id != a.store.Snapshot().Config.Homepage.Background {
			http.NotFound(w, r)
			return
		}
		data, err := readHomepageBackground(a.store.paths.Data, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(data)
	}))
	// Reuse administrator verification and throttling, with a separate cookie name.
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		u, _ := url.Parse(r.Header.Get("Origin"))
		secure := r.TLS != nil || (u != nil && u.Scheme == "https")
		a.login(homepageCookieWriter{ResponseWriter: w, secure: secure, admin: a}, r)
	})
	mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("gatehomepage_session"); err == nil {
			a.mu.Lock()
			delete(a.homepageSessions, sha256.Sum256([]byte(c.Value)))
			a.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: "gatehomepage_session", Path: "/", HttpOnly: true, MaxAge: -1, SameSite: http.SameSiteStrictMode})
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		name := map[string]string{"/": "homepage.html", "/homepage.js": "homepage.js", "/homepage.css": "homepage.css", "/icons.svg": "icons.svg"}[r.URL.Path]
		if name == "" {
			http.NotFound(w, r)
			return
		}
		data, err := webFiles.ReadFile("web/" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		mime := "text/html; charset=utf-8"
		if strings.HasSuffix(name, ".js") {
			mime = "text/javascript; charset=utf-8"
		}
		if strings.HasSuffix(name, ".css") {
			mime = "text/css; charset=utf-8"
		}
		if strings.HasSuffix(name, ".svg") {
			mime = "image/svg+xml"
		}
		w.Header().Set("Content-Type", mime)
		w.Write(data)
	})
	mux.HandleFunc("GET /manage", func(w http.ResponseWriter, r *http.Request) {
		access := a.store.Snapshot().Config.AdminAccess
		target := ""
		if access.Enabled && len(access.Origins) > 0 {
			target, _ = adminOrigin(access.Origins[0])
		}
		if target == "" {
			u, err := url.Parse("http://" + r.Host)
			if err != nil || u.Hostname() == "" {
				http.NotFound(w, r)
				return
			}
			target = "http://" + net.JoinHostPort(u.Hostname(), strconv.Itoa(a.adminPort))
		}
		http.Redirect(w, r, target+"/#homepage", http.StatusFound)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if !a.store.Snapshot().Config.Homepage.Enabled {
			http.Error(w, "GateHomePage 已关闭", http.StatusServiceUnavailable)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if r.Header.Get("X-Gatehouse-Request") != "1" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") || !homepageOriginAllowed(r) {
				apiError(w, 403, "请求来源验证失败")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

type homepageCookieWriter struct {
	http.ResponseWriter
	secure bool
	admin  *Admin
}

func (w homepageCookieWriter) WriteHeader(code int) {
	values := w.Header().Values("Set-Cookie")
	w.Header().Del("Set-Cookie")
	for _, value := range values {
		name, rest, _ := strings.Cut(value, "=")
		if name == "gatehouse_session" {
			token, _, _ := strings.Cut(rest, ";")
			key := sha256.Sum256([]byte(token))
			w.admin.mu.Lock()
			if w.admin.homepageSessions == nil {
				w.admin.homepageSessions = map[[32]byte]time.Time{}
			}
			now := time.Now()
			for k, expiry := range w.admin.homepageSessions {
				if !now.Before(expiry) {
					delete(w.admin.homepageSessions, k)
				}
			}
			if len(w.admin.homepageSessions) >= 32 {
				for k := range w.admin.homepageSessions {
					delete(w.admin.homepageSessions, k)
					break
				}
			}
			w.admin.homepageSessions[key] = now.Add(12 * time.Hour)
			delete(w.admin.sessions, key)
			w.admin.mu.Unlock()
		}
		value = strings.Replace(value, "gatehouse_session=", "gatehomepage_session=", 1)
		if w.secure && !strings.Contains(value, "; Secure") {
			value += "; Secure"
		}
		w.Header().Add("Set-Cookie", value)
	}
	w.ResponseWriter.WriteHeader(code)
}
