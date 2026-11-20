package gateway

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
)

type exposureEntry struct {
	Service   string `json:"service"`
	Entrance  string `json:"entrance"`
	Address   string `json:"address,omitempty"`
	Host      string `json:"host,omitempty"`
	Transport string `json:"transport"`
	Login     string `json:"login"`
	Firewall  string `json:"firewall,omitempty"`
}

// Called after binding all HTTP listeners and before accepting requests.
func (a *Admin) SetHTTPListeners(listeners []net.Listener) {
	a.adminAddress = listeners[0].Addr().String()
	a.httpAddresses = make(map[int]string, len(listeners))
	for _, listener := range listeners {
		if addr, ok := listener.Addr().(*net.TCPAddr); ok {
			a.httpAddresses[addr.Port] = addr.String()
		}
	}
}

func (a *Admin) httpAddress(port int) string {
	if address := a.httpAddresses[port]; address != "" {
		return address
	}
	return listenAddress(port)
}

// Binding settings come from startup. Route rules and firewall names can change without a restart.
func (a *Admin) exposureOverview(w http.ResponseWriter, r *http.Request) {
	current := a.store.Snapshot().Config
	adminAddr := a.adminAddress
	if adminAddr == "" {
		adminAddr = listenAddress(a.adminPort)
	}
	admin := exposureEntry{Service: "管理台", Entrance: "直接监听", Address: adminAddr, Transport: "HTTP", Login: "GateHome 管理员"}
	rows := []exposureEntry{admin}
	appliedPanel := a.ports.SunPanel
	panelLogin := "SunPanel 登录 · 公开模式状态未知"
	if appliedPanel.Enabled {
		if a.sunPanel != nil {
			state, err := a.sunPanel.bridgeCall(r.Context(), "public_access", 0, nil)
			if err == nil {
				switch string(state) {
				case "true":
					panelLogin = "公开浏览 · 编辑需 SunPanel 登录"
				case "false":
					panelLogin = "SunPanel 登录"
				}
			}
		}
		rows = append(rows,
			exposureEntry{Service: "SunPanel", Entrance: "独立端口", Address: a.httpAddress(appliedPanel.Port), Transport: "HTTP", Login: panelLogin},
			exposureEntry{Service: "SunPanel", Entrance: "管理台内嵌", Address: adminAddr, Host: "/sunpanel/", Transport: "HTTP", Login: "GateHome 管理员 · " + panelLogin},
		)
	}
	local := localAdminAddresses()
	routes, _ := a.proxy.routes.Load().(map[string]proxyRoute)
	usedPorts := make(map[int]bool)
	for _, route := range current.Routes {
		live, ok := routes[routeKey(route)]
		if !ok {
			continue
		}
		route = live.route
		group, ok := a.ports.Group(route.GroupID)
		if !ok || !group.Enabled {
			continue
		}
		row := exposureEntry{Service: route.Name, Entrance: "反向代理", Host: route.Host, Login: "未启用反代登录", Firewall: exposureFirewall(current, route.FirewallID)}
		if row.Service == "" {
			row.Service = route.Host
		}
		if live.auth != nil {
			row.Login = "反代账号"
		}
		upstream, _ := url.Parse(route.Upstream)
		destination := ""
		if isAdminUpstream(upstream, a.adminPort, local) {
			row.Service = "管理台 · " + row.Service
			destination = "GateHome 管理员"
		} else if appliedPanel.Enabled && isAdminUpstream(upstream, appliedPanel.Port, local) {
			row.Service = "SunPanel · " + row.Service
			destination = panelLogin
		}
		if destination != "" {
			if live.auth != nil {
				row.Login += " · " + destination
			} else {
				row.Login = destination
			}
		}
		if group.HTTPPort > 0 && !route.TLS {
			usedPorts[group.HTTPPort] = true
			row.Address, row.Transport = a.httpAddress(group.HTTPPort), "HTTP"
			rows = append(rows, row)
		}
		if group.HTTPSPort > 0 {
			usedPorts[group.HTTPSPort] = true
			row.Address, row.Transport = a.httpAddress(group.HTTPSPort), "HTTPS · 终止 TLS"
			rows = append(rows, row)
		}
	}
	for _, group := range a.ports.Groups {
		if !group.Enabled {
			continue
		}
		for _, listener := range []struct {
			port      int
			transport string
		}{{group.HTTPPort, "HTTP"}, {group.HTTPSPort, "HTTPS · 终止 TLS"}} {
			if listener.port > 0 && !usedPorts[listener.port] {
				rows = append(rows, exposureEntry{Service: group.Name, Entrance: "反代组监听", Address: a.httpAddress(listener.port), Transport: listener.transport, Login: "暂无匹配路由"})
			}
		}
	}
	for _, forward := range a.ports.Forwards {
		if !forward.Enabled {
			continue
		}
		name, login := forward.Name, "由上游决定"
		if _, localTarget := localForwardTarget(forward.Upstream, local); localTarget {
			_, port, _ := net.SplitHostPort(forward.Upstream)
			switch port {
			case strconv.Itoa(a.adminPort):
				name, login = "管理台 · "+name, "GateHome 管理员"
			case strconv.Itoa(appliedPanel.Port):
				if appliedPanel.Enabled {
					name, login = "SunPanel · "+name, panelLogin
				}
			}
		}
		for _, protocol := range forward.protocols() {
			transport := "UDP · 透传"
			if protocol == "tcp" {
				transport = "TCP · TLS 由上游决定"
			}
			rows = append(rows, exposureEntry{Service: name, Entrance: "端口转发", Address: a.forwardAddress(forward, protocol), Transport: transport, Login: login, Firewall: exposureFirewall(current, forward.FirewallID)})
		}
	}
	jsonResponse(w, 200, map[string]any{"entries": rows, "restart_required": listenerSignature(current) != listenerSignature(a.ports)})
}

func (a *Admin) forwardAddress(rule PortForward, protocol string) string {
	if a.forwarder != nil {
		for _, binding := range a.forwarder.bindings {
			if binding.rule.ID != rule.ID || binding.rule.Protocol != protocol {
				continue
			}
			if binding.tcp != nil {
				return binding.tcp.Addr().String()
			}
			if binding.udp != nil {
				return binding.udp.LocalAddr().String()
			}
		}
	}
	return listenAddress(rule.ListenPort)
}

func listenAddress(port int) string { return net.JoinHostPort("::", strconv.Itoa(port)) }

func exposureFirewall(current Config, id string) string {
	if id == "" {
		return ""
	}
	if policy, ok := current.Firewall(id); ok {
		return policy.Name
	}
	return "策略已移除 · 拒绝访问"
}
