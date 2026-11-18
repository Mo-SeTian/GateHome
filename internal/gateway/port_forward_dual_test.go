package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestDualProtocolForwardTransfersTCPAndUDP(t *testing.T) {
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp", tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	go func() {
		conn, err := tcp.Accept()
		if err == nil {
			defer conn.Close()
			io.Copy(conn, conn)
		}
	}()
	go func() {
		buffer := make([]byte, 2048)
		for {
			n, peer, err := udp.ReadFrom(buffer)
			if err != nil {
				return
			}
			udp.WriteTo(buffer[:n], peer)
		}
	}()
	c := DefaultConfig()
	rule := testForward("tcp+udp", testForwardPort(t, "tcp"), tcp.Addr().String())
	rule.ID = "combined-forward"
	rule.UDPIdleSeconds = 60
	c.Forwards = []PortForward{rule}
	dir := t.TempDir()
	history, err := OpenTraffic(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, handler := testAdmin(t)
	p, err := StartPortForwards(context.Background(), c, a.proxy, history)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	a.SetPortForwarder(p)
	cookie := loginForTest(t, handler)
	from := time.Now().UTC().Truncate(time.Hour)
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(rule.ListenPort))
	want := map[string]uint64{}
	for _, protocol := range []string{"tcp", "udp"} {
		conn, err := net.DialTimeout(protocol, address, 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		defer conn.Close()
		payload := []byte("TEST_ONLY_DUAL_FORWARD_" + protocol)
		if protocol == "udp" {
			payload = append(payload, '!')
		}
		want[protocol] = uint64(len(payload))
		if _, err := conn.Write(payload); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		response := make([]byte, len(payload))
		_, err = io.ReadFull(conn, response)
		if err != nil || !bytes.Equal(payload, response) {
			t.Fatalf("%s transfer failed: %v", protocol, err)
		}
	}
	// The counters must report traffic before a long-lived connection closes.
	deadline := time.Now().Add(2 * forwardTrafficInterval)
	for {
		live := p.trafficStatus()
		if len(live) == 2 && live[0].UploadBPS > 0 && live[0].DownloadBPS > 0 &&
			live[1].UploadBPS > 0 && live[1].DownloadBPS > 0 {
			if live[0].Active != 1 || live[1].Active != 1 {
				t.Fatalf("incorrect active counts: %+v", live)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("missing live traffic while connections are open: %+v", live)
		}
		time.Sleep(20 * time.Millisecond)
	}
	const reportPath = "/api/statistics/traffic"
	httpTraffic, err := OpenTraffic(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	httpTraffic.add(&Route{ID: rule.ID, Host: "same-id.example.test"}, 123, 456, time.Now())
	a.proxy.SetTraffic(httpTraffic)
	for protocol, expected := range map[string]uint64{"": want["tcp"] + want["udp"], "tcp": want["tcp"], "udp": want["udp"]} {
		path := reportPath + "?source=forward&rule=" + rule.ID + "&protocol=" + protocol
		w := adminRequest(handler, "GET", path, nil, cookie, "")
		var report trafficReport
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &report) != nil ||
			report.Upload != expected || report.Download != expected || report.Rule != rule.ID {
			t.Fatalf("incorrect %q filtered history: HTTP %d, %+v", protocol, w.Code, report)
		}
		for _, row := range report.Rules {
			if row.ID != rule.ID || row.Host != rule.Name || row.Upload != want[row.Protocol] ||
				(protocol != "" && row.Protocol != protocol) {
				t.Fatalf("incorrect protocol row: %+v", row)
			}
		}
	}
	w := adminRequest(handler, "GET", reportPath+"?rule="+rule.ID, nil, cookie, "")
	var report trafficReport
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &report) != nil || report.Upload != 123 || report.Download != 456 {
		t.Fatalf("forward traffic affected HTTP totals: HTTP %d, %+v", w.Code, report)
	}
	// Close must record bytes transferred after the most recent periodic sample.
	conn, err := net.DialTimeout("udp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := conn.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(conn, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	p.Close()
	if err := history.Flush(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenTraffic(dir)
	if err != nil {
		t.Fatal(err)
	}
	report = reopened.forwardReport(from, time.Now(), rule.ID, "udp")
	if report.Upload != want["udp"]+1 || report.Download != want["udp"]+1 || len(report.Rules) != 1 {
		t.Fatalf("shutdown sample was not persisted: %+v", report)
	}
	for _, protocol := range []string{"tcp", "udp"} {
		if protocol == "tcp" {
			listener, err := net.Listen(protocol, address)
			if err != nil {
				t.Fatal("TCP port was not released")
			}
			listener.Close()
		} else {
			listener, err := net.ListenPacket(protocol, address)
			if err != nil {
				t.Fatal("UDP port was not released")
			}
			listener.Close()
		}
	}
}

func TestDualForwardBindFailureReleasesTCP(t *testing.T) {
	occupied, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.LocalAddr().(*net.UDPAddr).Port
	c := DefaultConfig()
	rule := testForward("tcp+udp", port, "192.0.2.10:9999")
	rule.ID = "combined-forward"
	c.Forwards = []PortForward{rule}
	p, err := StartPortForwards(context.Background(), c, NewProxy(c, nil), nil)
	if err == nil {
		p.Close()
		t.Fatal("partial dual listener was accepted")
	}
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		t.Fatal("failed UDP bind leaked its TCP listener")
	}
	listener.Close()
	if checkRestartPorts(Config{}, c) == nil {
		t.Fatal("UDP port conflict did not prevent a dual-rule restart")
	}
}

func TestDualForwardValidationAndRestartSignature(t *testing.T) {
	c := DefaultConfig()
	dual := PortForward{ID: "dual", Name: "Dual", Protocol: "tcp+udp", ListenPort: 22000, Upstream: "192.0.2.10:9999", Enabled: true, UDPIdleSeconds: 60}
	c.Forwards = []PortForward{dual}
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	initial := listenerSignature(c)
	c.Forwards[0].UDPIdleSeconds = 120
	if initial == listenerSignature(c) {
		t.Fatal("dual UDP timeout change did not require restart")
	}
	if validateAdminPort(c, dual.ListenPort) == nil {
		t.Fatal("dual listener conflicts with management TCP")
	}
	for _, protocol := range []string{"tcp", "udp", "tcp+udp"} {
		other := PortForward{ID: "other", Name: "Other", Protocol: protocol, ListenPort: dual.ListenPort, Upstream: "192.0.2.10:9998"}
		c.Forwards = []PortForward{dual, other}
		if Validate(c) == nil {
			t.Fatal("overlapping protocol was accepted")
		}
		other.ListenPort = 22001
		other.Enabled = true
		dual.Upstream = "localhost:22001"
		c.Forwards = []PortForward{dual, other}
		if Validate(c) == nil {
			t.Fatal("dual forwarding loop was accepted")
		}
		dual.Upstream = "192.0.2.10:9999"
	}
	c.Forwards = []PortForward{dual}
	c.Forwards[0].ListenPort = c.Groups[0].HTTPPort
	if Validate(c) == nil {
		t.Fatal("dual listener conflicts with HTTP")
	}
}
