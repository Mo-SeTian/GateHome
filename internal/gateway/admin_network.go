package gateway

import (
	"net/http"
)

func (a *Admin) networkRoutes(mux *http.ServeMux) {
	for _, method := range []string{"GET", "POST"} {
		mux.HandleFunc(method+" /api/ddns/network", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
			c := a.store.Snapshot().Config
			input := c.DDNS.IPMonitor
			query := r.URL.Query()
			if query.Has("group_id") || query.Has("interface") || query.Has("ipv4_source") || query.Has("ipv6_source") {
				input = IPMonitorConfig{GroupID: query.Get("group_id"), Interface: query.Get("interface"), IPv4Source: query.Get("ipv4_source"), IPv6Source: query.Get("ipv6_source")}
			}
			if r.Method == "POST" {
				// An explicit preview is independent of the persisted GET defaults.
				input = IPMonitorConfig{}
				if !decodeBody(w, r, &input) {
					return
				}
			}
			g := DDNSGroup{Mode: "dual", Interface: input.Interface, IPv4Source: input.IPv4Source, IPv6Source: input.IPv6Source}
			key := "preview:" + g.Interface + ":" + ipSource(g, "A") + ":" + ipSource(g, "AAAA")
			if input.GroupID != "" {
				var ok bool
				g, ok = c.DNSGroup(input.GroupID)
				if !ok {
					apiError(w, 404, "DDNS 组不存在，请刷新页面")
					return
				}
				key = "group:" + g.ID
			}
			if err := validateIPSources(g); err != nil {
				apiError(w, 400, err.Error())
				return
			}
			interfaces, err := networkInterfaces()
			if err != nil {
				apiError(w, 500, err.Error())
				return
			}
			groups := map[string]IPStatus{}
			selected := a.jobs.IPStatus(key, g, r.Method == "POST")
			for _, group := range c.DDNS.Groups {
				groups[group.ID] = a.jobs.IPStatus("group:"+group.ID, group, false)
			}
			jsonResponse(w, 200, map[string]any{"interfaces": interfaces, "ip": selected, "groups": groups, "mode": g.Mode, "interface": g.Interface, "ipv4_source": ipSource(g, "A"), "ipv6_source": ipSource(g, "AAAA")})
		}))
	}
}
