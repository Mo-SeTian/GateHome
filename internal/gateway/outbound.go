package gateway

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

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
	return nil
}

func outboundClient(s State, timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if s.Config.OutboundProxy.Enabled {
		u, err := url.Parse(s.Config.OutboundProxy.URL)
		if err == nil && u.Hostname() != "" {
			if s.Config.OutboundProxy.Username != "" {
				u.User = url.UserPassword(s.Config.OutboundProxy.Username, s.ProxyPassword)
			}
			transport.Proxy = http.ProxyURL(u)
		}
	}
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
