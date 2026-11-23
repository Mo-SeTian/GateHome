package gateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	mihomoOutboundMu       sync.RWMutex
	mihomoOutboundResolver func() string
)

// SetMihomoOutboundResolver registers a dynamic resolver returning the local mihomo proxy URL
// when built-in takeover is enabled and running. Returns a cleanup function to restore the prior resolver.
func SetMihomoOutboundResolver(fn func() string) func() {
	mihomoOutboundMu.Lock()
	prev := mihomoOutboundResolver
	mihomoOutboundResolver = fn
	mihomoOutboundMu.Unlock()
	return func() {
		mihomoOutboundMu.Lock()
		mihomoOutboundResolver = prev
		mihomoOutboundMu.Unlock()
	}
}

func validateOutboundProxy(p OutboundProxyConfig) error {
	if len(p.Username) > 255 || strings.ContainsAny(p.Username, "\r\n") {
		return errors.New("代理用户名无效")
	}
	if p.URL == "" && !p.Enabled {
		return nil
	}
	u, err := url.Parse(p.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || len(p.URL) > 1024 {
		return errors.New("代理地址须为 http / https / socks5(socks5h)://主机:端口，不含账号、路径或参数；账号密码请单独填写")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return errors.New("代理地址需填写 1–65535 的端口")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil {
		if ip.Zone() != "" || !allowedServiceIP(ip.Unmap()) {
			return errors.New("代理地址不允许使用受限或元数据 IP")
		}
	}
	return nil
}

func safeDialContext(dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
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
			if !allowedServiceIP(ip) {
				return nil, errors.New("目标地址不允许访问")
			}
		}
		var last error
		for _, ip := range ips {
			connection, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return connection, nil
			}
			last = err
		}
		if last == nil {
			last = errors.New("目标地址无法连接")
		}
		return nil, last
	}
}

func outboundClient(s State, timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = safeDialContext(transport.DialContext)
	// Tier 1: Built-in mihomo proxy when takeover_outbound is active and core is running.
	mihomoOutboundMu.RLock()
	resolver := mihomoOutboundResolver
	mihomoOutboundMu.RUnlock()
	if resolver != nil {
		if proxyURL := resolver(); proxyURL != "" {
			if u, err := url.Parse(proxyURL); err == nil && u.Hostname() != "" {
				transport.Proxy = http.ProxyURL(u)
				return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(r *http.Request, via []*http.Request) error { return errors.New("不允许重定向") }}
			}
		}
	}
	// Tier 2: System Settings manual outbound proxy.
	if s.Config.OutboundProxy.Enabled {
		u, err := url.Parse(s.Config.OutboundProxy.URL)
		if err == nil && u.Hostname() != "" {
			if s.Config.OutboundProxy.Username != "" {
				u.User = url.UserPassword(s.Config.OutboundProxy.Username, s.ProxyPassword)
			}
			transport.Proxy = http.ProxyURL(u)
		}
	}
	// Tier 3: Direct connection (transport.Proxy == nil).
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(r *http.Request, via []*http.Request) error { return errors.New("不允许重定向") }}
}

func closeClient(c *http.Client) { c.CloseIdleConnections() }

type loggedTransport struct {
	base             http.RoundTripper
	logs             *Logs
	category, action string
}

func (t loggedTransport) RoundTrip(r *http.Request) (resp *http.Response, err error) {
	started := time.Now()
	resp, err = t.base.RoundTrip(r)
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	message := "HTTP 请求成功"
	if err != nil || status >= 400 {
		message = "HTTP 请求失败"
	}
	t.logs.Add(LogEntry{Category: t.category, Action: t.action, Target: r.URL.Scheme + "://" + r.URL.Host, Method: r.Method, Path: r.URL.Path, Status: status, OK: err == nil && status < 400, DurationMS: time.Since(started).Milliseconds(), Message: message})
	return resp, err
}
func (t loggedTransport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}
func instrumentClient(c *http.Client, logs *Logs, category, action string) *http.Client {
	c.Transport = loggedTransport{base: c.Transport, logs: logs, category: category, action: action}
	return c
}

func allowedServiceIP(ip netip.Addr) bool {
	return (ip.IsGlobalUnicast() || ip.IsLoopback()) && !ip.IsLinkLocalUnicast() && ip != netip.MustParseAddr("100.100.100.200") && ip != netip.MustParseAddr("fd00:ec2::254")
}

func serviceEndpointClient(state State, timeout time.Duration) *http.Client {
	client := outboundClient(state, timeout)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// Connect directly to LAN services and pin resolved addresses when dialing.
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil {
		proxy := transport.Proxy
		transport.Proxy = func(r *http.Request) (*url.URL, error) {
			addresses, err := net.DefaultResolver.LookupNetIP(r.Context(), "ip", r.URL.Hostname())
			if err != nil {
				return nil, errors.New("目标域名解析失败")
			}
			local := false
			for _, ip := range addresses {
				ip = ip.Unmap()
				if !allowedServiceIP(ip) {
					return nil, errors.New("目标地址不允许访问")
				}
				local = local || ip.IsPrivate() || ip.IsLoopback()
			}
			if local {
				return nil, nil
			}
			return proxy(r)
		}
	}
	return client
}
