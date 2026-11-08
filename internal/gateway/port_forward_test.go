package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func testForwardPort(t *testing.T, protocol string) int {
	t.Helper()
	if protocol == "udp" {
		conn, err := net.ListenUDP("udp", &net.UDPAddr{})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		return conn.LocalAddr().(*net.UDPAddr).Port
	}
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func testForward(protocol string, port int, upstream string) PortForward {
	return PortForward{ID: "forward-" + protocol, Name: protocol + " test", Protocol: protocol, ListenPort: port, Upstream: upstream, Enabled: true}
}

func startTestForwards(t *testing.T, c Config) *PortForwarder {
	t.Helper()
	p, err := StartPortForwards(context.Background(), c, NewProxy(c, nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestPortForwardValidation(t *testing.T) {
	base := DefaultConfig()
	base.Forwards = []PortForward{testForward("tcp", 20022, "127.0.0.1:22"), testForward("udp", 20022, "[::1]:53")}
	if err := Validate(base); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Config){
		"unknown protocol": func(c *Config) { c.Forwards[0].Protocol = "http" },
		"invalid ID":       func(c *Config) { c.Forwards[0].ID = "" },
		"duplicate ID":     func(c *Config) { c.Forwards[1].ID = c.Forwards[0].ID },
		"empty name":       func(c *Config) { c.Forwards[0].Name = " " },
		"privileged port":  func(c *Config) { c.Forwards[0].ListenPort = 22 },
		"large port":       func(c *Config) { c.Forwards[0].ListenPort = 65536 },
		"same protocol":    func(c *Config) { c.Forwards[1].Protocol = "tcp" },
		"HTTP conflict":    func(c *Config) { c.Forwards[0].ListenPort = c.Groups[0].HTTPPort },
		"HTTPS conflict":   func(c *Config) { c.Forwards[0].ListenPort = c.Groups[0].HTTPSPort },
		"panel conflict": func(c *Config) {
			c.SunPanel.Enabled = true
			c.Forwards[0].ListenPort = c.SunPanel.Port
		},
		"URL upstream":       func(c *Config) { c.Forwards[0].Upstream = "tcp://127.0.0.1:22" },
		"upstream path":      func(c *Config) { c.Forwards[0].Upstream = "localhost:22/path" },
		"missing port":       func(c *Config) { c.Forwards[0].Upstream = "localhost" },
		"zero upstream port": func(c *Config) { c.Forwards[0].Upstream = "localhost:0" },
		"named port":         func(c *Config) { c.Forwards[0].Upstream = "localhost:ssh" },
		"unspecified target": func(c *Config) { c.Forwards[0].Upstream = "0.0.0.0:22" },
		"multicast target":   func(c *Config) { c.Forwards[0].Upstream = "224.0.0.1:53" },
		"firewall reference": func(c *Config) { c.Forwards[0].FirewallID = "missing" },
		"TCP UDP timeout":    func(c *Config) { c.Forwards[0].UDPIdleSeconds = 300 },
		"short UDP timeout":  func(c *Config) { c.Forwards[1].UDPIdleSeconds = 29 },
		"long UDP timeout":   func(c *Config) { c.Forwards[1].UDPIdleSeconds = 86401 },
		"mapped unspecified": func(c *Config) { c.Forwards[0].Upstream = "[::ffff:0.0.0.0]:22" },
		"local self-loop":    func(c *Config) { c.Forwards[0].Upstream = "localhost:20022" },
		"loopback self-loop": func(c *Config) { c.Forwards[1].Upstream = "[::1]:20022" },
		"too many forwards": func(c *Config) {
			c.Forwards = make([]PortForward, 101)
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := base
			c.Forwards = append([]PortForward{}, base.Forwards...)
			change(&c)
			if Validate(c) == nil {
				t.Fatal("invalid forwarding configuration accepted")
			}
		})
	}
	if validateAdminPort(base, 20022) == nil {
		t.Fatal("management TCP port conflict accepted")
	}
	base.Forwards = base.Forwards[1:]
	base.Forwards[0].ListenPort = base.Groups[0].HTTPPort
	if err := Validate(base); err != nil {
		t.Fatal("UDP incorrectly conflicts with HTTP", err)
	}
	base.Forwards[0].ListenPort = 16666
	if err := validateAdminPort(base, base.Forwards[0].ListenPort); err != nil {
		t.Fatal("UDP incorrectly conflicts with management TCP", err)
	}
	for _, upstream := range []string{"localhost:22", "server.example.test:1234", "[2001:db8::1]:443"} {
		base.Forwards[0].Upstream = upstream
		if err := Validate(base); err != nil {
			t.Fatal(upstream, err)
		}
	}
}

func TestPortForwardTCPHalfCloseAndShutdown(t *testing.T) {
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	payload := bytes.Repeat([]byte{0, 1, 128, 255}, 128<<10)
	serverDone := make(chan error, 1)
	go func() {
		conn, err := backend.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		data, err := io.ReadAll(conn)
		if err == nil && !bytes.Equal(data, payload) {
			err = io.ErrUnexpectedEOF
		}
		if err == nil {
			_, err = conn.Write(data)
		}
		serverDone <- err
	}()
	c := DefaultConfig()
	port := testForwardPort(t, "tcp")
	c.Forwards = []PortForward{testForward("tcp", port, backend.Addr().String())}
	p := startTestForwards(t, c)
	client, err := net.DialTCP("tcp", nil, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	client.CloseWrite()
	got, err := io.ReadAll(client)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("half-closed stream did not return intact payload: %v, bytes %d", err, len(got))
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	idle, err := net.DialTimeout("tcp", client.RemoteAddr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	p.Close()
	idle.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := idle.Read(make([]byte, 1)); err == nil {
		t.Fatal("shutdown left a TCP connection open")
	}
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		t.Fatal("shutdown left TCP port bound", err)
	}
	listener.Close()
}

func TestPortForwardUDPClientsAndDatagrams(t *testing.T) {
	backend, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		buffer := make([]byte, 65535)
		for {
			n, remote, err := backend.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			backend.WriteToUDP(buffer[:n], remote)
		}
	}()
	port := testForwardPort(t, "udp")
	c := DefaultConfig()
	c.Forwards = []PortForward{testForward("udp", port, backend.LocalAddr().String()), testForward("tcp", port, "127.0.0.1:9")}
	c.Forwards[0].FirewallID = "udp-private"
	c.Firewalls = []Firewall{{ID: "udp-private", Name: "UDP private", DefaultAction: "deny"}}
	p := startTestForwards(t, c)
	blocked, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	blocked.SetDeadline(time.Now().Add(100 * time.Millisecond))
	blocked.Write([]byte("blocked"))
	if _, err := blocked.Read(make([]byte, 32)); err == nil {
		t.Fatal("UDP datagram bypassed the firewall")
	}
	blocked.Close()
	c.Firewalls[0].DefaultAction = "allow"
	if err := p.proxy.Configure(c); err != nil {
		t.Fatal(err)
	}
	var clients sync.WaitGroup
	for i := range 4 {
		clients.Add(1)
		go func() {
			defer clients.Done()
			client, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
			if err != nil {
				t.Error(err)
				return
			}
			defer client.Close()
			buffer := make([]byte, 65535)
			sizes := []int{0, 1, 4096, 60000}
			if runtime.GOOS == "darwin" {
				sizes[len(sizes)-1] = 8192 // macOS limits UDP datagrams to 9216 bytes by default.
			}
			for _, size := range sizes {
				payload := bytes.Repeat([]byte{byte(i + 1)}, size)
				client.SetDeadline(time.Now().Add(3 * time.Second))
				if _, err := client.Write(payload); err != nil {
					t.Error(err)
					return
				}
				n, err := client.Read(buffer)
				if err != nil || !bytes.Equal(buffer[:n], payload) {
					t.Errorf("client %d datagram %d corrupted or crossed sessions: %v", i, size, err)
					return
				}
			}
			for sequence := range 12 {
				client.Write([]byte{byte(i), byte(sequence), 0, 255})
			}
			received := map[byte]bool{}
			for range 12 {
				n, err := client.Read(buffer)
				if err != nil || n != 4 || buffer[0] != byte(i) || buffer[1] >= 12 || received[buffer[1]] {
					t.Errorf("client %d UDP burst lost, duplicated or crossed a session: %v", i, err)
					return
				}
				received[buffer[1]] = true
			}
		}()
	}
	clients.Wait()
	p.Close()
	if len(p.slots) != 0 {
		t.Fatal("shutdown leaked session slots")
	}
	if !p.pending.TryAcquire(32 << 20) {
		t.Fatal("shutdown leaked queued UDP byte budget")
	}
	p.pending.Release(32 << 20)
	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: port})
	if err != nil {
		t.Fatal("shutdown left UDP port bound", err)
	}
	conn.Close()
}

func TestPortForwardRestartSignature(t *testing.T) {
	base := DefaultConfig()
	base.Forwards = []PortForward{testForward("tcp", 20022, "127.0.0.1:22")}
	for name, change := range map[string]func(*PortForward){
		"target":   func(f *PortForward) { f.Upstream = "127.0.0.1:23" },
		"port":     func(f *PortForward) { f.ListenPort++ },
		"protocol": func(f *PortForward) { f.Protocol = "udp" },
		"firewall": func(f *PortForward) { f.FirewallID = "private" },
		"disabled": func(f *PortForward) { f.Enabled = false },
	} {
		t.Run(name, func(t *testing.T) {
			c := base
			c.Forwards = append([]PortForward{}, base.Forwards...)
			change(&c.Forwards[0])
			if listenerSignature(c) == listenerSignature(base) {
				t.Fatal("forwarding change did not require restart")
			}
		})
	}
	renamed := base
	renamed.Forwards = append([]PortForward{}, base.Forwards...)
	renamed.Forwards[0].Name = "new label"
	if listenerSignature(renamed) != listenerSignature(base) {
		t.Fatal("renaming a rule required a restart")
	}
	base.Forwards[0].Protocol = "udp"
	renamed.Forwards[0].Protocol = "udp"
	renamed.Forwards[0].UDPIdleSeconds = 300
	if listenerSignature(renamed) != listenerSignature(base) {
		t.Fatal("explicit default UDP timeout changed runtime signature")
	}
	renamed.Forwards[0].UDPIdleSeconds = 600
	if listenerSignature(renamed) == listenerSignature(base) {
		t.Fatal("UDP timeout change did not require restart")
	}
}

func TestPortForwardResolvedLoopAndContextCancellation(t *testing.T) {
	port := testForwardPort(t, "tcp")
	c := DefaultConfig()
	c.Forwards = []PortForward{testForward("tcp", port, "127.0.0.1:9"), testForward("udp", port, "127.0.0.1:9")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, err := StartPortForwards(ctx, c, NewProxy(c, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, rule := range c.Forwards {
		rule.Upstream = "localhost:" + strconv.Itoa(port)
		conn, err := p.dial(rule)
		if conn != nil {
			conn.Close()
		}
		if !errors.Is(err, errForwardLoop) {
			t.Fatal("resolved local target bypassed loop detection", err)
		}
	}
	cancel()
	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("parent cancellation left listeners running")
	}
	if len(p.slots) != 0 {
		t.Fatal("cancellation leaked session slots")
	}
	for _, protocol := range []string{"tcp", "udp"} {
		if err := checkRestartPorts(Config{}, Config{Forwards: []PortForward{testForward(protocol, port, "127.0.0.1:9")}}); err != nil {
			t.Fatal("cancelled listener retained its port", err)
		}
	}
	if _, err := StartPortForwards(ctx, c, p.proxy); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled context opened new listeners", err)
	}
}

func TestPortForwardTLSPassthrough(t *testing.T) {
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("nas-tls-passthrough"))
	}))
	defer backend.Close()
	port := testForwardPort(t, "tcp")
	c := DefaultConfig()
	c.Forwards = []PortForward{testForward("tcp", port, backend.Listener.Addr().String())}
	startTestForwards(t, c)
	client := backend.Client()
	client.Timeout = 3 * time.Second
	response, err := client.Get("https://127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		t.Fatal("TLS passthrough failed", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "nas-tls-passthrough" {
		t.Fatal("TLS passthrough changed the payload", err)
	}
}

func TestPortForwardFirewallReloadAndFailClosed(t *testing.T) {
	c := DefaultConfig()
	c.Firewalls = []Firewall{{ID: "private", Name: "private", DefaultAction: "deny", Groups: []FirewallGroup{{Name: "loopback", Match: "include", Action: "allow", CIDRs: []string{"127.0.0.0/8", "::1/128"}}}}}
	rule := testForward("tcp", 20022, "127.0.0.1:22")
	rule.FirewallID = "private"
	proxy := NewProxy(c, nil)
	p := &PortForwarder{proxy: proxy}
	for remote, expected := range map[string]bool{"127.0.0.1:9999": true, "[::1]:9999": true, "192.0.2.1:9999": false, "invalid": false} {
		if p.allowed(rule, remote) != expected {
			t.Fatal("unexpected firewall decision", remote)
		}
	}
	c.Firewalls[0].Groups = nil
	if err := proxy.Configure(c); err != nil {
		t.Fatal(err)
	}
	if p.allowed(rule, "127.0.0.1:9999") {
		t.Fatal("IP rule update did not apply to new packets and connections")
	}
	c.Firewalls = nil
	proxy.Configure(c)
	if p.allowed(rule, "127.0.0.1:9999") {
		t.Fatal("removed runtime firewall failed open")
	}
}

func TestPortForwardBindRollbackAndRestartPreflight(t *testing.T) {
	occupied, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.LocalAddr().(*net.UDPAddr).Port
	tcpPort := testForwardPort(t, "tcp")
	c := DefaultConfig()
	c.Forwards = []PortForward{testForward("tcp", tcpPort, "127.0.0.1:22"), testForward("udp", port, "127.0.0.1:53")}
	if _, err := StartPortForwards(context.Background(), c, NewProxy(c, nil)); err == nil {
		t.Fatal("occupied UDP port accepted")
	}
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(tcpPort))
	if err != nil {
		t.Fatal("failed startup leaked TCP port", err)
	}
	listener.Close()
	if err := checkRestartPorts(DefaultConfig(), c); err == nil || !strings.Contains(err.Error(), "UDP") {
		t.Fatal("occupied new UDP port did not block restart", err)
	}
	if err := checkRestartPorts(c, c); err != nil {
		t.Fatal("owned UDP port blocked restart", err)
	}
	occupied.Close()
	if err := checkRestartPorts(DefaultConfig(), c); err != nil {
		t.Fatal(err)
	}
}

func TestPortForwardConfigAPIPersistence(t *testing.T) {
	a, handler := testAdmin(t)
	cookie := loginForTest(t, handler)
	state := a.store.Snapshot()
	c := state.Config
	c.Forwards = []PortForward{testForward("tcp", 22022, "127.0.0.1:22")}
	request := func(config Config, revision int) int {
		return adminRequest(handler, "PUT", "/api/config", map[string]any{"config": config, "revision": revision}, cookie, "").Code
	}
	if code := request(c, state.Revision); code != 200 {
		t.Fatalf("save returned %d", code)
	}
	reopened, err := OpenStore(a.store.paths.Data)
	if err != nil || len(reopened.Snapshot().Config.Forwards) != 1 {
		t.Fatal("forward not persisted", err)
	}
	payload, err := snapshotBackup(a.store.paths.Data, a.store.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	archive, err := encodeBackup(payload, "")
	if err != nil {
		t.Fatal(err)
	}
	restored, _, err := decodeBackup(archive, "")
	if err != nil || len(restored.State.Config.Forwards) != 1 || restored.State.Config.Forwards[0] != c.Forwards[0] {
		t.Fatal("forward did not survive backup round trip", err)
	}
	if code := request(state.Config, state.Revision); code != 400 {
		t.Fatal("stale revision overwrote a forward", code)
	}
	response := adminRequest(handler, "GET", "/api/status", nil, cookie, "")
	var status struct {
		Restart bool `json:"restart_required"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil || !status.Restart {
		t.Fatal("new listener did not request restart", err)
	}
	next := a.store.Snapshot()
	next.Config.Forwards = append([]PortForward{}, next.Config.Forwards...)
	next.Config.Forwards[0].ListenPort = 16666
	if code := request(next.Config, next.Revision); code != 400 {
		t.Fatal("admin port conflict saved", code)
	}
}

func TestPortForwardIPv6Target(t *testing.T) {
	backend, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.ParseIP("::1")})
	if err != nil {
		t.Skip("IPv6 loopback unavailable")
	}
	defer backend.Close()
	c := DefaultConfig()
	port := testForwardPort(t, "udp")
	c.Forwards = []PortForward{testForward("udp", port, backend.LocalAddr().String())}
	startTestForwards(t, c)
	go func() {
		buffer := make([]byte, 64)
		n, remote, err := backend.ReadFromUDP(buffer)
		if err == nil {
			backend.WriteToUDP(buffer[:n], remote)
		}
	}()
	client, err := net.DialUDP("udp6", nil, net.UDPAddrFromAddrPort(netip.AddrPortFrom(netip.IPv6Loopback(), uint16(port))))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(2 * time.Second))
	client.Write([]byte("ipv6"))
	buffer := make([]byte, 64)
	n, err := client.Read(buffer)
	if err != nil || string(buffer[:n]) != "ipv6" {
		t.Fatal("IPv6 forwarding failed", err)
	}
}

func TestPortForwardMCPSection(t *testing.T) {
	a, handler := testAdmin(t)
	token, err := a.store.saveMCP(MCPConfig{Enabled: true, Read: true, Write: true}, false, a.store.Snapshot().Revision)
	if err != nil {
		t.Fatal(err)
	}
	ctx, client := mcpTestClient(t, handler, token)
	state := a.store.Snapshot()
	forwards := []PortForward{testForward("udp", 10053, "127.0.0.1:53")}
	mcpTestCall(t, ctx, client, "gatehome_update_config", map[string]any{"revision": state.Revision, "section": "forwards", "value": forwards}, false)
	expected := state.Config
	expected.Forwards = forwards
	if !reflect.DeepEqual(a.store.Snapshot().Config, expected) {
		t.Fatal("MCP forwarding write changed another configuration section")
	}
	mcpTestCall(t, ctx, client, "gatehome_update_config", map[string]any{"revision": state.Revision, "section": "forwards", "value": []PortForward{}}, true)
}

type forwardTemporaryListener struct {
	net.Listener
	calls int
}

func (l *forwardTemporaryListener) Accept() (net.Conn, error) {
	l.calls++
	if l.calls == 1 {
		return nil, &net.OpError{Op: "accept", Net: "tcp", Err: syscall.EMFILE}
	}
	return nil, net.ErrClosed
}

func TestPortForwardAcceptRetriesResourcePressure(t *testing.T) {
	listener := &forwardTemporaryListener{}
	p := &PortForwarder{ctx: context.Background(), failures: make(chan error, 1)}
	p.serveTCP(&forwardBinding{rule: testForward("tcp", 20022, "127.0.0.1:22"), tcp: listener})
	if listener.calls != 2 {
		t.Fatal("temporary accept error stopped the forwarding listener")
	}
}

func TestPortForwardUDPIdleExpiryAndReconnect(t *testing.T) {
	backend, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		buffer := make([]byte, 64)
		for {
			n, remote, err := backend.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			backend.WriteToUDP(buffer[:n], remote)
		}
	}()
	port := testForwardPort(t, "udp")
	c := DefaultConfig()
	c.Forwards = []PortForward{testForward("udp", port, backend.LocalAddr().String())}
	c.Forwards[0].UDPIdleSeconds = 30
	p := startTestForwards(t, c)
	client, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	exchange := func() {
		t.Helper()
		client.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := client.Write([]byte("idle")); err != nil {
			t.Fatal(err)
		}
		buffer := make([]byte, 64)
		n, err := client.Read(buffer)
		if err != nil || string(buffer[:n]) != "idle" {
			t.Fatal("UDP idle exchange failed", err)
		}
	}
	exchange()
	if len(p.slots) != 1 {
		t.Fatal("missing initial UDP session")
	}
	deadline := time.Now().Add(33 * time.Second)
	for len(p.slots) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("idle UDP session was not reclaimed")
		}
		time.Sleep(100 * time.Millisecond)
	}
	exchange()
	if len(p.slots) != 1 {
		t.Fatal("expired UDP session was not recreated")
	}
	if forwardUDPTimeout(PortForward{}) != 300*time.Second {
		t.Fatal("legacy UDP configuration did not use the default timeout")
	}
}
