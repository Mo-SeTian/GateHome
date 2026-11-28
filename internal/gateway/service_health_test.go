package gateway

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestServiceHealthTLSRedirectAndHTTPStates(t *testing.T) {
	var secureCalls atomic.Int32
	secure := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { secureCalls.Add(1); w.WriteHeader(200) }))
	secure.Config.ErrorLog = log.New(io.Discard, "", 0)
	secure.StartTLS()
	defer secure.Close()
	result := probeServiceHealth(context.Background(), secure.URL)
	if result.State != "tls_untrusted" || result.TLSVerified || result.Healthy {
		t.Fatal("untrusted TLS accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, secure.URL, 302)
		case "/auth":
			w.WriteHeader(401)
		case "/error":
			w.WriteHeader(503)
		case "/head":
			if r.Method == "HEAD" {
				w.WriteHeader(405)
			} else {
				w.WriteHeader(200)
			}
		default:
			w.WriteHeader(200)
		}
	}))
	defer server.Close()
	for _, test := range []struct {
		path, state string
		healthy     bool
	}{{"/", "healthy", true}, {"/redirect", "redirect", true}, {"/auth", "auth_required", true}, {"/error", "http_error", false}, {"/head", "healthy", true}} {
		row := probeServiceHealth(context.Background(), server.URL+test.path)
		if row.State != test.state || row.Healthy != test.healthy {
			t.Fatal("HTTP health classification incorrect")
		}
	}
	if secureCalls.Load() != 0 {
		t.Fatal("redirect reached untrusted HTTPS handler")
	}
}

func TestServiceHealthCacheDeduplicatesAndCancels(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer server.Close()
	a, _ := testAdmin(t)
	var work sync.WaitGroup
	for i := 0; i < 12; i++ {
		work.Add(1)
		go func() {
			defer work.Done()
			if !a.cachedServiceHealth(context.Background(), server.URL, time.Minute).Healthy {
				t.Error("health request failed")
			}
		}()
	}
	work.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate probes not collapsed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := a.cachedServiceHealth(ctx, server.URL+"/cancel", 0)
	if result.State != "cancelled" {
		t.Fatal("cancelled probe reported success")
	}
}

func TestBrowserProbeHealthValidatesRedirectChains(t *testing.T) {
	a, _ := testAdmin(t)
	var secureCalls atomic.Int32
	secure := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { secureCalls.Add(1); w.WriteHeader(200) }))
	secure.Config.ErrorLog = log.New(io.Discard, "", 0)
	secure.StartTLS()
	defer secure.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "HEAD" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("browser preflight sent unexpected method or credentials")
		}
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/desktop/", 307)
		case "/tls":
			http.Redirect(w, r, secure.URL, 307)
		case "/restricted":
			http.Redirect(w, r, "http://169.254.169.254/", 307)
		case "/loop":
			http.Redirect(w, r, "/loop", 307)
		case "/auth":
			w.WriteHeader(401)
		default:
			w.WriteHeader(200)
		}
	}))
	defer server.Close()
	for _, test := range []struct {
		path    string
		allowed bool
	}{{"/", true}, {"/redirect", true}, {"/auth", true}, {"/tls", false}, {"/restricted", false}, {"/loop", false}} {
		result := a.browserProbeHealth(context.Background(), server.URL+test.path)
		if result.BrowserProbeAllowed != test.allowed {
			t.Errorf("browser probe approval for %s = %v", test.path, result.BrowserProbeAllowed)
		}
	}
	if secureCalls.Load() != 0 {
		t.Fatal("untrusted HTTPS handler was reached")
	}
	ordinary := a.cachedServiceHealth(context.Background(), server.URL+"/redirect", time.Minute)
	if ordinary.State != "redirect" || ordinary.Status != 307 || ordinary.BrowserProbeAllowed {
		t.Fatal("browser preflight changed the ordinary health result")
	}
}

func TestServiceHealthNotificationsAreDeduplicated(t *testing.T) {
	a, _ := testAdmin(t)
	route := Route{ID: "fixture-route", Upstream: "http://127.0.0.1/"}
	a.cachedServiceHealth(context.Background(), "http://127.0.0.1:1/", time.Minute)
	down := serviceHealthResult{State: "connection_error", Label: "连接失败", CheckedAt: time.Now().UTC()}
	a.recordHealthTransition(route, down)
	a.recordHealthTransition(route, down)
	if a.health.events[route.ID].failures != 1 {
		t.Fatal("same observation counted twice")
	}
	down.CheckedAt = down.CheckedAt.Add(time.Second)
	a.recordHealthTransition(route, down)
	if !a.health.events[route.ID].down {
		t.Fatal("two failures did not mark outage")
	}
	recovered := serviceHealthResult{State: "healthy", Healthy: true, CheckedAt: down.CheckedAt.Add(time.Second)}
	a.recordHealthTransition(route, recovered)
	if a.health.events[route.ID].down || len(a.health.notices) != 2 || a.health.notices[0].Healthy || !a.health.notices[1].Healthy {
		t.Fatal("deduplicated outage/recovery notices incorrect")
	}
}

func TestServiceHealthCustomAlertDelay(t *testing.T) {
	a, _ := testAdmin(t)
	snap := a.store.Snapshot()
	for _, invalid := range []int{-1, 15, 29, 86401} {
		cfg := snap.Config
		cfg.ServiceHealth = ServiceHealthConfig{Enabled: true, Interval: 60, AlertDelaySeconds: invalid}
		if Validate(cfg) == nil {
			t.Fatalf("expected validation error for alert_delay_seconds=%d", invalid)
		}
	}
	for _, valid := range []int{0, 30, 300, 86400} {
		cfg := snap.Config
		cfg.ServiceHealth = ServiceHealthConfig{Enabled: true, Interval: 60, AlertDelaySeconds: valid}
		if err := Validate(cfg); err != nil {
			t.Fatalf("expected valid config for alert_delay_seconds=%d: %v", valid, err)
		}
	}

	cfg := snap.Config
	cfg.ServiceHealth = ServiceHealthConfig{Enabled: true, Interval: 60, AlertDelaySeconds: 300}
	if err := a.store.Update(cfg, nil, snap.Revision); err != nil {
		t.Fatal(err)
	}
	if err := a.store.changeWebhook(func(s *State) bool {
		s.Alerts.Enabled = true
		s.Alerts.Events = []string{"upstream"}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	route := Route{ID: "delay-route", Host: "delay.example.test", Upstream: "http://127.0.0.1/"}
	t0 := time.Now().UTC()
	failAt := func(offset time.Duration) {
		a.recordHealthTransition(route, serviceHealthResult{
			State: "timeout", Label: "响应超时", CheckedAt: t0.Add(offset),
		})
	}
	passAt := func(offset time.Duration) {
		a.recordHealthTransition(route, serviceHealthResult{
			State: "healthy", Healthy: true, CheckedAt: t0.Add(offset),
		})
	}

	failAt(0)
	failAt(60 * time.Second)
	failAt(120 * time.Second)
	failAt(240 * time.Second)
	if a.health.events[route.ID].down || len(a.health.notices) != 0 || len(a.store.Snapshot().Alerts.Pending) != 0 {
		t.Fatal("failures shorter than custom delay triggered early alert")
	}

	// Recovery before reaching delay resets failure window cleanly.
	passAt(260 * time.Second)
	if !a.health.events[route.ID].firstFailedAt.IsZero() || a.health.events[route.ID].failures != 0 || len(a.health.notices) != 0 {
		t.Fatal("healthy probe did not reset failure timer")
	}

	failAt(300 * time.Second)
	failAt(540 * time.Second)
	if a.health.events[route.ID].down || len(a.health.notices) != 0 {
		t.Fatal("240s since new failure series should not trigger 300s delay alert")
	}

	failAt(600 * time.Second)
	if !a.health.events[route.ID].down || len(a.health.notices) != 1 || !strings.Contains(a.health.notices[0].Message, "5 分钟") {
		t.Fatalf("expected outage notice after 300s continuous failures, got down=%v notices=%+v", a.health.events[route.ID].down, a.health.notices)
	}
	alerts := a.store.Snapshot().Alerts
	if !alerts.Active["route/"+route.ID] || len(alerts.Pending) != 1 || alerts.Pending[0].Type != "upstream" {
		t.Fatalf("expected active upstream alert, got active=%v pending=%+v", alerts.Active, alerts.Pending)
	}

	passAt(660 * time.Second)
	if a.health.events[route.ID].down || !a.health.events[route.ID].firstFailedAt.IsZero() || len(a.health.notices) != 2 || !a.health.notices[1].Healthy {
		t.Fatalf("expected clean recovery after custom delay outage, got notices=%+v", a.health.notices)
	}
}

func TestServiceHealthFallbacksAndErrorMessages(t *testing.T) {
	// 1. Upstream drops TCP connection on HEAD but serves 200 on GET.
	headReset := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					conn.Close()
					return
				}
			}
		}
		w.WriteHeader(200)
	}))
	defer headReset.Close()
	if res := probeServiceHealth(context.Background(), headReset.URL); !res.Healthy || res.State != "healthy" {
		t.Fatalf("expected HEAD reset fallback to GET to succeed, got %+v", res)
	}

	// 2. Upstream returns 404 on HEAD and rejects Range requests, but returns 200 on plain GET.
	head404NoRange := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("Range") != "" {
			w.WriteHeader(416)
			return
		}
		w.WriteHeader(200)
	}))
	defer head404NoRange.Close()
	if res := probeServiceHealth(context.Background(), head404NoRange.URL); !res.Healthy || res.State != "healthy" {
		t.Fatalf("expected HEAD 404 without Range GET fallback to succeed, got %+v", res)
	}

	// 3. HTTP upstream rejects raw IP Host header (drops connection / 404) but accepts proxied route Host.
	hostChecked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Host, "sunpanel.example.test") {
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					conn.Close()
					return
				}
			}
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(200)
	}))
	defer hostChecked.Close()
	if res := probeServiceHealthWithOptions(context.Background(), hostChecked.URL, "sunpanel.example.test", nil, nil); !res.Healthy || res.State != "healthy" {
		t.Fatalf("expected route Host fallback to succeed, got %+v", res)
	}

	// 4. Unbound port reports clear connection refused diagnostic.
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := closed.URL
	closed.Close()
	if res := probeServiceHealth(context.Background(), closedURL); res.Healthy || res.State != "connection_error" || !strings.Contains(res.Message, "上游拒绝连接") {
		t.Fatalf("expected clear ECONNREFUSED diagnostic, got %+v", res)
	}

	// 5. Server dropping every probe connection reports clear peer-reset diagnostic.
	alwaysDrop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				conn.Close()
			}
		}
	}))
	defer alwaysDrop.Close()
	if res := probeServiceHealth(context.Background(), alwaysDrop.URL); res.Healthy || res.State != "connection_error" || !strings.Contains(res.Message, "主动断开") {
		t.Fatalf("expected clear peer-reset diagnostic, got %+v", res)
	}
}

func TestPanelHealthRequiresLoginAndSavedOwnedIDs(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sunpanel/api/user/getInfo" {
			if r.Header.Get("token") != "TEST_ONLY_PANEL_SESSION" {
				jsonResponse(w, 200, map[string]any{"code": 1001})
				return
			}
			jsonResponse(w, 200, map[string]any{"code": 0, "data": map[string]uint{"id": 7}})
			return
		}
		var input struct {
			UserID uint `json:"userId"`
		}
		json.NewDecoder(r.Body).Decode(&input)
		if input.UserID != 7 {
			t.Error("worker request used another account")
		}
		jsonResponse(w, 200, map[string]any{"data": map[string]any{"items": []map[string]any{{"id": 11, "url": target.URL, "lanUrl": target.URL}}}})
	}))
	defer worker.Close()
	a, _ := testAdmin(t)
	a.sunPanel = &SunPanel{bridgeURL: worker.URL, bridgeKey: make([]byte, 32)}
	call := func(ids string, token string) map[string]any {
		request := httptest.NewRequest("POST", "/sunpanel/api/site/health", strings.NewReader(ids))
		request.Header.Set("token", token)
		response := httptest.NewRecorder()
		a.sunPanelSiteHealth(response, request)
		var result map[string]any
		json.Unmarshal(response.Body.Bytes(), &result)
		return result
	}
	if call(`{"ids":[11]}`, "")["code"] == float64(0) {
		t.Fatal("anonymous probing allowed")
	}
	if targetCalls.Load() != 0 {
		t.Fatal("target contacted before authentication")
	}
	if len(call(`{"ids":[999],"userId":99}`, "TEST_ONLY_PANEL_SESSION")["data"].([]any)) != 0 || targetCalls.Load() != 0 {
		t.Fatal("unowned URL probed")
	}
	result := call(`{"ids":[11],"url":"http://169.254.169.254/"}`, "TEST_ONLY_PANEL_SESSION")
	if result["code"] != float64(0) || targetCalls.Load() != 1 || result["data"].([]any)[0].(map[string]any)["url"] != target.URL {
		t.Fatal("saved owned URL not used or duplicate probe")
	}
	preflight := call(`{"ids":[11],"tlsOnly":true}`, "TEST_ONLY_PANEL_SESSION")
	lan := preflight["data"].([]any)[0].(map[string]any)["lan"].(map[string]any)
	if lan["browser_probe_allowed"] != true || targetCalls.Load() != 1 {
		t.Fatal("saved LAN preflight was not approved or reused from cache")
	}

	// Internal GateHome admin/bridge/mihomo ports must never be probed by SunPanel users.
	var adminHits atomic.Int32
	internalAdmin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		adminHits.Add(1)
		w.WriteHeader(200)
	}))
	defer internalAdmin.Close()
	var internalAdminPort int
	if u, err := url.Parse(internalAdmin.URL); err == nil {
		internalAdminPort = urlEffectivePort(u)
	}
	a.adminPort = internalAdminPort
	var worker2URL string
	worker2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sunpanel/api/user/getInfo" {
			jsonResponse(w, 200, map[string]any{"code": 0, "data": map[string]uint{"id": 7}})
			return
		}
		jsonResponse(w, 200, map[string]any{"data": map[string]any{"items": []map[string]any{{"id": 22, "url": internalAdmin.URL, "lanUrl": worker2URL}}}})
	}))
	defer worker2.Close()
	worker2URL = worker2.URL
	a.sunPanel.bridgeURL = worker2.URL
	blocked := call(`{"ids":[22]}`, "TEST_ONLY_PANEL_SESSION")
	item := blocked["data"].([]any)[0].(map[string]any)
	if item["default"].(map[string]any)["healthy"] != false || item["lan"].(map[string]any)["healthy"] != false {
		t.Fatalf("expected internal admin/bridge ports to be rejected, got %+v", item)
	}
	if adminHits.Load() != 0 {
		t.Fatalf("sunPanelSiteHealth dialed internal admin port %d times", adminHits.Load())
	}
}

func TestServiceHealthMaintenanceModePausesProbingAndAlerts(t *testing.T) {
	var upstreamHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	a, _ := testAdmin(t)
	snap := a.store.Snapshot()
	route := Route{
		ID:          "r-maint",
		GroupID:     snap.Config.Groups[0].ID,
		Name:        "MaintApp",
		Host:        "maint.example.test",
		Upstream:    srv.URL,
		Enabled:     true,
		Maintenance: true,
	}
	cfg := snap.Config
	cfg.Groups[0].Enabled = true
	cfg.Groups[0].HTTPPort = 18080
	cfg.Routes = []Route{route}
	if err := a.store.Update(cfg, nil, snap.Revision); err != nil {
		t.Fatal(err)
	}
	if err := a.store.changeWebhook(func(s *State) bool {
		s.Alerts.Enabled = true
		s.Alerts.Events = []string{"upstream"}
		return true
	}); err != nil {
		t.Fatal(err)
	}

	// Simulate an active prior failure before enabling maintenance.
	a.health.mu.Lock()
	a.health.events = map[string]healthEventState{
		route.ID: {upstream: route.Upstream, failures: 3, firstFailedAt: time.Now().Add(-5 * time.Minute), down: true},
	}
	a.health.mu.Unlock()

	results := a.routeServiceHealth(context.Background())
	if upstreamHits.Load() != 0 {
		t.Fatalf("maintenance mode must not dial upstream, got %d hits", upstreamHits.Load())
	}
	if res := results[route.ID]; res.State != "maintenance" || res.Label != "维护中" {
		t.Fatalf("expected maintenance health state, got %+v", res)
	}
	a.health.mu.Lock()
	ev := a.health.events[route.ID]
	a.health.mu.Unlock()
	if ev.failures != 0 || ev.down || !ev.firstFailedAt.IsZero() {
		t.Fatalf("expected paused maintenance state to reset failure counters, got %+v", ev)
	}
	if len(a.store.Snapshot().Alerts.Pending) != 0 {
		t.Fatalf("expected no alert queued when entering maintenance, got %+v", a.store.Snapshot().Alerts.Pending)
	}
}

func TestForwardTCPHealthProbeAndAlertDelay(t *testing.T) {
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	liveAddr := strings.TrimPrefix(live.URL, "http://")
	defer live.Close()

	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedAddr := strings.TrimPrefix(closed.URL, "http://")
	closed.Close()

	if res := probeTCPHealth(context.Background(), liveAddr); !res.Healthy || res.State != "healthy" || res.Label != "TCP 可达" {
		t.Fatalf("expected live TCP upstream healthy, got %+v", res)
	}
	if res := probeTCPHealth(context.Background(), closedAddr); res.Healthy || res.State != "connection_error" {
		t.Fatalf("expected closed TCP upstream error, got %+v", res)
	}

	a, _ := testAdmin(t)
	snap := a.store.Snapshot()
	cfg := snap.Config
	cfg.ServiceHealth = ServiceHealthConfig{Enabled: true, Interval: 60, AlertDelaySeconds: 180}
	cfg.Forwards = []PortForward{
		{ID: "fwd-tcp", Name: "SSH 转发", Protocol: "tcp", ListenPort: 29122, Upstream: liveAddr, Enabled: true},
		{ID: "fwd-udp", Name: "DNS 转发", Protocol: "udp", ListenPort: 29153, Upstream: closedAddr, Enabled: true},
		{ID: "fwd-off", Name: "停用转发", Protocol: "tcp", ListenPort: 29123, Upstream: closedAddr, Enabled: false},
	}
	if err := a.store.Update(cfg, nil, snap.Revision); err != nil {
		t.Fatal(err)
	}
	if err := a.store.changeWebhook(func(s *State) bool {
		s.Alerts.Enabled = true
		s.Alerts.Events = []string{"upstream"}
		return true
	}); err != nil {
		t.Fatal(err)
	}

	results := a.forwardServiceHealth(context.Background())
	if !results["fwd-tcp"].Healthy || results["fwd-udp"].State != "udp_unsupported" || results["fwd-off"].State != "disabled" {
		t.Fatalf("unexpected forwardServiceHealth results: %+v", results)
	}

	// Verify alert delay + recovery lifecycle for forward health transitions.
	fwd := cfg.Forwards[0]
	t0 := time.Now().UTC().Add(time.Minute)
	failAt := func(off time.Duration) {
		a.recordForwardHealthTransition(fwd, serviceHealthResult{State: "connection_error", Label: "连接失败", CheckedAt: t0.Add(off)})
	}
	failAt(0)
	failAt(120 * time.Second)
	if a.health.events["forward/"+fwd.ID].down || len(a.store.Snapshot().Alerts.Pending) != 0 {
		t.Fatal("forward failure shorter than alert delay triggered early alert")
	}
	failAt(180 * time.Second)
	if !a.health.events["forward/"+fwd.ID].down || len(a.store.Snapshot().Alerts.Pending) != 1 || a.store.Snapshot().Alerts.Pending[0].Type != "upstream" {
		t.Fatalf("expected upstream alert after 180s forward outage, got down=%v pending=%+v", a.health.events["forward/"+fwd.ID].down, a.store.Snapshot().Alerts.Pending)
	}
	a.recordForwardHealthTransition(fwd, serviceHealthResult{State: "healthy", Label: "TCP 可达", Healthy: true, CheckedAt: t0.Add(240 * time.Second)})
	if a.health.events["forward/"+fwd.ID].down || len(a.store.Snapshot().Alerts.Pending) != 2 || a.store.Snapshot().Alerts.Pending[1].Status != "resolved" {
		t.Fatalf("expected resolved alert after forward recovery, got pending=%+v", a.store.Snapshot().Alerts.Pending)
	}
}
