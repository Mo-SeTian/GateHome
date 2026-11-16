package gateway

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Only configured SunPanel entrances may receive bookmark data from the console.
func sunPanelOrigins(c Config) map[string]bool {
	origins := map[string]bool{}
	add := func(value string) {
		u, err := url.Parse(value)
		if err == nil && u.User == nil {
			if origin, ok := adminOrigin(u.Scheme + "://" + u.Host); ok {
				origins[origin] = true
			}
		}
	}
	add(c.SunPanel.ExternalURL)
	add(c.SunPanel.LaunchURL)
	addresses := localAdminAddresses()
	for _, route := range sunPanelRouteList(c) {
		u, _ := url.Parse(route["lanUrl"])
		if u != nil && u.Scheme == "http" && isAdminUpstream(u, c.SunPanel.Port, addresses) {
			add(route["url"])
		}
	}
	return origins
}

func (a *Admin) sunPanelOriginAllowed(r *http.Request, value string) bool {
	origin, ok := adminOrigin(value)
	if !ok {
		return false
	}
	c := a.store.Snapshot().Config
	if !c.SunPanel.Enabled {
		return false
	}
	if sunPanelOrigins(c)[origin] {
		return true
	}
	// Native listeners on the same host also support the connection window.
	target, _ := url.Parse(origin)
	host, _, _ := net.SplitHostPort(r.Host)
	if host == "" {
		host = r.Host
	}
	port := target.Port()
	if port == "" {
		port = "80"
	}
	return target.Scheme == "http" && port == strconv.Itoa(c.SunPanel.Port) && strings.EqualFold(target.Hostname(), host)
}

func (a *Admin) sunPanelConnectionInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("X-Gatehouse-Request") != "1" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") || r.Header.Get("Sec-Fetch-Site") == "cross-site" || !a.sunPanelOriginAllowed(r, r.Header.Get("Origin")) {
		apiError(w, 403, "请在 GateHome 的浏览器首页设置中配置 SunPanel 外网访问地址，再重新连接")
		return
	}
	if !decodeBody(w, r, &struct{}{}) {
		return
	}
	if _, err := a.authenticatedPanelUser(r); err != nil {
		apiError(w, 401, err.Error())
		return
	}
	origins := []string{}
	access := a.store.Snapshot().Config.AdminAccess
	if access.Enabled {
		for _, value := range access.Origins {
			if origin, ok := adminOrigin(value); ok {
				origins = append(origins, origin)
			}
		}
	}
	if len(origins) == 0 {
		host, _, _ := net.SplitHostPort(r.Host)
		if host == "" {
			host = r.Host
		}
		if origin, ok := adminOrigin("http://" + net.JoinHostPort(host, strconv.Itoa(a.adminPort))); ok {
			origins = append(origins, origin)
		}
	}
	jsonResponse(w, 200, map[string]any{"origins": origins})
}

func (a *Admin) sunPanelConnect(w http.ResponseWriter, r *http.Request) {
	if r.Context().Value(mcpInternalKey{}) == true {
		apiError(w, 403, "请使用 GateHome 管理会话连接")
		return
	}
	var input struct {
		Origin string `json:"origin"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	if !a.sunPanelOriginAllowed(r, input.Origin) {
		apiError(w, 403, "此 SunPanel 来源未配置，请检查浏览器首页外网访问地址或反代路由")
		return
	}
	a.sunPanelRoutes(w, r)
}
