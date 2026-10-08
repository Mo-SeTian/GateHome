package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProxyFreezeThresholdSessionsHeadersAndRuleIsolation(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer backend.Close()
	c := DefaultConfig()
	r := testRoute("nas.example.test", backend.URL)
	r.Auth = RouteAuthConfig{Enabled: true, Username: "visitor"}
	other := r
	other.Host = "other.example.test"
	c.Routes = []Route{r, other}
	hash, _ := HashPassword([]byte(testRoutePassword))
	p := NewProxy(c, nil)
	p.ConfigureState(State{Config: c, RoutePasswordHashes: map[string]string{routeKey(r): hash, routeKey(other): hash}})
	logs, _ := NewLogs(t.TempDir(), nil)
	p.SetLogs(logs)
	now := time.Now()
	p.defense.now = func() time.Time { return now }
	cookie := routeLoginTest(t, p, "default", r.Host, "visitor", testRoutePassword)
	for i := 1; i <= 5; i++ {
		form := url.Values{"username": {"visitor"}, "password": {"x"}, "return": {"/"}}
		req := httptest.NewRequest("POST", "http://"+r.Host+routeAuthPath, strings.NewReader(form.Encode()))
		req.RemoteAddr = "198.51.100.8:4321"
		if i%2 == 0 {
			req.RemoteAddr = "[::ffff:198.51.100.8]:4321"
		}
		req.Header.Set("Origin", "http://"+r.Host)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-For", "203.0.113."+strings.Repeat("9", i))
		req.Header.Set("X-Real-IP", "203.0.113.1")
		req.Header.Set("CF-Connecting-IP", "203.0.113.2")
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		want := 401
		if i == 5 {
			want = 403
		}
		if w.Code != want {
			t.Fatalf("attempt %d returned %d, want %d", i, w.Code, want)
		}
		if i == 5 && (!strings.Contains(w.Body.String(), "此来源 IP 已被冻结") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "style-src 'sha256-") || strings.Contains(w.Header().Get("Content-Security-Policy"), "unsafe-inline")) {
			t.Fatal("freeze response missing protected HTML page")
		}
	}
	blocks, _ := p.defense.list()
	if len(blocks) != 1 || blocks[0].IP != "198.51.100.8" || blocks[0].Failures != 5 || blocks[0].Until.Sub(blocks[0].StartedAt) != time.Hour {
		t.Fatal("default freeze or canonical source IP incorrect")
	}
	before := blocks[0].Until
	if routeAuthRequest(p, "default", "GET", "http://"+r.Host+"/private", nil, cookie).Code != 403 {
		t.Fatal("valid session bypassed IP freeze")
	}
	if routeAuthRequest(p, "default", "POST", "http://"+r.Host+routeAuthPath, url.Values{"username": {"visitor"}, "password": {testRoutePassword}, "return": {"/"}}).Code != 403 {
		t.Fatal("correct password bypassed freeze")
	}
	blocks, _ = p.defense.list()
	if blocks[0].Until != before {
		t.Fatal("blocked requests extended freeze")
	}
	routeLoginTest(t, p, "default", other.Host, "visitor", testRoutePassword)
	request := httptest.NewRequest("GET", "http://"+r.Host+"/private", nil)
	request.RemoteAddr = "198.51.100.9:4321"
	w := httptest.NewRecorder()
	p.ServeHTTP(w, request)
	if w.Code != 401 {
		t.Fatal("different IP was frozen")
	}
	stats := logs.Statistics(24, routeKey(r), time.Now())
	if stats.AuthSuccess != 1 || stats.AuthFailed != 5 || stats.FreezeCreated != 1 || stats.FrozenBlocked != 3 || stats.FirewallBlocked != 0 {
		t.Fatal("service auth/freeze statistics incorrect")
	}
	now = now.Add(time.Hour)
	if routeAuthRequest(p, "default", "GET", "http://"+r.Host+"/private", nil, cookie).Code != 200 {
		t.Fatal("freeze did not expire at configured deadline")
	}
	if rows, _ := p.defense.list(); len(rows) != 0 {
		t.Fatal("expired freeze remains active")
	}
}

func TestDefenseCustomPolicySuccessResetAndConcurrentAttempts(t *testing.T) {
	d := newIPDefense(nil)
	r := Route{GroupID: "default", Host: "test.example", Auth: RouteAuthConfig{FailureLimit: 2, FreezeSeconds: 120}}
	ip := "2001:db8::1"
	now := time.Now()
	d.now = func() time.Time { return now }
	if _, blocked, _ := d.record(r, ip, false); blocked {
		t.Fatal("froze too early")
	}
	d.record(r, ip, true)
	if _, blocked, _ := d.record(r, ip, false); blocked {
		t.Fatal("success did not reset failures")
	}
	b, blocked, created := d.record(r, ip, false)
	if !blocked || !created || b.Failures != 2 || b.Until.Sub(now) != 2*time.Minute {
		t.Fatal("custom policy failed")
	}
	if _, blocked, created := d.record(r, ip, true); !blocked || created {
		t.Fatal("in-flight success cleared freeze")
	}
	if !d.remove(routeKey(r), ip) {
		t.Fatal("unblock failed")
	}
	if _, blocked, _ := d.record(r, ip, false); blocked {
		t.Fatal("manual release did not reset failures")
	}
	now = now.Add(24 * time.Hour)
	if _, blocked, _ := d.record(r, ip, false); blocked {
		t.Fatal("idle failure counters did not expire")
	}
	d.remove(routeKey(r), ip)
	r.Auth.FailureLimit = 5
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); d.record(r, ip, false) }()
	}
	wg.Wait()
	rows, _ := d.list()
	if len(rows) != 1 || rows[0].Failures != 5 {
		t.Fatal("concurrent failures created inconsistent freezes")
	}
}

func TestDefensePersistsWithoutOverwritingConfiguration(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := newIPDefense(store)
	r := Route{GroupID: "default", Host: "test.example", Name: "Test service"}
	revision := store.Snapshot().Revision
	if err := d.add(r, "198.51.100.1", time.Hour, "Manual test reason"); err != nil {
		t.Fatal(err)
	}
	if store.Snapshot().Revision != revision {
		t.Fatal("freeze changed configuration revision")
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			c := store.Snapshot().Config
			c.Groups = append([]ProxyGroup(nil), c.Groups...)
			c.Groups[0].Name = "Updated name"
			if err := store.Update(c, nil, store.Snapshot().Revision); err != nil {
				t.Error("config update failed")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			if err := d.add(r, "2001:db8::1", time.Hour, "Concurrent freeze"); err != nil {
				t.Error(err)
			}
		}
	}()
	wg.Wait()
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal("cannot reload persisted defense state")
	}
	rows, _ := newIPDefense(reopened).list()
	if len(rows) != 2 || reopened.Snapshot().Config.Groups[0].Name != "Updated name" {
		t.Fatal("freeze/config updates overwrote each other")
	}
	if info, err := os.Stat(filepath.Join(dir, "state.json")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("freeze file permissions incorrect")
	}
	d.remove(routeKey(r), "198.51.100.1")
	reopened, _ = OpenStore(dir)
	rows, _ = newIPDefense(reopened).list()
	if len(rows) != 1 {
		t.Fatal("manual release did not persist")
	}
}

func TestDefensePersistenceFailureRemainsBlockedAndRetries(t *testing.T) {
	dir := t.TempDir()
	store, _ := OpenStore(dir)
	d := newIPDefense(store)
	path := store.path
	store.path = dir
	r := Route{GroupID: "default", Host: "test.example", Auth: RouteAuthConfig{FailureLimit: 1}}
	d.record(r, "198.51.100.1", false)
	if _, blocked := d.blocked(routeKey(r), "198.51.100.1"); !blocked {
		t.Fatal("failed persistence opened access")
	}
	if _, failed := d.list(); !failed {
		t.Fatal("failed persistence hidden from admin")
	}
	store.path = path
	if _, failed := d.list(); failed {
		t.Fatal("persistence did not recover")
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rows, _ := newIPDefense(reopened).list(); len(rows) != 1 {
		t.Fatal("recovered persistence dropped freeze")
	}
}

func TestIPBlockAdminAuthCSRFValidationAndRelease(t *testing.T) {
	a, h := testAdmin(t)
	c := a.store.Snapshot().Config
	r := testRoute("nas.example.test", "http://127.0.0.1:17777")
	c.Routes = []Route{r}
	if a.store.Update(c, nil, a.store.Snapshot().Revision) != nil {
		t.Fatal("fixture config failed")
	}
	a.proxy.ConfigureState(a.store.Snapshot())
	body := map[string]any{"rule": routeKey(r), "ip": "::ffff:198.51.100.1", "minutes": 60, "reason": "Manual test reason"}
	for _, method := range []string{"GET", "POST", "DELETE"} {
		if adminRequest(h, method, "/api/ip-blocks", body, nil, "").Code != 401 {
			t.Fatal("unauthenticated freeze API accepted")
		}
	}
	cookie := loginForTest(t, h)
	if adminRequest(h, "POST", "/api/ip-blocks", body, cookie, "https://evil.example").Code != 403 {
		t.Fatal("cross-origin manual freeze accepted")
	}
	for _, invalid := range []map[string]any{{"rule": routeKey(r), "ip": "bad", "minutes": 60}, {"rule": routeKey(r), "ip": "198.51.100.1", "minutes": 0}, {"rule": "missing/rule", "ip": "198.51.100.1", "minutes": 60}, {"rule": routeKey(r), "ip": "198.51.100.1", "minutes": 10081}} {
		if adminRequest(h, "POST", "/api/ip-blocks", invalid, cookie, "").Code != 400 {
			t.Fatal("invalid manual freeze accepted")
		}
	}
	w := adminRequest(h, "POST", "/api/ip-blocks", body, cookie, "")
	var data struct {
		Entries []IPBlock `json:"entries"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &data) != nil || len(data.Entries) != 1 || data.Entries[0].IP != "198.51.100.1" {
		t.Fatal("manual freeze response incorrect")
	}
	stats := adminRequest(h, "GET", "/api/statistics", nil, cookie, "")
	var counts AccessStatistics
	json.Unmarshal(stats.Body.Bytes(), &counts)
	if counts.ActiveFreezes != 1 {
		t.Fatal("active freeze missing from statistics")
	}
	if adminRequest(h, "DELETE", "/api/ip-blocks", map[string]string{"rule": routeKey(r), "ip": "198.51.100.1"}, cookie, "").Code != 200 {
		t.Fatal("release API failed")
	}
	if rows, _ := a.proxy.defense.list(); len(rows) != 0 {
		t.Fatal("release API did not remove freeze")
	}
}

func TestFirewallDecisionRecordsOrderedGroupDefaultAndUnavailableCache(t *testing.T) {
	c := DefaultConfig()
	c.Firewalls = []Firewall{{ID: "policy", Name: "Public access", DefaultAction: "deny", Groups: []FirewallGroup{
		{Name: "Exception", Match: "include", Action: "allow", CIDRs: []string{"198.51.100.1"}},
		{Name: "Blocked network", Match: "include", Action: "deny", CIDRs: []string{"198.51.100.0/24"}},
	}}}
	f := compileFirewalls(c, nil)["policy"]
	allowed, hit := f.decision(netip.MustParseAddr("198.51.100.8"))
	if allowed || hit.ID != "policy" || hit.Name != "Public access" || hit.Order != 2 || hit.Group != "Blocked network" || hit.Match != "include" {
		t.Fatal("wrong firewall group recorded")
	}
	if allowed, _ := f.decision(netip.MustParseAddr("198.51.100.1")); !allowed {
		t.Fatal("first matching allow lost")
	}
	if allowed, hit := f.decision(netip.MustParseAddr("203.0.113.1")); allowed || hit.Kind != "default" || hit.Order != 0 {
		t.Fatal("default deny reported as group match")
	}
	c.Firewalls[0].Groups[1].Match = "exclude"
	f = compileFirewalls(c, nil)["policy"]
	if allowed, hit := f.decision(netip.MustParseAddr("203.0.113.1")); allowed || hit.Match != "exclude" || hit.Order != 2 {
		t.Fatal("exclusion match missing")
	}
	c.Firewalls[0].Groups[1].Subscriptions = []string{"waiting"}
	f = compileFirewalls(c, nil)["policy"]
	if allowed, hit := f.decision(netip.MustParseAddr("198.51.100.1")); allowed || hit.Kind != "subscription_unavailable" || hit.Order != 0 {
		t.Fatal("incomplete cache reported as group match")
	}
}

func TestRouteFreezePolicyValidation(t *testing.T) {
	for _, auth := range []RouteAuthConfig{{FailureLimit: -1}, {FailureLimit: 101}, {FreezeSeconds: 1}, {FreezeSeconds: 61}, {FreezeSeconds: 604860}} {
		if validateRouteAuth(auth) == nil {
			t.Fatal("invalid freeze policy accepted")
		}
	}
	for _, auth := range []RouteAuthConfig{{}, {FailureLimit: 1, FreezeSeconds: 60}, {FailureLimit: 100, FreezeSeconds: 604800}} {
		if err := validateRouteAuth(auth); err != nil {
			t.Fatal("valid freeze policy rejected")
		}
	}
}
