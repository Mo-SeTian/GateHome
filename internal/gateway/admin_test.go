package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testAdmin(t *testing.T) (*Admin, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := HashPassword([]byte("TEST_ONLY_ADMIN_PASSWORD"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPassword(hash); err != nil {
		t.Fatal(err)
	}
	token := "TEST_ONLY_FAKE_TOKEN"
	if err := s.Update(DefaultConfig(), &token, 0); err != nil {
		t.Fatal(err)
	}
	subscriptions, err := NewSubscriptions(dir, s)
	if err != nil {
		t.Fatal(err)
	}
	p := NewProxy(s.Snapshot().Config, subscriptions)
	certs, err := NewCertificates(dir, s)
	if err != nil {
		t.Fatal(err)
	}
	a := NewAdmin(s, p, certs, NewJobs(s, certs), 16666)
	return a, a.Handler()
}

func adminRequest(h http.Handler, method, path string, body any, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "http://localhost:16666"+path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Gatehouse-Request", "1")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func loginForTest(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	w := adminRequest(h, "POST", "/api/login", map[string]string{"password": "TEST_ONLY_ADMIN_PASSWORD"}, nil, "")
	if w.Code != 200 {
		t.Fatal("login failed")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("session cookie missing")
	}
	if !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("insecure session cookie")
	}
	return cookies[0]
}

func TestAdminAuthCSRFAndSecretRedaction(t *testing.T) {
	a, h := testAdmin(t)
	for _, path := range []string{"/api/config", "/api/status", "/api/ddns/records"} {
		if w := adminRequest(h, "GET", path, nil, nil, ""); w.Code != 401 {
			t.Fatal("unauthenticated API access")
		}
	}
	cookie := loginForTest(t, h)
	w := adminRequest(h, "GET", "/api/config", nil, cookie, "")
	if w.Code != 200 {
		t.Fatal("config API failed")
	}
	for _, secret := range []string{"TEST_ONLY_FAKE_TOKEN", a.store.Snapshot().PasswordHash, "password_hash", "cloudflare_token"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("configuration response disclosed a secret field")
		}
	}
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": DefaultConfig(), "revision": 1}, cookie, "https://evil.example")
	if w.Code != 403 {
		t.Fatal("cross-origin write accepted")
	}
	r := httptest.NewRequest("POST", "http://localhost:16666/api/logout", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("write without CSRF header accepted")
	}
	w = adminRequest(h, "POST", "/api/logout", map[string]any{}, cookie, "")
	if w.Code != 200 {
		t.Fatal("logout failed")
	}
	if w = adminRequest(h, "GET", "/api/config", nil, cookie, ""); w.Code != 401 {
		t.Fatal("logged out session remained valid")
	}
}

func TestConfigAPIRejectsInvalidAndStaleWrites(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	c := DefaultConfig()
	c.DDNS.Groups = []DDNSGroup{testDDNSGroup()}
	w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 1}, cookie, "")
	if w.Code != 200 {
		t.Fatal("valid save failed")
	}
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": DefaultConfig(), "revision": 1}, cookie, "")
	if w.Code != 400 {
		t.Fatal("stale save accepted")
	}
	c = DefaultConfig()
	c.Groups[0].HTTPPort = 80
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 2}, cookie, "")
	if w.Code != 400 {
		t.Fatal("invalid save accepted")
	}
	if a.store.Snapshot().Config.DDNS.Groups[0].Zone != "example.com" || a.store.Snapshot().Config.Groups[0].HTTPPort == 80 {
		t.Fatal("invalid write changed config")
	}
	c = DefaultConfig()
	c.Groups[0].HTTPPort = 16666
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 2}, cookie, "")
	if w.Code != 400 {
		t.Fatal("management port conflict accepted")
	}
	w = adminRequest(h, "POST", "/api/jobs/ddns", map[string]any{}, cookie, "")
	if w.Code != 400 {
		t.Fatal("disabled task accepted")
	}
}

func TestLoginRateLimit(t *testing.T) {
	_, h := testAdmin(t)
	for i := 0; i < 9; i++ {
		w := adminRequest(h, "POST", "/api/login", map[string]string{"password": "wrong"}, nil, "")
		if i < 8 && w.Code != 401 {
			t.Fatal("unexpected initial login result")
		}
		if i == 8 && w.Code != 429 {
			t.Fatal("login is not rate limited")
		}
	}
}

func TestStaticAssetsAndSecurityHeaders(t *testing.T) {
	_, h := testAdmin(t)
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		w := adminRequest(h, "GET", path, nil, nil, "")
		if w.Code != 200 || w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("static asset or CSP missing")
		}
	}
	for _, path := range []string{"/state.json", "/certificates/test.json", "/.env"} {
		if w := adminRequest(h, "GET", path, nil, nil, ""); w.Code != 404 {
			t.Fatal("unintended static path served")
		}
	}
}

func TestAdminGroupRestartStatusAndSubscriptionActions(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	c := DefaultConfig()
	c.Groups[0].Name = "家庭服务"
	c.Subscriptions = []Subscription{{ID: "cn", Name: "中国 IPv4", URL: "https://example.com/list.txt", Enabled: true, Interval: 300}, {ID: "off", Name: "停用订阅", URL: "https://example.com/off.txt", Interval: 300}}
	r := testRoute("nas.example.com", "http://localhost:5000")
	testFirewall(&c, &r, "allow", nil, []string{"cn"})
	c.Routes = []Route{r}
	w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 1}, cookie, "")
	if w.Code != 200 {
		t.Fatal("group name and subscription save failed")
	}
	status := func() struct {
		Restart       bool                 `json:"restart_required"`
		Subscriptions []SubscriptionStatus `json:"subscriptions"`
	} {
		var v struct {
			Restart       bool                 `json:"restart_required"`
			Subscriptions []SubscriptionStatus `json:"subscriptions"`
		}
		w := adminRequest(h, "GET", "/api/status", nil, cookie, "")
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal("invalid status response")
		}
		return v
	}
	if v := status(); v.Restart || len(v.Subscriptions) != 2 || v.Subscriptions[0].Ready {
		t.Fatal("name-only change required restart or missing subscription status")
	}
	for _, tc := range []struct {
		id     string
		cookie *http.Cookie
		want   int
	}{{"cn", nil, 401}, {"cn", cookie, 202}, {"missing", cookie, 404}, {"off", cookie, 400}} {
		if w := adminRequest(h, "POST", "/api/subscriptions/"+tc.id+"/refresh", map[string]any{}, tc.cookie, ""); w.Code != tc.want {
			t.Fatalf("unexpected subscription action status: %d", w.Code)
		}
	}
	c.Groups = append(c.Groups, ProxyGroup{ID: "second", Name: "第二组", Enabled: true, HTTPPort: 19080})
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 2}, cookie, "")
	if w.Code != 200 || !status().Restart {
		t.Fatal("added listener did not require restart")
	}
	c.Subscriptions = append([]Subscription(nil), c.Subscriptions...)
	c.Subscriptions[0].Enabled = false
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 3}, cookie, "")
	if w.Code != 400 || !a.store.Snapshot().Config.Subscriptions[0].Enabled {
		t.Fatal("referenced subscription disabled or rejected save changed state")
	}
	c.Subscriptions[0].Enabled = true
	c.Firewalls = []Firewall{}
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 3}, cookie, "")
	if w.Code != 400 || len(a.store.Snapshot().Config.Firewalls) != 1 {
		t.Fatal("referenced firewall deleted or rejected save changed state")
	}
}
