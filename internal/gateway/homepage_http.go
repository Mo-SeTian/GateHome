package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

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

func homepageSecure(r *http.Request) bool {
	u, _ := url.Parse(r.Header.Get("Origin"))
	return r.TLS != nil || (u != nil && u.Scheme == "https")
}

func (a *Admin) homepageAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT /api/homepage", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Enabled  bool `json:"enabled"`
			Port     int  `json:"port"`
			Revision int  `json:"revision"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		if a.maintenance.Busy() {
			apiError(w, 409, "正在执行维护操作")
			return
		}
		c := a.store.Snapshot().Config
		c.Homepage = HomepageConfig{Enabled: input.Enabled, Port: input.Port}
		if err := validateAdminPort(c, a.adminPort); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		if err := a.store.Update(c, nil, input.Revision); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		a.getConfig(w, r)
	}))
}

func (a *Admin) homepageLogin(w http.ResponseWriter, r *http.Request) {
	if !a.beginCredentialAttempt(r) {
		w.Header().Set("Retry-After", "60")
		apiError(w, 429, "登录尝试过于频繁，请一分钟后再试")
		return
	}
	defer a.endCredentialAttempt()
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	a.authMu.RLock()
	defer a.authMu.RUnlock()
	state := a.store.Snapshot()
	id, hash, valid := homepageAdminSpace, state.PasswordHash, input.Username == state.AdminUsername
	if !valid {
		for key, user := range state.HomepageUsers {
			if user.Username == input.Username {
				id, hash, valid = key, user.PasswordHash, user.Enabled
				break
			}
		}
	}
	if len(input.Password) > 72 || bcrypt.CompareHashAndPassword([]byte(hash), []byte(input.Password)) != nil || !valid {
		apiError(w, 401, "首页账号或密码不正确")
		return
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		apiError(w, 500, "无法创建首页会话")
		return
	}
	token := hex.EncodeToString(secret)
	a.mu.Lock()
	if a.homepageSessions == nil {
		a.homepageSessions = map[[32]byte]homepageSession{}
	}
	now, count := time.Now(), 0
	for key, session := range a.homepageSessions {
		if !now.Before(session.Expires) {
			delete(a.homepageSessions, key)
		} else if session.UserID == id {
			count++
		}
	}
	if count >= 16 || len(a.homepageSessions) >= 512 {
		for key, session := range a.homepageSessions {
			if session.UserID == id || len(a.homepageSessions) >= 512 {
				delete(a.homepageSessions, key)
				break
			}
		}
	}
	a.homepageSessions[sha256.Sum256([]byte(token))] = homepageSession{UserID: id, Expires: now.Add(12 * time.Hour)}
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "gatehomepage_session", Value: token, Path: "/", HttpOnly: true, Secure: homepageSecure(r), SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *Admin) selectedHomepage(w http.ResponseWriter, r *http.Request) (string, homepageDocument, bool) {
	id := r.URL.Query().Get("space")
	if id == "" {
		id, _ = a.homepageUser(w, r)
		if id == "" {
			id = homepageAdminSpace
		}
	}
	if id != homepageAdminSpace {
		user, exists := a.store.Snapshot().HomepageUsers[id]
		if !exists || !user.Enabled {
			return "", homepageDocument{}, false
		}
	}
	document, ok := a.store.pages.snapshot(id)
	return id, document, ok
}

func (a *Admin) HomepageHandler() http.Handler {
	mux := http.NewServeMux()
	protect := func(next func(http.ResponseWriter, *http.Request, string, HomepageConfig)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			id, document, ok := a.selectedHomepage(w, r)
			if !ok {
				http.NotFound(w, r)
				return
			}
			if !document.Homepage.Public {
				owner, authenticated := a.homepageUser(w, r)
				if !homepageOriginAllowed(r) {
					apiError(w, 403, "首页请求来源未获允许")
					return
				}
				if !authenticated || owner != id {
					apiError(w, 401, "请先登录此桌面的首页账号")
					return
				}
			}
			next(w, r, id, document.Homepage)
		}
	}
	mux.HandleFunc("GET /api/homepage", protect(func(w http.ResponseWriter, r *http.Request, id string, c HomepageConfig) {
		c.CustomCSS = ""
		jsonResponse(w, 200, struct {
			HomepageConfig
			SpaceID string `json:"space_id"`
		}{c, id})
	}))
	mux.HandleFunc("GET /custom.css", protect(func(w http.ResponseWriter, r *http.Request, id string, c HomepageConfig) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		io.WriteString(w, c.CustomCSS)
	}))
	mux.HandleFunc("GET /images/{id}", protect(func(w http.ResponseWriter, r *http.Request, owner string, c HomepageConfig) {
		id := r.PathValue("id")
		if !homepageImages(c)[id] {
			http.NotFound(w, r)
			return
		}
		dir, err := homepageSpaceDirectory(a.store.paths.Data, owner)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		data, err := readRouteImage(dir, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", http.DetectContentType(data))
		w.Write(data)
	}))
	mux.HandleFunc("GET /background/{id}", protect(func(w http.ResponseWriter, r *http.Request, owner string, c HomepageConfig) {
		if r.PathValue("id") == "" || r.PathValue("id") != c.Background {
			http.NotFound(w, r)
			return
		}
		dir, err := homepageSpaceDirectory(a.store.paths.Data, owner)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		data, err := readHomepageBackground(dir, c.Background)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(data)
	}))
	mux.HandleFunc("POST /login", a.homepageLogin)
	mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("gatehomepage_session"); err == nil {
			a.mu.Lock()
			delete(a.homepageSessions, sha256.Sum256([]byte(cookie.Value)))
			a.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: "gatehomepage_session", Path: "/", HttpOnly: true, Secure: homepageSecure(r), MaxAge: -1, SameSite: http.SameSiteStrictMode})
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		if !homepageOriginAllowed(r) {
			apiError(w, 403, "首页请求来源未获允许")
			return
		}
		id, authenticated := a.homepageUser(w, r)
		username := ""
		if authenticated {
			state := a.store.Snapshot()
			username = state.AdminUsername
			if id != homepageAdminSpace {
				username = state.HomepageUsers[id].Username
			}
		}
		jsonResponse(w, 200, map[string]any{"authenticated": authenticated, "user_id": id, "username": username})
	})
	a.homepageEditorRoutes(mux)
	mux.HandleFunc("GET /manage", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/#edit", http.StatusFound) })
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		name := map[string]string{"/": "homepage.html", "/homepage.js": "homepage.js", "/homepage.css": "homepage.css", "/homepage-editor.js": "homepage-editor.js", "/homepage-editor.css": "homepage-editor.css", "/icons.svg": "icons.svg", "/search-baidu.svg": "search-baidu.svg", "/search-google.svg": "search-google.svg", "/search-generic.svg": "search-generic.svg"}[r.URL.Path]
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
		} else if strings.HasSuffix(name, ".css") {
			mime = "text/css; charset=utf-8"
		} else if strings.HasSuffix(name, ".svg") {
			mime = "image/svg+xml"
		}
		w.Header().Set("Content-Type", mime)
		w.Write(data)
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
			multipart := (r.URL.Path == "/api/editor/images/upload" || r.URL.Path == "/api/editor/backgrounds/upload") && strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;")
			if r.Header.Get("X-Gatehouse-Request") != "1" || (!multipart && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")) || !homepageOriginAllowed(r) {
				apiError(w, 403, "请求来源验证失败")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
