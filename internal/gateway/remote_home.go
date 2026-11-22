package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

const wireGuardProxyIP = "10.254.254.254"

var wireGuardClient = &http.Client{
	Timeout: 12 * time.Second,
	Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/run/gatehome-wireguard/control.sock")
	}},
}

type wireGuardInput struct {
	Endpoint  string `json:"endpoint,omitempty"`
	Port      int    `json:"port,omitempty"`
	LAN       string `json:"lan,omitempty"`
	Tunnel    string `json:"tunnel,omitempty"`
	SNAT      bool   `json:"snat"`
	AdminPort int    `json:"admin_port,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Target    string `json:"target,omitempty"`
	Route     string `json:"route,omitempty"`
	Host      string `json:"host,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
	Admin     bool   `json:"admin"`
}

func wireGuardCall(ctx context.Context, op string, input any) (map[string]any, int, error) {
	method := http.MethodGet
	var payload []byte
	if input != nil {
		method = http.MethodPost
		var err error
		payload, err = json.Marshal(input)
		if err != nil {
			return nil, 400, errors.New("WireGuard 请求无法编码")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://localhost/"+op, bytes.NewReader(payload))
	if err != nil {
		return nil, 500, err
	}
	resp, err := wireGuardClient.Do(req)
	if err != nil {
		return nil, 503, errors.New("无法连接 WireGuard 宿主机服务，请检查安装、套接字挂载和用户组")
	}
	defer resp.Body.Close()
	var data map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&data); err != nil || data == nil {
		return nil, 502, errors.New("WireGuard 宿主机响应无效")
	}
	if resp.StatusCode != 200 {
		msg, _ := data["error"].(string)
		if msg == "" {
			msg = "WireGuard 操作失败"
		}
		return nil, resp.StatusCode, errors.New(msg)
	}
	return data, 200, nil
}

type homeRoute struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

func (a *Admin) homeRoutes() []homeRoute {
	cfg := a.store.Snapshot().Config
	routes := []homeRoute{}
	addrs := localAdminAddresses()
	for _, r := range cfg.Routes {
		g, ok := cfg.Group(r.GroupID)
		upstream, err := url.Parse(r.Upstream)
		if !ok || !cfg.RouteActive(r) || r.ID == "" || err != nil || isAdminUpstream(upstream, a.adminPort, addrs) {
			continue
		}
		p := g.HTTPPort
		if r.TLS || p == 0 {
			p = g.HTTPSPort
		}
		if p != 0 {
			routes = append(routes, homeRoute{r.ID, r.Name, r.Host, p})
		}
	}
	return routes
}

func (a *Admin) remoteHomeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/remote-home", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		data, code, err := wireGuardCall(r.Context(), "status", nil)
		if err != nil {
			if code != 503 {
				apiError(w, code, err.Error())
				return
			}
			data = map[string]any{"available": false, "error": err.Error()}
		}
		hosts := []string{}
		for _, g := range a.store.Snapshot().Config.DDNS.Groups {
			if g.Enabled {
				hosts = append(hosts, g.Hosts...)
			}
		}
		data["ddns"] = hosts
		if _, err := net.InterfaceByName("gh-wg0"); err == nil {
			data["routes"] = a.homeRoutes()
		}
		jsonResponse(w, 200, data)
	}))
	for _, op := range []string{"settings", "add", "edit", "remove"} {
		mux.HandleFunc("POST /api/remote-home/"+op, a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
			var input wireGuardInput
			if !decodeBody(w, r, &input) {
				return
			}
			input.AdminPort = a.adminPort
			if (op == "edit" || op == "add") && input.Mode == "route" {
				found := false
				if _, err := net.InterfaceByName("gh-wg0"); err == nil {
					for _, rule := range a.homeRoutes() {
						if rule.ID == input.Route {
							input.Host, input.Port, found = rule.Host, rule.Port, true
							break
						}
					}
				}
				if !found {
					apiError(w, 400, "请选择当前可用的反代规则（需要宿主机网络模式）")
					return
				}
			}
			data, code, err := wireGuardCall(r.Context(), op, input)
			if err != nil {
				apiError(w, code, err.Error())
				return
			}
			jsonResponse(w, 200, data)
		}))
	}
}

// Use the physical TCP peer, before any trusted-proxy header rewriting.
func wireGuardAdminAllowed(r *http.Request, remote string) bool {
	peer, err := netip.ParseAddrPort(remote)
	addr := peer.Addr().Unmap()
	if err != nil || !addr.Is4() || !addr.IsPrivate() {
		return true
	}
	iface, err := net.InterfaceByName("gh-wg0")
	if err != nil {
		return true
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addresses {
		prefix, err := netip.ParsePrefix(a.String())
		if err == nil && prefix.Contains(addr) {
			data, _, err := wireGuardCall(r.Context(), "authorize", map[string]any{"address": addr.String(), "management": true})
			return err == nil && data["allowed"] == true
		}
	}
	return true
}

func wireGuardRouteAllowed(r *http.Request, peer string, route proxyRoute) bool {
	if route.adminUpstream && !wireGuardAdminAllowed(r, peer) {
		return false
	}
	addr, ok := r.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr)
	if !ok || addr.IP.String() != wireGuardProxyIP {
		return true
	}
	if _, err := net.InterfaceByName("gh-wg0"); err != nil {
		return true
	}
	if route.adminUpstream {
		return false
	}
	ip, _, err := net.SplitHostPort(peer)
	if err != nil {
		return false
	}
	data, _, err := wireGuardCall(r.Context(), "authorize", map[string]string{"address": ip, "route": route.route.ID})
	return err == nil && data["allowed"] == true
}

func wireGuardAssistant(parent context.Context) any {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	data, _, err := wireGuardCall(ctx, "status", nil)
	if err != nil {
		return map[string]bool{"available": false}
	}
	snapshot := map[string]any{}
	for _, field := range []string{"available", "running", "listen_port", "forwarding", "missing", "fault"} {
		snapshot[field] = data[field]
	}
	devices := []map[string]any{}
	if list, ok := data["devices"].([]any); ok {
		for _, item := range list {
			peer, ok := item.(map[string]any)
			if !ok {
				continue
			}
			summary := map[string]any{}
			for _, field := range []string{"name", "address", "mode", "target", "port", "host", "admin", "handshake", "rx", "tx"} {
				summary[field] = peer[field]
			}
			devices = append(devices, summary)
		}
	}
	snapshot["devices"] = devices
	if settings, ok := data["settings"].(map[string]any); ok {
		snapshot["lan"] = settings["lan"]
		snapshot["tunnel"] = settings["tunnel"]
	}
	return snapshot
}
