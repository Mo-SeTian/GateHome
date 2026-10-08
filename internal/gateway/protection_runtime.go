package gateway

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

func (p *Proxy) inspectProtection(route proxyRoute, r *http.Request) ([]SecurityHit, int, int) {
	f := route.firewall
	policy := f.protection
	ip, ok := sourceIP(r.RemoteAddr)
	if !ok {
		return []SecurityHit{{Engine: "system", RuleID: 1, Name: "来源 IP 无效", Action: "block"}}, 403, 0
	}
	if policy.Rate.Enabled && strings.HasPrefix(r.URL.Path, policy.Rate.Path) {
		if limited, retry, available := f.counters.increment(routeKey(route.route)+"|"+ip+"|rate", policy.Rate.WindowSeconds, policy.Rate.Requests); !available {
			return []SecurityHit{{Engine: "system", RuleID: 3, Name: "访问计数容量已满，请稍后重试", Action: "block"}}, 503, retry
		} else if limited {
			return []SecurityHit{{Engine: "rate", RuleID: 1, Name: "超过路径访问频率限制", Action: "block"}}, 429, retry
		}
	}
	hits := []SecurityHit{}
	for i, rule := range policy.Rules {
		value := r.UserAgent()
		if rule.Target == "path" {
			value = r.URL.Path
		}
		if rule.Target == "method" {
			value = r.Method
		}
		if f.rules[i].MatchString(value) {
			hits = append(hits, SecurityHit{Engine: "custom", RuleID: 10000 + i, Name: rule.Name, Action: rule.Action})
			if rule.Action == "block" {
				return hits, 403, 0
			}
		}
	}

	return hits, 0, 0
}
func (p *Proxy) inspectRouteWAF(w http.ResponseWriter, route proxyRoute, r *http.Request) ([]SecurityHit, int, func()) {
	select {
	case p.wafSlots <- struct{}{}:
		defer func() { <-p.wafSlots }()
	default:
		return []SecurityHit{{Engine: "system", RuleID: 2, Name: "WAF 检查繁忙，请稍后重试", Action: "block"}}, 503, nil
	}
	var release func()
	if r.Body != nil && r.Body != http.NoBody {
		// Account for input growth, Coraza buffering and parser allocations.
		reserve := int64(route.firewall.protection.bodyLimit()) * 4
		if !p.wafMemory.TryAcquire(reserve) {
			return []SecurityHit{{Engine: "system", RuleID: 2, Name: "请求体检查繁忙，请稍后重试", Action: "block"}}, 503, nil
		}
		var once sync.Once
		release = func() { once.Do(func() { p.wafMemory.Release(reserve) }) }
		controller := http.NewResponseController(w)
		if controller.SetReadDeadline(time.Now().Add(time.Minute)) == nil {
			defer controller.SetReadDeadline(time.Time{})
		}
	}
	hits, status := inspectWAF(route.firewall.waf, route.firewall.protection, r, release)
	return hits, status, release
}
func (p *Proxy) freezeProtection(route proxyRoute, r *http.Request, hits []SecurityHit) (IPBlock, bool) {
	policy := route.firewall.protection.Freeze
	if !policy.Enabled || len(hits) == 0 {
		return IPBlock{}, false
	}
	hit := hits[len(hits)-1]
	// Resource errors and inspection limits are not evidence of an attack.
	if hit.Engine == "system" || (hit.Engine == "waf" && hit.RuleID < 200000) {
		return IPBlock{}, false
	}
	ip, ok := sourceIP(r.RemoteAddr)
	if !ok {
		return IPBlock{}, false
	}
	key := routeKey(route.route) + "|" + ip + "|freeze:" + hit.Engine
	if reached, _, available := route.firewall.counters.increment(key, policy.WindowSeconds, policy.Failures-1); !available || !reached {
		return IPBlock{}, false
	}
	b, created := p.defense.freezeSecurity(route.route, ip, securityReason(hit), policy)
	if created {
		route.firewall.counters.clear(routeKey(route.route), ip)
	}
	return b, created
}
func (d *IPDefense) freezeSecurity(route Route, ip, reason string, policy FreezePolicy) (IPBlock, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prune()
	key := blockKey(routeKey(route), ip)
	if _, ok := d.blocks[key]; ok || len(d.blocks) >= 65536 {
		return IPBlock{}, false
	}
	now := d.now()
	b := IPBlock{IP: ip, Rule: routeKey(route), RuleName: route.Name, Reason: reason, Source: "security", Failures: policy.Failures, StartedAt: now, Until: now.Add(time.Duration(policy.Seconds) * time.Second)}
	d.blocks[key] = b
	d.persist()
	return b, true
}
