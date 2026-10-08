package gateway

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
)

type IPBlock struct {
	IP        string    `json:"ip"`
	Rule      string    `json:"rule"`
	RuleName  string    `json:"rule_name"`
	Reason    string    `json:"reason"`
	Source    string    `json:"source"`
	Failures  int       `json:"failures"`
	StartedAt time.Time `json:"started_at"`
	Until     time.Time `json:"until"`
}

func blockKey(rule, ip string) string { return rule + "|" + ip }

func sourceIP(remote string) (string, bool) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return "", false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || addr.Zone() != "" {
		return "", false
	}
	return addr.Unmap().String(), true
}

func validateIPBlocks(blocks map[string]IPBlock) error {
	for key, b := range blocks {
		ip, err := netip.ParseAddr(b.IP)
		if err != nil || ip.Zone() != "" || ip.Unmap().String() != b.IP || key != blockKey(b.Rule, b.IP) || len(b.Rule) > 320 || !strings.Contains(b.Rule, "/") || len(b.RuleName) > 320 || len(b.Reason) > 500 || strings.ContainsAny(b.Reason, "\r\n\x00") || (b.Source != "automatic" && b.Source != "manual" && b.Source != "security") || b.Failures < 0 || b.StartedAt.IsZero() || !b.Until.After(b.StartedAt) || b.Until.Sub(b.StartedAt) > 7*24*time.Hour {
			return errors.New("IP 冻结记录无效")
		}
	}
	return nil
}

func (s *Store) saveIPBlocks(blocks map[string]IPBlock) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state
	next.IPBlocks = make(map[string]IPBlock, len(blocks))
	for key, block := range blocks {
		next.IPBlocks[key] = block
	}
	if err := writeJSON(s.path, next); err != nil {
		return errors.New("IP 冻结记录保存失败")
	}
	s.state = next
	return nil
}

type failedAccess struct {
	count int
	last  time.Time
}

type IPDefense struct {
	mu         sync.Mutex
	store      *Store
	blocks     map[string]IPBlock
	failures   map[string]failedAccess
	now        func() time.Time
	writeError bool
}

func newIPDefense(store *Store) *IPDefense {
	d := &IPDefense{store: store, blocks: map[string]IPBlock{}, failures: map[string]failedAccess{}, now: time.Now}
	if store != nil {
		for key, block := range store.Snapshot().IPBlocks {
			d.blocks[key] = block
		}
	}
	return d
}

func (d *IPDefense) persist() {
	if d.store != nil {
		d.writeError = d.store.saveIPBlocks(d.blocks) != nil
	}
}

func (d *IPDefense) prune() {
	now := d.now()
	changed := false
	for key, b := range d.blocks {
		if !now.Before(b.Until) {
			delete(d.blocks, key)
			delete(d.failures, key)
			changed = true
		}
	}
	for key, f := range d.failures {
		if now.Sub(f.last) >= 24*time.Hour {
			delete(d.failures, key)
		}
	}
	if changed || d.writeError {
		d.persist()
	}
}

func (d *IPDefense) blocked(rule, ip string) (IPBlock, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	b, ok := d.blocks[blockKey(rule, ip)]
	if ok && !d.now().Before(b.Until) {
		delete(d.blocks, blockKey(rule, ip))
		delete(d.failures, blockKey(rule, ip))
		d.persist()
		return IPBlock{}, false
	}
	return b, ok
}

func authPolicy(a RouteAuthConfig) (int, time.Duration) {
	limit, seconds := a.FailureLimit, a.FreezeSeconds
	if limit == 0 {
		limit = 5
	}
	if seconds == 0 {
		seconds = 3600
	}
	return limit, time.Duration(seconds) * time.Second
}

// Only a checked password attempt affects consecutive failures. Headers never select the IP.
// The same lock serializes success with failures that may create a freeze in flight.
func (d *IPDefense) record(route Route, ip string, valid bool) (IPBlock, bool, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prune()
	key := blockKey(routeKey(route), ip)
	if b, ok := d.blocks[key]; ok {
		return b, true, false
	}
	if valid {
		delete(d.failures, key)
		return IPBlock{}, false, false
	}
	f := d.failures[key]
	f.count++
	f.last = d.now()
	limit, duration := authPolicy(route.Auth)
	if f.count < limit {
		d.failures[key] = f
		return IPBlock{}, false, false
	}
	b := IPBlock{IP: ip, Rule: routeKey(route), RuleName: route.Name, Reason: fmt.Sprintf("独立访问账号连续验证失败 %d 次，触发 %d 次冻结规则", f.count, limit), Source: "automatic", Failures: f.count, StartedAt: f.last, Until: f.last.Add(duration)}
	d.blocks[key] = b
	delete(d.failures, key)
	d.persist()
	return b, true, true
}

func (d *IPDefense) capacity(rule, ip string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.failures) < 8192 && len(d.blocks) < 65536 {
		return true
	}
	d.prune()
	_, exists := d.failures[blockKey(rule, ip)]
	return len(d.blocks) < 65536 && (exists || len(d.failures) < 8192)
}

func (d *IPDefense) list() ([]IPBlock, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prune()
	rows := make([]IPBlock, 0, len(d.blocks))
	for _, b := range d.blocks {
		rows = append(rows, b)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].StartedAt.After(rows[j].StartedAt) })
	return rows, d.writeError
}

func (d *IPDefense) add(route Route, ip string, duration time.Duration, reason string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.prune()
	if len(d.blocks) >= 65536 {
		return errors.New("IP 拦截名单已满")
	}
	now := d.now()
	if reason == "" {
		reason = "管理员手动冻结"
	}
	b := IPBlock{IP: ip, Rule: routeKey(route), RuleName: route.Name, Reason: reason, Source: "manual", StartedAt: now, Until: now.Add(duration)}
	if err := validateIPBlocks(map[string]IPBlock{blockKey(b.Rule, ip): b}); err != nil {
		return err
	}
	d.blocks[blockKey(b.Rule, ip)] = b
	delete(d.failures, blockKey(b.Rule, ip))
	d.persist()
	return nil
}

func (d *IPDefense) remove(rule, ip string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := blockKey(rule, ip)
	_, ok := d.blocks[key]
	delete(d.blocks, key)
	delete(d.failures, key)
	d.persist()
	return ok
}
