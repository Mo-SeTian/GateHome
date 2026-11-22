package gateway

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRuntimeInventoryTracksWireGuardListener(t *testing.T) {
	a, handler := testAdmin(t)
	t.Cleanup(a.CloseRemoteHome)
	cookie := loginForTest(t, handler)
	read := func() []runtimeListener {
		t.Helper()
		w := adminRequest(handler, "GET", "/api/runtime", nil, cookie, "")
		var result struct{ Listeners []runtimeListener }
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatalf("runtime inventory returned HTTP %d", w.Code)
		}
		var wireGuard []runtimeListener
		for _, listener := range result.Listeners {
			if listener.Purpose == "远程回家" {
				wireGuard = append(wireGuard, listener)
			}
		}
		return wireGuard
	}
	expectPort := func(port int) {
		t.Helper()
		want := []runtimeListener{{Port: port, IP: "*", Protocol: "UDP", Purpose: "远程回家", Services: []string{"WireGuard"}}}
		if got := read(); !reflect.DeepEqual(got, want) {
			t.Fatalf("WireGuard listeners: got %+v, want %+v", got, want)
		}
	}
	if got := read(); len(got) != 0 {
		t.Fatal("unconfigured WireGuard appeared in the inventory")
	}
	settings := homeInput{Endpoint: "vpn.example.test", Port: unusedHomePort(t), LAN: "192.168.2.0/24", Tunnel: "10.77.0.0/24"}
	save := func(want int) {
		t.Helper()
		w := adminRequest(handler, "POST", "/api/remote-home/settings", settings, cookie, "")
		if w.Code != want {
			t.Fatalf("save WireGuard settings returned HTTP %d, want %d", w.Code, want)
		}
	}
	save(http.StatusOK)
	expectPort(settings.Port)
	initialPort := settings.Port

	occupied, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { occupied.Close() })
	settings.Port = occupied.LocalAddr().(*net.UDPAddr).Port
	save(http.StatusBadRequest)
	expectPort(initialPort)
	occupied.Close()
	save(http.StatusOK)
	expectPort(settings.Port)

	a.CloseRemoteHome()
	if got := read(); len(got) != 0 {
		t.Fatal("closed WireGuard listener was still reported")
	}
}

func TestRuntimeInventoryAndHistory(t *testing.T) {
	a, handler := testAdmin(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listen := func() net.Listener {
		t.Helper()
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { l.Close() })
		return l
	}
	admin, proxy, panel, bridge := listen(), listen(), listen(), listen()
	forwardTCP := listen()
	port := forwardTCP.Addr().(*net.TCPAddr).Port
	forwardUDP, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { forwardUDP.Close() })
	config := DefaultConfig()
	config.Groups[0].HTTPPort = proxy.Addr().(*net.TCPAddr).Port
	config.Groups[0].HTTPSPort = 0
	config.SunPanel = SunPanelConfig{Enabled: true, Port: panel.Addr().(*net.TCPAddr).Port}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	config.Routes = []Route{
		{ID: "one", Name: "第一条", GroupID: "default", Host: "one.example.test", Upstream: upstream.URL, Enabled: true},
		{ID: "two", Name: "第二条", GroupID: "default", Host: "two.example.test", Upstream: upstream.URL, Enabled: true},
	}
	a.SetHTTPListeners([]net.Listener{admin, proxy, panel})
	a.ports = config
	panelWorker := &SunPanel{}
	panelWorker.bridgePort.Store(int32(bridge.Addr().(*net.TCPAddr).Port))
	a.SetSunPanel(panelWorker)
	a.SetPortForwarder(&PortForwarder{bindings: []*forwardBinding{
		{rule: PortForward{Name: "双协议"}, tcp: forwardTCP},
		{rule: PortForward{Name: "双协议"}, udp: forwardUDP},
	}})
	if err := a.store.Update(config, nil, a.store.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	if err := a.proxy.Configure(config); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	a.recordHealthTransition(config.Routes[0], serviceHealthResult{CheckedAt: now.Add(-2 * time.Minute), Label: "响应超时"})
	a.routeServiceHealth(ctx)
	a.flushHealthHistory()
	cookie := loginForTest(t, handler)
	if code := adminRequest(handler, "GET", "/api/runtime", nil, nil, "").Code; code != 401 {
		t.Fatal("runtime status requires login")
	}
	panelWorker.mu.Lock()
	read := adminRequest(handler, "GET", "/api/runtime", nil, cookie, "")
	panelWorker.mu.Unlock()
	var runtime struct {
		Listeners []runtimeListener
		Routes    []runtimeRoute
	}
	if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &runtime) != nil || len(runtime.Listeners) != 6 || len(runtime.Routes) != 2 {
		t.Fatal("incomplete runtime inventory")
	}
	var protocols []string
	for _, row := range runtime.Listeners {
		if row.Port == port {
			protocols = append(protocols, row.Protocol)
		}
		if row.Port == config.Groups[0].HTTPPort && len(row.Services) != 2 {
			t.Fatal("shared proxy port should list both services")
		}
	}
	if strings.Join(protocols, ",") != "TCP,UDP" || runtime.Routes[0].Health == nil || !runtime.Routes[0].Health.Healthy {
		t.Fatal("missing protocol or health information")
	}

	// Reopen the store and read the route's persisted samples before any new probe.
	reopened, err := OpenStore(a.store.paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	newAdmin := &Admin{store: reopened}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.SetPathValue("id", "one")
	newAdmin.routeHistory(rec, req)
	var result struct{ Samples []healthSample }
	if json.Unmarshal(rec.Body.Bytes(), &result) != nil || len(result.Samples) != 2 || result.Samples[0].Healthy || !result.Samples[1].Healthy {
		t.Fatal("route samples did not survive restart")
	}
}
