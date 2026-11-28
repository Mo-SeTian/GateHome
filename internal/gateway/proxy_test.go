package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func testRoute(host, upstream string) Route {
	return Route{GroupID: "default", Name: host, Host: host, Upstream: upstream, Enabled: true}
}

func TestProxyHostRoutingAndHeaders(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-For") != "198.51.100.8" {
			t.Error("forwarded IP must come from connection")
		}
		if r.Header.Get("X-Real-IP") != "" || r.Header.Get("CF-Connecting-IP") != "" || r.Header.Get("Forwarded") != "" {
			t.Error("untrusted forwarding headers leaked")
		}
		if r.Host != "NAS.EXAMPLE.COM:18443" || r.URL.RequestURI() != "/file?x=1" {
			t.Error("host or path was changed")
		}
		io.WriteString(w, "nas")
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "photos") }))
	defer second.Close()
	c := DefaultConfig()
	c.Routes = []Route{testRoute("nas.example.com", first.URL), testRoute("photos.example.com", second.URL)}
	p := NewProxy(c, nil)
	for _, tc := range []struct {
		host, body string
		code       int
	}{{"NAS.EXAMPLE.COM:18443", "nas", 200}, {"photos.example.com", "photos", 200}, {"unknown.example.com", "404 page not found\n", 404}} {
		r := httptest.NewRequest("GET", "http://"+tc.host+"/file?x=1", nil)
		r.RemoteAddr = "198.51.100.8:4000"
		for _, h := range []string{"X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP", "Forwarded"} {
			r.Header.Set(h, "127.0.0.1")
		}
		w := httptest.NewRecorder()
		p.ServeHTTP(w, r)
		if w.Code != tc.code || w.Body.String() != tc.body {
			t.Fatalf("unexpected routing result for %s: HTTP %d", tc.host, w.Code)
		}
	}
	c.Routes[1].Enabled = false
	p.Configure(c)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest("GET", "http://photos.example.com", nil))
	if w.Code != 404 {
		t.Fatal("disabled route remained reachable")
	}
}

func TestAccessControlCannotBeSpoofed(t *testing.T) {
	for _, tc := range []struct {
		mode, cidr, remote string
		allow              bool
	}{
		{"allow", "192.168.1.0/24", "192.168.1.20:1000", true},
		{"allow", "192.168.1.0/24", "198.51.100.1:1000", false},
		{"deny", "198.51.100.0/24", "198.51.100.1:1000", false},
		{"deny", "198.51.100.0/24", "192.168.1.20:1000", true},
		{"allow", "2001:db8::/32", "[2001:db8::1]:1000", true},
		{"allow", "192.168.1.0/24", "[::ffff:192.168.1.20]:1000", true},
		{"deny", "192.168.1.0/24", "invalid", false},
	} {
		t.Run(tc.mode+tc.remote, func(t *testing.T) {
			c := DefaultConfig()
			route := testRoute("nas.example.com", "http://127.0.0.1:1")
			testFirewall(&c, &route, tc.mode, []string{tc.cidr}, nil)
			c.Routes = []Route{route}
			p := NewProxy(c, nil)
			r := httptest.NewRequest("GET", "http://nas.example.com/", nil)
			r.RemoteAddr = tc.remote
			r.Header.Set("X-Forwarded-For", "192.168.1.20")
			w := httptest.NewRecorder()
			p.ServeHTTP(w, r)
			if (w.Code != 403) != tc.allow {
				t.Fatalf("wrong access result: HTTP %d", w.Code)
			}
		})
	}
}

func TestTLSOnlyRoute(t *testing.T) {
	c := DefaultConfig()
	r := testRoute("nas.example.com", "http://127.0.0.1:1")
	r.TLS = true
	c.Routes = []Route{r}
	p := NewProxy(c, nil)
	for _, tlsState := range []*tls.ConnectionState{nil, {}} {
		req := httptest.NewRequest("GET", "http://nas.example.com/", nil)
		req.TLS = tlsState
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		want := 502
		if tlsState == nil {
			want = 426
		}
		if w.Code != want {
			t.Fatalf("got %d want %d", w.Code, want)
		}
	}
}

func TestWebSocketUpgradeAndDuplex(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		rw.Flush()
		line, _ := rw.ReadString('\n')
		rw.WriteString(line)
		rw.Flush()
		if _, err := io.Copy(io.Discard, rw); err == nil {
			rw.WriteString("after half-close\n")
			rw.Flush()
		}
	}))
	defer backend.Close()
	c := DefaultConfig()
	c.Routes = []Route{testRoute("ws.example.com", backend.URL)}
	p := NewProxy(c, nil)
	proxy := httptest.NewUnstartedServer(p)
	proxy.Config.ConnContext = ProxyConnContext
	proxy.Start()
	defer proxy.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	io.WriteString(conn, "GET /socket HTTP/1.1\r\nHost: ws.example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 101 {
		t.Fatalf("upgrade failed: %d", resp.StatusCode)
	}
	const payload = "bidirectional-test\n"
	io.WriteString(conn, payload)
	line, err := r.ReadString('\n')
	if err != nil || line != payload {
		t.Fatal("duplex stream did not pass through")
	}
	if got := p.liveTraffic.status().Total.Active; got != 1 {
		t.Fatalf("active upgraded connections = %d", got)
	}
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if line, err := r.ReadString('\n'); err != nil || line != "after half-close\n" {
		t.Fatalf("proxy did not preserve the upgrade half-close: %q, %v", line, err)
	}
	waitForProxyConnectionsToClose(t, p)
	p.liveTraffic.sample(time.Second)
	if got := p.liveTraffic.status().Total; got != (proxyTrafficMetrics{
		UploadBPS: float64(len(payload)), DownloadBPS: float64(len(payload + "after half-close\n")),
	}) {
		t.Fatalf("upgraded traffic = %+v", got)
	}
}

func TestProxyLiveTrafficDeduplicatesHTTP2Connections(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != "request" {
			t.Errorf("unexpected upstream body: %q, %v", body, err)
		}
		entered <- struct{}{}
		select {
		case <-release:
			_, _ = io.WriteString(w, "response")
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	c := DefaultConfig()
	first := testRoute("first.example.com", upstream.URL)
	first.ID = "first"
	second := testRoute("second.example.com", upstream.URL)
	second.ID = "second"
	c.Routes = []Route{first, second}
	p := NewProxy(c, nil)
	proxy := httptest.NewUnstartedServer(p)
	proxy.EnableHTTP2 = true
	proxy.Config.ConnContext = ProxyConnContext
	proxy.StartTLS()
	defer proxy.Close()
	defer cancel()

	transport := proxy.Client().Transport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	separateClient := &http.Client{Transport: transport}
	done := make(chan error, 4)
	for i, route := range []Route{first, first, second, second} {
		client := proxy.Client()
		if i == 3 {
			client = separateClient
		}
		req, err := http.NewRequestWithContext(ctx, "POST", proxy.URL, strings.NewReader("request"))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = route.Host
		go func() {
			res, err := client.Do(req)
			if err != nil {
				done <- err
				return
			}
			defer res.Body.Close()
			if res.ProtoMajor != 2 || res.StatusCode != http.StatusOK {
				t.Errorf("unexpected response: %s %s", res.Proto, res.Status)
			}
			_, err = io.Copy(io.Discard, res.Body)
			done <- err
		}()
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("request did not reach upstream")
		}
	}

	live := p.liveTraffic.status()
	if live.Sampled || live.Total.Active != 2 || live.Routes[first.ID].Active != 1 || live.Routes[second.ID].Active != 2 {
		t.Fatalf("two connections with four streams counted incorrectly: %+v", live)
	}
	close(release)
	for range 4 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	waitForProxyConnectionsToClose(t, p)
	p.liveTraffic.sample(time.Second)
	live = p.liveTraffic.status()
	if !live.Sampled || live.Total != (proxyTrafficMetrics{UploadBPS: 28, DownloadBPS: 32}) ||
		live.Routes[first.ID] != (proxyTrafficMetrics{UploadBPS: 14, DownloadBPS: 16}) ||
		live.Routes[second.ID] != (proxyTrafficMetrics{UploadBPS: 14, DownloadBPS: 16}) {
		t.Fatalf("unexpected per-route and total rates: %+v", live)
	}
	p.liveTraffic.sample(time.Second)
	if idle := p.liveTraffic.status(); !idle.Sampled || idle.Total != (proxyTrafficMetrics{}) || len(idle.Routes) != 0 {
		t.Fatalf("idle connections or an expired sample still shown: %+v", idle)
	}
}

func waitForProxyConnectionsToClose(t *testing.T, p *Proxy) {
	t.Helper()
	for deadline := time.Now().Add(time.Second); p.liveTraffic.status().Total.Active != 0; {
		if time.Now().After(deadline) {
			t.Fatal("active connections were not released")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestProxyGroupsIsolateSameHostOnDifferentListeners(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "first") }))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "second") }))
	defer second.Close()
	c := DefaultConfig()
	c.Groups = append(c.Groups, ProxyGroup{ID: "second", Name: "第二组", Enabled: true, HTTPPort: 19080})
	one, two := testRoute("nas.example.com", first.URL), testRoute("nas.example.com", second.URL)
	two.GroupID = "second"
	only := testRoute("photos.example.com", first.URL)
	c.Routes = []Route{one, two, only}
	p := NewProxy(c, nil)
	listeners := []*httptest.Server{httptest.NewServer(p.Handler("default")), httptest.NewServer(p.Handler("second"))}
	defer listeners[0].Close()
	defer listeners[1].Close()
	for i, want := range []string{"first", "second"} {
		req, _ := http.NewRequest("GET", listeners[i].URL, nil)
		req.Host = "nas.example.com"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != want {
			t.Fatal("same host escaped its listener group")
		}
	}
	w := httptest.NewRecorder()
	p.Handler("second").ServeHTTP(w, httptest.NewRequest("GET", "http://photos.example.com", nil))
	if w.Code != 404 {
		t.Fatal("route from another group was exposed")
	}
	c.Groups[1].Enabled = false
	p.Configure(c)
	w = httptest.NewRecorder()
	p.Handler("second").ServeHTTP(w, httptest.NewRequest("GET", "http://nas.example.com", nil))
	if w.Code != 404 {
		t.Fatal("disabled group's route remained reachable")
	}
}

func TestUntrustedForwardedHeadersRightmostProtoAssistantRedactionAndLoopGuard(t *testing.T) {
	// 1. Untrusted X-Forwarded-For and Forwarded headers must not reach upstream intact.
	var seenXFF, seenForwarded string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenXFF = r.Header.Get("X-Forwarded-For")
		seenForwarded = r.Header.Get("Forwarded")
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	cfg := DefaultConfig()
	cfg.Routes = []Route{{GroupID: "default", Host: "app.example.test", Upstream: upstream.URL, Enabled: true}}
	p := NewProxy(cfg, nil)

	req := httptest.NewRequest("GET", "http://app.example.test/", nil)
	req.RemoteAddr = "198.51.100.77:54321"
	req.Header.Set("X-Forwarded-For", "127.0.0.1, 10.0.0.1")
	req.Header.Set("Forwarded", "for=127.0.0.1;proto=https")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code != 200 || seenXFF != "198.51.100.77" || seenForwarded != "" {
		t.Fatalf("expected spoofed XFF/Forwarded stripped: code=%d xff=%q forwarded=%q", w.Code, seenXFF, seenForwarded)
	}

	// 2. routeRequestHTTPS must consult the rightmost X-Forwarded-Proto value.
	rHTTPS := httptest.NewRequest("GET", "http://app.example.test/", nil)
	rHTTPS = rHTTPS.Clone(req.Context())
	rHTTPS.Header.Add("X-Forwarded-Proto", "http, https")
	if routeRequestHTTPS(rHTTPS) {
		t.Fatal("expected false without trustedProxyKey context")
	}
	trusted := parseTrustedProxiesOrEmpty(t, []string{"192.0.2.0/24"})
	r1 := httptest.NewRequest("GET", "http://app.example.test/", nil)
	r1.RemoteAddr = "192.0.2.10:1234"
	r1.Header.Set("X-Forwarded-For", "198.51.100.1")
	r1.Header.Add("X-Forwarded-Proto", "https")
	r1.Header.Add("X-Forwarded-Proto", "http")
	if routeRequestHTTPS(proxySourceRequest(r1, trusted)) {
		t.Fatal("expected rightmost X-Forwarded-Proto=http to return false")
	}
	r2 := httptest.NewRequest("GET", "http://app.example.test/", nil)
	r2.RemoteAddr = "192.0.2.10:1234"
	r2.Header.Set("X-Forwarded-For", "198.51.100.1")
	r2.Header.Set("X-Forwarded-Proto", "http, https")
	if !routeRequestHTTPS(proxySourceRequest(r2, trusted)) {
		t.Fatal("expected rightmost X-Forwarded-Proto=https to return true")
	}

	// 3. stateSecrets must include Assistant.Key and AssistantProviders[].Key across Logs and MCP redaction.
	a, _ := testAdmin(t)
	s := a.store.Snapshot()
	s.Assistant.Key = "TEST_ONLY_PRIMARY_ASSIST_KEY"
	s.AssistantProviders = []assistantEndpoint{{ID: "p1", Name: "extra", Key: "TEST_ONLY_SECOND_ASSIST_KEY"}}
	if err := writeJSON(a.store.path, s); err != nil {
		t.Fatal(err)
	}
	a.store.mu.Lock()
	a.store.state = s
	a.store.mu.Unlock()
	logs := &Logs{store: a.store}
	logs.Add(LogEntry{Category: "admin", Action: "测试", Message: "leaked " + s.Assistant.Key + " and " + s.AssistantProviders[0].Key})
	entries := logs.Page(LogFilter{Scope: "admin"}, 1, 10, 0).Entries
	if len(entries) == 0 || strings.Contains(entries[0].Message, "ASSIST_KEY") {
		t.Fatalf("Logs.Add failed to redact assistant keys: %+v", entries)
	}
	scrubbed := a.mcpSafeText("err: " + s.Assistant.Key + " / " + s.AssistantProviders[0].Key)
	if strings.Contains(scrubbed, "ASSIST_KEY") {
		t.Fatalf("mcpSafeText failed to redact assistant keys: %q", scrubbed)
	}

	// 4. localForwardTarget must treat 0.0.0.0 and :: as local targets to prevent port forward loops.
	for _, addr := range []string{"0.0.0.0:8080", "[::]:8080", "127.0.0.1:8080"} {
		if port, isLocal := localForwardTarget(addr, map[netip.Addr]bool{}); !isLocal || port != 8080 {
			t.Fatalf("expected %s to be detected as local loop target, got port=%d isLocal=%v", addr, port, isLocal)
		}
	}
}

func parseTrustedProxiesOrEmpty(t *testing.T, raw []string) []netip.Prefix {
	t.Helper()
	parsed, err := parseTrustedProxies(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestProxyRouteMaintenanceMode(t *testing.T) {
	var upstreamHits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	denyFirewall := Firewall{
		ID: "fw-deny", Name: "DenyAll", DefaultAction: "deny",
		Groups: []FirewallGroup{{Name: "allow-test", Match: "include", Action: "allow", CIDRs: []string{"203.0.113.0/24"}}},
	}
	route := Route{
		ID:                 "maint-1",
		GroupID:            "default",
		Name:               "Nextcloud <云盘>",
		Host:               "cloud.example.test",
		Upstream:           upstream.URL,
		Enabled:            true,
		FirewallID:         "fw-deny",
		Maintenance:        true,
		MaintenanceMessage: "正在升级数据库 <script>alert(1)</script>",
	}
	p := NewProxy(Config{
		Groups:    []ProxyGroup{{ID: "default", Name: "Default", Enabled: true, HTTPPort: 80}},
		Firewalls: []Firewall{denyFirewall},
		Routes:    []Route{route},
	}, nil)

	// 1. Firewall still rejects disallowed source IP before revealing maintenance page.
	deniedReq := httptest.NewRequest("GET", "http://cloud.example.test/", nil)
	deniedReq.Host = "cloud.example.test"
	deniedReq.RemoteAddr = "198.51.100.10:1234"
	deniedRec := httptest.NewRecorder()
	p.ServeHTTP(deniedRec, deniedReq)
	if deniedRec.Code != http.StatusForbidden || upstreamHits != 0 {
		t.Fatalf("expected firewall 403 before maintenance, got code=%d hits=%d", deniedRec.Code, upstreamHits)
	}

	// 2. Allowed source gets 503 HTML maintenance page with escaped content and never hits upstream.
	htmlReq := httptest.NewRequest("GET", "http://cloud.example.test/", nil)
	htmlReq.Host = "cloud.example.test"
	htmlReq.RemoteAddr = "203.0.113.5:1234"
	htmlReq.Header.Set("Accept", "text/html")
	htmlRec := httptest.NewRecorder()
	p.ServeHTTP(htmlRec, htmlReq)
	body := htmlRec.Body.String()
	if htmlRec.Code != http.StatusServiceUnavailable || htmlRec.Header().Get("Retry-After") != "60" ||
		!strings.Contains(body, "503 · 服务维护中") || !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") ||
		strings.Contains(body, "<script>alert(1)") || upstreamHits != 0 {
		t.Fatalf("unexpected maintenance HTML response: code=%d body=%s hits=%d", htmlRec.Code, body, upstreamHits)
	}

	// 3. JSON client receives 503 structured JSON payload.
	jsonReq := httptest.NewRequest("GET", "http://cloud.example.test/api/ping", nil)
	jsonReq.Host = "cloud.example.test"
	jsonReq.RemoteAddr = "203.0.113.5:1234"
	jsonReq.Header.Set("Accept", "application/json")
	jsonRec := httptest.NewRecorder()
	p.ServeHTTP(jsonRec, jsonReq)
	if jsonRec.Code != http.StatusServiceUnavailable || !strings.Contains(jsonRec.Body.String(), `"maintenance":true`) || upstreamHits != 0 {
		t.Fatalf("unexpected maintenance JSON response: code=%d body=%s", jsonRec.Code, jsonRec.Body.String())
	}

	// 4. HEAD request returns 503 with empty body.
	headReq := httptest.NewRequest("HEAD", "http://cloud.example.test/", nil)
	headReq.Host = "cloud.example.test"
	headReq.RemoteAddr = "203.0.113.5:1234"
	headRec := httptest.NewRecorder()
	p.ServeHTTP(headRec, headReq)
	if headRec.Code != http.StatusServiceUnavailable || headRec.Body.Len() != 0 {
		t.Fatalf("unexpected HEAD maintenance response: code=%d bodyLen=%d", headRec.Code, headRec.Body.Len())
	}
}
