package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

//go:embed web/*
var webFiles embed.FS

type Admin struct {
	sunPanel     *SunPanel
	store        *Store
	proxy        *Proxy
	certs        *Certificates
	jobs         *Jobs
	started      time.Time
	ports        Config
	adminPort    int
	updateMu     sync.Mutex
	mu           sync.Mutex
	authMu       sync.RWMutex
	sessions     map[[32]byte]time.Time
	attempts     map[string]routeAttempts
	activeLogins int
	logs         *Logs
	maintenance  *Maintenance
	onlineUpdate onlineUpdateJob
	dashboard    dashboardCache
	telemetry    telemetryCollector
	domainDNS    *domainDNS
	discovery    serviceDiscovery
}

func NewAdmin(store *Store, proxy *Proxy, certs *Certificates, jobs *Jobs, adminPort int) *Admin {
	proxy.adminPort = adminPort
	proxy.defense = newIPDefense(store)
	proxy.ConfigureState(store.Snapshot())
	return &Admin{store: store, proxy: proxy, certs: certs, jobs: jobs, started: time.Now(), ports: store.Snapshot().Config, adminPort: adminPort, sessions: map[[32]byte]time.Time{}, attempts: map[string]routeAttempts{}, domainDNS: newDomainDNS(store)}
}

func jsonResponse(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func apiError(w http.ResponseWriter, code int, message string) {
	if logged, ok := w.(*loggedResponseWriter); ok {
		logged.errorMessage = message
	}
	jsonResponse(w, code, map[string]string{"error": message})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	controller := http.NewResponseController(w)
	if controller.SetReadDeadline(time.Now().Add(15*time.Second)) == nil {
		defer controller.SetReadDeadline(time.Time{})
	}
	limit := int64(128 << 10)
	if r.URL.Path == "/api/config" || r.URL.Path == "/api/editor/config" {
		limit = 1 << 20
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if decoder.Decode(v) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		apiError(w, 400, "请求 JSON 格式错误或内容过大")
		return false
	}
	return true
}

func (a *Admin) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, map[string]bool{"ok": true}) })
	mux.HandleFunc("POST /api/login", a.login)
	mux.HandleFunc("POST /api/logout", a.requireAuth(a.logout))
	a.accountRoutes(mux)
	mux.HandleFunc("GET /api/config", a.requireAuth(a.getConfig))
	mux.HandleFunc("PUT /api/config", a.requireAuth(a.putConfig))
	mux.HandleFunc("GET /api/status", a.requireAuth(a.status))
	a.maintenanceRoutes(mux)
	a.onlineUpdateRoutes(mux)
	a.dashboardRoutes(mux)
	a.networkRoutes(mux)
	a.domainDNSRoutes(mux)
	a.statisticsRoutes(mux)
	a.ipBlockRoutes(mux)
	a.serviceDiscoveryRoutes(mux)
	a.routeImageRoutes(mux)
	mux.HandleFunc("GET /api/logs", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		category, result := r.URL.Query().Get("category"), r.URL.Query().Get("result")
		scope, rule := r.URL.Query().Get("scope"), r.URL.Query().Get("rule")
		if scope == "" {
			scope = "project"
		}
		if (scope != "project" && scope != "access" && scope != "firewall" && scope != "security") || len(rule) > 320 {
			apiError(w, 400, "无效的日志范围或规则")
			return
		}
		if category != "" && category != "access" && category != "admin" && category != "ddns" && category != "subscriptions" && category != "certificates" {
			apiError(w, 400, "无效的日志分类")
			return
		}
		if result != "" && result != "success" && result != "error" {
			apiError(w, 400, "无效的日志结果")
			return
		}
		before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		filter := LogFilter{Scope: scope, Category: category, Result: result, Rule: rule, Search: strings.TrimSpace(r.URL.Query().Get("search")), Method: r.URL.Query().Get("method")}
		filter.Firewall, filter.Engine, filter.Decision = r.URL.Query().Get("firewall"), r.URL.Query().Get("engine"), r.URL.Query().Get("decision")
		if len(filter.Firewall) > 100 || (filter.Decision != "" && filter.Decision != "block" && filter.Decision != "detect") {
			apiError(w, 400, "无效的防火墙、检测引擎或处理结果")
			return
		}
		switch filter.Engine {
		case "", "ip", "waf", "custom", "rate", "system", "auth", "freeze":
		default:
			apiError(w, 400, "无效的检测引擎")
			return
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("ip")); raw != "" {
			ip, err := netip.ParseAddr(raw)
			if err != nil || ip.Zone() != "" {
				apiError(w, 400, "请输入有效的来源 IPv4 / IPv6 地址")
				return
			}
			filter.IP = ip.Unmap().String()
		}
		if raw := r.URL.Query().Get("rule_id"); raw != "" {
			id, err := strconv.Atoi(raw)
			if err != nil || id < 1 || id > 999999999 {
				apiError(w, 400, "检测规则编号须为 1–999999999")
				return
			}
			filter.RuleID = id
		}
		if len(filter.Search) > 200 {
			apiError(w, 400, "日志关键词过长")
			return
		}
		if raw := r.URL.Query().Get("status"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 100 || value > 599 {
				apiError(w, 400, "无效的 HTTP 状态码")
				return
			}
			filter.Status = value
		}
		if filter.Method != "" && !strings.Contains("|GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|CONNECT|", "|"+filter.Method+"|") {
			apiError(w, 400, "无效的请求方法")
			return
		}
		for _, item := range []struct {
			raw    string
			target *time.Time
		}{{r.URL.Query().Get("from"), &filter.From}, {r.URL.Query().Get("to"), &filter.To}} {
			if item.raw != "" {
				value, err := time.Parse(time.RFC3339, item.raw)
				if err != nil {
					apiError(w, 400, "无效的日志时间")
					return
				}
				*item.target = value
			}
		}
		if !filter.From.IsZero() && !filter.To.IsZero() && filter.From.After(filter.To) {
			apiError(w, 400, "开始时间不能晚于结束时间")
			return
		}
		limit := 50
		if r.URL.Query().Has("page") {
			page, err := strconv.Atoi(r.URL.Query().Get("page"))
			size, sizeErr := strconv.Atoi(r.URL.Query().Get("size"))
			var through int64
			var throughErr error
			if raw := r.URL.Query().Get("through"); raw != "" {
				through, throughErr = strconv.ParseInt(raw, 10, 64)
			}
			if err != nil || sizeErr != nil || throughErr != nil || page < 1 || page > maxLogEntries || (size != 20 && size != 50 && size != 100 && size != 200) || through < 0 {
				apiError(w, 400, "无效的页码、每页条数或日志快照")
				return
			}
			result := LogPage{Entries: []LogEntry{}, Page: 1, Size: size, Pages: 1}
			if a.logs != nil {
				result = a.logs.Page(filter, page, size, through)
			}
			enrichLogRegions(result.Entries)
			jsonResponse(w, 200, result)
			return
		}
		rows := []LogEntry{}
		next := int64(0)
		writeError := false
		if a.logs != nil {
			rows, next, writeError = a.logs.Query(filter, before, limit)
		}
		enrichLogRegions(rows)
		jsonResponse(w, 200, map[string]any{"entries": rows, "next_before": next, "write_error": writeError})
	}))
	mux.HandleFunc("POST /api/ddns/{id}/run", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		for _, g := range a.store.Snapshot().Config.DDNS.Groups {
			if g.ID == r.PathValue("id") {
				if !g.Enabled {
					apiError(w, 400, "请先启用此 DDNS 组")
					return
				}
				if !a.jobs.TriggerDDNS(g.ID) {
					apiError(w, 409, "DDNS 队列已满")
					return
				}
				jsonResponse(w, 202, map[string]string{"message": "DDNS 组已加入同步队列"})
				return
			}
		}
		apiError(w, 404, "DDNS 组不存在")
	}))
	mux.HandleFunc("POST /api/subscriptions/{id}/refresh", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		for _, definition := range a.store.Snapshot().Config.Subscriptions {
			if definition.ID != id {
				continue
			}
			if !definition.Enabled {
				apiError(w, 400, "请先启用此订阅")
				return
			}
			if a.proxy.subscriptions == nil || !a.proxy.subscriptions.Trigger(id) {
				apiError(w, 409, "订阅更新队列已满，请稍后重试")
				return
			}
			jsonResponse(w, 202, map[string]string{"message": "订阅更新已加入队列"})
			return
		}
		apiError(w, 404, "订阅不存在")
	}))
	mux.HandleFunc("POST /api/jobs/{kind}", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		kind := r.PathValue("kind")
		if kind != "ddns" && kind != "acme" {
			apiError(w, 404, "任务不存在")
			return
		}
		c := a.store.Snapshot().Config
		if (kind == "ddns" && !c.DDNSEnabled()) || (kind == "acme" && !c.ACME.Enabled) {
			apiError(w, 400, "请先配置并启用此功能")
			return
		}
		a.jobs.Trigger(kind)
		jsonResponse(w, 202, map[string]string{"message": "任务已加入队列"})
	}))
	sub, _ := fs.Sub(webFiles, "web")
	files := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/app.js" && r.URL.Path != "/style.css" && r.URL.Path != "/icons.svg" && r.URL.Path != "/sunpanel-admin.js" && r.URL.Path != "/dashboard.js" && r.URL.Path != "/china-outline.svg" {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/status" && r.URL.Path != "/api/dashboard" && r.URL.Path != "/api/logs" && r.URL.Path != "/api/statistics" && r.URL.Path != "/api/ddns/records" && r.URL.Path != "/api/maintenance/online-update-status" && !(r.Method == "GET" && (r.URL.Path == "/api/ip-blocks" || strings.HasPrefix(r.URL.Path, "/api/service-discovery/") || strings.HasPrefix(r.URL.Path, "/api/route-images/"))) {
			started := time.Now()
			logged := &loggedResponseWriter{ResponseWriter: w}
			w = logged
			defer func() {
				status := logged.status
				if status == 0 {
					status = 200
				}
				message := "管理接口调用"
				if logged.errorMessage != "" {
					message = logged.errorMessage
				}
				a.logs.Add(LogEntry{Category: "admin", Action: "管理 API", Method: r.Method, Path: r.URL.Path, Remote: remoteIP(r.RemoteAddr), Status: status, OK: status < 400, DurationMS: time.Since(started).Milliseconds(), Message: message})
			}()
		}
		if strings.HasPrefix(r.URL.Path, "/sunpanel/") && a.sunPanel != nil {
			a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" && r.Method != "HEAD" && !a.requestOriginAllowed(r) {
					apiError(w, 403, "请求来源未获允许")
					return
				}
				a.sunPanel.Handler().ServeHTTP(w, r)
			})(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.Method != "GET" && r.Method != "HEAD" {
			isUpload := r.URL.Path == "/api/maintenance/inspect-update" || r.URL.Path == "/api/maintenance/inspect-backup" || r.URL.Path == "/api/route-images/upload"
			contentOK := strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") || (isUpload && strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;"))
			if r.Header.Get("X-Gatehouse-Request") != "1" || !contentOK {
				apiError(w, 403, "请求来源验证失败")
				return
			}
			if !a.requestOriginAllowed(r) {
				apiError(w, 403, "请求来源未获允许；通过公网反代访问时，请先在设置中启用反代访问并添加当前公网地址")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *Admin) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" || !a.requestOriginAllowed(r) {
			apiError(w, 403, "管理请求来源未获允许")
			return
		}
		cookie, err := r.Cookie("gatehouse_session")
		if err != nil {
			apiError(w, 401, "请先登录")
			return
		}
		key := sha256.Sum256([]byte(cookie.Value))
		a.mu.Lock()
		expires := a.sessions[key]
		if !time.Now().Before(expires) {
			delete(a.sessions, key)
		}
		a.mu.Unlock()
		if !time.Now().Before(expires) {
			apiError(w, 401, "登录已过期，请重新登录")
			return
		}
		next(w, r)
	}
}

func (a *Admin) login(w http.ResponseWriter, r *http.Request) {
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
	if len(input.Password) > 72 || bcrypt.CompareHashAndPassword([]byte(state.PasswordHash), []byte(input.Password)) != nil || input.Username != state.AdminUsername {
		apiError(w, 401, "管理员账号或密码不正确")
		return
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		apiError(w, 500, "无法创建会话")
		return
	}
	token := hex.EncodeToString(secret)
	a.mu.Lock()
	now := time.Now()
	for k, exp := range a.sessions {
		if !now.Before(exp) {
			delete(a.sessions, k)
		}
	}
	if len(a.sessions) >= 32 {
		for k := range a.sessions {
			delete(a.sessions, k)
			break
		}
	}
	a.sessions[sha256.Sum256([]byte(token))] = now.Add(12 * time.Hour)
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "gatehouse_session", Value: token, Path: "/", HttpOnly: true, Secure: a.secureSession(r), SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *Admin) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("gatehouse_session"); err == nil {
		a.mu.Lock()
		delete(a.sessions, sha256.Sum256([]byte(c.Value)))
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "gatehouse_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secureSession(r), SameSite: http.SameSiteStrictMode})
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *Admin) getConfig(w http.ResponseWriter, r *http.Request) {
	s := a.store.Snapshot()
	configured := map[string]bool{}
	for _, g := range s.Config.DDNS.Groups {
		configured[g.ID] = s.DNSCredentials[g.ID].Token != ""
	}
	certConfigured := map[string]bool{}
	for _, request := range s.Config.ACME.Requests {
		certConfigured[request.ID] = s.CertificateCredentials[request.ID].Token != ""
	}
	routeConfigured := map[string]bool{}
	for _, route := range s.Config.Routes {
		routeConfigured[routeKey(route)] = s.RoutePasswordHashes[routeKey(route)] != ""
	}
	jsonResponse(w, 200, struct {
		RoutePasswordsConfigured         map[string]bool `json:"route_passwords_configured"`
		Config                           Config          `json:"config"`
		TokenConfigured                  bool            `json:"token_configured"`
		Revision                         int             `json:"revision"`
		ProxyPasswordConfigured          bool            `json:"proxy_password_configured"`
		DNSCredentialsConfigured         map[string]bool `json:"dns_credentials_configured"`
		CertificateCredentialsConfigured map[string]bool `json:"certificate_credentials_configured"`
	}{routeConfigured, s.Config, s.CloudflareToken != "", s.Revision, s.ProxyPassword != "", configured, certConfigured})
}

func (a *Admin) putConfig(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Config            Config             `json:"config"`
		Revision          int                `json:"revision"`
		ProxyPassword     *string            `json:"proxy_password"`
		DNSTokens         map[string]*string `json:"dns_tokens"`
		CertificateTokens map[string]*string `json:"certificate_tokens"`
		RoutePasswords    map[string]*string `json:"route_passwords"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	if input.Config.Zone != "" {
		apiError(w, 400, "全局 DNS 账户已迁移，请刷新后在组内配置")
		return
	}
	if err := validateAdminPort(input.Config, a.adminPort); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	for _, token := range input.DNSTokens {
		if token != nil && (len(*token) > 512 || containsCredentialNewline(*token)) {
			apiError(w, 400, "DNS Token 格式无效")
			return
		}
	}
	for _, token := range input.CertificateTokens {
		if token != nil && (len(*token) > 512 || containsCredentialNewline(*token)) {
			apiError(w, 400, "证书 Token 格式无效")
			return
		}
	}
	if input.ProxyPassword != nil && (len(*input.ProxyPassword) > 255 || strings.ContainsAny(*input.ProxyPassword, "\r\n")) {
		apiError(w, 400, "代理密码格式无效")
		return
	}
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	if a.maintenance.Busy() {
		apiError(w, 409, "维护操作执行中，请待重启完成后再修改配置")
		return
	}
	if err := Validate(input.Config); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	if input.Revision != a.store.Snapshot().Revision {
		apiError(w, 400, "配置已被修改，请刷新页面后重试")
		return
	}
	prepared, err := a.proxy.prepareFirewalls(input.Config)
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	if err := a.store.UpdateRouteCredentials(input.Config, input.DNSTokens, input.CertificateTokens, nil, input.ProxyPassword, input.RoutePasswords, input.Revision); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	if a.proxy.subscriptions != nil {
		a.proxy.subscriptions.Reload()
		a.proxy.subscriptions.Trigger("")
	}
	state := a.store.Snapshot()
	_ = a.logs.Cleanup(time.Now())
	a.proxy.configurePrepared(state.Config, state.RoutePasswordHashes, prepared)
	a.certs.Reload()
	a.jobs.Trigger("ddns")
	a.jobs.Trigger("acme")
	a.getConfig(w, r)
}

func (a *Admin) status(w http.ResponseWriter, r *http.Request) {
	c := a.store.Snapshot().Config
	jobs, events := a.jobs.Snapshot()
	subscriptions := []SubscriptionStatus{}
	if a.proxy.subscriptions != nil {
		subscriptions = a.proxy.subscriptions.Status()
	}
	jsonResponse(w, 200, map[string]any{
		"version": Version, "maintenance_available": a.maintenance != nil && a.maintenance.Available(), "maintenance_busy": a.maintenance.Busy(),
		"dns_providers":     []map[string]string{{"id": "cloudflare", "name": "Cloudflare"}},
		"ip_query_defaults": map[string][]string{"ipv4": defaultIPEndpoints("A"), "ipv6": defaultIPEndpoints("AAAA")},
		"uptime_seconds":    int(time.Since(a.started).Seconds()), "requests": a.proxy.Requests.Load(), "blocked": a.proxy.Blocked.Load(), "failures": a.proxy.Failures.Load(),
		"listeners":        a.ports.Groups,
		"restart_required": listenerSignature(c) != listenerSignature(a.ports),
		"subscriptions":    subscriptions,
		"log_storage":      a.logs.StorageStatus(),
		"jobs":             jobs, "events": events, "certificates": a.certs.Status(),
	})
}

func HashPassword(password []byte) (string, error) {
	if len(password) < 12 || len(password) > 72 {
		return "", errors.New("密码长度须为 12–72 字节")
	}
	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	return string(hash), err
}

func (s *Admin) SetLogs(logs *Logs) { s.logs = logs }

func (a *Admin) SetSunPanel(panel *SunPanel) { a.sunPanel = panel }
