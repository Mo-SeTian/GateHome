package gateway

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAdminUpstreamClassification(t *testing.T) {
	addresses := map[netip.Addr]bool{netip.MustParseAddr("192.168.2.25"): true, netip.MustParseAddr("2001:db8::25"): true}
	for _, item := range []struct {
		url     string
		allowed bool
	}{
		{"http://192.168.2.25:16666/", true}, {"http://127.0.0.1:16666/", true}, {"http://localhost:16666", true},
		{"http://[::1]:16666/", true}, {"http://[2001:db8::25]:16666/", true}, {"http://[::ffff:192.168.2.25]:16666/", true},
		{"http://192.168.2.26:16666/", false}, {"http://192.168.2.25:7777/", false}, {"http://example.test:16666/", false},
		{"http://localhost.evil.test:16666/", false}, {"http://0.0.0.0:16666/", false}, {"http://[::]:16666/", false},
	} {
		u, err := url.Parse(item.url)
		if err != nil || isAdminUpstream(u, 16666, addresses) != item.allowed {
			t.Errorf("incorrect local management classification for %s", item.url)
		}
		if isAdminUpstream(u, 0, addresses) {
			t.Fatal("unset management port trusted a backend")
		}
	}
}

func TestAdminLANUpstreamKeepsSession(t *testing.T) {
	var ip netip.Addr
	for address := range localAdminAddresses() {
		if address.Is4() && !address.IsLoopback() && address.IsGlobalUnicast() {
			ip = address
			break
		}
	}
	if !ip.IsValid() {
		t.Skip("no local non-loopback IPv4 interface")
	}
	a, h := testAdmin(t)
	listener, err := net.Listen("tcp", net.JoinHostPort(ip.String(), "0"))
	if err != nil {
		t.Fatal("local test management listener failed")
	}
	backend := httptest.NewUnstartedServer(h)
	backend.Listener.Close()
	backend.Listener = listener
	backend.Start()
	defer backend.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	a.proxy.adminPort, _ = strconv.Atoi(port)
	c := a.store.Snapshot().Config
	c.Routes = []Route{{GroupID: "default", Host: "console.example.test", Upstream: backend.URL + "/", Enabled: true, TLS: true}}
	a.proxy.Configure(c)
	front := httptest.NewTLSServer(a.proxy.Handler("default"))
	defer front.Close()
	_, publicPort, _ := net.SplitHostPort(strings.TrimPrefix(front.URL, "https://"))
	publicHost := "console.example.test:" + publicPort
	publicOrigin := "https://" + publicHost
	c.AdminAccess = AdminAccessConfig{Enabled: true, Origins: []string{publicOrigin}}
	if a.store.Update(c, nil, a.store.Snapshot().Revision) != nil {
		t.Fatal("test reverse proxy access setup failed")
	}
	request := func(method, path string, body any, cookie *http.Cookie) *http.Response {
		data, _ := json.Marshal(body)
		r, _ := http.NewRequest(method, front.URL+path, bytes.NewReader(data))
		r.Host = publicHost
		r.Header.Set("Origin", publicOrigin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Gatehouse-Request", "1")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		resp, err := front.Client().Do(r)
		if err != nil {
			t.Fatal("LAN management reverse proxy request failed")
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	login := request("POST", "/api/login", map[string]string{"password": "TEST_ONLY_ADMIN_PASSWORD"}, nil)
	if login.StatusCode != 200 || len(login.Cookies()) != 1 {
		t.Fatal("successful LAN login lost its management session cookie")
	}
	cookie := login.Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure {
		t.Fatal("LAN login weakened cookie protection")
	}
	if resp := request("GET", "/api/config", nil, cookie); resp.StatusCode != 200 {
		t.Fatal("LAN domain login did not remain authenticated")
	}
	if resp := request("PUT", "/api/config", map[string]any{"config": c, "revision": a.store.Snapshot().Revision}, cookie); resp.StatusCode != 200 {
		t.Fatal("LAN management session could not save config")
	}
	if resp := request("GET", "/api/config", nil, nil); resp.StatusCode != 401 {
		t.Fatal("LAN upstream bypassed management login")
	}
}

func TestAdminAccessConfigValidation(t *testing.T) {
	for _, origin := range []string{"https://console.example.test:18443", "https://console.example.test/", "http://127.0.0.1:16668", "https://[::1]:18443"} {
		if err := validateAdminAccess(AdminAccessConfig{Enabled: true, Origins: []string{origin}}); err != nil {
			t.Fatal("valid public address rejected", origin)
		}
	}
	for _, origins := range [][]string{nil, {"*"}, {"null"}, {"https://*.example.test"}, {"https://user:YOUR_PASSWORD_HERE@example.test"}, {"https://console.example.test/path"}, {"https://console.example.test?x=1"}, {"https://console.example.test#x"}, {"https://console.example.test:99999"}, {"https://console.example.test", "https://CONSOLE.example.test:443/"}} {
		if validateAdminAccess(AdminAccessConfig{Enabled: true, Origins: origins}) == nil {
			t.Fatal("invalid or duplicate public address accepted")
		}
	}
}

func TestAdminReverseProxyOriginAndSecureSession(t *testing.T) {
	a, h := testAdmin(t)
	backend := httptest.NewServer(h)
	defer backend.Close()
	_, backendPort, _ := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
	a.proxy.adminPort, _ = strconv.Atoi(backendPort)
	c := a.store.Snapshot().Config
	c.Routes = []Route{{GroupID: "default", Host: "console.example.test", Upstream: backend.URL, Enabled: true, TLS: true}}
	a.proxy.Configure(c)
	front := httptest.NewTLSServer(a.proxy.Handler("default"))
	defer front.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(front.URL, "https://"))
	host := "console.example.test:" + port
	origin := "https://" + host
	request := func(method, path, source string, body any, cookie *http.Cookie) *http.Response {
		t.Helper()
		data, _ := json.Marshal(body)
		r, _ := http.NewRequest(method, front.URL+path, bytes.NewReader(data))
		r.Host = host
		r.Header.Set("Origin", source)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Gatehouse-Request", "1")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		response, err := front.Client().Do(r)
		if err != nil {
			t.Fatal("reverse proxy request failed")
		}
		t.Cleanup(func() { response.Body.Close() })
		return response
	}
	login := map[string]string{"password": "TEST_ONLY_ADMIN_PASSWORD"}
	if response := request("POST", "/api/login", origin, login, nil); response.StatusCode != 403 {
		t.Fatal("HTTPS to HTTP mismatch must reproduce before enabling compatibility")
	}
	c.AdminAccess = AdminAccessConfig{Enabled: true, Origins: []string{origin}}
	if err := a.store.Update(c, nil, a.store.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	response := request("POST", "/api/login", origin, login, nil)
	if response.StatusCode != 200 || len(response.Cookies()) != 1 {
		t.Fatal("login through HTTPS reverse proxy failed")
	}
	cookie := response.Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("reverse proxy session lost its cookie protections")
	}
	if response := request("PUT", "/api/config", origin, map[string]any{"config": c, "revision": a.store.Snapshot().Revision}, cookie); response.StatusCode != 200 {
		t.Fatal("saving through HTTPS reverse proxy failed")
	}
	reopened, err := OpenStore(filepath.Dir(a.store.path))
	if err != nil || !reopened.Snapshot().Config.AdminAccess.Enabled || !allowedAdminOrigin(reopened.Snapshot().Config.AdminAccess, origin) {
		t.Fatal("public access settings were not persisted")
	}
	for _, source := range []string{"https://evil.example.test", origin + ".evil.example.test", "http://" + host, "null"} {
		if response := request("POST", "/api/logout", source, map[string]any{}, cookie); response.StatusCode != 403 {
			t.Fatal("unlisted source was accepted")
		}
	}
	if response := request("GET", "/api/config", origin, nil, nil); response.StatusCode != 401 {
		t.Fatal("enabling reverse proxy bypassed authentication")
	}
	r := httptest.NewRequest("POST", "http://localhost:16666/api/login", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	if a.secureSession(r) {
		t.Fatal("untrusted forwarded header changed session security")
	}
	if response := request("POST", "/api/logout", origin, map[string]any{}, cookie); response.StatusCode != 200 || !response.Cookies()[0].Secure {
		t.Fatal("secure session logout failed")
	}
}
