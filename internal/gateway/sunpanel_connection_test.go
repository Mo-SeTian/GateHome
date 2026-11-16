package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestSunPanelConnectionRequiresBothSessionsAndConfiguredOrigin(t *testing.T) {
	a, h := testAdmin(t)
	s := a.store.Snapshot()
	s.Config.SunPanel = SunPanelConfig{Enabled: true, Port: 16680, ExternalURL: "https://page.example.test:18443"}
	s.Config.AdminAccess = AdminAccessConfig{Enabled: true, Origins: []string{"https://gatehome.example.test:18443"}}
	s.Config.Groups = []ProxyGroup{{ID: "web", Name: "Web fixture", Enabled: true, HTTPPort: 18080}}
	s.Config.Routes = []Route{{ID: "nas", GroupID: "web", Host: "nas.example.test", Name: "NAS", Upstream: "http://192.0.2.10:5000", Enabled: true}}
	if err := a.store.Update(s.Config, nil, s.Revision); err != nil {
		t.Fatal(err)
	}
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sunpanel/api/user/getInfo" || r.Header.Get("token") != "TEST_ONLY_PANEL_SESSION" {
			jsonResponse(w, 200, map[string]any{"code": 1001})
			return
		}
		jsonResponse(w, 200, map[string]any{"code": 0, "data": map[string]uint{"id": 7}})
	}))
	defer worker.Close()
	a.SetSunPanel(&SunPanel{bridgeURL: worker.URL})
	panel := a.SunPanelHandler()
	bootstrap := func(token, origin string, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://localhost:16680/api/sunpanel/connection", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("token", token)
		if csrf {
			r.Header.Set("X-Gatehouse-Request", "1")
		}
		w := httptest.NewRecorder()
		panel.ServeHTTP(w, r)
		return w
	}
	page, management := s.Config.SunPanel.ExternalURL, s.Config.AdminAccess.Origins[0]
	if bootstrap("", page, true).Code != 401 || bootstrap("TEST_ONLY_INVALID_SESSION", page, true).Code != 401 || bootstrap("TEST_ONLY_PANEL_SESSION", page, false).Code != 403 || bootstrap("TEST_ONLY_PANEL_SESSION", "https://evil.example", true).Code != 403 {
		t.Fatal("connection discovery bypassed SunPanel login or source validation")
	}
	w := bootstrap("TEST_ONLY_PANEL_SESSION", page, true)
	var info struct {
		Origins []string `json:"origins"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &info) != nil || !reflect.DeepEqual(info.Origins, []string{management}) || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "192.0.2.10") {
		t.Fatal("connection discovery returned route data or incorrect management entrances")
	}
	if adminRequest(h, "POST", "/api/sunpanel/connect", map[string]string{"origin": page}, nil, management).Code != 401 {
		t.Fatal("SunPanel login substituted for GateHome login")
	}
	cookie := loginForTest(t, h)
	before := a.store.Snapshot()
	for _, target := range []string{"https://evil.example", "https://page.example.test:18444", "http://page.example.test:18443", page + "/private", "https://page.example.test.evil:18443"} {
		if adminRequest(h, "POST", "/api/sunpanel/connect", map[string]string{"origin": target}, cookie, management).Code != 403 {
			t.Fatal("unconfigured recipient accepted")
		}
	}
	if adminRequest(h, "POST", "/api/sunpanel/connect", map[string]string{"origin": page}, cookie, page).Code != 403 {
		t.Fatal("direct cross-origin credential request accepted")
	}
	w = adminRequest(h, "POST", "/api/sunpanel/connect", map[string]string{"origin": page}, cookie, management)
	var routes []map[string]string
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &routes) != nil || len(routes) != 1 || len(routes[0]) != 6 || routes[0]["title"] != "NAS" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "" || !reflect.DeepEqual(before, a.store.Snapshot()) {
		t.Fatal("connection response modified configuration, broadened CORS or exposed unexpected fields")
	}
	r := httptest.NewRequest("POST", "/api/sunpanel/connect", strings.NewReader(`{"origin":"https://page.example.test:18443"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Gatehouse-Request", "1")
	r = r.WithContext(context.WithValue(r.Context(), mcpInternalKey{}, true))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("MCP bypassed browser-session connection boundary")
	}
	for _, native := range []struct {
		panelPort, adminPort int
		origin, expected     string
	}{
		{80, 16666, "http://localhost", "http://localhost:16666"},
		{16680, 80, "http://localhost:16680", "http://localhost"},
	} {
		s := a.store.Snapshot()
		s.Config.SunPanel = SunPanelConfig{Enabled: true, Port: native.panelPort}
		s.Config.AdminAccess = AdminAccessConfig{}
		if err := a.store.Update(s.Config, nil, s.Revision); err != nil {
			t.Fatal(err)
		}
		a.adminPort = native.adminPort
		r := httptest.NewRequest("POST", native.origin+"/api/sunpanel/connection", strings.NewReader(`{}`))
		r.Header.Set("Origin", native.origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Gatehouse-Request", "1")
		r.Header.Set("token", "TEST_ONLY_PANEL_SESSION")
		w := httptest.NewRecorder()
		panel.ServeHTTP(w, r)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &info) != nil || !reflect.DeepEqual(info.Origins, []string{native.expected}) {
			t.Fatal("native default-port entrance was not normalized")
		}
	}
}

func TestSunPanelConnectionInfersOnlyEnabledLocalPanelRoutes(t *testing.T) {
	c := DefaultConfig()
	c.SunPanel = SunPanelConfig{Enabled: true, Port: 16680}
	c.Groups = []ProxyGroup{{ID: "web", Enabled: true, HTTPSPort: 18443}}
	c.Routes = []Route{{GroupID: "web", Host: "page.example.test", Upstream: "http://127.0.0.1:16680/sunpanel/", Enabled: true, TLS: true}, {GroupID: "web", Host: "other.example.test", Upstream: "http://192.0.2.15:16680", Enabled: true, TLS: true}}
	origins := sunPanelOrigins(c)
	if !origins["https://page.example.test:18443"] || origins["https://other.example.test:18443"] || len(origins) != 1 {
		t.Fatal("public panel origin inference accepted another service")
	}
	c.Routes[0].Enabled = false
	if len(sunPanelOrigins(c)) != 0 {
		t.Fatal("disabled panel route remained trusted")
	}
}
