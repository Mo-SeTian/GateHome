package gateway

import (
	"net/http"
	"net/netip"
	"strings"
	"time"
)

func (a *Admin) ipBlockRoutes(mux *http.ServeMux) {
	respond := func(w http.ResponseWriter) {
		rows, failed := a.proxy.defense.list()
		jsonResponse(w, 200, map[string]any{"entries": rows, "write_error": failed, "now": time.Now()})
	}
	mux.HandleFunc("GET /api/ip-blocks", a.requireAuth(func(w http.ResponseWriter, r *http.Request) { respond(w) }))
	mux.HandleFunc("POST /api/ip-blocks", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Rule    string `json:"rule"`
			IP      string `json:"ip"`
			Minutes int    `json:"minutes"`
			Reason  string `json:"reason"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		ip, err := netip.ParseAddr(strings.TrimSpace(req.IP))
		if err != nil || ip.Zone() != "" || req.Minutes < 1 || req.Minutes > 10080 || len(req.Reason) > 500 || strings.ContainsAny(req.Reason, "\r\n\x00") {
			apiError(w, 400, "请输入有效 IPv4 / IPv6 地址、1–10080 分钟冻结时长和最多 500 字节的原因")
			return
		}
		for _, route := range a.store.Snapshot().Config.Routes {
			if routeKey(route) == req.Rule {
				if err := a.proxy.defense.add(route, ip.Unmap().String(), time.Duration(req.Minutes)*time.Minute, strings.TrimSpace(req.Reason)); err != nil {
					apiError(w, 400, err.Error())
					return
				}
				respond(w)
				return
			}
		}
		apiError(w, 400, "反代规则不存在")
	}))
	mux.HandleFunc("DELETE /api/ip-blocks", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Rule string `json:"rule"`
			IP   string `json:"ip"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		ipStr := strings.TrimSpace(req.IP)
		if addr, err := netip.ParseAddr(ipStr); err == nil && addr.Zone() == "" {
			ipStr = addr.Unmap().String()
		}
		if !a.proxy.defense.remove(req.Rule, ipStr) {
			apiError(w, 404, "冻结记录不存在或已自动到期")
			return
		}
		if firewalls, ok := a.proxy.firewalls.Load().(map[string]*compiledFirewall); ok {
			for _, fw := range firewalls {
				if fw != nil && fw.counters != nil {
					fw.counters.clear(req.Rule, ipStr)
				}
			}
		}
		respond(w)
	}))
}
