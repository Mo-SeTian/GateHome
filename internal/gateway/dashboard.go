package gateway

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

var dashboardWidgets = []string{"server", "cpu", "memory", "storage", "traffic", "transfer", "requests", "blocked", "trend", "provinces", "map", "services", "listeners", "events"}

type DashboardConfig struct {
	Widgets          []string `json:"widgets"`
	NetworkInterface string   `json:"network_interface"`
	MapProvince      string   `json:"map_province"`
}

var provinceNames = []string{"北京", "天津", "河北", "山西", "内蒙古", "辽宁", "吉林", "黑龙江", "上海", "江苏", "浙江", "安徽", "福建", "江西", "山东", "河南", "湖北", "湖南", "广东", "广西", "海南", "重庆", "四川", "贵州", "云南", "西藏", "陕西", "甘肃", "青海", "宁夏", "新疆", "台湾", "香港", "澳门"}

func validateDashboard(c DashboardConfig) error {
	if len(c.Widgets) > len(dashboardWidgets) || len(c.NetworkInterface) > 64 || strings.ContainsAny(c.NetworkInterface, "/\\\r\n\x00") {
		return errors.New("概览组件或网卡设置无效")
	}
	seen := map[string]bool{}
	for _, id := range c.Widgets {
		found := false
		for _, allowed := range dashboardWidgets {
			found = found || id == allowed
		}
		if !found || seen[id] {
			return errors.New("概览组件须存在且不能重复")
		}
		seen[id] = true
	}
	if c.MapProvince != "" {
		for _, name := range provinceNames {
			if c.MapProvince == name {
				return nil
			}
		}
		return errors.New("地图汇聚省份无效")
	}
	return nil
}

type provinceCounts struct {
	Name     string `json:"name"`
	Requests int    `json:"requests"`
	IPs      int    `json:"ips"`
}

type dashboardVisit struct {
	ID       int64     `json:"id"`
	IP       string    `json:"ip"`
	Province string    `json:"province"`
	Region   string    `json:"region"`
	Blocked  bool      `json:"blocked"`
	Time     time.Time `json:"time"`
}

type dashboardRegions struct {
	Provinces []provinceCounts `json:"provinces"`
	Visits    []dashboardVisit `json:"visits"`
	UniqueIPs int              `json:"unique_ips"`
	Unmapped  int              `json:"unmapped"`
	From      time.Time        `json:"from"`
	Sample    int              `json:"sample"`
}

func provinceFromRegion(region string) string {
	for _, part := range strings.Split(region, " · ") {
		for _, name := range provinceNames {
			if part == name || strings.HasPrefix(part, name+"省") || strings.HasPrefix(part, name+"市") || strings.HasPrefix(part, name+"自治区") || strings.HasPrefix(part, name+"壮族") || strings.HasPrefix(part, name+"回族") || strings.HasPrefix(part, name+"维吾尔") || strings.HasPrefix(part, name+"特别行政区") {
				return name
			}
		}
	}
	return ""
}

func summarizeDashboardRegions(entries []LogEntry, now time.Time, lookup func(string) string) dashboardRegions {
	result := dashboardRegions{Provinces: []provinceCounts{}, Visits: []dashboardVisit{}, From: now.Add(-24 * time.Hour)}
	regions := map[string]string{}
	ipSets := map[string]map[string]bool{}
	counts := map[string]int{}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Category != "access" || e.Time.Before(result.From) || e.Time.After(now) {
			continue
		}
		result.Sample++
		region, exists := regions[e.Remote]
		if !exists {
			region = lookup(e.Remote)
			regions[e.Remote] = region
		}
		province := provinceFromRegion(region)
		name := province
		if name == "" {
			name = "其他 / 未定位"
			result.Unmapped++
		}
		counts[name]++
		if ipSets[name] == nil {
			ipSets[name] = map[string]bool{}
		}
		ipSets[name][e.Remote] = true
		if len(result.Visits) < 20 {
			result.Visits = append(result.Visits, dashboardVisit{ID: e.ID, IP: e.Remote, Province: province, Region: region, Time: e.Time, Blocked: firewallBlocked(e) || e.Outcome == "ip_frozen" || strings.HasPrefix(e.Outcome, "auth_") && e.Status >= 400})
		}
	}
	result.UniqueIPs = len(regions)
	for name, count := range counts {
		result.Provinces = append(result.Provinces, provinceCounts{Name: name, Requests: count, IPs: len(ipSets[name])})
	}
	sort.Slice(result.Provinces, func(i, j int) bool {
		a, b := result.Provinces[i], result.Provinces[j]
		if a.Requests != b.Requests {
			return a.Requests > b.Requests
		}
		return a.Name < b.Name
	})
	return result
}

type dashboardCache struct {
	mu      sync.Mutex
	updated time.Time
	regions dashboardRegions
}

func (a *Admin) dashboardRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/dashboard", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		a.dashboard.mu.Lock()
		if now.Sub(a.dashboard.updated) >= 5*time.Second {
			var entries []LogEntry
			if a.logs != nil {
				a.logs.mu.Lock()
				entries = append(entries, a.logs.entries...)
				a.logs.mu.Unlock()
			}
			a.dashboard.regions = summarizeDashboardRegions(entries, now, ipRegion)
			a.dashboard.updated = now
		}
		regions := a.dashboard.regions
		a.dashboard.mu.Unlock()
		jsonResponse(w, 200, map[string]any{"sampled_at": now, "resources": a.telemetry.snapshot(a.store.paths, a.store.Snapshot().Config.Dashboard.NetworkInterface, now), "regions": regions, "access": a.logs.Statistics(24, "", now)})
	}))
}
