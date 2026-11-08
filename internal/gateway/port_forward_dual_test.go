package gateway

import (
	"bytes"
	"context"
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
		n, peer, err := udp.ReadFrom(buffer)
		if err == nil {
			udp.WriteTo(buffer[:n], peer)
		}
	}()
	c := DefaultConfig()
	rule := testForward("tcp+udp", testForwardPort(t, "tcp"), tcp.Addr().String())
	rule.ID = "combined-forward"
	rule.UDPIdleSeconds = 60
	c.Forwards = []PortForward{rule}
	p := startTestForwards(t, c)
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(rule.ListenPort))
	for _, protocol := range []string{"tcp", "udp"} {
		conn, err := net.DialTimeout(protocol, address, 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		payload := []byte("TEST_ONLY_DUAL_FORWARD")
		if _, err := conn.Write(payload); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		response := make([]byte, len(payload))
		_, err = io.ReadFull(conn, response)
		conn.Close()
		if err != nil || !bytes.Equal(payload, response) {
			t.Fatalf("%s transfer failed: %v", protocol, err)
		}
	}
	p.Close()
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
	p, err := StartPortForwards(context.Background(), c, NewProxy(c, nil))
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
