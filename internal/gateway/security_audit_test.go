package gateway

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSecurityAuditAllAdminEndpointsRequireSession(t *testing.T) {
	a, h := testAdmin(t)
	before := a.store.Snapshot()
	endpoints := []struct{ method, path string }{
		{"GET", "/api/config"}, {"HEAD", "/api/config"}, {"PUT", "/api/config"},
		{"GET", "/api/account"}, {"PUT", "/api/account"},
		{"GET", "/api/status"}, {"GET", "/api/dashboard"}, {"GET", "/api/logs"}, {"GET", "/api/statistics"},
		{"GET", "/api/ip-blocks"}, {"POST", "/api/ip-blocks"}, {"DELETE", "/api/ip-blocks"},
		{"GET", "/api/ddns/records?refresh=1"}, {"GET", "/api/ddns/network"}, {"POST", "/api/ddns/network"},
		{"POST", "/api/logout"}, {"POST", "/api/ddns/example/run"},
		{"POST", "/api/subscriptions/example/refresh"}, {"POST", "/api/jobs/ddns"}, {"POST", "/api/jobs/acme"},
		{"GET", "/api/maintenance"}, {"POST", "/api/maintenance/restart"},
		{"POST", "/api/maintenance/backup"}, {"POST", "/api/maintenance/inspect-update"},
		{"POST", "/api/maintenance/inspect-backup"}, {"POST", "/api/maintenance/apply-update"},
		{"POST", "/api/maintenance/apply-restore"},
		{"POST", "/api/maintenance/check-online-update"}, {"POST", "/api/maintenance/download-online-update"}, {"GET", "/api/maintenance/online-update-status"},
		{"POST", "/api/service-discovery"}, {"GET", "/api/service-discovery/missing"}, {"POST", "/api/service-discovery/missing/cancel"}, {"GET", "/api/service-discovery/missing/icon/80"},
		{"POST", "/api/route-images/upload"}, {"POST", "/api/route-images/import"}, {"GET", "/api/route-images/missing"}, {"HEAD", "/api/route-images/missing"},
	}
	for _, endpoint := range endpoints {
		for _, value := range []string{"", "TEST_ONLY_FORGED_SESSION", strings.Repeat("0", 64)} {
			r := httptest.NewRequest(endpoint.method, "http://localhost:16666"+endpoint.path, strings.NewReader(`{}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Gatehouse-Request", "1")
			r.Header.Set("Origin", "http://localhost:16666")
			for _, header := range []string{"X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP"} {
				r.Header.Set(header, "127.0.0.1")
			}
			r.Header.Set("X-Forwarded-User", "admin")
			r.Header.Set("X-Forwarded-Proto", "https")
			r.Header.Set("Authorization", "Bearer TEST_ONLY_FORGED_SESSION")
			if value != "" {
				r.AddCookie(&http.Cookie{Name: "gatehouse_session", Value: value})
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Errorf("%s %s: expected 401, got %d", endpoint.method, endpoint.path, w.Code)
			}
			if strings.Contains(w.Body.String(), `"config"`) || strings.Contains(w.Body.String(), "TEST_ONLY_FAKE_TOKEN") {
				t.Fatal("unauthenticated response disclosed configuration")
			}
		}
	}
	if !reflect.DeepEqual(before, a.store.Snapshot()) {
		t.Fatal("unauthenticated requests changed persistent state")
	}
}

func TestManagementCookieIsolationFromBusinessUpstreams(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, cookie := range r.Cookies() {
			if cookie.Name == "gatehouse_session" || strings.HasPrefix(cookie.Name, routeCookiePrefix) {
				t.Error("protected session reached a business upstream")
			}
		}
		if r.Header.Get("Cookie") != "business_session=TEST_ONLY_BUSINESS" {
			t.Error("business cookies were unexpectedly changed")
		}
		w.Header().Add("Set-Cookie", "gatehouse_session=TEST_ONLY_FORGED; Path=/")
		w.Header().Add("Set-Cookie", "business_session=TEST_ONLY_NEW; Path=/")
		w.WriteHeader(200)
	}))
	defer backend.Close()
	c := DefaultConfig()
	c.Routes = []Route{{GroupID: "default", Host: "service.example.test", Upstream: backend.URL, Enabled: true}}
	p := NewProxy(c, nil)
	r := httptest.NewRequest("GET", "http://service.example.test/", nil)
	r.Header.Add("Cookie", "gatehouse_session=TEST_ONLY_ADMIN; business_session=TEST_ONLY_BUSINESS")
	r.Header.Add("Cookie", routeCookiePrefix+"test=TEST_ONLY_ROUTE; gatehouse_session=TEST_ONLY_DUPLICATE")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].Name != "business_session" {
		t.Fatal("business backend could overwrite a protected session")
	}
}

func TestAdminCrossOriginReadsAndIndependentLoginLimits(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	for _, path := range []string{"/api/config", "/api/status", "/api/maintenance", "/api/logs"} {
		if w := adminRequest(h, "GET", path, nil, cookie, "https://evil.example.test"); w.Code != 403 {
			t.Fatal("cross-origin read with a session was accepted")
		}
	}
	r := httptest.NewRequest("GET", "http://localhost:16666/api/config", nil)
	r.AddCookie(cookie)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-site navigation could read configuration")
	}
	// A hostile connection must not exhaust the limit of an unrelated client.
	for i := 0; i < 8; i++ {
		request := httptest.NewRequest("POST", "http://localhost:16666/api/login", strings.NewReader(`{"password":"TEST_ONLY_WRONG"}`))
		request.RemoteAddr = "192.0.2.10:1234"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Gatehouse-Request", "1")
		h.ServeHTTP(httptest.NewRecorder(), request)
	}
	loginForTest(t, h)
	if len(a.attempts) > 1024 || a.activeLogins != 0 {
		t.Fatal("login limiter did not release resources")
	}
}

func TestSecurityAuditLoginInjectionAndStrictJSON(t *testing.T) {
	_, h := testAdmin(t)
	for _, body := range []string{
		`{"password":"' OR 1=1 --"}`, `{"password":{"$ne":null}}`,
		`{"password":"$(id); whoami"}`, `{"password":"<svg onload=alert(1)>"}`,
		`{"password":null}`, `{"password":"TEST_ONLY_ADMIN_PASSWORD","admin":true}`,
		`{"password":"TEST_ONLY_ADMIN_PASSWORD"}{"admin":true}`,
	} {
		r := httptest.NewRequest("POST", "http://localhost:16666/api/login", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Gatehouse-Request", "1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if (w.Code != 400 && w.Code != 401) || len(w.Result().Cookies()) != 0 {
			t.Fatalf("invalid login created a session or returned unexpected status %d", w.Code)
		}
	}
	loginForTest(t, h)
}

func TestSecurityAuditSessionExpiryAndRestart(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	a.mu.Lock()
	a.sessions[sha256.Sum256([]byte(cookie.Value))] = time.Now().Add(-time.Second)
	a.mu.Unlock()
	if w := adminRequest(h, "GET", "/api/config", nil, cookie, ""); w.Code != 401 {
		t.Fatal("expired session accepted")
	}
	cookie = loginForTest(t, h)
	restarted := NewAdmin(a.store, a.proxy, a.certs, a.jobs, a.adminPort)
	if w := adminRequest(restarted.Handler(), "GET", "/api/config", nil, cookie, ""); w.Code != 401 {
		t.Fatal("session survived a new admin instance")
	}
}

func TestSecurityAuditPathAndMethodConfusion(t *testing.T) {
	a, h := testAdmin(t)
	before := a.store.Snapshot()
	paths := []string{"/api/config", "/api/%63onfig", "/api%2fconfig", "/api/config/", "/api/config;anything", "/api/../api/config", "//api/config", "/api//config", "/api/config%00", "/state.json", "/data/state.json", "/../state.json", "/%2e%2e/state.json", "/.env", "/certificates/production-example.json"}
	for _, path := range paths {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CONNECT"} {
			r := httptest.NewRequest(method, "http://localhost:16666"+path, strings.NewReader(`{}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Gatehouse-Request", "1")
			r.Header.Set("X-HTTP-Method-Override", "GET")
			r.Header.Set("X-Original-URL", "/api/config")
			r.Header.Set("X-Rewrite-URL", "/api/config")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code < 300 || w.Code >= 500 || strings.Contains(w.Body.String(), `"config"`) {
				t.Errorf("%s %s: unexpected result %d", method, path, w.Code)
			}
			if location := w.Header().Get("Location"); strings.HasPrefix(location, "/api/") {
				follow := adminRequest(h, "GET", location, nil, nil, "")
				if follow.Code < 400 {
					t.Fatal("canonical redirect bypassed authentication")
				}
			}
		}
	}
	if !reflect.DeepEqual(before, a.store.Snapshot()) {
		t.Fatal("path or method confusion changed configuration")
	}
}

func TestSecurityAuditCrossSiteWriteAndPreflight(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	before := a.store.Snapshot()
	for _, headers := range []map[string]string{
		{"Content-Type": "application/json"},
		{"Content-Type": "text/plain", "X-Gatehouse-Request": "1"},
		{"Content-Type": "application/x-www-form-urlencoded", "X-Gatehouse-Request": "1"},
		{"Content-Type": "application/json", "X-Gatehouse-Request": "1", "Origin": "https://evil.example.test"},
		{"Content-Type": "application/json", "X-Gatehouse-Request": "1", "Origin": "null"},
	} {
		r := httptest.NewRequest("PUT", "http://localhost:16666/api/config", strings.NewReader(`{}`))
		r.AddCookie(cookie)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("cross-site write returned %d", w.Code)
		}
	}
	r := httptest.NewRequest("OPTIONS", "http://localhost:16666/api/config", nil)
	r.Header.Set("Origin", "https://evil.example.test")
	r.Header.Set("Access-Control-Request-Method", "PUT")
	r.Header.Set("Access-Control-Request-Headers", "content-type,x-gatehouse-request")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Code < 400 {
		t.Fatal("cross-site preflight granted access")
	}
	if !reflect.DeepEqual(before, a.store.Snapshot()) {
		t.Fatal("cross-site request changed configuration")
	}
}
