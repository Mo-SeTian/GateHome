package gateway

import (
	"fmt"
	"golang.org/x/sync/semaphore"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type proxyRoute struct {
	route     Route
	handler   http.Handler
	firewall  *compiledFirewall
	groupName string
	auth      *routeGate
}

type Proxy struct {
	routes        atomic.Value
	firewalls     atomic.Value
	Requests      atomic.Uint64
	Blocked       atomic.Uint64
	Failures      atomic.Uint64
	subscriptions *Subscriptions
	logs          *Logs
	defense       *IPDefense
	wafSlots      chan struct{}
	wafMemory     *semaphore.Weighted
	adminPort     int
}

func NewProxy(c Config, subscriptions *Subscriptions) *Proxy {
	p := &Proxy{subscriptions: subscriptions, defense: newIPDefense(nil), wafSlots: make(chan struct{}, 16), wafMemory: semaphore.NewWeighted(128 << 20)}
	p.routes.Store(map[string]proxyRoute{})
	p.Configure(c)
	return p
}

func (p *Proxy) Configure(c Config) error { return p.configure(c, nil) }

func (p *Proxy) ConfigureState(s State) error { return p.configure(s.Config, s.RoutePasswordHashes) }

func (p *Proxy) configure(c Config, hashes map[string]string) error {
	firewalls, err := p.prepareFirewalls(c)
	if err != nil {
		return err
	}
	p.configurePrepared(c, hashes, firewalls)
	return nil
}
func (p *Proxy) configurePrepared(c Config, hashes map[string]string, firewalls map[string]*compiledFirewall) {
	previous, _ := p.routes.Load().(map[string]proxyRoute)
	routes := make(map[string]proxyRoute)
	adminAddresses := localAdminAddresses()
	for _, r := range c.Routes {
		if !c.RouteActive(r) {
			continue
		}
		u, _ := url.Parse(r.Upstream)
		upstream := u
		adminUpstream := isAdminUpstream(upstream, p.adminPort, adminAddresses)
		h := &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(upstream)
				// HTTPS upstreams use their own name for both Host and SNI.
				if upstream.Scheme == "http" {
					pr.Out.Host = pr.In.Host
				}
				pr.Out.Header.Del("X-Real-IP")
				pr.Out.Header.Del("CF-Connecting-IP")
				stripRouteCookies(pr.Out)
				if !adminUpstream {
					stripAdminCookies(pr.Out)
				}
				pr.SetXForwarded()
			},
			Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ForceAttemptHTTP2: true, MaxIdleConns: 100, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 60 * time.Second},
			ModifyResponse: func(response *http.Response) error {
				if !adminUpstream {
					stripAdminResponseCookies(response)
				}
				return stripRouteResponseCookies(response)
			},
			ErrorLog: log.New(io.Discard, "", 0),
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				p.Failures.Add(1)
				http.Error(w, "后端服务暂不可用", http.StatusBadGateway)
			},
		}
		group, _ := c.Group(r.GroupID)
		entry := proxyRoute{route: r, handler: h, firewall: firewalls[r.FirewallID], groupName: group.Name}
		if r.Auth.Enabled {
			key := routeKey(r)
			entry.auth = previous[key].auth
			if entry.auth == nil || entry.auth.username != r.Auth.Username || entry.auth.passwordHash != hashes[key] || entry.auth.defense != p.defense {
				entry.auth = newRouteGate(key, r.Auth.Username, hashes[key])
				entry.auth.defense = p.defense
			}
		}
		routes[routeKey(r)] = entry
	}
	p.firewalls.Store(firewalls)
	old := p.routes.Swap(routes)
	if old != nil {
		for _, r := range old.(map[string]proxyRoute) {
			r.handler.(*httputil.ReverseProxy).Transport.(*http.Transport).CloseIdleConnections()
		}
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.serveGroup("default", w, r)
}

func (p *Proxy) Handler(groupID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { p.serveGroup(groupID, w, r) })
}

func (p *Proxy) serveGroup(groupID string, w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logged := &loggedResponseWriter{ResponseWriter: w}
	w = logged
	target := "未知域名"
	ruleKey := ""
	message := "未匹配到代理规则"
	outcome := "unmatched"
	var firewallHit *FirewallHit
	var securityHits []SecurityHit
	var route proxyRoute
	defer func() {
		status := logged.status
		if status == 0 {
			status = 200
		}
		if len(securityHits) > 0 && firewallHit == nil && route.firewall != nil {
			firewallHit = &FirewallHit{ID: route.firewall.id, Name: route.firewall.name, Kind: "protection", Reason: securityReason(securityHits[len(securityHits)-1])}
		}
		p.logs.Add(LogEntry{Category: "access", Action: "反向代理请求", Target: target, Rule: ruleKey, Method: r.Method, Path: r.URL.Path, Remote: remoteIP(r.RemoteAddr), Status: status, OK: status < 400, DurationMS: time.Since(started).Milliseconds(), Message: message, Outcome: outcome, AuthResult: logged.authResult, FreezeCreated: logged.freezeCreated, Firewall: firewallHit, Security: securityHits})
	}()
	p.Requests.Add(1)
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	route, ok := p.routes.Load().(map[string]proxyRoute)[groupID+"/"+host]
	if !ok {
		http.NotFound(w, r)
		return
	}
	target = route.route.Host
	ruleKey = groupID + "/" + route.route.Host
	message = "反代组：" + route.groupName
	if ip, valid := sourceIP(r.RemoteAddr); valid {
		if b, blocked := p.defense.blocked(ruleKey, ip); blocked {
			outcome = "ip_frozen"
			message += "；" + b.Reason
			p.Blocked.Add(1)
			frozenResponse(w, b)
			return
		}
	} else if route.auth != nil {
		outcome = "auth_rejected"
		message += "；无法验证来源 IP"
		http.Error(w, "无法验证来源 IP", http.StatusForbidden)
		return
	}
	if allowed, hit := route.firewallDecision(r.RemoteAddr); !allowed {
		firewallHit = &hit
		outcome = "blocked"
		message += "；防火墙拒绝来源 IP：" + hit.Reason
		p.Blocked.Add(1)
		http.Error(w, "访问被 IP 规则拒绝", http.StatusForbidden)
		return
	}
	if route.route.TLS && r.TLS == nil {
		outcome = "https_required"
		message += "；此规则要求 HTTPS"
		http.Error(w, "此服务只允许 HTTPS，请使用已配置的外网 HTTPS 端口", http.StatusUpgradeRequired)
		return
	}
	rejectProtection := func(code, retry int) bool {
		if code == 0 {
			return false
		}
		outcome = "blocked"
		p.Blocked.Add(1)
		hit := FirewallHit{ID: route.firewall.id, Name: route.firewall.name, Kind: "protection", Reason: securityReason(securityHits[len(securityHits)-1])}
		firewallHit = &hit
		message += "；" + hit.Reason
		if b, created := p.freezeProtection(route, r, securityHits); created {
			logged.freezeCreated = true
			frozenResponse(w, b)
			return true
		}
		if retry > 0 {
			w.Header().Set("Retry-After", fmt.Sprint(retry))
		}
		http.Error(w, "请求被服务防护策略拦截", code)
		return true
	}
	if route.firewall != nil && route.firewall.protection.active() {
		var code, retry int
		securityHits, code, retry = p.inspectProtection(route, r)
		if rejectProtection(code, retry) {
			return
		}
	}
	if route.auth == nil && strings.HasPrefix(r.URL.Path, routeAuthPath) {
		http.Error(w, "此服务未开启访问账号验证", http.StatusNotFound)
		return
	}
	if route.auth != nil && !route.auth.authorize(w, r, route.route) {
		outcome = logged.authResult
		if logged.freezeCreated {
			outcome = "ip_frozen"
		}
		message += "；" + logged.authMessage
		if logged.status >= 400 {
			p.Blocked.Add(1)
		}
		return
	}
	// Internal access credentials are handled by routeGate; CRS inspects only
	// requests about to be sent upstream, after successful access verification.
	if route.firewall != nil && route.firewall.waf != nil {
		matches, code, release := p.inspectRouteWAF(w, route, r)
		if release != nil {
			defer release()
		}
		securityHits = append(securityHits, matches...)
		if rejectProtection(code, 0) {
			return
		}
	}
	outcome = "forwarded"
	logged.authResult = ""
	route.handler.ServeHTTP(w, r)
}

func (r proxyRoute) allowed(remote string) bool {
	allowed, _ := r.firewallDecision(remote)
	return allowed
}

func (r proxyRoute) firewallDecision(remote string) (bool, FirewallHit) {
	if r.route.FirewallID == "" {
		return true, FirewallHit{}
	}
	if r.firewall == nil {
		return false, FirewallHit{ID: r.route.FirewallID, Kind: "unavailable", Reason: "绑定的防火墙不可用"}
	}
	host, ok := sourceIP(remote)
	addr, err := netip.ParseAddr(host)
	if !ok || err != nil {
		return false, FirewallHit{ID: r.firewall.id, Name: r.firewall.name, Kind: "invalid_source", Reason: "来源 IP 无效"}
	}
	return r.firewall.decision(addr.Unmap())
}

func (s *Proxy) SetLogs(logs *Logs) { s.logs = logs }
