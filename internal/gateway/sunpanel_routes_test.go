package gateway

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSunPanelRouteImport(t *testing.T) {
	a, _ := testAdmin(t)
	s := a.store.Snapshot()
	s.Config.Groups = []ProxyGroup{{ID: "web", Name: "家庭", Enabled: true, HTTPPort: 18080, HTTPSPort: 18443}, {ID: "off", Name: "停用", Enabled: false, HTTPPort: 18081}}
	s.Config.Routes = []Route{
		{GroupID: "web", Host: "nas.example.com", Name: "NAS", Upstream: "http://192.168.1.2:5000", Enabled: true, TLS: true},
		{GroupID: "web", Host: "plain.example.com", Upstream: "http://[::1]:8000/", Enabled: true},
		{GroupID: "web", Host: "disabled.example.com", Upstream: "http://localhost:8000"},
		{GroupID: "off", Host: "off.example.com", Upstream: "http://localhost:8000", Enabled: true},
	}
	if err := a.store.Update(s.Config, nil, s.Revision); err != nil {
		t.Fatal(err)
	}
	a.SetSunPanel(NewSunPanel(a.store.paths))
	for _, handler := range []http.Handler{a.Handler(), a.SunPanelHandler()} {
		if w := adminRequest(handler, "GET", "/api/sunpanel/routes", nil, nil, ""); w.Code != 401 {
			t.Fatal("unauthenticated import accepted")
		}
		cookie := loginForTest(t, a.Handler())
		if w := adminRequest(handler, "GET", "/api/sunpanel/routes", nil, cookie, "https://evil.example"); w.Code != 403 {
			t.Fatal("cross-origin import accepted")
		}
		w := adminRequest(handler, "GET", "/api/sunpanel/routes", nil, cookie, "")
		var items []map[string]string
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &items) != nil || len(items) != 2 {
			t.Fatal("route list incorrect")
		}
		if len(items[0]) != 5 || items[0]["id"] == "" || items[0]["url"] != "https://nas.example.com:18443/" || items[0]["lanUrl"] != "http://192.168.1.2:5000" || items[0]["title"] != "NAS" {
			t.Fatal("HTTPS mapping or export fields incorrect")
		}
		if items[1]["url"] != "http://plain.example.com:18080/" || items[1]["lanUrl"] != "http://[::1]:8000/" {
			t.Fatal("HTTP or IPv6 mapping incorrect")
		}
	}
}
