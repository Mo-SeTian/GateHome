package gateway

import (
	"fmt"
	"net/netip"
	"reflect"
	"regexp"
)

type FirewallHit struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Group  string `json:"group,omitempty"`
	Order  int    `json:"order,omitempty"`
	Match  string `json:"match,omitempty"`
	Reason string `json:"reason"`
}

type compiledFirewallGroup struct {
	exclude, allow bool
	networks       prefixIndex
	refs           []subscriptionRef
	name           string
}

type compiledFirewall struct {
	protection    Protection
	waf           *compiledWAF
	rules         []*regexp.Regexp
	counters      *protectionCounters
	defaultAllow  bool
	groups        []compiledFirewallGroup
	subscriptions *Subscriptions
	id, name      string
}

func compileFirewalls(c Config, subscriptions *Subscriptions) map[string]*compiledFirewall {
	sources := map[string]string{}
	for _, s := range c.Subscriptions {
		sources[s.ID] = s.URL
	}
	result := map[string]*compiledFirewall{}
	for _, definition := range c.Firewalls {
		f := &compiledFirewall{defaultAllow: definition.DefaultAction == "allow", subscriptions: subscriptions, id: definition.ID, name: definition.Name}
		for _, group := range definition.Groups {
			compiled := compiledFirewallGroup{exclude: group.Match == "exclude", allow: group.Action == "allow", name: group.Name}
			prefixes := []netip.Prefix{}
			for _, cidr := range group.CIDRs {
				prefix, _ := parsePrefix(cidr)
				prefixes = append(prefixes, prefix)
			}
			compiled.networks = newPrefixIndex(prefixes)
			for _, id := range group.Subscriptions {
				compiled.refs = append(compiled.refs, subscriptionRef{id, sources[id]})
			}
			f.groups = append(f.groups, compiled)
		}
		result[definition.ID] = f
	}
	return result
}

func (f *compiledFirewall) allowed(addr netip.Addr) bool {
	allowed, _ := f.decision(addr)
	return allowed
}

func (f *compiledFirewall) decision(addr netip.Addr) (bool, FirewallHit) {
	hit := FirewallHit{ID: f.id, Name: f.name}
	var entries map[string]subscriptionEntry
	if f.subscriptions != nil {
		entries = f.subscriptions.entries.Load().(map[string]subscriptionEntry)
	}
	// Evaluate one cache snapshot. An incomplete firewall must never fail open,
	// especially when exclusion rules could otherwise match an empty list.
	for _, group := range f.groups {
		for _, ref := range group.refs {
			entry, ok := entries[ref.id]
			if !ok || entry.source != ref.source || !entry.status.Ready {
				hit.Kind = "subscription_unavailable"
				hit.Reason = "防火墙「" + f.name + "」的订阅 " + ref.id + " 尚未就绪，按安全策略拒绝"
				return false, hit
			}
		}
	}
	for index, group := range f.groups {
		matched := group.networks.contains(addr)
		for _, ref := range group.refs {
			matched = matched || entries[ref.id].index.contains(addr)
		}
		if group.exclude {
			matched = !matched
		}
		if matched {
			hit.Kind, hit.Group, hit.Order, hit.Match = "group", group.name, index+1, "include"
			match := "包含"
			if group.exclude {
				hit.Match, match = "exclude", "排除"
			}
			hit.Reason = fmt.Sprintf("防火墙「%s」第 %d 组「%s」（%s匹配）", f.name, index+1, group.name, match)
			return group.allow, hit
		}
	}
	hit.Kind, hit.Reason = "default", "防火墙「"+f.name+"」默认拒绝（未命中任何 IP 组）"
	return f.defaultAllow, hit
}

// Compile before persistence. Reuse unchanged policies so unrelated edits do not
// reset rate/freeze counters or repeatedly compile the CRS.
func (p *Proxy) prepareFirewalls(c Config) (map[string]*compiledFirewall, error) {
	result := compileFirewalls(c, p.subscriptions)
	previous := map[string]*compiledFirewall{}
	if saved, ok := p.firewalls.Load().(map[string]*compiledFirewall); ok {
		previous = saved
	}
	for _, definition := range c.Firewalls {
		policy := definition.Protection
		if err := validateProtection(policy); err != nil {
			return nil, err
		}
		f := result[definition.ID]
		f.protection = policy
		if old := previous[definition.ID]; old != nil && reflect.DeepEqual(old.protection, policy) {
			f.waf, f.rules, f.counters = old.waf, old.rules, old.counters
			continue
		}
		// Identical WAF settings share compiled CRS; native rules and counters
		// remain separate for each firewall and route.
		for id, other := range result {
			if id != definition.ID && other.waf != nil && reflect.DeepEqual(other.protection.WAF, policy.WAF) {
				f.waf = other.waf
				break
			}
		}
		var err error
		if f.waf == nil {
			f.waf, err = compileWAF(policy)
		}
		if err != nil {
			return nil, fmt.Errorf("防火墙「%s」WAF 规则编译失败，原策略未修改", definition.Name)
		}
		f.counters = newProtectionCounters()
		for _, rule := range policy.Rules {
			re, err := regexp.Compile(rule.Pattern)
			if err != nil {
				return nil, fmt.Errorf("自定义规则表达式无效")
			}
			f.rules = append(f.rules, re)
		}
	}
	return result, nil
}
