package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"testing"
)

func testFirewall(c *Config, r *Route, action string, cidrs, subscriptions []string) {
	defaultAction := "allow"
	if action == "allow" {
		defaultAction = "deny"
	}
	c.Firewalls = []Firewall{{ID: "test-policy", Name: "测试防火墙", DefaultAction: defaultAction, Groups: []FirewallGroup{{Name: "IP 组", Match: "include", Action: action, CIDRs: cidrs, Subscriptions: subscriptions}}}}
	r.FirewallID = "test-policy"
}

func TestFirewallOrderedIncludeExcludeAndDefault(t *testing.T) {
	f := Firewall{ID: "ordered", Name: "顺序匹配", DefaultAction: "deny", Groups: []FirewallGroup{
		{Name: "例外允许", Match: "include", Action: "allow", CIDRs: []string{"192.0.2.7"}},
		{Name: "阻止网段", Match: "include", Action: "deny", CIDRs: []string{"192.0.2.0/24"}},
		{Name: "排除组外允许", Match: "exclude", Action: "allow", CIDRs: []string{"198.51.100.0/24", "2001:db8::/32"}},
	}}
	c := DefaultConfig()
	c.Firewalls = []Firewall{f}
	compiled := compileFirewalls(c, nil)[f.ID]
	for _, tc := range []struct {
		addr  string
		allow bool
	}{{"192.0.2.7", true}, {"192.0.2.8", false}, {"::ffff:192.0.2.7", true}, {"203.0.113.1", true}, {"198.51.100.1", false}, {"2001:db8::1", false}, {"2001:db9::1", true}} {
		if compiled.allowed(netip.MustParseAddr(tc.addr)) != tc.allow {
			t.Fatalf("wrong ordered firewall result for %s", tc.addr)
		}
	}
	c.Firewalls[0].Groups[0], c.Firewalls[0].Groups[1] = c.Firewalls[0].Groups[1], c.Firewalls[0].Groups[0]
	if compileFirewalls(c, nil)[f.ID].allowed(netip.MustParseAddr("192.0.2.7")) {
		t.Fatal("later allow overrode first matching deny")
	}
	c.Firewalls[0].Groups = nil
	c.Firewalls[0].DefaultAction = "allow"
	if !compileFirewalls(c, nil)[f.ID].allowed(netip.MustParseAddr("192.0.2.7")) {
		t.Fatal("unmatched default allow not applied")
	}
	c.Firewalls[0].Groups = []FirewallGroup{{Name: "组外禁止", Match: "exclude", Action: "deny", CIDRs: []string{"192.0.2.0/24"}}}
	compiled = compileFirewalls(c, nil)[f.ID]
	if !compiled.allowed(netip.MustParseAddr("192.0.2.7")) || compiled.allowed(netip.MustParseAddr("203.0.113.1")) {
		t.Fatal("exclude deny did not enforce group membership")
	}
}

func TestSharedFirewallAndNoFirewallHotUpdates(t *testing.T) {
	c := DefaultConfig()
	r := testRoute("nas.example.com", "http://127.0.0.1:1")
	testFirewall(&c, &r, "deny", []string{"198.51.100.0/24"}, nil)
	second := r
	second.Host = "photos.example.com"
	public := testRoute("public.example.com", r.Upstream)
	c.Routes = []Route{r, second, public}
	p := NewProxy(c, nil)
	request := func(host string) int {
		req := httptest.NewRequest("GET", "http://"+host, nil)
		req.RemoteAddr = "198.51.100.1:1000"
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		return w.Code
	}
	if request(r.Host) != 403 || request(second.Host) != 403 || request(public.Host) != 502 {
		t.Fatal("shared firewall or no-firewall route behaved incorrectly")
	}
	c.Firewalls[0].Groups[0].Action = "allow"
	p.Configure(c)
	if request(r.Host) != 502 || request(second.Host) != 502 {
		t.Fatal("shared firewall update did not apply to both routes")
	}
	c.Routes[0].FirewallID = ""
	c.Firewalls[0].Groups[0].Action = "deny"
	p.Configure(c)
	if request(r.Host) != 502 || request(second.Host) != 403 {
		t.Fatal("removing one route's firewall changed another route")
	}
}

func TestFirewallIncompleteSubscriptionCannotMatchExclusions(t *testing.T) {
	c := DefaultConfig()
	c.Subscriptions = []Subscription{{ID: "cn", URL: "https://example.com/list.txt"}}
	c.Firewalls = []Firewall{{ID: "fw", DefaultAction: "allow", Groups: []FirewallGroup{
		{Name: "手动允许", Match: "include", Action: "allow", CIDRs: []string{"192.0.2.0/24"}},
		{Name: "组外禁止", Match: "exclude", Action: "deny", Subscriptions: []string{"cn"}},
	}}}
	s := &Subscriptions{}
	s.entries.Store(map[string]subscriptionEntry{})
	f := compileFirewalls(c, s)["fw"]
	if f.allowed(netip.MustParseAddr("192.0.2.1")) || f.allowed(netip.MustParseAddr("198.51.100.1")) {
		t.Fatal("missing subscription bypassed earlier rule or exclusion default")
	}
	s.entries.Store(map[string]subscriptionEntry{"cn": {source: c.Subscriptions[0].URL, index: newPrefixIndex([]netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}), status: SubscriptionStatus{Ready: true}}})
	if !f.allowed(netip.MustParseAddr("192.0.2.1")) || !f.allowed(netip.MustParseAddr("198.51.100.1")) || f.allowed(netip.MustParseAddr("203.0.113.1")) {
		t.Fatal("ready subscription exclusion did not apply")
	}
	s.entries.Store(map[string]subscriptionEntry{"cn": {source: "https://example.com/other.txt", status: SubscriptionStatus{Ready: true}}})
	if f.allowed(netip.MustParseAddr("192.0.2.1")) {
		t.Fatal("mismatched subscription source reused cache")
	}
}

func TestFirewallValidationAndReferenceProtection(t *testing.T) {
	base := DefaultConfig()
	base.Subscriptions = []Subscription{{ID: "cn", Name: "中国 IP", URL: "https://example.com/list.txt", Enabled: true, Interval: 300}}
	r := testRoute("nas.example.com", "http://localhost:5000")
	testFirewall(&base, &r, "deny", []string{"192.0.2.0/24"}, []string{"cn"})
	base.Routes = []Route{r}
	for name, mutate := range map[string]func(*Config){
		"deleted firewall in use": func(c *Config) { c.Firewalls = nil },
		"duplicate firewall":      func(c *Config) { c.Firewalls = append(c.Firewalls, c.Firewalls[0]) },
		"invalid default":         func(c *Config) { c.Firewalls[0].DefaultAction = "public" },
		"invalid match":           func(c *Config) { c.Firewalls[0].Groups[0].Match = "contains" },
		"invalid action":          func(c *Config) { c.Firewalls[0].Groups[0].Action = "public" },
		"empty group":             func(c *Config) { c.Firewalls[0].Groups[0].CIDRs = nil; c.Firewalls[0].Groups[0].Subscriptions = nil },
		"invalid CIDR":            func(c *Config) { c.Firewalls[0].Groups[0].CIDRs = []string{"bad"} },
		"deleted source in use":   func(c *Config) { c.Subscriptions = nil },
		"disabled source in use":  func(c *Config) { c.Subscriptions[0].Enabled = false },
		"duplicate source":        func(c *Config) { c.Firewalls[0].Groups[0].Subscriptions = []string{"cn", "cn"} },
		"legacy policy submitted": func(c *Config) { c.Routes[0].Access = &Access{Mode: "public"} },
	} {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(base)
			var c Config
			if err := json.Unmarshal(data, &c); err != nil {
				t.Fatal(err)
			}
			mutate(&c)
			if Validate(c) == nil {
				t.Fatal("invalid firewall configuration accepted")
			}
		})
	}
}

func TestLegacyAccessMigratesWithoutChangingDecisions(t *testing.T) {
	dir := t.TempDir()
	c := DefaultConfig()
	c.Firewalls = nil
	for _, mode := range []string{"public", "allow", "deny"} {
		r := testRoute(mode+".example.com", "http://127.0.0.1:1")
		r.Access = &Access{Mode: mode, CIDRs: []string{"198.51.100.0/24"}}
		c.Routes = append(c.Routes, r)
	}
	if err := writeJSON(filepath.Join(dir, "state.json"), State{Config: c, Revision: 4}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s, err := OpenStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		state := s.Snapshot()
		if state.Revision != 5 || len(state.Config.Firewalls) != 2 || state.Config.Routes[0].FirewallID != "" {
			t.Fatal("policy migration did not persist exactly once")
		}
		p := NewProxy(state.Config, nil)
		for _, mode := range []string{"public", "allow", "deny"} {
			for _, addr := range []string{"198.51.100.1:1000", "203.0.113.1:1000"} {
				req := httptest.NewRequest("GET", "http://"+mode+".example.com", nil)
				req.RemoteAddr = addr
				w := httptest.NewRecorder()
				p.ServeHTTP(w, req)
				allowed := mode == "public" || (mode == "allow" && addr == "198.51.100.1:1000") || (mode == "deny" && addr == "203.0.113.1:1000")
				if (w.Code != http.StatusForbidden) != allowed {
					t.Fatal("migration changed old access decisions")
				}
			}
		}
		for _, r := range state.Config.Routes {
			if r.Access != nil {
				t.Fatal("legacy access field remained")
			}
		}
	}
}
