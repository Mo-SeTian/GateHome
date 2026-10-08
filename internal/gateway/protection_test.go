package gateway

import (
	"bufio"
	"bytes"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func protectionProxy(t *testing.T, policy Protection) (*Proxy, Config, *int) {
	t.Helper()
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; body, _ := io.ReadAll(r.Body); w.Write(body) }))
	t.Cleanup(upstream.Close)
	c := DefaultConfig()
	c.Firewalls = []Firewall{{ID: "secure", Name: "测试防护", DefaultAction: "allow", Protection: policy}}
	c.Routes = []Route{{GroupID: "default", Name: "服务", Host: "app.example.com", Upstream: upstream.URL, Enabled: true, FirewallID: "secure"}}
	p := NewProxy(DefaultConfig(), nil)
	if err := p.Configure(c); err != nil {
		t.Fatal(err)
	}
	p.logs = &Logs{}
	return p, c, &calls
}
func protectionRequest(p *Proxy, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://app.example.com"+path, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("User-Agent", "GatehouseTest/1.0")
	r.Header.Set("Accept", "*/*")
	if body != "" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	return w
}
func TestWAFBlocksAttacksAndPreservesBodies(t *testing.T) {
	p, _, calls := protectionProxy(t, Protection{WAF: WAFPolicy{Mode: "block"}})
	for _, path := range []string{"/?q=%27%20OR%201%3D1--", "/?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E", "/?file=../../../../etc/passwd"} {
		if w := protectionRequest(p, "GET", path, ""); w.Code != 403 {
			t.Fatalf("attack status %d", w.Code)
		}
	}
	if *calls != 0 {
		t.Fatal("attack reached upstream")
	}
	if w := protectionRequest(p, "POST", "/", "name=hello"); w.Code != 200 || w.Body.String() != "name=hello" {
		t.Fatalf("normal body was changed: %d", w.Code)
	}
	if len(p.logs.entries[0].Security) == 0 {
		t.Fatal("missing WAF events")
	}
	for _, e := range p.logs.entries {
		for _, h := range e.Security {
			if strings.Contains(h.Name, "script") || strings.Contains(h.Name, "passwd") {
				t.Fatal("request content leaked into logs")
			}
		}
	}
}
func TestWAFDetectionExceptionsAndLimits(t *testing.T) {
	p, c, calls := protectionProxy(t, Protection{WAF: WAFPolicy{Mode: "detect", BodyLimit: 65536}})
	protectionRequest(p, "GET", "/?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E", "")
	if *calls != 1 || len(p.logs.entries[0].Security) == 0 {
		t.Fatal("detection did not record and forward")
	}
	body := strings.Repeat("x", 65537)
	if w := protectionRequest(p, "POST", "/", body); w.Code != 200 || w.Body.Len() != len(body) {
		t.Fatalf("oversized detection body not preserved: status=%d length=%d want=%d", w.Code, w.Body.Len(), len(body))
	}
	c.Firewalls[0].Protection.WAF.Mode = "block"
	if err := p.Configure(c); err != nil {
		t.Fatal(err)
	}
	before := *calls
	if w := protectionRequest(p, "POST", "/", body); w.Code != 413 || *calls != before {
		t.Fatal("oversized request was forwarded")
	}
	// Precisely exclude one CRS rule for one path/host; other attacks remain inspected.
	c.Firewalls[0].Protection.WAF.Exceptions = []WAFException{{RuleID: 941100, Host: "app.example.com", Path: "/editor", Parameter: "q"}}
	if err := p.Configure(c); err != nil {
		t.Fatal(err)
	}
	if w := protectionRequest(p, "GET", "/?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E", ""); w.Code != 403 {
		t.Fatal("exception leaked outside path")
	}
}
func TestRateAndCustomFreezeIsolation(t *testing.T) {
	policy := Protection{Rate: RatePolicy{Enabled: true, Requests: 1, WindowSeconds: 60, Path: "/api"}, Freeze: FreezePolicy{Enabled: true, Failures: 2, WindowSeconds: 300, Seconds: 3600}}
	p, c, _ := protectionProxy(t, policy)
	for i, want := range []int{200, 429, 403, 403} {
		if w := protectionRequest(p, "GET", "/api", ""); w.Code != want {
			t.Fatalf("request %d: %d", i, w.Code)
		}
	}
	blocks, _ := p.defense.list()
	if len(blocks) != 1 || blocks[0].Source != "security" {
		t.Fatal("missing security freeze")
	}
	if err := validateIPBlocks(map[string]IPBlock{blockKey(blocks[0].Rule, blocks[0].IP): blocks[0]}); err != nil {
		t.Fatal(err)
	}
	c.Routes = append(c.Routes, Route{GroupID: "default", Name: "other", Host: "other.example.com", Upstream: c.Routes[0].Upstream, Enabled: true, FirewallID: "secure"})
	if err := p.Configure(c); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://other.example.com/api", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("freeze leaked across routes")
	}
}
func TestProtectionValidationAndFailedReload(t *testing.T) {
	p, c, calls := protectionProxy(t, Protection{})
	c.Firewalls[0].Protection.Rules = []HTTPRule{{Name: "broken", Target: "path", Pattern: "[", Action: "block"}}
	if err := p.Configure(c); err == nil {
		t.Fatal("invalid regex accepted")
	}
	if w := protectionRequest(p, "GET", "/", ""); w.Code != 200 || *calls != 1 {
		t.Fatal("failed reload changed live policy")
	}
	for _, path := range []string{"/\"\nInclude /etc/passwd", "/a b", ""} {
		if validateProtection(Protection{WAF: WAFPolicy{Exceptions: []WAFException{{RuleID: 941100, Path: path}}}}) == nil {
			t.Fatal("unsafe exception accepted")
		}
	}
}
func TestWAFChunkedAndJSON(t *testing.T) {
	p, _, calls := protectionProxy(t, Protection{WAF: WAFPolicy{Mode: "block"}})
	r := httptest.NewRequest("POST", "http://app.example.com/api", bytes.NewBufferString(`{"q":"<script>alert(1)</script>"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("User-Agent", "GatehouseTest/1.0")
	r.Header.Set("Accept", "*/*")
	r.ContentLength = -1
	r.TransferEncoding = []string{"chunked"}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 403 || *calls != 0 {
		t.Fatal("chunked JSON attack reached upstream")
	}
}
func TestProtectionCountersExpiry(t *testing.T) {
	c := newProtectionCounters()
	now := time.Now()
	c.now = func() time.Time { return now }
	if hit, _, _ := c.increment("key", 1, 1); hit {
		t.Fatal("first request limited")
	}
	if hit, _, _ := c.increment("key", 1, 1); !hit {
		t.Fatal("second request allowed")
	}
	now = now.Add(2 * time.Second)
	if hit, _, _ := c.increment("key", 1, 1); hit {
		t.Fatal("window did not reset")
	}
}

func TestProtectionSaveIsAtomic(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	before := a.store.Snapshot()
	c := DefaultConfig()
	c.Firewalls = []Firewall{{ID: "secure", Name: "test", DefaultAction: "allow", Protection: Protection{WAF: WAFPolicy{Mode: "block"}, Rules: []HTTPRule{{Name: "invalid", Target: "path", Pattern: "[", Action: "block"}}}}}
	w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": before.Revision}, cookie, "")
	if w.Code != 400 || a.store.Snapshot().Revision != before.Revision || len(a.store.Snapshot().Config.Firewalls) != len(before.Config.Firewalls) {
		t.Fatal("invalid policy changed saved configuration")
	}
	c.Firewalls[0].Protection.Rules = nil
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": before.Revision}, cookie, "")
	if w.Code != 200 || a.store.Snapshot().Revision != before.Revision+1 {
		t.Fatalf("valid WAF policy not saved: %d", w.Code)
	}
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": DefaultConfig(), "revision": before.Revision}, cookie, "")
	if w.Code != 400 || len(a.store.Snapshot().Config.Firewalls) != 1 {
		t.Fatal("stale write replaced policy")
	}
}
func TestWAFExceptionScopeAndScore(t *testing.T) {
	p, c, _ := protectionProxy(t, Protection{WAF: WAFPolicy{Mode: "detect"}})
	path := "/editor?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E"
	protectionRequest(p, "GET", path, "")
	found := false
	for _, h := range p.logs.entries[0].Security {
		if h.RuleID == 941100 {
			found = true
		}
		if h.Score < 5 {
			t.Fatal("missing anomaly score")
		}
	}
	if !found {
		t.Fatalf("fixture did not trigger expected CRS rule: %+v", p.logs.entries[0].Security)
	}
	c.Firewalls[0].Protection.WAF.Exceptions = []WAFException{{RuleID: 941100, Host: "app.example.com", Path: "/editor", Parameter: "q"}}
	if err := p.Configure(c); err != nil {
		t.Fatal(err)
	}
	protectionRequest(p, "GET", path, "")
	for _, h := range p.logs.entries[1].Security {
		if h.RuleID == 941100 {
			t.Fatal("parameter exception failed")
		}
	}
	protectionRequest(p, "GET", strings.Replace(path, "/editor", "/outside", 1), "")
	found = false
	for _, h := range p.logs.entries[2].Security {
		if h.RuleID == 941100 {
			found = true
		}
	}
	if !found {
		t.Fatal("exception leaked to another path")
	}
}
func TestProtectionStatisticsAndCustomRules(t *testing.T) {
	p, _, calls := protectionProxy(t, Protection{Rules: []HTTPRule{{Name: "observe", Target: "path", Pattern: "^/watch", Action: "detect"}, {Name: "deny", Target: "path", Pattern: "^/deny", Action: "block"}}, WAF: WAFPolicy{Mode: "detect"}})
	protectionRequest(p, "GET", "/watch", "")
	protectionRequest(p, "GET", "/deny", "")
	protectionRequest(p, "GET", "/?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E", "")
	d := p.logs.Statistics(24, "", time.Now())
	if *calls != 2 || d.Total != 3 || d.Blocked != 1 || d.CustomBlocked != 1 || d.WAFMatched != 1 || d.WAFBlocked != 0 || p.logs.Page(LogFilter{Scope: "security"}, 1, 20, 0).Total != 3 || len(d.SecurityRules) < 3 {
		t.Fatalf("security counts: calls=%d total=%d blocked=%d custom=%d waf=%d wafblocked=%d records=%d rules=%d", *calls, d.Total, d.Blocked, d.CustomBlocked, d.WAFMatched, d.WAFBlocked, p.logs.Page(LogFilter{Scope: "security"}, 1, 20, 0).Total, len(d.SecurityRules))
	}
}
func TestWAFMultipartPreservesUpload(t *testing.T) {
	p, _, calls := protectionProxy(t, Protection{WAF: WAFPolicy{Mode: "block"}})
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "sample.txt")
	part.Write([]byte("test upload content"))
	writer.Close()
	original := append([]byte(nil), body.Bytes()...)
	r := httptest.NewRequest("POST", "http://app.example.com/upload", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("User-Agent", "GatehouseTest/1.0")
	r.Header.Set("Accept", "*/*")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 200 || *calls != 1 || !bytes.Equal(w.Body.Bytes(), original) {
		t.Fatalf("multipart changed or blocked: %d", w.Code)
	}
}
func TestWAFStreamingResponse(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-release
		io.WriteString(w, "data: last\n\n")
	}))
	defer upstream.Close()
	p, c, _ := protectionProxy(t, Protection{WAF: WAFPolicy{Mode: "block"}})
	c.Routes[0].Upstream = upstream.URL
	if err := p.Configure(c); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(p)
	defer server.Close()
	defer close(release)
	client := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequest("GET", server.URL+"/events", nil)
	req.Host = "app.example.com"
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "GatehouseTest/1.0")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || line != "data: first\n" {
		t.Fatal("SSE was buffered or blocked")
	}
}
func BenchmarkWAFNormalRequest(b *testing.B) {
	policy := Protection{WAF: WAFPolicy{Mode: "block"}}
	waf, err := compileWAF(policy)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := httptest.NewRequest("GET", "http://app.example.com/hello", nil)
		r.Header.Set("User-Agent", "GatehouseTest/1.0")
		r.Header.Set("Accept", "*/*")
		_, status := inspectWAF(waf, policy, r, nil)
		if status != 0 {
			b.Fatal(status)
		}
	}
}

func TestWAFWebSocketUpgradeAndDuplex(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nUser-Agent: GatehouseTest/1.0\r\nAccept: */*\r\n\r\n")
		rw.Flush()
		line, _ := rw.ReadString('\n')
		rw.WriteString(line)
		rw.Flush()
	}))
	defer backend.Close()
	c := DefaultConfig()
	c.Routes = []Route{testRoute("ws.example.com", backend.URL)}
	c.Firewalls = []Firewall{{ID: "waf", Name: "waf", DefaultAction: "allow", Protection: Protection{WAF: WAFPolicy{Mode: "block"}}}}
	c.Routes[0].FirewallID = "waf"
	proxy := httptest.NewServer(NewProxy(c, nil))
	defer proxy.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	io.WriteString(conn, "GET /socket HTTP/1.1\r\nHost: ws.example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nUser-Agent: GatehouseTest/1.0\r\nAccept: */*\r\n\r\n")
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 101 {
		t.Fatalf("upgrade failed: %d", resp.StatusCode)
	}
	io.WriteString(conn, "bidirectional-test\n")
	line, err := r.ReadString('\n')
	if err != nil || line != "bidirectional-test\n" {
		t.Fatal("duplex stream did not pass through")
	}
}

func TestWAFAccessCredentialsStaySeparate(t *testing.T) {
	p, c, calls := protectionProxy(t, Protection{WAF: WAFPolicy{Mode: "block"}})
	c.Routes[0].Auth = RouteAuthConfig{Enabled: true, Username: "test-user"}
	// An attack-like literal is valid as a password and is never sent to Coraza.
	password := "TEST_ONLY_<script>alert(1)</script>"
	hash, _ := HashPassword([]byte(password))
	if err := p.ConfigureState(State{Config: c, RoutePasswordHashes: map[string]string{routeKey(c.Routes[0]): hash}}); err != nil {
		t.Fatal(err)
	}
	cookie := routeLoginTest(t, p, "default", c.Routes[0].Host, "test-user", password)
	if *calls != 0 {
		t.Fatal("login reached upstream")
	}
	r := httptest.NewRequest("GET", "http://app.example.com/?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E", nil)
	r.RemoteAddr = "198.51.100.8:4000"
	r.AddCookie(cookie)
	r.Header.Set("Accept", "*/*")
	r.Header.Set("User-Agent", "GatehouseTest/1.0")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 403 || *calls != 0 {
		t.Fatal("authenticated attack bypassed WAF")
	}
}
func TestWAFFreezeCountsRequestsAndPersists(t *testing.T) {
	p, _, calls := protectionProxy(t, Protection{WAF: WAFPolicy{Mode: "block"}, Freeze: FreezePolicy{Enabled: true, Failures: 3, WindowSeconds: 300, Seconds: 3600}})
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p.defense = newIPDefense(store)
	for i := 0; i < 2; i++ {
		protectionRequest(p, "GET", "/?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E", "")
	}
	rows, _ := p.defense.list()
	if len(rows) != 0 {
		t.Fatal("multiple rule matches were counted as multiple attacks")
	}
	protectionRequest(p, "GET", "/?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E", "")
	rows, _ = p.defense.list()
	if len(rows) != 1 || rows[0].Failures != 3 {
		t.Fatal("request threshold did not freeze")
	}
	reloaded := newIPDefense(store)
	if _, blocked := reloaded.blocked("default/app.example.com", "192.0.2.1"); !blocked {
		t.Fatal("security freeze was not persisted")
	}
	if w := protectionRequest(p, "GET", "/normal", ""); w.Code != 403 || *calls != 0 {
		t.Fatal("frozen request reached upstream")
	}
}
func TestWAFBufferReservationAndBusyHandling(t *testing.T) {
	p, _, _ := protectionProxy(t, Protection{WAF: WAFPolicy{Mode: "block"}, Freeze: FreezePolicy{Enabled: true, Failures: 2, WindowSeconds: 300, Seconds: 3600}})
	if !p.wafMemory.TryAcquire(128 << 20) {
		t.Fatal("reservation failed")
	}
	for i := 0; i < 3; i++ {
		if w := protectionRequest(p, "POST", "/", "name=test"); w.Code != 503 {
			t.Fatal("busy WAF failed open")
		}
	}
	rows, _ := p.defense.list()
	if len(rows) != 0 {
		t.Fatal("busy inspection froze visitor")
	}
	p.wafMemory.Release(128 << 20)
	if w := protectionRequest(p, "POST", "/", "name=test"); w.Code != 200 {
		t.Fatal("reservation did not recover")
	}
	if !p.wafMemory.TryAcquire(128 << 20) {
		t.Fatal("request leaked memory reservation")
	}
	p.wafMemory.Release(128 << 20)
}
func TestProtectionReloadReusesPolicies(t *testing.T) {
	p, c, _ := protectionProxy(t, Protection{WAF: WAFPolicy{Mode: "detect"}, Rate: RatePolicy{Enabled: true, Requests: 1, WindowSeconds: 60, Path: "/"}})
	first := p.firewalls.Load().(map[string]*compiledFirewall)["secure"]
	protectionRequest(p, "GET", "/", "")
	c.Firewalls = append(c.Firewalls, Firewall{ID: "unused", Name: "unused", DefaultAction: "allow", Protection: c.Firewalls[0].Protection})
	if err := p.Configure(c); err != nil {
		t.Fatal(err)
	}
	next := p.firewalls.Load().(map[string]*compiledFirewall)
	if first.waf != next["secure"].waf || first.counters != next["secure"].counters || next["secure"].waf != next["unused"].waf {
		t.Fatal("unchanged or identical WAF was recompiled")
	}
	if w := protectionRequest(p, "GET", "/", ""); w.Code != 429 {
		t.Fatal("unrelated save reset counter")
	}
}

func TestProtectionReleaseKeepsOtherIPAndRouteLimits(t *testing.T) {
	policy := Protection{Rate: RatePolicy{Enabled: true, Requests: 1, WindowSeconds: 60, Path: "/"}, Freeze: FreezePolicy{Enabled: true, Failures: 2, WindowSeconds: 300, Seconds: 3600}}
	p, c, _ := protectionProxy(t, policy)
	c.Routes = append(c.Routes, Route{GroupID: "default", Name: "other", Host: "other.example.com", Upstream: c.Routes[0].Upstream, Enabled: true, FirewallID: "secure"})
	a, h := testAdmin(t)
	if err := a.store.Update(c, nil, a.store.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	a.proxy = p
	p.defense = newIPDefense(a.store)
	if err := p.ConfigureState(a.store.Snapshot()); err != nil {
		t.Fatal(err)
	}
	request := func(host, ip string) int {
		r := httptest.NewRequest("GET", "http://"+host+"/", nil)
		r.RemoteAddr = ip + ":1234"
		w := httptest.NewRecorder()
		p.ServeHTTP(w, r)
		return w.Code
	}
	for _, want := range []int{200, 429, 403} {
		if got := request("app.example.com", "192.0.2.1"); got != want {
			t.Fatalf("freeze request status: got %d, want %d", got, want)
		}
	}
	if request("app.example.com", "192.0.2.2") != 200 || request("other.example.com", "192.0.2.1") != 200 {
		t.Fatal("freeze affected another visitor or route")
	}
	cookie := loginForTest(t, h)
	if adminRequest(h, "DELETE", "/api/ip-blocks", map[string]string{"rule": "default/app.example.com", "ip": "192.0.2.1"}, cookie, "").Code != 200 {
		t.Fatal("security freeze release failed")
	}
	if request("app.example.com", "192.0.2.1") != 200 {
		t.Fatal("released visitor retained the old rate counter")
	}
	if request("app.example.com", "192.0.2.2") != 429 || request("other.example.com", "192.0.2.1") != 429 {
		t.Fatal("release reset another visitor or route counter")
	}
}
