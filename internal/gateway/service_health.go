package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
	"time"
)

type ServiceHealthConfig struct {
	Enabled  bool `json:"enabled"`
	Interval int  `json:"interval"`
}

func serviceHealthOptions(c Config) ServiceHealthConfig {
	if c.ServiceHealth.Interval == 0 {
		return ServiceHealthConfig{Enabled: true, Interval: 60}
	}
	return c.ServiceHealth
}

type serviceHealthResult struct {
	State        string     `json:"state"`
	Label        string     `json:"label"`
	Message      string     `json:"message"`
	Status       int        `json:"status,omitempty"`
	DurationMS   int64      `json:"duration_ms"`
	CheckedAt    time.Time  `json:"checked_at"`
	TLSVerified  bool       `json:"tls_verified"`
	TLSExpiresAt *time.Time `json:"tls_expires_at,omitempty"`
	Healthy      bool       `json:"healthy"`
}
type healthNotice struct {
	RouteID string    `json:"route_id"`
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
	Healthy bool      `json:"healthy"`
}

type healthPending struct {
	done   chan struct{}
	result serviceHealthResult
}
type healthEventState struct {
	upstream string
	failures int
	down     bool
}
type serviceHealthCache struct {
	mu      sync.Mutex
	results map[string]serviceHealthResult
	pending map[string]*healthPending
	events  map[string]healthEventState
	slots   chan struct{}
	notices []healthNotice
}

func healthError(err error) (string, string) {
	var unknown x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var host x509.HostnameError
	var verification *tls.CertificateVerificationError
	if errors.As(err, &unknown) || errors.As(err, &invalid) || errors.As(err, &host) || errors.As(err, &verification) {
		return "tls_untrusted", "证书不可信、已过期或域名不匹配；请配置可信证书"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns_error", "服务器无法解析服务域名，请检查 DNS"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "timeout", "服务响应超时，请检查网络与服务负载"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled", "检测已取消"
	}
	return "connection_error", "服务器无法连接上游，请检查地址、端口与服务状态"
}

func probeServiceHealth(ctx context.Context, raw string) serviceHealthResult {
	started := time.Now()
	result := serviceHealthResult{CheckedAt: started.UTC()}
	finish := func() serviceHealthResult { result.DurationMS = time.Since(started).Milliseconds(); return result }
	target, err := url.Parse(raw)
	if err != nil || target.User != nil || target.Hostname() == "" || (target.Scheme != "http" && target.Scheme != "https") {
		result.State, result.Label, result.Message = "invalid", "地址无效", "请配置不含账号密码的 HTTP(S) 地址"
		return finish()
	}
	ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 2 * time.Second, MaxResponseHeaderBytes: 32 << 10, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			ip = ip.Unmap()
			if (!ip.IsGlobalUnicast() && !ip.IsLoopback()) || ip.IsLinkLocalUnicast() || ip == netip.MustParseAddr("100.100.100.200") {
				return nil, errors.New("restricted address")
			}
		}
		var last error
		for _, ip := range ips {
			conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, _ := http.NewRequestWithContext(ctx, "HEAD", target.String(), nil)
	request.Header.Set("User-Agent", "GateHome-Health/1")
	response, err := client.Do(request)
	if err == nil && (response.StatusCode == 405 || response.StatusCode == 501) {
		response.Body.Close()
		request, _ = http.NewRequestWithContext(ctx, "GET", target.String(), nil)
		request.Header.Set("Range", "bytes=0-0")
		request.Header.Set("User-Agent", "GateHome-Health/1")
		response, err = client.Do(request)
	}
	if err != nil {
		result.State, result.Message = healthError(err)
		result.Label = map[string]string{"tls_untrusted": "证书异常", "dns_error": "DNS 异常", "timeout": "响应超时", "connection_error": "连接失败", "cancelled": "已取消"}[result.State]
		return finish()
	}
	response.Body.Close()
	result.Status = response.StatusCode
	result.TLSVerified = response.TLS != nil && len(response.TLS.VerifiedChains) > 0
	if result.TLSVerified && len(response.TLS.PeerCertificates) > 0 {
		expires := response.TLS.PeerCertificates[0].NotAfter
		result.TLSExpiresAt = &expires
	}
	switch {
	case response.StatusCode >= 200 && response.StatusCode < 300:
		result.State, result.Label, result.Message, result.Healthy = "healthy", "服务正常", "上游返回成功响应", true
	case response.StatusCode >= 300 && response.StatusCode < 400:
		result.State, result.Label, result.Message, result.Healthy = "redirect", "服务可达", "上游返回跳转，检测不会继续跟随", true
	case response.StatusCode == 401 || response.StatusCode == 403:
		result.State, result.Label, result.Message, result.Healthy = "auth_required", "需要验证", "服务可达，需要登录或访问授权", true
	default:
		result.State, result.Label, result.Message = "http_error", "HTTP 异常", "服务已连通，但返回异常状态码"
	}
	return finish()
}

func (a *Admin) cachedServiceHealth(ctx context.Context, raw string, ttl time.Duration) serviceHealthResult {
	cache := &a.health
	cache.mu.Lock()
	if cache.results == nil {
		cache.results = map[string]serviceHealthResult{}
		cache.pending = map[string]*healthPending{}
		cache.events = map[string]healthEventState{}
		cache.slots = make(chan struct{}, 6)
	}
	if result, ok := cache.results[raw]; ok && time.Since(result.CheckedAt) < ttl {
		cache.mu.Unlock()
		return result
	}
	if pending := cache.pending[raw]; pending != nil {
		cache.mu.Unlock()
		select {
		case <-pending.done:
			return pending.result
		case <-ctx.Done():
			return serviceHealthResult{State: "cancelled", Label: "已取消"}
		}
	}
	pending := &healthPending{done: make(chan struct{})}
	cache.pending[raw] = pending
	slots := cache.slots
	cache.mu.Unlock()
	var result serviceHealthResult
	select {
	case slots <- struct{}{}:
		result = probeServiceHealth(ctx, raw)
		<-slots
	case <-ctx.Done():
		result = serviceHealthResult{State: "cancelled", Label: "已取消"}
	}
	cache.mu.Lock()
	pending.result = result
	if result.State != "cancelled" {
		cache.results[raw] = result
	}
	for key, row := range cache.results {
		if time.Since(row.CheckedAt) > 30*time.Minute {
			delete(cache.results, key)
		}
	}
	delete(cache.pending, raw)
	close(pending.done)
	cache.mu.Unlock()
	return result
}

func (a *Admin) routeServiceHealth(ctx context.Context) map[string]serviceHealthResult {
	config := a.store.Snapshot().Config
	options := serviceHealthOptions(config)
	results := map[string]serviceHealthResult{}
	var mu sync.Mutex
	var work sync.WaitGroup
	for _, route := range config.Routes {
		group, exists := config.Group(route.GroupID)
		if !options.Enabled || !route.Enabled || !exists || !group.Enabled {
			mu.Lock()
			results[route.ID] = serviceHealthResult{State: "disabled", Label: "未检测"}
			mu.Unlock()
			continue
		}
		work.Add(1)
		go func(route Route) {
			defer work.Done()
			result := a.cachedServiceHealth(ctx, route.Upstream, time.Duration(options.Interval)*time.Second)
			mu.Lock()
			results[route.ID] = result
			mu.Unlock()
			a.recordHealthTransition(route, result)
		}(route)
	}
	work.Wait()
	return results
}

func (a *Admin) recordHealthTransition(route Route, result serviceHealthResult) {
	if result.State == "cancelled" {
		return
	}
	cache := &a.health
	cache.mu.Lock()
	previous := cache.events[route.ID]
	if previous.upstream != route.Upstream {
		previous = healthEventState{upstream: route.Upstream}
	}
	// Only one observation per completed check counts toward the failure threshold.
	key := "event:" + route.ID
	last := cache.results[key]
	if !result.CheckedAt.After(last.CheckedAt) {
		cache.mu.Unlock()
		return
	}
	cache.results[key] = result
	message := ""
	if result.Healthy {
		if previous.down {
			message = "服务已恢复"
		}
		previous.failures = 0
		previous.down = false
	} else {
		previous.failures++
		if previous.failures >= 2 && !previous.down {
			previous.down = true
			message = "服务连续检测失败"
		}
	}
	cache.events[route.ID] = previous
	if message != "" {
		cache.notices = append(cache.notices, healthNotice{RouteID: route.ID, Time: result.CheckedAt, Message: message + " · " + result.Label, Healthy: result.Healthy})
		if len(cache.notices) > 60 {
			cache.notices = append([]healthNotice{}, cache.notices[len(cache.notices)-60:]...)
		}
	}
	cache.mu.Unlock()
	if message != "" && a.logs != nil {
		a.logs.Add(LogEntry{Category: "admin", Action: "服务健康", Target: route.ID, OK: result.Healthy, DurationMS: result.DurationMS, Message: message + " · " + result.Label})
	}
}

func (a *Admin) StartServiceHealth(ctx context.Context) {
	go func() {
		a.routeServiceHealth(ctx)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if serviceHealthOptions(a.store.Snapshot().Config).Enabled {
					a.routeServiceHealth(ctx)
				}
			}
		}
	}()
}

func (a *Admin) serviceHealthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/service-health", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		routes := a.routeServiceHealth(r.Context())
		a.health.mu.Lock()
		notices := append([]healthNotice{}, a.health.notices...)
		a.health.mu.Unlock()
		jsonResponse(w, 200, map[string]any{"options": serviceHealthOptions(a.store.Snapshot().Config), "routes": routes, "notices": notices})
	}))
}

func (a *Admin) authenticatedPanelUser(r *http.Request) (uint, error) {
	if a.sunPanel == nil || r.Header.Get("token") == "" {
		return 0, errors.New("请先登录 Sun-Panel")
	}
	a.sunPanel.mu.Lock()
	address := a.sunPanel.bridgeURL
	a.sunPanel.mu.Unlock()
	if address == "" {
		return 0, errors.New("Sun-Panel 未运行")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "POST", address+"/sunpanel/api/user/getInfo", nil)
	request.Header.Set("token", r.Header.Get("token"))
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return 0, errors.New("Sun-Panel 登录状态检查失败")
	}
	defer response.Body.Close()
	var data struct {
		Code *int `json:"code"`
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 32<<10)).Decode(&data) != nil || data.Code == nil || *data.Code != 0 || data.Data.ID == 0 {
		return 0, errors.New("Sun-Panel 登录已过期，请重新登录")
	}
	return data.Data.ID, nil
}

type panelSiteHealth struct {
	ID      uint                `json:"id"`
	Default serviceHealthResult `json:"default"`
	LAN     serviceHealthResult `json:"lan"`
	LanURL  string              `json:"lanUrl"`
	URL     string              `json:"url"`
}

func (a *Admin) sunPanelSiteHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	fail := func(message string) { jsonResponse(w, 200, map[string]any{"code": -1, "msg": message}) }
	var input struct {
		IDs     []uint `json:"ids"`
		TLSOnly bool   `json:"tlsOnly"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || len(input.IDs) == 0 || len(input.IDs) > 50 {
		fail("请选择 1–50 个网站")
		return
	}
	userID, err := a.authenticatedPanelUser(r)
	if err != nil {
		fail(err.Error())
		return
	}
	data, err := a.sunPanel.bridgeCall(r.Context(), "desktop", userID, nil)
	if err != nil {
		fail("无法读取当前账号网站")
		return
	}
	encoded, _ := json.Marshal(data)
	var desktop mcpDesktop
	if json.Unmarshal(encoded, &desktop) != nil {
		fail("无法读取当前账号网站")
		return
	}
	wanted := map[uint]bool{}
	for _, id := range input.IDs {
		wanted[id] = true
	}
	selected := desktop.Items[:0]
	for _, item := range desktop.Items {
		if wanted[item.ID] {
			selected = append(selected, item)
		}
	}
	results := make([]panelSiteHealth, len(selected))
	var work sync.WaitGroup
	for i, item := range selected {
		work.Add(1)
		go func(i int, itemID uint, defaultURL, lanURL string) {
			defer work.Done()
			row := panelSiteHealth{ID: itemID, URL: defaultURL, LanURL: lanURL}
			if !input.TLSOnly {
				row.Default = a.cachedServiceHealth(r.Context(), defaultURL, time.Minute)
			}
			if lanURL != "" {
				row.LAN = a.cachedServiceHealth(r.Context(), lanURL, time.Minute)
			}
			results[i] = row
		}(i, item.ID, item.URL, item.LanURL)
	}
	work.Wait()
	jsonResponse(w, 200, map[string]any{"code": 0, "data": results})
}
