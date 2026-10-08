package gateway

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type AccessCounts struct {
	WAFMatched      int `json:"waf_matched"`
	WAFBlocked      int `json:"waf_blocked"`
	RateBlocked     int `json:"rate_blocked"`
	CustomBlocked   int `json:"custom_blocked"`
	Total           int `json:"total"`
	Normal          int `json:"normal"`
	Blocked         int `json:"blocked"`
	Failed          int `json:"failed"`
	AuthSuccess     int `json:"auth_success"`
	AuthFailed      int `json:"auth_failed"`
	AuthRequired    int `json:"auth_required"`
	AuthLimited     int `json:"auth_limited"`
	FrozenBlocked   int `json:"frozen_blocked"`
	FreezeCreated   int `json:"freeze_created"`
	FirewallBlocked int `json:"firewall_blocked"`
}

func firewallBlocked(e LogEntry) bool {
	return e.Outcome == "blocked" || (e.Outcome == "" && strings.Contains(e.Message, "防火墙拒绝来源 IP"))
}

func (c *AccessCounts) add(e LogEntry) {
	c.Total++
	seen := map[string]bool{}
	for _, h := range e.Security {
		if h.Engine == "waf" && !seen["waf_matched"] {
			c.WAFMatched++
			seen["waf_matched"] = true
		}
		if h.Action != "block" || seen[h.Engine] {
			continue
		}
		seen[h.Engine] = true
		switch h.Engine {
		case "waf":
			c.WAFBlocked++
		case "rate":
			c.RateBlocked++
		case "custom":
			c.CustomBlocked++
		}
	}
	if e.AuthResult == "auth_success" {
		c.AuthSuccess++
	}
	if e.AuthResult == "auth_failed" {
		c.AuthFailed++
	}
	if e.AuthResult == "auth_required" {
		c.AuthRequired++
	}
	if e.AuthResult == "auth_rate_limited" {
		c.AuthLimited++
	}
	if e.FreezeCreated {
		c.FreezeCreated++
	}
	if e.Outcome == "ip_frozen" || (e.FreezeCreated && e.Outcome == "blocked") {
		c.FrozenBlocked++
	}
	firewall := firewallBlocked(e)
	if firewall {
		c.FirewallBlocked++
	}
	if firewall || e.Outcome == "ip_frozen" {
		c.Blocked++
	} else if e.Status >= 100 && e.Status < 400 {
		c.Normal++
	} else {
		c.Failed++
	}
}

type AccessBucket struct {
	Time time.Time `json:"time"`
	AccessCounts
}

type RuleCounts struct {
	Rule string `json:"rule"`
	AccessCounts
}

type StatusCounts struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type FirewallCounts struct {
	FirewallHit
	Count    int       `json:"count"`
	LastSeen time.Time `json:"last_seen"`
}

type SecurityRuleCounts struct {
	Engine   string    `json:"engine"`
	RuleID   int       `json:"rule_id"`
	Name     string    `json:"name"`
	Count    int       `json:"count"`
	Blocked  int       `json:"blocked"`
	LastSeen time.Time `json:"last_seen"`
}
type AccessStatistics struct {
	SecurityRules   []SecurityRuleCounts `json:"security_rules"`
	From            time.Time            `json:"from"`
	To              time.Time            `json:"to"`
	RetainedFrom    time.Time            `json:"retained_from"`
	RetainedEntries int                  `json:"retained_entries"`
	WriteError      bool                 `json:"write_error"`
	AccessCounts
	Trend            []AccessBucket   `json:"trend"`
	Rules            []RuleCounts     `json:"rules"`
	Statuses         []StatusCounts   `json:"statuses"`
	Firewalls        []FirewallCounts `json:"firewalls"`
	ActiveFreezes    int              `json:"active_freezes"`
	FreezeWriteError bool             `json:"freeze_write_error"`
}

func (l *Logs) Statistics(hours int, rule string, now time.Time) AccessStatistics {
	step := time.Hour
	if hours > 24 {
		step = 24 * time.Hour
	}
	from := now.Add(-time.Duration(hours) * time.Hour)
	result := AccessStatistics{From: from, To: now, Trend: []AccessBucket{}, Rules: []RuleCounts{}, Statuses: []StatusCounts{}, Firewalls: []FirewallCounts{}}
	for start := from; start.Before(now); start = start.Add(step) {
		result.Trend = append(result.Trend, AccessBucket{Time: start})
	}
	if l == nil {
		return result
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	result.RetainedEntries, result.WriteError = len(l.entries), l.writeError || l.cleanupError
	if len(l.entries) > 0 {
		result.RetainedFrom = l.entries[0].Time
	}
	rules := map[string]*RuleCounts{}
	statuses := map[string]int{}
	firewalls := map[FirewallHit]*FirewallCounts{}
	securityRules := map[string]*SecurityRuleCounts{}
	for _, e := range l.entries {
		if !matchesLog(e, LogFilter{Scope: "access", Rule: rule, From: from, To: now}) {
			continue
		}
		result.add(e)
		if firewallBlocked(e) {
			hit := FirewallHit{Kind: "legacy", Reason: "旧版本记录未保存具体防火墙规则"}
			if e.Firewall != nil {
				hit = *e.Firewall
			}
			if firewalls[hit] == nil {
				firewalls[hit] = &FirewallCounts{FirewallHit: hit}
			}
			firewalls[hit].Count++
			firewalls[hit].LastSeen = e.Time
		}
		seen := map[string]bool{}
		for _, h := range e.Security {
			key := h.Engine + "/" + strconv.Itoa(h.RuleID) + "/" + h.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			row := securityRules[key]
			if row == nil {
				row = &SecurityRuleCounts{Engine: h.Engine, RuleID: h.RuleID, Name: h.Name}
				securityRules[key] = row
			}
			row.Count++
			if h.Action == "block" {
				row.Blocked++
			}
			row.LastSeen = e.Time
		}
		index := int(e.Time.Sub(from) / step)
		if index == len(result.Trend) {
			index--
		}
		result.Trend[index].add(e)
		if rules[e.Rule] == nil {
			rules[e.Rule] = &RuleCounts{Rule: e.Rule}
		}
		rules[e.Rule].add(e)
		label := "其他"
		if e.Status >= 100 && e.Status < 600 {
			label = strconv.Itoa(e.Status/100) + "xx"
		}
		statuses[label]++
	}
	for _, row := range securityRules {
		result.SecurityRules = append(result.SecurityRules, *row)
	}
	sort.Slice(result.SecurityRules, func(i, j int) bool {
		a, b := result.SecurityRules[i], result.SecurityRules[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.Engine != b.Engine {
			return a.Engine < b.Engine
		}
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		return a.Name < b.Name
	})
	for _, row := range rules {
		result.Rules = append(result.Rules, *row)
	}
	for _, row := range firewalls {
		result.Firewalls = append(result.Firewalls, *row)
	}
	sort.Slice(result.Firewalls, func(i, j int) bool {
		if result.Firewalls[i].Count == result.Firewalls[j].Count {
			return result.Firewalls[i].Reason < result.Firewalls[j].Reason
		}
		return result.Firewalls[i].Count > result.Firewalls[j].Count
	})
	sort.Slice(result.Rules, func(i, j int) bool {
		if result.Rules[i].Total == result.Rules[j].Total {
			return result.Rules[i].Rule < result.Rules[j].Rule
		}
		return result.Rules[i].Total > result.Rules[j].Total
	})
	for _, label := range []string{"1xx", "2xx", "3xx", "4xx", "5xx", "其他"} {
		result.Statuses = append(result.Statuses, StatusCounts{Label: label, Count: statuses[label]})
	}
	return result
}

func (a *Admin) statisticsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/statistics", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		hours := 24
		if raw := r.URL.Query().Get("hours"); raw != "" {
			var err error
			hours, err = strconv.Atoi(raw)
			if err != nil || (hours != 24 && hours != 168 && hours != 720) {
				apiError(w, 400, "统计时间范围须为最近 24 小时、7 天或 30 天")
				return
			}
		}
		rule := r.URL.Query().Get("rule")
		if len(rule) > 320 {
			apiError(w, 400, "统计规则无效")
			return
		}
		result := a.logs.Statistics(hours, rule, time.Now())
		blocks, failed := a.proxy.defense.list()
		result.FreezeWriteError = failed
		for _, block := range blocks {
			if rule == "" || block.Rule == rule {
				result.ActiveFreezes++
			}
		}
		jsonResponse(w, 200, result)
	}))
}
