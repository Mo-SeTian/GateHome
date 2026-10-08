package gateway

import (
	"bufio"
	"crypto/sha256"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const testRoutePassword = "TEST_ONLY_SERVICE_PASSWORD"

func routeAuthRequest(p *Proxy, group, method, target string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	r.RemoteAddr = "198.51.100.8:4000"
	r.Header.Set("Accept", "text/html")
	if method == "POST" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "http://"+r.Host)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	p.Handler(group).ServeHTTP(w, r)
	return w
}

func routeLoginTest(t *testing.T, p *Proxy, group, host, username, password string) *http.Cookie {
	t.Helper()
	w := routeAuthRequest(p, group, "POST", "http://"+host+routeAuthPath, url.Values{"username": {username}, "password": {password}, "return": {"/private?view=1"}})
	if w.Code != 303 || w.Header().Get("Location") != "/private?view=1" || len(w.Result().Cookies()) != 1 {
		t.Fatalf("service login failed: HTTP %d", w.Code)
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("service cookie protections missing")
	}
	return cookie
}

func TestRouteAuthBlocksBeforeUpstreamAndKeepsAdminSeparate(t *testing.T) {
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		for _, cookie := range r.Cookies() {
			if strings.HasPrefix(cookie.Name, routeCookiePrefix) {
				t.Error("service session forwarded to upstream")
			}
		}
		if r.Header.Get("Authorization") != "Bearer TEST_ONLY_BACKEND_AUTH" {
			t.Error("backend authorization was changed")
		}
		w.Header().Add("Set-Cookie", routeCookiePrefix+"forged=TEST_ONLY_VALUE; Path=/")
		w.Header().Add("Set-Cookie", "backend_session=TEST_ONLY_BACKEND_VALUE; Path=/")
		io.WriteString(w, "BACKEND_ONLY_CONTENT")
	}))
	defer backend.Close()
	c := DefaultConfig()
	r := testRoute("nas.example.test", backend.URL)
	r.Auth = RouteAuthConfig{Enabled: true, Username: "nas-user"}
	c.Routes = []Route{r}
	hash, _ := HashPassword([]byte(testRoutePassword))
	s := State{Config: c, RoutePasswordHashes: map[string]string{routeKey(r): hash}}
	p := NewProxy(c, nil)
	p.ConfigureState(s)
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "OPTIONS"} {
		w := routeAuthRequest(p, "default", method, "http://nas.example.test/private", nil)
		if w.Code != 401 || strings.Contains(w.Body.String(), "BACKEND_ONLY_CONTENT") {
			t.Fatal("unauthenticated request reached upstream")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("upstream called before verification")
	}
	page := routeAuthRequest(p, "default", "GET", "http://nas.example.test/private", nil)
	if page.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatal("native login form would suppress the Origin required for source validation")
	}
	if !strings.Contains(page.Body.String(), "不是 Gatehouse 管理员账号") || !strings.Contains(page.Body.String(), "访问账号") {
		t.Fatal("service login page did not distinguish accounts")
	}
	for _, credentials := range [][2]string{{"nas-user", "TEST_ONLY_ADMIN_PASSWORD"}, {"other-user", testRoutePassword}, {"' OR 1=1 --", testRoutePassword}} {
		w := routeAuthRequest(p, "default", "POST", "http://nas.example.test"+routeAuthPath, url.Values{"username": {credentials[0]}, "password": {credentials[1]}, "return": {"/"}})
		if w.Code != 401 || len(w.Result().Cookies()) != 0 {
			t.Fatal("wrong service credentials accepted")
		}
	}
	cookie := routeLoginTest(t, p, "default", r.Host, r.Auth.Username, testRoutePassword)
	req := httptest.NewRequest("GET", "http://nas.example.test/private", nil)
	req.AddCookie(cookie)
	req.AddCookie(&http.Cookie{Name: routeCookiePrefix + "other", Value: "TEST_ONLY_OTHER"})
	req.Header.Set("Authorization", "Bearer TEST_ONLY_BACKEND_AUTH")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.String() != "BACKEND_ONLY_CONTENT" || calls.Load() != 1 {
		t.Fatal("verified visitor could not access upstream")
	}
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].Name != "backend_session" {
		t.Fatal("upstream injected a reserved service cookie")
	}
	_, admin := testAdmin(t)
	if w := adminRequest(admin, "GET", "/api/config", nil, cookie, ""); w.Code != 401 {
		t.Fatal("service session granted administrator access")
	}
	adminCookie := loginForTest(t, admin)
	if w := routeAuthRequest(p, "default", "GET", "http://nas.example.test/private", nil, adminCookie); w.Code != 401 {
		t.Fatal("administrator cookie bypassed service login")
	}
}

func TestRouteAuthIsolationReloadRevocationAndLogout(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer backend.Close()
	c := DefaultConfig()
	first := testRoute("nas.example.test", backend.URL)
	first.Auth = RouteAuthConfig{Enabled: true, Username: "nas-user"}
	second := first
	second.Host = "photos.example.test"
	third := first
	third.GroupID = "second"
	c.Groups = append(c.Groups, ProxyGroup{ID: "second", Name: "Second", Enabled: true, HTTPPort: 19080})
	c.Routes = []Route{first, second, third}
	hash, _ := HashPassword([]byte(testRoutePassword))
	s := State{Config: c, RoutePasswordHashes: map[string]string{routeKey(first): hash, routeKey(second): hash, routeKey(third): hash}}
	p := NewProxy(c, nil)
	p.ConfigureState(s)
	cookie := routeLoginTest(t, p, "default", first.Host, "nas-user", testRoutePassword)
	for _, item := range []struct{ group, host string }{{"default", second.Host}, {"second", first.Host}} {
		if routeAuthRequest(p, item.group, "GET", "http://"+item.host+"/", nil, cookie).Code != 401 {
			t.Fatal("session crossed route boundary")
		}
		g := p.routes.Load().(map[string]proxyRoute)[item.group+"/"+item.host].auth
		forged := &http.Cookie{Name: g.cookieName, Value: cookie.Value}
		if routeAuthRequest(p, item.group, "GET", "http://"+item.host+"/", nil, forged).Code != 401 {
			t.Fatal("renamed session crossed route boundary")
		}
	}
	p.ConfigureState(s)
	if routeAuthRequest(p, "default", "GET", "http://"+first.Host+"/", nil, cookie).Code != 200 {
		t.Fatal("unrelated config reload logged visitor out")
	}
	newHash, _ := HashPassword([]byte("TEST_ONLY_NEW_SERVICE_PASSWORD"))
	s.RoutePasswordHashes[routeKey(first)] = newHash
	p.ConfigureState(s)
	if routeAuthRequest(p, "default", "GET", "http://"+first.Host+"/", nil, cookie).Code != 401 {
		t.Fatal("password change kept old session valid")
	}
	cookie = routeLoginTest(t, p, "default", first.Host, "nas-user", "TEST_ONLY_NEW_SERVICE_PASSWORD")
	w := routeAuthRequest(p, "default", "POST", "http://"+first.Host+routeAuthPath+"/logout", nil, cookie)
	if w.Code != 303 || routeAuthRequest(p, "default", "GET", "http://"+first.Host+"/", nil, cookie).Code != 401 {
		t.Fatal("logout failed to revoke visitor")
	}
	cookie = routeLoginTest(t, p, "default", first.Host, "nas-user", "TEST_ONLY_NEW_SERVICE_PASSWORD")
	s.Config.Routes[0].Auth.Enabled = false
	p.ConfigureState(s)
	if routeAuthRequest(p, "default", "POST", "http://"+first.Host+routeAuthPath, url.Values{"username": {"nas-user"}, "password": {testRoutePassword}, "return": {"/"}}).Code != 404 {
		t.Fatal("stale login form was forwarded after disabling verification")
	}
	if routeAuthRequest(p, "default", "GET", "http://"+first.Host+"/", nil).Code != 200 {
		t.Fatal("disabled service authentication still blocked access")
	}
	s.Config.Routes[0].Auth.Enabled = true
	p.ConfigureState(s)
	if routeAuthRequest(p, "default", "GET", "http://"+first.Host+"/", nil, cookie).Code != 401 {
		t.Fatal("reenabling reused revoked visitor session")
	}
	cookie = routeLoginTest(t, p, "default", first.Host, "nas-user", "TEST_ONLY_NEW_SERVICE_PASSWORD")
	gate := p.routes.Load().(map[string]proxyRoute)[routeKey(first)].auth
	gate.mu.Lock()
	gate.sessions[sha256.Sum256([]byte(cookie.Value))] = time.Now().Add(-time.Second)
	gate.mu.Unlock()
	if routeAuthRequest(p, "default", "GET", "http://"+first.Host+"/", nil, cookie).Code != 401 {
		t.Fatal("expired service session accepted")
	}
	restarted := NewProxy(s.Config, nil)
	restarted.ConfigureState(s)
	if routeAuthRequest(restarted, "default", "GET", "http://"+first.Host+"/", nil, cookie).Code != 401 {
		t.Fatal("service session survived restart")
	}
}

func TestRouteAuthOriginTLSRateLimitsAndRedirects(t *testing.T) {
	hash, _ := HashPassword([]byte(testRoutePassword))
	g := newRouteGate("default/nas.example.test", "nas-user", hash)
	route := testRoute("nas.example.test", "http://127.0.0.1:1")
	form := url.Values{"username": {"nas-user"}, "password": {testRoutePassword}, "return": {"//evil.example.test"}}
	for _, origin := range []string{"", "null", "https://evil.example.test", "http://nas.example.test"} {
		r := httptest.NewRequest("POST", "https://nas.example.test"+routeAuthPath, strings.NewReader(form.Encode()))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		g.authorize(w, r, route)
		if w.Code != 403 {
			t.Fatal("cross-origin service login accepted")
		}
	}
	r := httptest.NewRequest("POST", "https://nas.example.test"+routeAuthPath, strings.NewReader(form.Encode()))
	r.Header.Set("Origin", "https://nas.example.test")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	g.authorize(w, r, route)
	if w.Code != 303 || w.Header().Get("Location") != "/" || !w.Result().Cookies()[0].Secure {
		t.Fatal("TLS login or safe redirect failed")
	}
	for _, value := range []string{"//evil.test", "https://evil.test", "/\\evil.test", "/%2f%2fevil.test", "/%5cevil.test", "/%0d%0aevil", "/.gatehouse-auth", "/path#fragment"} {
		if safeRouteReturn(value) != "/" {
			t.Fatal("unsafe return destination accepted")
		}
	}
	if safeRouteReturn("/photos?sort=new") != "/photos?sort=new" {
		t.Fatal("safe original location not preserved")
	}
	for i := 0; i < 8; i++ {
		r := httptest.NewRequest("POST", "http://nas.example.test", nil)
		r.RemoteAddr = "198.51.100.8:54321"
		if !g.beginAttempt(r) {
			t.Fatal("early per-client lockout")
		}
		g.mu.Lock()
		g.active--
		g.mu.Unlock()
	}
	r = httptest.NewRequest("POST", "http://nas.example.test", nil)
	r.RemoteAddr = "198.51.100.8:54321"
	r.Header.Set("X-Forwarded-For", "198.51.100.9")
	if g.beginAttempt(r) {
		t.Fatal("forged proxy IP bypassed service limit")
	}
	r.RemoteAddr = "198.51.100.9:54321"
	if !g.beginAttempt(r) {
		t.Fatal("one visitor locked out a different source")
	}
	g.mu.Lock()
	g.active--
	g.mu.Unlock()
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(testRoutePassword)) != nil {
		t.Fatal("fixture hash invalid")
	}
}

func TestRouteAuthWebSocketAndFirewall(t *testing.T) {
	var upgrades atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrades.Add(1)
		for _, cookie := range r.Cookies() {
			if strings.HasPrefix(cookie.Name, routeCookiePrefix) {
				t.Error("service session reached websocket backend")
			}
		}
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
	}))
	defer backend.Close()
	c := DefaultConfig()
	route := testRoute("ws.example.test", backend.URL)
	route.Auth = RouteAuthConfig{Enabled: true, Username: "ws-user"}
	c.Routes = []Route{route}
	hash, _ := HashPassword([]byte(testRoutePassword))
	s := State{Config: c, RoutePasswordHashes: map[string]string{routeKey(route): hash}}
	p := NewProxy(c, nil)
	p.ConfigureState(s)
	req := httptest.NewRequest("GET", "http://ws.example.test/socket", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code != 401 || upgrades.Load() != 0 {
		t.Fatal("unauthenticated websocket reached upstream")
	}
	cookie := routeLoginTest(t, p, "default", route.Host, "ws-user", testRoutePassword)
	front := httptest.NewServer(p)
	defer front.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(front.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	io.WriteString(conn, "GET /socket HTTP/1.1\r\nHost: ws.example.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nCookie: "+cookie.Name+"="+cookie.Value+"\r\n\r\n")
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil || response.StatusCode != 101 {
		t.Fatal("verified websocket upgrade failed")
	}
	io.WriteString(conn, "TEST_ONLY_WEBSOCKET_MESSAGE\n")
	line, err := reader.ReadString('\n')
	if err != nil || line != "TEST_ONLY_WEBSOCKET_MESSAGE\n" {
		t.Fatal("verified websocket duplex failed")
	}
	conn.Close()
	// A verified account still cannot bypass the route's IP firewall.
	s.Config.Firewalls = []Firewall{{ID: "deny-all", Name: "Deny all", DefaultAction: "deny"}}
	s.Config.Routes[0].FirewallID = "deny-all"
	p.ConfigureState(s)
	if routeAuthRequest(p, "default", "GET", "http://ws.example.test/", nil, cookie).Code != 403 {
		t.Fatal("service login bypassed IP firewall")
	}
}
