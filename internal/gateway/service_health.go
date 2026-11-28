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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type ServiceHealthConfig struct {
	Enabled           bool `json:"enabled"`
	Interval          int  `json:"interval"`
	AlertDelaySeconds int  `json:"alert_delay_seconds,omitempty"`
}

func serviceHealthOptions(c Config) ServiceHealthConfig {
	options := c.ServiceHealth
	if options.Interval == 0 {
		options.Enabled = true
		options.Interval = 60
	}
	return options
}

func formatHealthDelay(seconds int) string {
	if seconds <= 0 {
		return "0 秒"
	}
	if seconds >= 3600 {
		hours := seconds / 3600
		mins := (seconds % 3600) / 60
		secs := seconds % 60
		out := strconv.Itoa(hours) + " 小时"
		if mins > 0 {
			out += " " + strconv.Itoa(mins) + " 分钟"
		}
		if secs > 0 {
			out += " " + strconv.Itoa(secs) + " 秒"
		}
		return out
	}
	if seconds >= 60 {
		mins := seconds / 60
		secs := seconds % 60
		if secs == 0 {
			return strconv.Itoa(mins) + " 分钟"
		}
		return strconv.Itoa(mins) + " 分 " + strconv.Itoa(secs) + " 秒"
	}
	return strconv.Itoa(seconds) + " 秒"
}

type serviceHealthResult struct {
	State               string     `json:"state"`
	Label               string     `json:"label"`
	Message             string     `json:"message"`
	Status              int        `json:"status,omitempty"`
	DurationMS          int64      `json:"duration_ms"`
	CheckedAt           time.Time  `json:"checked_at"`
	TLSVerified         bool       `json:"tls_verified"`
	TLSExpiresAt        *time.Time `json:"tls_expires_at,omitempty"`
	Healthy             bool       `json:"healthy"`
	BrowserProbeAllowed bool       `json:"browser_probe_allowed"`
	redirectURL         string
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
	upstream      string
	result        serviceHealthResult
	failures      int
	firstFailedAt time.Time
	down          bool
}
type serviceHealthCache struct {
	mu           sync.Mutex
	results      map[string]serviceHealthResult
	pending      map[string]*healthPending
	events       map[string]healthEventState
	slots        chan struct{}
	notices      []healthNotice
	history      map[string]routeHealthHistory
	historyDirty bool
}

func isPeerResetError(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET)
}

func isHealthyProbeStatus(code int) bool {
	return (code >= 200 && code < 400) || code == 401 || code == 403
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
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "connection_error", "上游拒绝连接（目标端口未监听或被防火墙拦截），请检查服务状态与监听地址"
	}
	if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return "connection_error", "服务器网络无法路由至上游主机，请检查网段连通性或容器网络模式"
	}
	if isPeerResetError(err) {
		return "connection_error", "上游建立连接后主动断开，请检查 HTTP/HTTPS 协议是否匹配或上游是否限制探测请求"
	}
	if msg := err.Error(); strings.Contains(msg, "HTTP response to HTTPS client") || strings.Contains(msg, "malformed HTTP response") {
		return "connection_error", "协议不匹配或响应非标准 HTTP，请检查上游地址应为 http:// 还是 https://"
	}
	return "connection_error", "服务器无法连接上游，请检查地址、端口与服务状态"
}

func probeServiceHealth(ctx context.Context, raw string) serviceHealthResult {
	return probeServiceHealthWithBlockedPorts(ctx, raw, nil, nil)
}

func probeServiceHealthWithBlockedPorts(ctx context.Context, raw string, blockedPorts map[int]bool, localAddrs map[netip.Addr]bool) serviceHealthResult {
	return probeServiceHealthWithOptions(ctx, raw, "", blockedPorts, localAddrs)
}

func isBlockedInternalTargetURL(target *url.URL, blockedPorts map[int]bool, localAddrs map[netip.Addr]bool) bool {
	if target == nil || len(blockedPorts) == 0 {
		return false
	}
	port := urlEffectivePort(target)
	if !blockedPorts[port] {
		return false
	}
	host := target.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		return ip.IsLoopback() || ip.IsUnspecified() || localAddrs[ip]
	}
	return false
}

func probeServiceHealthWithOptions(ctx context.Context, raw, requestHost string, blockedPorts map[int]bool, localAddrs map[netip.Addr]bool) serviceHealthResult {
	started := time.Now()
	result := serviceHealthResult{CheckedAt: started.UTC()}
	finish := func() serviceHealthResult { result.DurationMS = time.Since(started).Milliseconds(); return result }
	target, err := url.Parse(raw)
	if err != nil || target.User != nil || target.Hostname() == "" || (target.Scheme != "http" && target.Scheme != "https") {
		result.State, result.Label, result.Message = "invalid", "地址无效", "请配置不含账号密码的 HTTP(S) 地址"
		return finish()
	}
	if isBlockedInternalTargetURL(target, blockedPorts, localAddrs) {
		result.State, result.Label, result.Message = "invalid", "地址受限", "禁止探测 GateHome 管理或内部服务端口"
		return finish()
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 2 * time.Second, MaxResponseHeaderBytes: 32 << 10, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, portStr, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		port, portErr := strconv.Atoi(portStr)
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			ip = ip.Unmap()
			if ip.IsUnspecified() || !allowedServiceIP(ip) {
				return nil, errors.New("restricted address")
			}
			if portErr == nil && blockedPorts[port] && (ip.IsLoopback() || ip.IsUnspecified() || localAddrs[ip]) {
				return nil, errors.New("restricted internal port")
			}
		}
		var last error
		for _, ip := range ips {
			conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), portStr))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	doProbe := func(method, hostHeader string) (*http.Response, error) {
		request, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("User-Agent", "GateHome-Health/1")
		if hostHeader != "" {
			request.Host = hostHeader
		}
		return client.Do(request)
	}
	response, err := doProbe("HEAD", "")
	if isPeerResetError(err) || (err == nil && (response.StatusCode == 400 || response.StatusCode == 404 || response.StatusCode == 405 || response.StatusCode == 501)) {
		if response != nil {
			response.Body.Close()
		}
		response, err = doProbe("GET", "")
	}
	if target.Scheme == "http" && requestHost != "" && !strings.EqualFold(requestHost, target.Host) {
		if isPeerResetError(err) || (err == nil && (response.StatusCode == 400 || response.StatusCode == 403 || response.StatusCode == 404 || response.StatusCode == 421)) {
			altResp, altErr := doProbe("GET", requestHost)
			if altErr == nil && (err != nil || isHealthyProbeStatus(altResp.StatusCode)) {
				if response != nil {
					response.Body.Close()
				}
				response, err = altResp, nil
			} else if altResp != nil {
				altResp.Body.Close()
			}
		}
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
		if location, err := response.Location(); err == nil {
			result.redirectURL = location.String()
		}
	case response.StatusCode == 401 || response.StatusCode == 403:
		result.State, result.Label, result.Message, result.Healthy = "auth_required", "需要验证", "服务可达，需要登录或访问授权", true
	default:
		result.State, result.Label, result.Message = "http_error", "HTTP 异常", "服务已连通，但返回异常状态码"
	}
	return finish()
}

func probeTCPHealth(ctx context.Context, upstream string) serviceHealthResult {
	started := time.Now()
	result := serviceHealthResult{CheckedAt: started.UTC()}
	finish := func() serviceHealthResult { result.DurationMS = time.Since(started).Milliseconds(); return result }
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(upstream))
	if err != nil || host == "" {
		result.State, result.Label, result.Message = "invalid", "地址无效", "TCP 目标地址须包含主机和端口"
		return finish()
	}
	port, portErr := strconv.Atoi(portStr)
	if portErr != nil || port < 1 || port > 65535 {
		result.State, result.Label, result.Message = "invalid", "端口无效", "TCP 目标端口须为 1–65535"
		return finish()
	}
	probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupNetIP(probeCtx, "ip", host)
	if err != nil {
		result.State, result.Message = healthError(err)
		result.Label = map[string]string{"dns_error": "DNS 异常", "timeout": "响应超时", "connection_error": "连接失败", "cancelled": "已取消"}[result.State]
		if result.Label == "" {
			result.Label = "连接失败"
		}
		return finish()
	}
	for _, ip := range ips {
		ip = ip.Unmap()
		if ip.IsUnspecified() || !allowedServiceIP(ip) {
			result.State, result.Label, result.Message = "invalid", "地址受限", "禁止探测受限或保留地址"
			return finish()
		}
	}
	var lastErr error
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	for _, ip := range ips {
		conn, dialErr := dialer.DialContext(probeCtx, "tcp", net.JoinHostPort(ip.String(), portStr))
		if dialErr == nil {
			_ = conn.Close()
			result.State, result.Label, result.Message, result.Healthy = "healthy", "TCP 可达", "TCP 端口握手成功", true
			return finish()
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = errors.New("no resolved addresses")
	}
	result.State, result.Message = healthError(lastErr)
	result.Label = map[string]string{"tls_untrusted": "证书异常", "dns_error": "DNS 异常", "timeout": "响应超时", "connection_error": "连接失败", "cancelled": "已取消"}[result.State]
	if result.Label == "" {
		result.Label = "连接失败"
	}
	return finish()
}

// Verify every redirect before allowing a browser probe to follow it, without
// changing ordinary health checks or exposing untrusted TLS to a WebView.
func (a *Admin) browserProbeHealth(ctx context.Context, raw string) serviceHealthResult {
	return a.browserProbeHealthWithPolicy(ctx, raw, "", nil, nil)
}

func (a *Admin) browserProbeHealthWithPolicy(ctx context.Context, raw, cachePrefix string, blockedPorts map[int]bool, localAddrs map[netip.Addr]bool) serviceHealthResult {
	result := a.cachedServiceHealthWithPolicy(ctx, raw, time.Minute, cachePrefix, blockedPorts, localAddrs)
	last := result
	for hops := 0; last.State == "redirect" && last.redirectURL != "" && hops < 3; hops++ {
		last = a.cachedServiceHealthWithPolicy(ctx, last.redirectURL, time.Minute, cachePrefix, blockedPorts, localAddrs)
	}
	if last.Status == 0 || last.State == "redirect" {
		return last
	}
	result.BrowserProbeAllowed = true
	return result
}

func (a *Admin) cachedServiceHealth(ctx context.Context, raw string, ttl time.Duration) serviceHealthResult {
	return a.cachedServiceHealthWithOptions(ctx, raw, "", ttl, "", nil, nil)
}

func (a *Admin) cachedServiceHealthForRoute(ctx context.Context, raw, requestHost string, ttl time.Duration) serviceHealthResult {
	return a.cachedServiceHealthWithOptions(ctx, raw, requestHost, ttl, "", nil, nil)
}

func (a *Admin) cachedServiceHealthWithPolicy(ctx context.Context, raw string, ttl time.Duration, cachePrefix string, blockedPorts map[int]bool, localAddrs map[netip.Addr]bool) serviceHealthResult {
	return a.cachedServiceHealthWithOptions(ctx, raw, "", ttl, cachePrefix, blockedPorts, localAddrs)
}

func (a *Admin) cachedServiceHealthWithOptions(ctx context.Context, raw, requestHost string, ttl time.Duration, cachePrefix string, blockedPorts map[int]bool, localAddrs map[netip.Addr]bool) serviceHealthResult {
	if target, err := url.Parse(raw); err == nil && isBlockedInternalTargetURL(target, blockedPorts, localAddrs) {
		return serviceHealthResult{State: "invalid", Label: "地址受限", Message: "禁止探测 GateHome 管理或内部服务端口", CheckedAt: time.Now().UTC()}
	}
	cacheKey := cachePrefix + raw
	if requestHost != "" {
		cacheKey += "|host=" + strings.ToLower(requestHost)
	}
	cache := &a.health
	cache.mu.Lock()
	if cache.results == nil {
		cache.results = map[string]serviceHealthResult{}
		cache.pending = map[string]*healthPending{}
		cache.slots = make(chan struct{}, 6)
	}
	if result, ok := cache.results[cacheKey]; ok && time.Since(result.CheckedAt) < ttl {
		cache.mu.Unlock()
		return result
	}
	if pending := cache.pending[cacheKey]; pending != nil {
		cache.mu.Unlock()
		select {
		case <-pending.done:
			return pending.result
		case <-ctx.Done():
			return serviceHealthResult{State: "cancelled", Label: "已取消"}
		}
	}
	pending := &healthPending{done: make(chan struct{})}
	cache.pending[cacheKey] = pending
	slots := cache.slots
	cache.mu.Unlock()
	go func() {
		probeCtx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		var result serviceHealthResult
		select {
		case slots <- struct{}{}:
			result = probeServiceHealthWithOptions(probeCtx, raw, requestHost, blockedPorts, localAddrs)
			<-slots
		case <-probeCtx.Done():
			result = serviceHealthResult{State: "cancelled", Label: "已取消"}
		}
		cache.mu.Lock()
		pending.result = result
		if result.State != "cancelled" {
			cache.results[cacheKey] = result
		}
		for key, row := range cache.results {
			if time.Since(row.CheckedAt) > 30*time.Minute {
				delete(cache.results, key)
			}
		}
		delete(cache.pending, cacheKey)
		close(pending.done)
		cache.mu.Unlock()
	}()
	select {
	case <-pending.done:
		return pending.result
	case <-ctx.Done():
		return serviceHealthResult{State: "cancelled", Label: "已取消"}
	}
}

func (a *Admin) cachedTCPHealth(ctx context.Context, upstream string, ttl time.Duration) serviceHealthResult {
	cacheKey := "tcp|" + strings.TrimSpace(upstream)
	cache := &a.health
	cache.mu.Lock()
	if cache.results == nil {
		cache.results = map[string]serviceHealthResult{}
		cache.pending = map[string]*healthPending{}
		cache.slots = make(chan struct{}, 6)
	}
	if result, ok := cache.results[cacheKey]; ok && time.Since(result.CheckedAt) < ttl {
		cache.mu.Unlock()
		return result
	}
	if pending := cache.pending[cacheKey]; pending != nil {
		cache.mu.Unlock()
		select {
		case <-pending.done:
			return pending.result
		case <-ctx.Done():
			return serviceHealthResult{State: "cancelled", Label: "已取消"}
		}
	}
	pending := &healthPending{done: make(chan struct{})}
	cache.pending[cacheKey] = pending
	slots := cache.slots
	cache.mu.Unlock()
	go func() {
		probeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var result serviceHealthResult
		select {
		case slots <- struct{}{}:
			result = probeTCPHealth(probeCtx, upstream)
			<-slots
		case <-probeCtx.Done():
			result = serviceHealthResult{State: "cancelled", Label: "已取消"}
		}
		cache.mu.Lock()
		pending.result = result
		if result.State != "cancelled" {
			cache.results[cacheKey] = result
		}
		for key, row := range cache.results {
			if time.Since(row.CheckedAt) > 30*time.Minute {
				delete(cache.results, key)
			}
		}
		delete(cache.pending, cacheKey)
		close(pending.done)
		cache.mu.Unlock()
	}()
	select {
	case <-pending.done:
		return pending.result
	case <-ctx.Done():
		return serviceHealthResult{State: "cancelled", Label: "已取消"}
	}
}

func (a *Admin) forwardServiceHealth(ctx context.Context) map[string]serviceHealthResult {
	config := a.store.Snapshot().Config
	options := serviceHealthOptions(config)
	results := map[string]serviceHealthResult{}
	var mu sync.Mutex
	var work sync.WaitGroup
	for _, forward := range config.Forwards {
		if !options.Enabled || !forward.Enabled {
			mu.Lock()
			results[forward.ID] = serviceHealthResult{State: "disabled", Label: "未检测"}
			mu.Unlock()
			continue
		}
		if strings.EqualFold(forward.Protocol, "udp") {
			mu.Lock()
			results[forward.ID] = serviceHealthResult{
				State:   "udp_unsupported",
				Label:   "UDP 不探测",
				Message: "UDP 无连接协议不支持主动握手探测",
			}
			mu.Unlock()
			continue
		}
		work.Add(1)
		go func(forward PortForward) {
			defer work.Done()
			result := a.cachedTCPHealth(ctx, forward.Upstream, time.Duration(options.Interval)*time.Second)
			mu.Lock()
			results[forward.ID] = result
			mu.Unlock()
			a.recordForwardHealthTransition(forward, result)
		}(forward)
	}
	work.Wait()
	return results
}

func (a *Admin) routeServiceHealth(ctx context.Context) map[string]serviceHealthResult {
	config := a.store.Snapshot().Config
	options := serviceHealthOptions(config)
	results := map[string]serviceHealthResult{}
	var mu sync.Mutex
	var work sync.WaitGroup
	for _, route := range config.Routes {
		group, exists := config.Group(route.GroupID)
		if route.Enabled && exists && group.Enabled && route.Maintenance {
			a.pauseHealthEvent("route/" + route.ID)
			a.pauseHealthEvent(route.ID)
			mu.Lock()
			results[route.ID] = serviceHealthResult{
				State:   "maintenance",
				Label:   "维护中",
				Message: "已开启维护模式，健康探测与故障告警已暂停",
			}
			mu.Unlock()
			continue
		}
		if !options.Enabled || !route.Enabled || !exists || !group.Enabled {
			mu.Lock()
			results[route.ID] = serviceHealthResult{State: "disabled", Label: "未检测"}
			mu.Unlock()
			continue
		}
		work.Add(1)
		go func(route Route) {
			defer work.Done()
			result := a.cachedServiceHealthForRoute(ctx, route.Upstream, route.Host, time.Duration(options.Interval)*time.Second)
			mu.Lock()
			results[route.ID] = result
			mu.Unlock()
			a.recordHealthTransition(route, result)
		}(route)
	}
	work.Wait()
	return results
}

func (a *Admin) pauseHealthEvent(key string) {
	cache := &a.health
	cache.mu.Lock()
	if previous, ok := cache.events[key]; ok {
		previous.failures = 0
		previous.firstFailedAt = time.Time{}
		previous.down = false
		cache.events[key] = previous
	}
	cache.mu.Unlock()
}

func (a *Admin) recordHealthTransition(route Route, result serviceHealthResult) {
	if result.State == "cancelled" {
		return
	}
	alertDelaySeconds := 0
	if a.store != nil {
		alertDelaySeconds = serviceHealthOptions(a.store.Snapshot().Config).AlertDelaySeconds
	}
	cache := &a.health
	cache.mu.Lock()
	previous := cache.events[route.ID]
	// Only one observation per completed check counts toward the failure threshold.
	if !result.CheckedAt.After(previous.result.CheckedAt) {
		cache.mu.Unlock()
		return
	}
	if previous.upstream != route.Upstream {
		previous = healthEventState{upstream: route.Upstream, down: previous.down}
	}
	previous.result = result
	a.recordHealthSample(route, result)
	message := ""
	alertFiringMessage := "反代上游连续检测失败 · " + result.Label
	if result.Healthy {
		if previous.down {
			message = "服务已恢复"
		}
		previous.failures = 0
		previous.firstFailedAt = time.Time{}
		previous.down = false
	} else {
		previous.failures++
		if previous.firstFailedAt.IsZero() {
			previous.firstFailedAt = result.CheckedAt
		}
		reached := previous.failures >= 2
		if alertDelaySeconds > 0 {
			reached = result.CheckedAt.Sub(previous.firstFailedAt) >= time.Duration(alertDelaySeconds)*time.Second
			alertFiringMessage = "反代上游持续不可达已满 " + formatHealthDelay(alertDelaySeconds) + " · " + result.Label
		}
		if reached && !previous.down {
			previous.down = true
			if alertDelaySeconds > 0 {
				message = "服务持续不可达已满 " + formatHealthDelay(alertDelaySeconds)
			} else {
				message = "服务连续检测失败"
			}
		}
	}
	if cache.events == nil {
		cache.events = make(map[string]healthEventState)
	}
	cache.events[route.ID] = previous
	if message != "" {
		cache.notices = append(cache.notices, healthNotice{RouteID: route.ID, Time: result.CheckedAt, Message: message + " · " + result.Label, Healthy: result.Healthy})
		if len(cache.notices) > 60 {
			cache.notices = append([]healthNotice{}, cache.notices[len(cache.notices)-60:]...)
		}
	}
	// Record in observation order, including the first healthy sample after a restart.
	shouldAlert := (result.Healthy || previous.down) && a.store != nil
	cache.mu.Unlock()
	if shouldAlert {
		a.observeAlert("route/"+route.ID, "upstream", route.Host, !result.Healthy, alertFiringMessage, "反代上游已恢复", result.CheckedAt)
	}
	if message != "" && a.logs != nil {
		a.logs.Add(LogEntry{Category: "admin", Action: "服务健康", Target: route.ID, OK: result.Healthy, DurationMS: result.DurationMS, Message: message + " · " + result.Label})
	}
}

func (a *Admin) recordForwardHealthTransition(forward PortForward, result serviceHealthResult) {
	if result.State == "cancelled" {
		return
	}
	alertDelaySeconds := 0
	if a.store != nil {
		alertDelaySeconds = serviceHealthOptions(a.store.Snapshot().Config).AlertDelaySeconds
	}
	eventKey := "forward/" + forward.ID
	cache := &a.health
	cache.mu.Lock()
	previous := cache.events[eventKey]
	if !result.CheckedAt.After(previous.result.CheckedAt) {
		cache.mu.Unlock()
		return
	}
	if previous.upstream != forward.Upstream {
		previous = healthEventState{upstream: forward.Upstream, down: previous.down}
	}
	previous.result = result
	message := ""
	alertFiringMessage := "TCP 端口转发上游连续检测失败 · " + result.Label
	if result.Healthy {
		if previous.down {
			message = "TCP 端口转发上游已恢复"
		}
		previous.failures = 0
		previous.firstFailedAt = time.Time{}
		previous.down = false
	} else {
		previous.failures++
		if previous.firstFailedAt.IsZero() {
			previous.firstFailedAt = result.CheckedAt
		}
		reached := previous.failures >= 2
		if alertDelaySeconds > 0 {
			reached = result.CheckedAt.Sub(previous.firstFailedAt) >= time.Duration(alertDelaySeconds)*time.Second
			alertFiringMessage = "TCP 端口转发上游持续不可达已满 " + formatHealthDelay(alertDelaySeconds) + " · " + result.Label
		}
		if reached && !previous.down {
			previous.down = true
			if alertDelaySeconds > 0 {
				message = "TCP 端口转发持续不可达已满 " + formatHealthDelay(alertDelaySeconds)
			} else {
				message = "TCP 端口转发连续检测失败"
			}
		}
	}
	if cache.events == nil {
		cache.events = make(map[string]healthEventState)
	}
	cache.events[eventKey] = previous
	if message != "" {
		cache.notices = append(cache.notices, healthNotice{RouteID: eventKey, Time: result.CheckedAt, Message: message + " · " + result.Label, Healthy: result.Healthy})
		if len(cache.notices) > 60 {
			cache.notices = append([]healthNotice{}, cache.notices[len(cache.notices)-60:]...)
		}
	}
	shouldAlert := (result.Healthy || previous.down) && a.store != nil
	cache.mu.Unlock()
	resource := forward.Name
	if resource == "" {
		resource = forward.Upstream
	} else {
		resource += " (" + forward.Upstream + ")"
	}
	if shouldAlert {
		a.observeAlert(eventKey, "upstream", resource, !result.Healthy, alertFiringMessage, "TCP 端口转发上游已恢复", result.CheckedAt)
	}
	if message != "" && a.logs != nil {
		a.logs.Add(LogEntry{Category: "admin", Action: "端口转发健康", Target: forward.ID, OK: result.Healthy, DurationMS: result.DurationMS, Message: message + " · " + result.Label})
	}
}

func (a *Admin) StartServiceHealth(ctx context.Context) {
	go func() {
		a.routeServiceHealth(ctx)
		a.forwardServiceHealth(ctx)
		a.flushHealthHistory()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		defer a.flushHealthHistory()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if serviceHealthOptions(a.store.Snapshot().Config).Enabled {
					a.routeServiceHealth(ctx)
					a.forwardServiceHealth(ctx)
				}
				a.flushHealthHistory()
			}
		}
	}()
}

func (a *Admin) serviceHealthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/service-health/{id}/history", a.requireAuth(a.routeHistory))
	mux.HandleFunc("GET /api/service-health", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		routes := a.routeServiceHealth(r.Context())
		forwards := a.forwardServiceHealth(r.Context())
		a.health.mu.Lock()
		notices := append([]healthNotice{}, a.health.notices...)
		a.health.mu.Unlock()
		jsonResponse(w, 200, map[string]any{
			"options":  serviceHealthOptions(a.store.Snapshot().Config),
			"routes":   routes,
			"forwards": forwards,
			"notices":  notices,
		})
	}))
}

func (a *Admin) authenticatedPanelUser(r *http.Request) (uint, error) {
	if a.sunPanel == nil || r.Header.Get("token") == "" {
		return 0, errors.New("请先登录 Sun-Panel")
	}
	if !a.sunPanel.mu.TryRLock() {
		return 0, errors.New("Sun-Panel 正在启动或备份，请稍后重试")
	}
	address := a.sunPanel.bridgeURL
	a.sunPanel.mu.RUnlock()
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
	blockedPorts, localAddrs := a.forbiddenInternalPorts()
	results := make([]panelSiteHealth, len(selected))
	var work sync.WaitGroup
	for i, item := range selected {
		work.Add(1)
		go func(i int, itemID uint, defaultURL, lanURL string) {
			defer work.Done()
			row := panelSiteHealth{ID: itemID, URL: defaultURL, LanURL: lanURL}
			if !input.TLSOnly {
				row.Default = a.cachedServiceHealthWithPolicy(r.Context(), defaultURL, time.Minute, "panel|", blockedPorts, localAddrs)
			}
			if lanURL != "" {
				if input.TLSOnly {
					row.LAN = a.browserProbeHealthWithPolicy(r.Context(), lanURL, "panel|", blockedPorts, localAddrs)
				} else {
					row.LAN = a.cachedServiceHealthWithPolicy(r.Context(), lanURL, time.Minute, "panel|", blockedPorts, localAddrs)
				}
			}
			results[i] = row
		}(i, item.ID, item.URL, item.LanURL)
	}
	work.Wait()
	jsonResponse(w, 200, map[string]any{"code": 0, "data": results})
}
