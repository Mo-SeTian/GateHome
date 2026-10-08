package gateway

import (
	"bufio"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	}))
	defer backend.Close()
	c := DefaultConfig()
	c.Routes = []Route{testRoute("ws.example.com", backend.URL)}
	proxy := httptest.NewServer(NewProxy(c, nil))
	defer proxy.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	io.WriteString(conn, "GET /socket HTTP/1.1\r\nHost: ws.example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 101 {
		t.Fatalf("upgrade failed: %d", resp.StatusCode)
	}
	io.WriteString(conn, "bidirectional-test\n")
	line, err := r.ReadString('\n')
	if err != nil || line != "bidirectional-test\n" {
		t.Fatal("duplex stream did not pass through")
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
