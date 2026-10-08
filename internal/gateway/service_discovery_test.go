package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

func discoveryTestPort(t *testing.T, address string) int {
	t.Helper()
	u, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func waitDiscovery(t *testing.T, scan *discoveryScan) discoveryStatus {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		status := scan.snapshot()
		if status.State != "running" && status.State != "stopping" {
			return status
		}
		time.Sleep(10 * time.Millisecond)
	}
	scan.stop()
	t.Fatal("discovery did not finish in time")
	return discoveryStatus{}
}

func scanDiscoveryTestServer(t *testing.T, address string) (*discoveryScan, discoveryStatus) {
	t.Helper()
	manager := &serviceDiscovery{}
	scan, err := manager.start(netip.MustParseAddr("127.0.0.1"), []int{discoveryTestPort(t, address)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scan.stop)
	return scan, waitDiscovery(t, scan)
}

func discoveryTestPNG(t *testing.T, width int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, width, 16))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestDiscoveryTargetAndPorts(t *testing.T) {
	for _, ip := range []string{"192.168.2.10", "10.0.0.1", "172.16.0.1", "127.0.0.1", "::1", "fd00::1", "::ffff:192.168.2.10"} {
		if _, err := discoveryIP(ip); err != nil {
			t.Errorf("private target rejected: %s", ip)
		}
	}
	for _, ip := range []string{"example.com", "192.168.2.0/24", "8.8.8.8", "2001:4860::1", "169.254.169.254", "fe80::1", "fd00::1%en0", "0.0.0.0", "224.0.0.1", "192.168.2.10:8080"} {
		if _, err := discoveryIP(ip); err == nil {
			t.Errorf("invalid target accepted: %s", ip)
		}
	}
	ports, err := discoveryPorts(" 443,80,8000-8002,80,65535 ")
	if err != nil || !reflect.DeepEqual(ports, []int{80, 443, 8000, 8001, 8002, 65535}) {
		t.Fatal("ports not sorted and deduplicated")
	}
	for _, input := range []string{"0", "65536", "80-79", "80,,443", "80-", "1-2-3", "abc", strings.Repeat("1,", 1025)} {
		if _, err := discoveryPorts(input); err == nil {
			t.Errorf("invalid ports accepted: %s", input[:min(len(input), 32)])
		}
	}
	defaults, err := discoveryPorts("")
	if err != nil || len(defaults) < 40 || len(defaults) > 100 {
		t.Fatal("invalid common port defaults")
	}
	all, err := discoveryPorts("1-65535")
	if err != nil || len(all) != 65535 {
		t.Fatal("full custom range not supported")
	}
}

func TestDiscoveryHTTPMetadataAndIcon(t *testing.T) {
	icon := discoveryTestPNG(t, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("discovery sent credentials")
		}
		switch r.URL.Path {
		case "/":
			http.Redirect(w, r, "/login", 302)
		case "/login":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, `<title> 家庭 NAS &amp; 相册 </title><link rel="icon" href="/app.png"><script>throw new Error('must not run')</script>`)
		case "/app.png":
			w.Write(icon)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	scan, status := scanDiscoveryTestServer(t, server.URL)
	if status.State != "completed" || status.Completed != 1 || status.OpenTCP != 1 || len(status.Services) != 1 {
		t.Fatal("HTTP service was not discovered")
	}
	service := status.Services[0]
	if service.Name != "家庭 NAS & 相册" || service.Upstream != server.URL || service.Scheme != "http" || service.Status != 200 || service.Icon == "" || service.TLSUntrusted {
		t.Fatal("incorrect HTTP metadata")
	}
	if !bytes.Equal(scan.icons[service.Port].data, icon) {
		t.Fatal("favicon missing")
	}
	status.Services[0].Name = "changed"
	if scan.snapshot().Services[0].Name == "changed" {
		t.Fatal("snapshot aliases mutable services")
	}
	name, _ := discoveryHTML(strings.NewReader(`<meta name="application-name" content="应用名"><title></title>`))
	if name != "应用名" {
		t.Fatal("application-name fallback missing")
	}
	long := discoveryName(strings.Repeat("中文", 100))
	if len(long) > 96 || !utf8.ValidString(long) {
		t.Fatal("name truncation broke UTF-8 or route name limit")
	}
}

func TestDiscoveryDoesNotFollowOtherTargets(t *testing.T) {
	var requests atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(200) }))
	defer other.Close()
	for _, kind := range []string{"redirect", "icon"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					http.NotFound(w, r)
					return
				}
				if kind == "redirect" {
					http.Redirect(w, r, other.URL, 302)
					return
				}
				fmt.Fprintf(w, `<title>Local</title><link rel="icon" href="%s/icon.png">`, other.URL)
			}))
			defer server.Close()
			_, status := scanDiscoveryTestServer(t, server.URL)
			if len(status.Services) != 1 || status.Services[0].RedirectOther != (kind == "redirect") {
				t.Fatal("unsafe redirect not reported")
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatal("scanner followed a different target or port")
	}
	base, _ := url.Parse("http://192.168.2.10:8080/")
	for _, raw := range []string{"https://evil.example/x", "http://user:pass@192.168.2.10:8080/x", "http://192.168.2.11:8080/", "http://192.168.2.10:80/", "file:///tmp/x", "javascript:alert(1)"} {
		if discoveryURL(base, raw) != nil {
			t.Error("disallowed discovery URL accepted")
		}
	}
	if discoveryURL(base, "/favicon.ico") == nil {
		t.Fatal("same target relative URL rejected")
	}
}

func TestDiscoverySelfSignedHTTPS(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, "<title>HTTPS NAS</title>")
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	_, status := scanDiscoveryTestServer(t, server.URL)
	if len(status.Services) != 1 || status.Services[0].Scheme != "https" || !status.Services[0].TLSUntrusted || status.Services[0].Name != "HTTPS NAS" {
		t.Fatal("self-signed HTTPS not identified and flagged")
	}
}

func TestDiscoveryRejectsUnsafeIcons(t *testing.T) {
	ico := make([]byte, 23)
	copy(ico, []byte{0, 0, 1, 0, 1, 0, 16, 16})
	binary.LittleEndian.PutUint32(ico[14:18], 1)
	binary.LittleEndian.PutUint32(ico[18:22], 22)
	brokenICO := append([]byte{}, ico...)
	binary.LittleEndian.PutUint32(brokenICO[18:22], 0xffffffff)
	for _, tc := range []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"png", discoveryTestPNG(t, 16), true}, {"ico", ico, true}, {"broken-ico", brokenICO, false},
		{"svg", []byte(`<svg onload="alert(1)"></svg>`), false}, {"html", []byte(`<img src=x onerror=alert(1)>`), false},
		{"oversize", bytes.Repeat([]byte{0}, (64<<10)+1), false}, {"dimensions", discoveryTestPNG(t, 513), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(tc.data) }))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			if (fetchDiscoveryIcon(context.Background(), server.Client(), base, "/icon") != nil) != tc.valid {
				t.Fatal("unexpected icon validation result")
			}
		})
	}
}

func TestDiscoveryCancellationAndNonWebPorts(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	manager := &serviceDiscovery{}
	scan, err := manager.start(netip.MustParseAddr("127.0.0.1"), []int{discoveryTestPort(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	defer scan.stop()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	if _, err = manager.start(netip.MustParseAddr("127.0.0.1"), []int{80}); err == nil {
		t.Fatal("parallel scans were permitted")
	}
	scan.stop()
	if waitDiscovery(t, scan).State != "cancelled" {
		t.Fatal("scan did not stop")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Write([]byte("SSH-2.0-TestFixture\r\n"))
			conn.Close()
		}
	}()
	_, status := scanDiscoveryTestServer(t, "http://"+listener.Addr().String())
	if status.OpenTCP != 1 || len(status.Services) != 0 {
		t.Fatal("non-Web TCP port treated as a proxy service")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := &discoveryScan{status: discoveryStatus{State: "running"}, cancel: cancel}
	cancelled.run(ctx, netip.MustParseAddr("127.0.0.1"), []int{80, 443})
	if cancelled.snapshot().State != "cancelled" || cancelled.snapshot().Completed != 0 {
		t.Fatal("pre-cancelled job continued scanning")
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	timedOut := &discoveryScan{status: discoveryStatus{State: "running"}, cancel: cancel}
	timedOut.run(ctx, netip.MustParseAddr("127.0.0.1"), []int{80})
	if timedOut.snapshot().State != "timed_out" || timedOut.snapshot().Completed != 0 {
		t.Fatal("expired job did not report timeout")
	}
}

func TestDiscoveryAPIAuthenticationAndConfigurationIsolation(t *testing.T) {
	admin, handler := testAdmin(t)
	for _, path := range []string{"/api/service-discovery", "/api/service-discovery/missing/cancel"} {
		if adminRequest(handler, "POST", path, map[string]string{"ip": "127.0.0.1"}, nil, "").Code != 401 {
			t.Fatal("anonymous scanner mutation allowed")
		}
	}
	for _, path := range []string{"/api/service-discovery/missing", "/api/service-discovery/missing/icon/80"} {
		if adminRequest(handler, "GET", path, nil, nil, "").Code != 401 {
			t.Fatal("anonymous scanner results accessible")
		}
	}
	cookie := loginForTest(t, handler)
	if adminRequest(handler, "POST", "/api/service-discovery", map[string]string{"ip": "127.0.0.1"}, cookie, "https://evil.example").Code != 403 {
		t.Fatal("cross-origin scan allowed")
	}
	r := httptest.NewRequest("POST", "http://localhost:16666/api/service-discovery", strings.NewReader(`{"ip":"127.0.0.1"}`))
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("scan without CSRF header allowed")
	}
	for _, body := range []map[string]string{{"ip": "8.8.8.8"}, {"ip": "127.0.0.1", "ports": "65536"}} {
		if adminRequest(handler, "POST", "/api/service-discovery", body, cookie, "").Code != 400 {
			t.Fatal("invalid scan input accepted")
		}
	}
	icon := discoveryTestPNG(t, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" {
			w.Write(icon)
		} else {
			io.WriteString(w, "<title>Local NAS</title>")
		}
	}))
	defer server.Close()
	revision := admin.store.Snapshot().Revision
	w = adminRequest(handler, "POST", "/api/service-discovery", map[string]string{"ip": "127.0.0.1", "ports": strconv.Itoa(discoveryTestPort(t, server.URL))}, cookie, "")
	if w.Code != 202 {
		t.Fatal("scan API did not accept valid input")
	}
	var response discoveryStatus
	if json.Unmarshal(w.Body.Bytes(), &response) != nil {
		t.Fatal("invalid scanner response")
	}
	status := waitDiscovery(t, admin.discovery.find(response.ID))
	if len(status.Services) != 1 {
		t.Fatal("scan API did not identify service")
	}
	w = adminRequest(handler, "GET", "/api/service-discovery/"+response.ID, nil, cookie, "")
	if w.Code != 200 {
		t.Fatal("authenticated progress unavailable")
	}
	w = adminRequest(handler, "GET", status.Services[0].Icon, nil, cookie, "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Cache-Control") != "no-store" || !bytes.Equal(w.Body.Bytes(), icon) {
		t.Fatal("icon endpoint unsafe or unavailable")
	}
	if admin.store.Snapshot().Revision != revision {
		t.Fatal("scanning modified configuration")
	}
	w = adminRequest(handler, "POST", "/api/service-discovery/"+response.ID+"/cancel", map[string]any{}, cookie, "")
	if w.Code != 200 {
		t.Fatal("completed job cancel was not idempotent")
	}
	if adminRequest(handler, "GET", "/api/service-discovery/expired", nil, cookie, "").Code != 404 {
		t.Fatal("expired scan result not rejected")
	}
}
