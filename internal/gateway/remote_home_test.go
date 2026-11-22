package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

func TestRemoteHomeRequiresAdminAndOrigin(t *testing.T) {
	_, h := testAdmin(t)
	for _, op := range []string{"settings", "add", "edit", "remove"} {
		if w := adminRequest(h, "POST", "/api/remote-home/"+op, map[string]any{}, nil, ""); w.Code != 401 {
			t.Fatalf("%s without session: HTTP %d", op, w.Code)
		}
	}
	cookie := loginForTest(t, h)
	if w := adminRequest(h, "POST", "/api/remote-home/settings", map[string]any{}, cookie, "https://unrelated.example"); w.Code != 403 {
		t.Fatalf("unexpected origin: HTTP %d", w.Code)
	}
}

func homeClient(t *testing.T, profile string) *netstack.Net {
	t.Helper()
	lines := map[string]string{}
	for line := range strings.SplitSeq(profile, "\n") {
		if k, v, ok := strings.Cut(line, " = "); ok {
			lines[k] = v
		}
	}
	address, err := netip.ParsePrefix(lines["Address"])
	if err != nil {
		t.Fatal("invalid client address")
	}
	tun, stack, err := netstack.CreateNetTUN([]netip.Addr{address.Addr()}, nil, 1420)
	if err != nil {
		t.Fatal(err)
	}
	client := device.NewDevice(tun, conn.NewStdNetBind(), device.NewLogger(device.LogLevelSilent, ""))
	t.Cleanup(client.Close)
	priv, _ := base64.StdEncoding.DecodeString(lines["PrivateKey"])
	pub, _ := base64.StdEncoding.DecodeString(lines["PublicKey"])
	if err := client.IpcSet(fmt.Sprintf("private_key=%x\npublic_key=%x\nendpoint=%s\nallowed_ip=%s\n", priv, pub, lines["Endpoint"], lines["AllowedIPs"])); err != nil {
		t.Fatal("cannot initialize client")
	}
	if err := client.Up(); err != nil {
		t.Fatal("cannot start client")
	}
	return stack
}

func unusedHomePort(t *testing.T) int {
	t.Helper()
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	return udp.LocalAddr().(*net.UDPAddr).Port
}

func TestBuiltInWireGuardProxyAndRevocation(t *testing.T) {
	a, _ := testAdmin(t)
	defer a.CloseRemoteHome()
	streamClosed := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			close(streamClosed)
			return
		}
		w.Write([]byte("photo"))
	}))
	defer upstream.Close()
	config := a.store.Snapshot()
	config.Config.Routes = []Route{
		{ID: "photos", Name: "Photos", Host: "photos.example.test", GroupID: "default", Upstream: upstream.URL, Enabled: true},
		{ID: "other", Name: "Other", Host: "other.example.test", GroupID: "default", Upstream: upstream.URL, Enabled: true},
	}
	if err := a.store.Update(config.Config, nil, config.Revision); err != nil {
		t.Fatal(err)
	}
	if err := a.proxy.Configure(config.Config); err != nil {
		t.Fatal(err)
	}

	settings := homeInput{Endpoint: "127.0.0.1", Port: unusedHomePort(t), LAN: "192.168.2.0/24", Tunnel: "10.77.0.0/24"}
	if _, err := a.changeRemoteHome("settings", settings); err != nil {
		t.Fatal(err)
	}
	peer, err := a.changeRemoteHome("add", homeInput{Name: "phone", Mode: "route", Route: "photos"})
	if err != nil {
		t.Fatal(err)
	}
	stack := homeClient(t, peer["profile"])
	port := config.Config.Groups[0].HTTPPort
	request := func(host string) int {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		c, err := stack.DialContextTCPAddrPort(ctx, netip.AddrPortFrom(homeServiceIP, uint16(port)))
		if err != nil {
			return 0
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(4 * time.Second))
		req, _ := http.NewRequest("GET", "http://"+host, nil)
		req.Close = true
		if err := req.Write(c); err != nil {
			return 0
		}
		response, err := http.ReadResponse(bufio.NewReader(c), req)
		if err != nil {
			return 0
		}
		defer response.Body.Close()
		if response.StatusCode == 200 {
			body, _ := io.ReadAll(response.Body)
			if string(body) != "photo" {
				t.Fatalf("unexpected proxy response: %q", body)
			}
		}
		return response.StatusCode
	}
	if code := request("photos.example.test"); code != 200 {
		t.Fatalf("authorized reverse proxy returned %d", code)
	}
	if code := request("other.example.test"); code != 403 {
		t.Fatalf("unapproved reverse proxy returned %d", code)
	}

	dnsConn, err := stack.DialUDPAddrPort(netip.AddrPort{}, netip.AddrPortFrom(homeServiceIP, 53))
	if err != nil {
		t.Fatal(err)
	}
	defer dnsConn.Close()
	query := new(dns.Msg).SetQuestion("photos.example.test.", dns.TypeA)
	packet, _ := query.Pack()
	dnsConn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := dnsConn.Write(packet); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 512)
	n, err := dnsConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	var response dns.Msg
	if response.Unpack(buf[:n]) != nil || len(response.Answer) != 1 || response.Answer[0].(*dns.A).A.String() != homeServiceIP.String() {
		t.Fatal("unexpected tunnel DNS answer")
	}

	status, _ := a.remoteHomeStatus()
	if !status.Running || len(status.Devices) != 1 || status.Devices[0].Handshake == 0 || status.Devices[0].Rx == 0 || status.Devices[0].Tx == 0 {
		t.Fatal("missing live WireGuard counters")
	}
	snapshot, _ := json.Marshal(status)
	for _, field := range []string{`"private"`, `"profile"`, `"key"`} {
		if strings.Contains(string(snapshot), field) {
			t.Fatal("private configuration in status")
		}
	}
	info, err := os.Stat(filepath.Join(a.store.paths.Data, homeStateFile))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("incorrect WireGuard state permissions")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	live, err := stack.DialContextTCPAddrPort(ctx, netip.AddrPortFrom(homeServiceIP, uint16(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	live.SetDeadline(time.Now().Add(4 * time.Second))
	req, _ := http.NewRequest("GET", "http://photos.example.test/stream", nil)
	if err := req.Write(live); err != nil {
		t.Fatal(err)
	}
	stream, err := http.ReadResponse(bufio.NewReader(live), req)
	if err != nil || stream.StatusCode != 200 {
		t.Fatal("could not establish a live request through WireGuard")
	}
	defer stream.Body.Close()
	if _, err := a.changeRemoteHome("remove", homeInput{ID: peer["id"]}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-streamClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("revocation did not close the live upstream connection")
	}
	if code := request("photos.example.test"); code != 0 {
		t.Fatalf("revoked peer still reached proxy: %d", code)
	}
	a.CloseRemoteHome()
	a.StartRemoteHome()
	status, _ = a.remoteHomeStatus()
	if !status.Running || len(status.Devices) != 0 {
		t.Fatal("saved configuration did not survive restart")
	}
}

func TestBuiltInWireGuardImportAndPortConflict(t *testing.T) {
	a, _ := testAdmin(t)
	t.Cleanup(a.CloseRemoteHome)
	occupied, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	input := homeInput{Endpoint: "vpn.example.test", Port: unusedHomePort(t), LAN: "192.168.2.0/24", Tunnel: "10.77.0.0/24"}
	if _, err := a.changeRemoteHome("settings", input); err != nil {
		t.Fatal(err)
	}
	peer, err := a.changeRemoteHome("add", homeInput{Name: "Phone", Mode: "lan"})
	if err != nil {
		t.Fatal(err)
	}
	status, public := a.remoteHomeStatus()
	originalPort := input.Port
	input.Port = occupied.LocalAddr().(*net.UDPAddr).Port
	if _, err := a.changeRemoteHome("settings", input); err == nil {
		t.Fatal("saved conflicting listen port")
	}
	current, _ := a.remoteHomeStatus()
	if !current.Running || current.Port != originalPort {
		t.Fatal("rejected port change stopped the running tunnel")
	}
	data, err := os.ReadFile(filepath.Join(a.store.paths.Data, homeStateFile))
	if err != nil {
		t.Fatal("could not read generated state")
	}
	a.CloseRemoteHome()

	migrated, _ := testAdmin(t)
	t.Cleanup(migrated.CloseRemoteHome)
	if err := ImportRemoteHome(migrated.store.paths.Data, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if err := ImportRemoteHome(migrated.store.paths.Data, bytes.NewReader(data)); err == nil {
		t.Fatal("import overwrote an existing configuration")
	}
	migrated.StartRemoteHome()
	result, key := migrated.remoteHomeStatus()
	if !result.Running || result.Port != originalPort || key != public || len(result.Devices) != 1 ||
		result.Devices[0].ID != peer["id"] || result.Devices[0].Address != status.Devices[0].Address {
		t.Fatal("migration changed the running server identity or client authorization")
	}
}

func TestBuiltInWireGuardPreservesCorruptStateAndPortConflicts(t *testing.T) {
	a, h := testAdmin(t)
	t.Cleanup(a.CloseRemoteHome)
	cookie := loginForTest(t, h)
	statePath := filepath.Join(a.store.paths.Data, homeStateFile)
	if err := os.WriteFile(statePath, []byte(`{"settings":`), 0600); err != nil {
		t.Fatal(err)
	}
	a.StartRemoteHome()
	st, _ := a.remoteHomeStatus()
	if st.Fault == "" || !a.remoteHome.loadBlocked {
		t.Fatalf("expected fault and loadBlocked on corrupt state, got %+v blocked=%v", st, a.remoteHome.loadBlocked)
	}
	if w := adminRequest(h, "POST", "/api/remote-home/settings", homeInput{
		Endpoint: "vpn.example.test", Port: unusedHomePort(t), LAN: "192.168.2.0/24", Tunnel: "10.77.0.0/24",
	}, cookie, ""); w.Code != 400 {
		t.Fatalf("saving over corrupt wireguard.json must be blocked, got HTTP %d", w.Code)
	}
	raw, _ := os.ReadFile(statePath)
	if string(raw) != `{"settings":` {
		t.Fatal("corrupt wireguard.json was overwritten")
	}

	_ = os.Remove(statePath)
	a.StartRemoteHome()
	wgPort := unusedHomePort(t)
	if w := adminRequest(h, "POST", "/api/remote-home/settings", homeInput{
		Endpoint: "vpn.example.test", Port: wgPort, LAN: "192.168.2.0/24", Tunnel: "10.77.0.0/24",
	}, cookie, ""); w.Code != 200 {
		t.Fatalf("initial WireGuard save failed: %d %s", w.Code, w.Body.String())
	}
	snap := a.store.Snapshot()
	cfg := snap.Config
	cfg.Forwards = []PortForward{{ID: "dns-udp", Name: "UDP Conflict", Protocol: "udp", ListenPort: wgPort, Upstream: "192.168.2.5:5353", Enabled: true}}
	if w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": cfg, "revision": snap.Revision}, cookie, ""); w.Code != 400 {
		t.Fatalf("conflicting UDP port forward in PUT /api/config expected 400, got %d", w.Code)
	}

	otherPort := unusedHomePort(t)
	cfg.Forwards[0].ListenPort = otherPort
	if w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": cfg, "revision": snap.Revision}, cookie, ""); w.Code != 200 {
		t.Fatalf("valid UDP port forward failed: %d %s", w.Code, w.Body.String())
	}
	if w := adminRequest(h, "POST", "/api/remote-home/settings", homeInput{
		Endpoint: "vpn.example.test", Port: otherPort, LAN: "192.168.2.0/24", Tunnel: "10.77.0.0/24",
	}, cookie, ""); w.Code != 400 {
		t.Fatalf("WireGuard port conflicting with UDP forward expected 400, got %d", w.Code)
	}

	a.maintenance = &Maintenance{busy: true}
	if w := adminRequest(h, "POST", "/api/remote-home/add", homeInput{Name: "blocked", Mode: "lan"}, cookie, ""); w.Code != 409 {
		t.Fatalf("expected HTTP 409 while maintenance busy, got %d", w.Code)
	}
}

func TestBuiltInWireGuardDNSBurstsAndIsolation(t *testing.T) {
	a, _ := testAdmin(t)
	defer a.CloseRemoteHome()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	config := a.store.Snapshot()
	config.Config.Routes = []Route{
		{ID: "svc", Name: "Svc", Host: "svc.example.test", GroupID: "default", Upstream: upstream.URL, Enabled: true},
	}
	if err := a.store.Update(config.Config, nil, config.Revision); err != nil {
		t.Fatal(err)
	}
	if err := a.proxy.Configure(config.Config); err != nil {
		t.Fatal(err)
	}
	if _, err := a.changeRemoteHome("settings", homeInput{
		Endpoint: "127.0.0.1", Port: unusedHomePort(t), LAN: "192.168.2.0/24", Tunnel: "10.77.0.0/24",
	}); err != nil {
		t.Fatal(err)
	}
	peer, err := a.changeRemoteHome("add", homeInput{Name: "phone", Mode: "route", Route: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	stack := homeClient(t, peer["profile"])

	// Burst >256 UDP DNS queries on distinct source sockets to verify flows do not linger and exhaust capacity.
	for i := range 270 {
		dnsConn, err := stack.DialUDPAddrPort(netip.AddrPort{}, netip.AddrPortFrom(homeServiceIP, 53))
		if err != nil {
			t.Fatalf("DNS socket %d failed: %v", i, err)
		}
		target := "svc.example.test."
		if i == 0 {
			target = "router.local."
		} else if i == 1 {
			target = "internal."
		}
		packet, _ := new(dns.Msg).SetQuestion(target, dns.TypeA).Pack()
		dnsConn.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := dnsConn.Write(packet); err != nil {
			dnsConn.Close()
			t.Fatalf("DNS write %d failed: %v", i, err)
		}
		buf := make([]byte, 512)
		n, err := dnsConn.Read(buf)
		dnsConn.Close()
		if err != nil {
			t.Fatalf("DNS read %d failed: %v", i, err)
		}
		var reply dns.Msg
		if err := reply.Unpack(buf[:n]); err != nil {
			t.Fatal(err)
		}
		if i < 2 && reply.Rcode != dns.RcodeNameError {
			t.Fatalf("internal DNS %q should return NXDOMAIN, got rcode=%d answers=%v", target, reply.Rcode, reply.Answer)
		}
	}

	// TCP data flow must still succeed immediately after >256 DNS queries.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	c, err := stack.DialContextTCPAddrPort(ctx, netip.AddrPortFrom(homeServiceIP, uint16(config.Config.Groups[0].HTTPPort)))
	if err != nil {
		t.Fatalf("TCP flow exhausted after DNS burst: %v", err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(4 * time.Second))
	req, _ := http.NewRequest("GET", "http://svc.example.test/", nil)
	req.Close = true
	if err := req.Write(c); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(c), req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("proxy request after DNS burst failed: resp=%v err=%v", resp, err)
	}
	resp.Body.Close()

	// Verify unspecified and alternate loopback admin target checks.
	addrs := map[netip.Addr]bool{netip.MustParseAddr("192.168.2.25"): true}
	for _, target := range []string{"http://0.0.0.0:16666/", "http://[::]:16666/", "http://127.0.0.2:16666/"} {
		u, _ := url.Parse(target)
		if !isAdminAccessBlockedTarget(u, 16666, addrs) {
			t.Fatalf("expected %s to be blocked for tunnel/assistant access", target)
		}
	}
	u0, _ := url.Parse("http://0.0.0.0:16666/")
	if isAdminUpstream(u0, 16666, addrs) {
		t.Fatal("isAdminUpstream must keep unspecified address untrusted for cookies")
	}
	u80, _ := url.Parse("http://localhost/")
	if !isAdminUpstream(u80, 80, addrs) {
		t.Fatal("default port 80 admin URL should match adminPort 80")
	}
}
