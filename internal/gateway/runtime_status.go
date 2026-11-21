package gateway

import (
	"cmp"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"
)

type runtimeListener struct {
	Port     int      `json:"port"`
	IP       string   `json:"ip"`
	Protocol string   `json:"protocol"`
	Purpose  string   `json:"purpose"`
	Services []string `json:"services,omitempty"`
}

type runtimeRoute struct {
	ID       string               `json:"id"`
	Name     string               `json:"name"`
	Host     string               `json:"host"`
	Upstream string               `json:"upstream"`
	Entries  []string             `json:"entries"`
	Health   *serviceHealthResult `json:"health,omitempty"`
	Down     bool                 `json:"down"`
	State    string               `json:"state"`
}

func (a *Admin) runtimeInventory(config Config) ([]runtimeListener, []runtimeRoute) {
	listeners := make([]runtimeListener, 0, len(a.httpAddresses))
	httpPorts := make(map[int]int)
	bind := func(address, protocol, purpose string, services ...string) int {
		ip, raw, err := net.SplitHostPort(address)
		port, portErr := strconv.Atoi(raw)
		if err != nil || portErr != nil {
			return -1
		}
		listeners = append(listeners, runtimeListener{port, ip, protocol, purpose, services})
		return len(listeners) - 1
	}
	adminServices := []string{"GateHome 管理控制台"}
	if a.ports.SunPanel.Enabled {
		adminServices = append(adminServices, "内嵌 SunPanel")
	}
	bind(a.adminAddress, "TCP", "管理台 · HTTP", adminServices...)
	if a.ports.SunPanel.Enabled {
		bind(a.httpAddresses[a.ports.SunPanel.Port], "TCP", "独立入口 · HTTP", "SunPanel")
		if a.sunPanel != nil && a.sunPanel.mu.TryRLock() {
			bridge, _ := url.Parse(a.sunPanel.bridgeURL)
			if bridge != nil {
				bind(bridge.Host, "TCP", "内部桥接 · HTTP", "SunPanel")
			}
			a.sunPanel.mu.RUnlock()
		}
	}
	for _, group := range a.ports.Groups {
		if !group.Enabled {
			continue
		}
		for _, service := range []struct {
			port     int
			protocol string
		}{{group.HTTPPort, "HTTP"}, {group.HTTPSPort, "HTTPS"}} {
			if index := bind(a.httpAddresses[service.port], "TCP", group.Name+" · "+service.protocol); index >= 0 {
				httpPorts[service.port] = index
			}
		}
	}
	if a.forwarder != nil {
		for _, active := range a.forwarder.bindings {
			if active.tcp != nil {
				bind(active.tcp.Addr().String(), "TCP", "端口转发", active.rule.Name)
			}
			if active.udp != nil {
				bind(active.udp.LocalAddr().String(), "UDP", "端口转发", active.rule.Name)
			}
		}
	}
	routes := make([]runtimeRoute, 0, len(config.Routes))
	options := serviceHealthOptions(config)
	liveRoutes, _ := a.proxy.routes.Load().(map[string]proxyRoute)
	a.health.mu.Lock()
	for _, route := range config.Routes {
		name := route.Name
		if name == "" {
			name = route.Host
		}
		row := runtimeRoute{ID: route.ID, Name: name, Host: route.Host, Upstream: route.Upstream, State: "disabled"}
		if live, ok := liveRoutes[routeKey(route)]; ok {
			group, _ := a.ports.Group(live.route.GroupID)
			for _, service := range []struct {
				port     int
				protocol string
			}{{group.HTTPPort, "HTTP"}, {group.HTTPSPort, "HTTPS"}} {
				if index, bound := httpPorts[service.port]; group.Enabled && bound && (service.protocol == "HTTPS" || !live.route.TLS) {
					listeners[index].Services = append(listeners[index].Services, name)
					row.Entries = append(row.Entries, strconv.Itoa(service.port)+" / "+service.protocol)
				}
			}
			row.State = "unbound"
			if len(row.Entries) > 0 {
				row.State = "unchecked"
				if options.Enabled {
					row.State = "pending"
					if check := a.health.events[route.ID]; check.upstream == route.Upstream && !check.result.CheckedAt.IsZero() {
						result := check.result
						row.State, row.Health, row.Down = result.State, &result, check.down
					}
				}
			}
		}
		routes = append(routes, row)
	}
	a.health.mu.Unlock()
	slices.SortFunc(listeners, func(a, b runtimeListener) int {
		return cmp.Or(cmp.Compare(a.Port, b.Port), cmp.Compare(a.Protocol, b.Protocol), cmp.Compare(a.IP, b.IP))
	})
	slices.SortStableFunc(routes, func(a, b runtimeRoute) int {
		rank := func(r runtimeRoute) int {
			if r.Down || r.Health != nil && !r.Health.Healthy {
				return 0
			}
			if r.State == "unbound" {
				return 1
			}
			return 2
		}
		return cmp.Compare(rank(a), rank(b))
	})
	return listeners, routes
}

func (a *Admin) runtimeStatus(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	config := a.store.Snapshot().Config
	listeners, routes := a.runtimeInventory(config)
	sampled := a.telemetry.snapshot(a.store.paths, config.Dashboard.NetworkInterface, now)
	jsonResponse(w, 200, map[string]any{
		"sampled_at": now, "resources": sampled, "listeners": listeners, "routes": routes,
		"restart_required": listenerSignature(config) != listenerSignature(a.ports),
		"health":           serviceHealthOptions(config),
	})
}
