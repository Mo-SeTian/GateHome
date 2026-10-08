package gateway

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOutboundProxyHTTPSConnectAndDirect(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "subscription payload") }))
	defer target.Close()
	u, _ := url.Parse(target.URL)
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || r.Host != u.Host || r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("test:TEST_ONLY_PASSWORD")) {
			t.Error("proxy destination or authentication incorrect")
			http.Error(w, "bad", 400)
			return
		}
		calls.Add(1)
		upstream, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, "bad", 502)
			return
		}
		client, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		defer client.Close()
		defer upstream.Close()
		rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		rw.Flush()
		go func() { io.Copy(upstream, client); upstream.Close() }()
		io.Copy(client, upstream)
	}))
	defer proxy.Close()
	s := State{Config: DefaultConfig(), ProxyPassword: "TEST_ONLY_PASSWORD"}
	s.Config.OutboundProxy = OutboundProxyConfig{Enabled: true, URL: proxy.URL, Username: "test"}
	for _, enabled := range []bool{true, false} {
		s.Config.OutboundProxy.Enabled = enabled
		c := outboundClient(s, time.Second)
		c.Transport.(*http.Transport).TLSClientConfig = target.Client().Transport.(*http.Transport).TLSClientConfig
		r, err := c.Get(target.URL)
		if err != nil {
			t.Fatal("test outbound request failed")
		}
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		closeClient(c)
		if string(body) != "subscription payload" {
			t.Fatal("proxy altered response")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("disabled proxy was used")
	}
}

func TestSOCKS5ProxyConnectsToDestination(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "SOCKS_OK") }))
	defer target.Close()
	u, _ := url.Parse(target.URL)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		header := make([]byte, 2)
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		methods := make([]byte, int(header[1]))
		if _, err := io.ReadFull(conn, methods); err != nil {
			return
		}
		conn.Write([]byte{5, 0})
		req := make([]byte, 4)
		if _, err := io.ReadFull(conn, req); err != nil {
			return
		}
		var host string
		switch req[3] {
		case 1:
			b := make([]byte, 4)
			io.ReadFull(conn, b)
			host = net.IP(b).String()
		case 3:
			b := make([]byte, 1)
			io.ReadFull(conn, b)
			name := make([]byte, int(b[0]))
			io.ReadFull(conn, name)
			host = string(name)
		default:
			return
		}
		port := make([]byte, 2)
		io.ReadFull(conn, port)
		destination := net.JoinHostPort(host, fmt.Sprint(binary.BigEndian.Uint16(port)))
		if destination != u.Host {
			t.Error("SOCKS proxy received wrong destination")
			return
		}
		upstream, err := net.Dial("tcp", destination)
		if err != nil {
			return
		}
		defer upstream.Close()
		conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
		go func() { io.Copy(upstream, conn); upstream.Close() }()
		io.Copy(conn, upstream)
	}()
	s := State{Config: DefaultConfig()}
	s.Config.OutboundProxy = OutboundProxyConfig{Enabled: true, URL: "socks5://" + listener.Addr().String()}
	client := outboundClient(s, 3*time.Second)
	resp, err := client.Get(target.URL)
	if err != nil {
		t.Fatal("SOCKS5 request failed")
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	closeClient(client)
	<-done
	if string(body) != "SOCKS_OK" {
		t.Fatal("SOCKS response mismatch")
	}
}

func TestProxyValidationAndPasswordNotReturned(t *testing.T) {
	for _, raw := range []string{"http://user:TEST_ONLY@localhost:8080", "http://localhost", "ftp://localhost:8080", "socks5://localhost:0", "http://localhost:8080/path"} {
		if validateOutboundProxy(OutboundProxyConfig{Enabled: true, URL: raw}) == nil {
			t.Fatal("invalid proxy accepted")
		}
	}
	for _, scheme := range []string{"http", "https", "socks5", "socks5h"} {
		if err := validateOutboundProxy(OutboundProxyConfig{Enabled: true, URL: scheme + "://localhost:1080"}); err != nil {
			t.Fatal(err)
		}
	}
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	c := DefaultConfig()
	c.OutboundProxy = OutboundProxyConfig{URL: "http://localhost:8080", Username: "test"}
	w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 1, "proxy_password": "TEST_ONLY_PROXY_PASSWORD"}, cookie, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "TEST_ONLY_PROXY_PASSWORD") || a.store.Snapshot().ProxyPassword == "" {
		t.Fatal("proxy password save or redaction failed")
	}
	c.OutboundProxy.URL = "http://localhost:8888"
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 2}, cookie, "")
	if w.Code != 200 || a.store.Snapshot().ProxyPassword != "" {
		t.Fatal("changed proxy reused old credentials")
	}
}
