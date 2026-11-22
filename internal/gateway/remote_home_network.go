package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const homeMTU = 1420

// The packet channel lives wholly in this process. WireGuard reads encrypted
// traffic from its UDP socket and exchanges IPv4 packets with this stack.
type homeTunnel struct {
	link   *channel.Endpoint
	stack  *stack.Stack
	events chan tun.Event
}

func (t *homeTunnel) File() *os.File           { return nil }
func (t *homeTunnel) Events() <-chan tun.Event { return t.events }
func (t *homeTunnel) Name() (string, error)    { return "gatehome", nil }
func (t *homeTunnel) MTU() (int, error)        { return homeMTU, nil }
func (t *homeTunnel) BatchSize() int           { return 1 }

func (t *homeTunnel) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	pkt := t.link.ReadContext(context.Background())
	if pkt == nil {
		return 0, os.ErrClosed
	}
	defer pkt.DecRef()
	view := pkt.ToView()
	defer view.Release()
	if view.Size() > len(bufs[0])-offset {
		return 0, io.ErrShortBuffer
	}
	sizes[0] = copy(bufs[0][offset:], view.AsSlice())
	return 1, nil
}

func (t *homeTunnel) Write(bufs [][]byte, offset int) (int, error) {
	for _, buf := range bufs {
		packet := buf[offset:]
		if len(packet) == 0 || packet[0]>>4 != 4 {
			continue
		}
		pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(packet)})
		t.link.InjectInbound(ipv4.ProtocolNumber, pkt)
		pkt.DecRef()
	}
	return len(bufs), nil
}

func (t *homeTunnel) Close() error {
	t.link.Close()
	close(t.events)
	return nil
}

type homeRuntime struct {
	device *device.Device
	tunnel *homeTunnel
	mu     sync.Mutex
	policy homePolicy
	active map[*homeFlow]struct{}
	closed bool
	admin  *Admin
}

type homePolicy struct {
	lan   netip.Prefix
	peers map[netip.Addr]homePeer
}

type homeRelaySocket struct {
	source      string
	destination string
}

type homeFlow struct {
	peer   homePeer
	cancel context.CancelFunc
}

func newHomeRuntime(admin *Admin, config homeState) (*homeRuntime, error) {
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	t := &homeTunnel{link: channel.New(256, homeMTU, ""), stack: s, events: make(chan tun.Event)}
	r := &homeRuntime{admin: admin, tunnel: t, policy: config.policy(), active: make(map[*homeFlow]struct{})}
	if err := s.CreateNIC(1, t.link); err != nil {
		s.Destroy()
		return nil, errors.New("无法初始化内置 WireGuard 网络")
	}
	s.SetPromiscuousMode(1, true)
	s.SetSpoofing(1, true)
	s.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: 1}})
	tcpForwarder := tcp.NewForwarder(s, 0, 64, r.forwardTCP)
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpForwarder.HandlePacket)
	udpForwarder := udp.NewForwarder(s, r.forwardUDP)
	s.SetTransportProtocolHandler(udp.ProtocolNumber, func(id stack.TransportEndpointID, pkt *stack.PacketBuffer) bool {
		r.mu.Lock()
		_, allowed := r.allowed(id, false)
		r.mu.Unlock()
		if !allowed {
			return true
		}
		return udpForwarder.HandlePacket(id, pkt)
	})

	r.device = device.NewDevice(t, conn.NewStdNetBind(), device.NewLogger(device.LogLevelSilent, ""))
	if err := r.device.IpcSet(config.deviceConfig()); err != nil {
		r.close()
		return nil, errors.New("无法加载 WireGuard 设备配置")
	}
	if err := r.device.Up(); err != nil {
		r.close()
		return nil, errors.New("无法监听 WireGuard UDP 端口，请检查端口占用和部署配置")
	}
	return r, nil
}

func (r *homeRuntime) close() {
	r.mu.Lock()
	r.closed = true
	for flow := range r.active {
		flow.cancel()
	}
	r.mu.Unlock()
	r.device.Close()
	r.tunnel.stack.Destroy()
}

func (r *homeRuntime) setPolicy(policy homePolicy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policy = policy
	for flow := range r.active {
		next, exists := policy.peers[flow.peer.Address]
		if !exists || next != flow.peer {
			flow.cancel()
		}
	}
}

func (r *homeRuntime) allowed(id stack.TransportEndpointID, tcpFlow bool) (homePeer, bool) {
	source, ok := netip.AddrFromSlice(id.RemoteAddress.AsSlice())
	peer, found := r.policy.peers[source]
	dest, valid := netip.AddrFromSlice(id.LocalAddress.AsSlice())
	if r.closed || !ok || !found || !valid || id.LocalPort == 0 {
		return peer, false
	}
	if peer.Mode == "route" {
		return peer, dest == homeServiceIP && (id.LocalPort == 53 || tcpFlow && int(id.LocalPort) == peer.Port)
	}
	if !r.policy.lan.Contains(dest) || !homeUsableHost(r.policy.lan, dest) ||
		tcpFlow && int(id.LocalPort) == r.admin.adminPort && !peer.Admin {
		return peer, false
	}
	return peer, peer.Mode == "lan" || peer.Mode == "host" &&
		dest == peer.Target && (peer.Port == 0 || peer.Port == int(id.LocalPort))
}

func (r *homeRuntime) begin(id stack.TransportEndpointID, tcpFlow bool) (context.Context, *homeFlow) {
	r.mu.Lock()
	defer r.mu.Unlock()
	peer, allowed := r.allowed(id, tcpFlow)
	if !allowed || len(r.active) >= 256 {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &homeFlow{peer: peer, cancel: cancel}
	r.active[f] = struct{}{}
	return ctx, f
}

func (r *homeRuntime) end(f *homeFlow) {
	f.cancel()
	r.mu.Lock()
	delete(r.active, f)
	r.mu.Unlock()
}

func (r *homeRuntime) forwardTCP(req *tcp.ForwarderRequest) {
	id := req.ID()
	ctx, flow := r.begin(id, true)
	if flow == nil {
		req.Complete(true)
		return
	}
	defer r.end(flow)
	var queue waiter.Queue
	ep, err := req.CreateEndpoint(&queue)
	req.Complete(err != nil)
	if err != nil {
		return
	}
	client := gonet.NewTCPConn(&queue, ep)
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { client.Close() })
	defer stop()
	if flow.peer.Mode == "route" {
		if id.LocalPort == 53 {
			r.admin.homeDNS(ctx, flow.peer, client, true)
		} else {
			r.admin.serveHomeProxy(ctx, flow.peer, client)
		}
		return
	}
	r.relay(ctx, id, "tcp4", client, flow.peer)
}

func (r *homeRuntime) forwardUDP(req *udp.ForwarderRequest) {
	id := req.ID()
	ctx, flow := r.begin(id, false)
	if flow == nil {
		return
	}
	var queue waiter.Queue
	ep, err := req.CreateEndpoint(&queue)
	if err != nil {
		r.end(flow)
		return
	}
	client := gonet.NewUDPConn(&queue, ep)
	go func() {
		defer r.end(flow)
		defer client.Close()
		stop := context.AfterFunc(ctx, func() { client.Close() })
		defer stop()
		if flow.peer.Mode == "route" {
			r.admin.homeDNS(ctx, flow.peer, client, false)
			return
		}
		r.relay(ctx, id, "udp4", client, flow.peer)
	}()
}

func (r *homeRuntime) relay(ctx context.Context, id stack.TransportEndpointID, network string, client net.Conn, peer homePeer) {
	dest, _ := netip.AddrFromSlice(id.LocalAddress.AsSlice())
	wan, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, netip.AddrPortFrom(dest, id.LocalPort).String())
	if err != nil {
		return
	}
	defer wan.Close()
	stop := context.AfterFunc(ctx, func() { wan.Close() })
	defer stop()
	if network == "tcp4" {
		socket := homeRelaySocket{wan.LocalAddr().String(), wan.RemoteAddr().String()}
		r.admin.proxy.homeRelaySources.Store(socket, peer)
		defer r.admin.proxy.homeRelaySources.Delete(socket)
		done := make(chan struct{})
		go func() {
			defer close(done)
			io.Copy(wan, client)
			wan.(*net.TCPConn).CloseWrite()
		}()
		io.Copy(client, wan)
		client.(*gonet.TCPConn).CloseWrite()
		<-done
		return
	}
	// A UDP flow expires after inactivity in either direction.
	extend := func() {
		deadline := time.Now().Add(45 * time.Second)
		client.SetReadDeadline(deadline)
		wan.SetReadDeadline(deadline)
	}
	copyPackets := func(to, from net.Conn) {
		buf := make([]byte, 65535)
		for {
			n, err := from.Read(buf)
			if err != nil {
				return
			}
			extend()
			if _, err := to.Write(buf[:n]); err != nil {
				return
			}
		}
	}
	extend()
	done := make(chan struct{})
	go func() {
		defer close(done)
		copyPackets(wan, client)
		wan.Close()
	}()
	copyPackets(client, wan)
	client.Close()
	<-done
}
