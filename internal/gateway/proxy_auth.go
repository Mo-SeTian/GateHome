package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const routeAuthPath = "/.gatehouse-auth"
const routeCookiePrefix = "gatehouse_proxy_"

type routeAttempts struct {
	started time.Time
	count   int
}
type routeGate struct {
	username, passwordHash, cookieName string
	mu                                 sync.Mutex
	sessions                           map[[32]byte]time.Time
	attempts                           map[string]routeAttempts
	active                             int
	defense                            *IPDefense
}

func newRouteGate(key, username, hash string) *routeGate {
	sum := sha256.Sum256([]byte(key))
	return &routeGate{username: username, passwordHash: hash, cookieName: routeCookiePrefix + hex.EncodeToString(sum[:16]), sessions: map[[32]byte]time.Time{}, attempts: map[string]routeAttempts{}, defense: newIPDefense(nil)}
}

func authEvent(w http.ResponseWriter, result, message string, created bool) {
	if logged, ok := w.(*loggedResponseWriter); ok {
		logged.authResult, logged.authMessage, logged.freezeCreated = result, message, created
	}
}

func frozenResponse(w http.ResponseWriter, b IPBlock) {
	css, _ := fs.ReadFile(webFiles, "web/proxy-auth.css")
	styleHash := sha256.Sum256(css)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'sha256-"+base64.StdEncoding.EncodeToString(styleHash[:])+"'; frame-ancestors 'none'; base-uri 'none'")
	seconds := int(time.Until(b.Until).Seconds())
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", fmt.Sprint(seconds))
	w.WriteHeader(http.StatusForbidden)
	_ = frozenPage.Execute(w, struct {
		CSS   template.CSS
		Until string
	}{template.CSS(css), b.Until.Format("2006-01-02 15:04:05 -07:00")})
}

var frozenPage = template.Must(template.New("frozen-ip").Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>来源 IP 已冻结 · Gatehouse</title><style>{{.CSS}}</style></head><body><main class="auth-card"><div class="brand"><span class="brand-mark" aria-hidden="true">G</span><span>GATEHOUSE <small>服务访问保护</small></span></div><div class="service-label">ACCESS RESTRICTED</div><h1>此来源 IP 已被冻结</h1><p class="intro">当前来源已触发此服务的访问限制。请在解冻后重试，或联系管理员解除冻结。</p><div class="notice">预计解冻时间<br><b>{{.Until}}</b></div><a class="secondary" href="/"><svg class="auth-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path stroke="none" d="M0 0h24v24H0z" fill="none" />
  <path d="M20 11a8.1 8.1 0 0 0 -15.5 -2m-.5 -4v4h4" />
  <path d="M4 13a8.1 8.1 0 0 0 15.5 2m.5 4v-4h-4" />
</svg>解冻后重试</a><p class="account-note">冻结期间所有访问均被拦截，已有服务会话也不能进入。</p><footer>由 Gatehouse 保护此服务的访问入口</footer></main></body></html>`))

// The gate's cookies are reserved and must never reach an upstream or be set by it.
func stripRouteCookies(r *http.Request) {
	values := []string{}
	for _, header := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			name, _, _ := strings.Cut(strings.TrimSpace(part), "=")
			if !strings.HasPrefix(strings.TrimSpace(name), routeCookiePrefix) {
				values = append(values, strings.TrimSpace(part))
			}
		}
	}
	r.Header.Del("Cookie")
	if len(values) > 0 {
		r.Header.Set("Cookie", strings.Join(values, "; "))
	}
}

func stripRouteResponseCookies(response *http.Response) error {
	values := response.Header.Values("Set-Cookie")
	response.Header.Del("Set-Cookie")
	for _, value := range values {
		name, _, _ := strings.Cut(strings.TrimSpace(value), "=")
		if !strings.HasPrefix(strings.TrimSpace(name), routeCookiePrefix) {
			response.Header.Add("Set-Cookie", value)
		}
	}
	return nil
}

func (g *routeGate) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(g.cookieName)
	if err != nil {
		return false
	}
	key := sha256.Sum256([]byte(cookie.Value))
	g.mu.Lock()
	defer g.mu.Unlock()
	if !time.Now().Before(g.sessions[key]) {
		delete(g.sessions, key)
		return false
	}
	return true
}

func safeRouteReturn(value string) string {
	u, err := url.Parse(value)
	if err != nil || len(value) > 4096 || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\\r\n\x00") || strings.ContainsAny(u.Path, "\\\r\n\x00") || strings.HasPrefix(u.Path, "//") || u.IsAbs() || u.Host != "" || u.Fragment != "" || strings.HasPrefix(u.Path, routeAuthPath) {
		return "/"
	}
	return value
}

func routeLoginOriginAllowed(r *http.Request) bool {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	origin, ok := adminOrigin(r.Header.Get("Origin"))
	native, nativeOK := adminOrigin(scheme + "://" + r.Host)
	return ok && nativeOK && origin == native && r.Header.Get("Sec-Fetch-Site") != "cross-site"
}

func (g *routeGate) beginAttempt(r *http.Request) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	for ip, attempt := range g.attempts {
		if now.Sub(attempt.started) >= time.Minute {
			delete(g.attempts, ip)
		}
	}
	ip := remoteIP(r.RemoteAddr)
	attempt := g.attempts[ip]
	if g.active >= 2 || attempt.count >= 8 || (attempt.count == 0 && len(g.attempts) >= 1024) {
		return false
	}
	if attempt.count == 0 {
		attempt.started = now
	}
	attempt.count++
	g.attempts[ip] = attempt
	g.active++
	return true
}

func (g *routeGate) authorize(w http.ResponseWriter, r *http.Request, route Route) bool {
	authEvent(w, "auth_page", "服务验证页面", false)
	if strings.HasPrefix(r.URL.Path, routeAuthPath) {
		w.Header().Set("Cache-Control", "no-store")
	}
	if r.URL.Path == routeAuthPath+"/style.css" && (r.Method == "GET" || r.Method == "HEAD") {
		data, _ := fs.ReadFile(webFiles, "web/proxy-auth.css")
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(data)
		return false
	}
	if r.URL.Path == routeAuthPath || r.URL.Path == routeAuthPath+"/logout" {
		if r.Method == "GET" && r.URL.Path == routeAuthPath {
			g.page(w, r, route, 200, "", safeRouteReturn(r.URL.Query().Get("return")))
			return false
		}
		if r.Method != "POST" {
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "不支持的验证请求", 405)
			return false
		}
		if !routeLoginOriginAllowed(r) {
			authEvent(w, "auth_rejected", "验证请求来源被拒绝", false)
			g.page(w, r, route, 403, "请求来源验证失败，请从当前服务页面重新登录。", "/")
			return false
		}
		if r.URL.Path == routeAuthPath+"/logout" {
			authEvent(w, "auth_logout", "退出服务验证", false)
			if cookie, err := r.Cookie(g.cookieName); err == nil {
				g.mu.Lock()
				delete(g.sessions, sha256.Sum256([]byte(cookie.Value)))
				g.mu.Unlock()
			}
			http.SetCookie(w, &http.Cookie{Name: g.cookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, routeAuthPath, http.StatusSeeOther)
			return false
		}
		g.login(w, r, route)
		return false
	}
	if g.authenticated(r) {
		return true
	}
	authEvent(w, "auth_required", "尚未验证独立访问账号", false)
	if (r.Method == "GET" || r.Method == "HEAD") && strings.Contains(r.Header.Get("Accept"), "text/html") {
		g.page(w, r, route, 401, "", safeRouteReturn(r.URL.RequestURI()))
	} else {
		w.Header().Set("Cache-Control", "no-store")
		jsonResponse(w, 401, map[string]string{"error": "请先验证此反代服务的访问账号", "login_url": routeAuthPath})
	}
	return false
}

func (g *routeGate) login(w http.ResponseWriter, r *http.Request, route Route) {
	authEvent(w, "auth_rejected", "验证表单被拒绝", false)
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if contentType != "application/x-www-form-urlencoded" || r.ParseForm() != nil {
		g.page(w, r, route, 400, "验证表单格式错误或内容过大。", "/")
		return
	}
	for _, field := range []string{"username", "password", "return"} {
		if len(r.PostForm[field]) != 1 {
			g.page(w, r, route, 400, "验证表单字段无效。", "/")
			return
		}
	}
	username, password := r.PostForm.Get("username"), r.PostForm.Get("password")
	destination := safeRouteReturn(r.PostForm.Get("return"))
	ip, ok := sourceIP(r.RemoteAddr)
	if !ok {
		g.page(w, r, route, 403, "无法验证来源 IP。", destination)
		return
	}
	if b, blocked := g.defense.blocked(routeKey(route), ip); blocked {
		authEvent(w, "ip_frozen", b.Reason, false)
		frozenResponse(w, b)
		return
	}
	if !g.defense.capacity(routeKey(route), ip) || !g.beginAttempt(r) {
		authEvent(w, "auth_rate_limited", "验证尝试速率超过限制", false)
		w.Header().Set("Retry-After", "60")
		g.page(w, r, route, 429, "验证尝试过于频繁，请一分钟后重试。", destination)
		return
	}
	defer func() { g.mu.Lock(); g.active--; g.mu.Unlock() }()
	valid := len(username) <= 64 && len(password) >= 12 && len(password) <= 72
	if valid {
		valid = bcrypt.CompareHashAndPassword([]byte(g.passwordHash), []byte(password)) == nil && subtle.ConstantTimeCompare([]byte(username), []byte(g.username)) == 1
	}
	b, blocked, created := g.defense.record(route, ip, valid)
	if blocked {
		result := "ip_frozen"
		if created {
			result = "auth_failed"
		}
		authEvent(w, result, b.Reason, created)
		frozenResponse(w, b)
		return
	}
	if !valid {
		authEvent(w, "auth_failed", "独立访问账号或密码验证失败", false)
		g.page(w, r, route, 401, "访问账号或密码不正确。", destination)
		return
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		g.page(w, r, route, 500, "暂时无法创建访问会话，请重试。", destination)
		return
	}
	token := hex.EncodeToString(secret)
	g.mu.Lock()
	now := time.Now()
	for key, expires := range g.sessions {
		if !now.Before(expires) {
			delete(g.sessions, key)
		}
	}
	if len(g.sessions) >= 256 {
		for key := range g.sessions {
			delete(g.sessions, key)
			break
		}
	}
	g.sessions[sha256.Sum256([]byte(token))] = now.Add(12 * time.Hour)
	g.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: g.cookieName, Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	w.Header().Set("Cache-Control", "no-store")
	authEvent(w, "auth_success", "独立访问账号验证成功", false)
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

var routeLoginPage = template.Must(template.New("route-login").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>服务访问验证 · Gatehouse</title><link rel="stylesheet" href="/.gatehouse-auth/style.css"></head>
<body><main class="auth-card"><div class="brand"><span class="brand-mark" aria-hidden="true">G</span><span>GATEHOUSE <small>服务访问验证</small></span></div><div class="service-label">PROTECTED SERVICE</div><h1>{{.Name}}</h1><p class="domain">{{.Host}}</p>
{{if .Authenticated}}<div class="notice">此服务已验证，可以继续访问。</div><a class="primary" href="{{.Return}}"><svg class="auth-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 6h-6a2 2 0 0 0 -2 2v10a2 2 0 0 0 2 2h10a2 2 0 0 0 2 -2v-6" />
  <path d="M11 13l9 -9" />
  <path d="M15 4h5v5" />
</svg>继续访问服务</a><form action="/.gatehouse-auth/logout" method="post"><button class="secondary" type="submit"><svg class="auth-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path stroke="none" d="M0 0h24v24H0z" fill="none" />
  <path d="M14 8v-2a2 2 0 0 0 -2 -2h-7a2 2 0 0 0 -2 2v12a2 2 0 0 0 2 2h7a2 2 0 0 0 2 -2v-2" />
  <path d="M9 12h12l-3 -3" />
  <path d="M18 15l3 -3" />
</svg>退出此服务验证</button></form>
{{else}}<p class="intro">此服务已开启独立账号验证。请输入为本服务设置的访问账号和密码。</p><form action="/.gatehouse-auth" method="post"><input type="hidden" name="return" value="{{.Return}}"><label for="username">访问账号</label><input id="username" name="username" autocomplete="username" maxlength="64" required autofocus><label for="password">访问密码</label><input id="password" name="password" type="password" autocomplete="current-password" maxlength="72" required aria-describedby="auth-error"><p id="auth-error" class="error" role="alert">{{.Error}}</p><button class="primary" type="submit"><svg class="auth-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 8v-2a2 2 0 0 0 -2 -2h-7a2 2 0 0 0 -2 2v12a2 2 0 0 0 2 2h7a2 2 0 0 0 2 -2v-2" />
  <path d="M21 12h-13l3 -3" />
  <path d="M11 15l-3 -3" />
</svg>验证并进入服务</button></form>{{end}}
<p class="account-note">使用此反代服务的独立账号，不是 Gatehouse 管理员账号。</p><footer>由 Gatehouse 保护此服务的访问入口</footer></main></body></html>`))

func (g *routeGate) page(w http.ResponseWriter, r *http.Request, route Route, code int, message, destination string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.WriteHeader(code)
	name := route.Name
	if name == "" {
		name = route.Host
	}
	_ = routeLoginPage.Execute(w, struct {
		Name, Host, Error, Return string
		Authenticated             bool
	}{name, route.Host, message, destination, g.authenticated(r)})
}
