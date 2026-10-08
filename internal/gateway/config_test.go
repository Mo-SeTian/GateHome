package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigValidation(t *testing.T) {
	valid := DefaultConfig()
	if err := Validate(valid); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*Config){
		"equal ports":     func(c *Config) { c.Groups[0].HTTPPort = c.Groups[0].HTTPSPort },
		"privileged port": func(c *Config) { c.Groups[0].HTTPPort = 80 },
		"short interval":  func(c *Config) { c.DDNS.Groups = []DDNSGroup{testDDNSGroup()}; c.DDNS.Groups[0].Interval = 1 },
		"unsupported IP endpoint": func(c *Config) {
			c.DDNS.Groups = []DDNSGroup{testDDNSGroup()}
			c.DDNS.Groups[0].IPv4URL = "file:///tmp/ip"
		},
		"upstream credentials": func(c *Config) { c.Routes = []Route{testRoute("nas.example.com", "http://user:TEST_ONLY@localhost")} },
		"upstream path":        func(c *Config) { c.Routes = []Route{testRoute("nas.example.com", "http://localhost/path")} },
		"unknown scheme":       func(c *Config) { c.Routes = []Route{testRoute("nas.example.com", "file:///tmp")} },
		"duplicate route":      func(c *Config) { r := testRoute("nas.example.com", "http://localhost"); c.Routes = []Route{r, r} },
		"empty allowlist": func(c *Config) {
			r := testRoute("nas.example.com", "http://localhost")
			testFirewall(c, &r, "allow", nil, nil)
			c.Routes = []Route{r}
		},
		"invalid prefix": func(c *Config) {
			r := testRoute("nas.example.com", "http://localhost")
			testFirewall(c, &r, "deny", []string{"1.2.3.4/99"}, nil)
			c.Routes = []Route{r}
		},
		"wrong DNS zone": func(c *Config) {
			c.Zone = "example.com"
			c.DDNS.Groups = []DDNSGroup{testDDNSGroup()}
			c.DDNS.Groups[0].Hosts = []string{"notexample.com"}
		},
		"duplicate DNS record": func(c *Config) {
			c.Zone = "example.com"
			c.DDNS.Groups = []DDNSGroup{testDDNSGroup()}
			c.DDNS.Groups[0].Hosts = []string{"nas.example.com", "nas.example.com"}
		},
		"ACME terms required": func(c *Config) { c.Zone = "example.com"; c.ACME.Enabled = true; c.ACME.Email = "admin@example.com" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c := DefaultConfig()
			mutate(&c)
			if Validate(c) == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

func TestLegacyConfigMigratesOnce(t *testing.T) {
	dir := t.TempDir()
	c := DefaultConfig()
	c.Groups, c.Subscriptions = nil, nil
	c.HTTPPort, c.HTTPSPort = 28080, 28443
	r := testRoute("nas.example.com", "http://localhost:5000")
	r.GroupID = ""
	r.Access = &Access{Mode: "public"}
	c.Routes = []Route{r}
	old := State{Config: c, PasswordHash: "TEST_ONLY_HASH", CloudflareToken: "TEST_ONLY_TOKEN", Revision: 7}
	if err := writeJSON(filepath.Join(dir, "state.json"), old); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s, err := OpenStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		state := s.Snapshot()
		if state.Revision != 8 || state.PasswordHash != old.PasswordHash || state.CloudflareToken != old.CloudflareToken {
			t.Fatal("migration changed credentials or repeated revision bump")
		}
		if len(state.Config.Groups) != 1 || state.Config.Groups[0].HTTPPort != 28080 || state.Config.Groups[0].HTTPSPort != 28443 || state.Config.Routes[0].GroupID != "default" {
			t.Fatal("original listeners and route were not preserved")
		}
		if state.Config.HTTPPort != 0 || state.Config.HTTPSPort != 0 {
			t.Fatal("legacy listener fields remained")
		}
	}
}

func TestGroupAndSubscriptionValidation(t *testing.T) {
	base := DefaultConfig()
	base.Groups = append(base.Groups, ProxyGroup{ID: "second", Name: "第二组", Enabled: true, HTTPPort: 19080, HTTPSPort: 19443})
	base.Subscriptions = []Subscription{{ID: "cn", Name: "中国 IPv4", URL: "https://example.com/list.txt", Enabled: true, Interval: 300}}
	r := testRoute("nas.example.com", "http://localhost:5000")
	testFirewall(&base, &r, "allow", nil, []string{"cn"})
	second := r
	second.GroupID = "second"
	base.Routes = []Route{r, second}
	if err := Validate(base); err != nil {
		t.Fatal("same host across groups or subscription-only allowlist rejected")
	}
	tests := map[string]func(*Config){
		"duplicate port across groups": func(c *Config) { c.Groups[1].HTTPPort = c.Groups[0].HTTPSPort },
		"duplicate group":              func(c *Config) { c.Groups[1].ID = "default" },
		"group without listeners":      func(c *Config) { c.Groups[0].HTTPPort, c.Groups[0].HTTPSPort = 0, 0 },
		"missing HTTP listener":        func(c *Config) { c.Groups[0].HTTPPort = 0 },
		"missing HTTPS listener":       func(c *Config) { c.Routes[0].TLS = true; c.Groups[0].HTTPSPort = 0 },
		"unknown group":                func(c *Config) { c.Routes[0].GroupID = "missing" },
		"duplicate host in group":      func(c *Config) { c.Routes[1].GroupID = "default" },
		"unknown subscription":         func(c *Config) { c.Subscriptions = nil },
		"disabled subscription in use": func(c *Config) { c.Subscriptions[0].Enabled = false },
		"unknown firewall":             func(c *Config) { c.Routes[0].FirewallID = "missing" },
		"duplicate subscription ref":   func(c *Config) { c.Firewalls[0].Groups[0].Subscriptions = []string{"cn", "cn"} },
		"unsafe cache path":            func(c *Config) { c.Subscriptions[0].ID = "../outside" },
		"HTTP source":                  func(c *Config) { c.Subscriptions[0].URL = "http://example.com/list.txt" },
		"source credentials":           func(c *Config) { c.Subscriptions[0].URL = "https://user:TEST_ONLY@example.com/list.txt" },
		"short refresh interval":       func(c *Config) { c.Subscriptions[0].Interval = 1 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(base)
			var c Config
			json.Unmarshal(data, &c)
			mutate(&c)
			if Validate(c) == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	base.Groups[0].Enabled = false
	base.Groups[0].HTTPPort, base.Groups[0].HTTPSPort = 0, 0
	if err := Validate(base); err != nil {
		t.Fatal("disabled group should preserve routes without listeners")
	}
}

func TestAtomicPersistenceAndRevision(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	c.DDNS.Groups = []DDNSGroup{testDDNSGroup()}
	token := "TEST_ONLY_FAKE_TOKEN"
	if err := s.UpdateDNS(c, map[string]*string{"home": &token}, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(DefaultConfig(), nil, 0); err == nil {
		t.Fatal("stale revision accepted")
	}
	c = DefaultConfig()
	c.Groups[0].HTTPPort = 80
	if err := s.Update(c, nil, 1); err == nil {
		t.Fatal("invalid config accepted")
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot().Config.DDNS.Groups[0].Zone != "example.com" || reopened.Snapshot().Revision != 1 || reopened.Snapshot().DNSCredentials["home"].Token != token {
		t.Fatal("persisted state mismatch")
	}
	info, err := os.Stat(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("state permissions must be 0600")
	}
	c = DefaultConfig()
	c.DDNS.Groups = []DDNSGroup{testDDNSGroup()}
	if err := s.Update(c, nil, 1); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().DNSCredentials["home"].Token != token {
		t.Fatal("omitted token must remain unchanged")
	}
}
