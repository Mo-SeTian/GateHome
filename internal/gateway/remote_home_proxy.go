package gateway

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

type homeRouteContextKey struct{}

// A tunnel-only stream enters the existing proxy, carrying its verified route
// identity in the request context. It never listens on an additional host port.
func (a *Admin) serveHomeProxy(ctx context.Context, peer homePeer, client net.Conn) {
	cfg := a.store.Snapshot().Config
	for _, route := range cfg.Routes {
		if route.ID != peer.Route || route.Host != peer.Host || !cfg.RouteActive(route) {
			continue
		}
		group, ok := cfg.Group(route.GroupID)
		if !ok {
			return
		}
		https := route.TLS || group.HTTPPort == 0
		port := group.HTTPPort
		if https {
			port = group.HTTPSPort
		}
		if port != peer.Port {
			return
		}
		l := &homeStream{client: client, done: make(chan struct{})}
		server := &http.Server{
			Handler:           a.proxy.Handler(route.GroupID),
			ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
			BaseContext: func(net.Listener) context.Context {
				return context.WithValue(ctx, homeRouteContextKey{}, peer.Route)
			},
		}
		stop := context.AfterFunc(ctx, func() { l.Close(); client.Close() })
		defer stop()
		if https {
			if a.certs == nil {
				return
			}
			_ = server.Serve(tls.NewListener(l, &tls.Config{
				MinVersion: tls.VersionTLS12, GetCertificate: a.certs.ForGroup(route.GroupID),
				NextProtos: []string{"http/1.1"},
			}))
		} else {
			_ = server.Serve(l)
		}
		return
	}
}

type homeStream struct {
	client   net.Conn
	done     chan struct{}
	once     sync.Once
	accepted bool
}

func (l *homeStream) Addr() net.Addr { return l.client.LocalAddr() }
func (l *homeStream) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *homeStream) Accept() (net.Conn, error) {
	if !l.accepted {
		l.accepted = true
		return &homeStreamConn{Conn: l.client, listener: l}, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

type homeStreamConn struct {
	net.Conn
	listener *homeStream
}

func (c *homeStreamConn) Close() error {
	c.listener.Close()
	return c.Conn.Close()
}

func (a *Admin) homeDNS(ctx context.Context, peer homePeer, client net.Conn, stream bool) {
	defer client.Close()
	for {
		client.SetDeadline(time.Now().Add(10 * time.Second))
		var data []byte
		if stream {
			var length uint16
			if binary.Read(client, binary.BigEndian, &length) != nil || length == 0 {
				return
			}
			data = make([]byte, int(length))
			if _, err := io.ReadFull(client, data); err != nil {
				return
			}
		} else {
			data = make([]byte, 4096)
			n, err := client.Read(data)
			if err != nil {
				return
			}
			data = data[:n]
		}
		var query dns.Msg
		if query.Unpack(data) != nil || query.Response || query.Opcode != dns.OpcodeQuery || len(query.Question) != 1 {
			return
		}
		reply := a.homeDNSReply(ctx, peer, &query)
		if !stream {
			size := uint16(512)
			if opt := query.IsEdns0(); opt != nil {
				size = min(max(512, opt.UDPSize()), 1232)
			}
			reply.Truncate(int(size))
		}
		packet, err := reply.Pack()
		if err != nil {
			return
		}
		if stream {
			packet = append(binary.BigEndian.AppendUint16(nil, uint16(len(packet))), packet...)
		}
		if _, err := client.Write(packet); err != nil {
			return
		}
	}
}

func (a *Admin) homeDNSReply(ctx context.Context, peer homePeer, query *dns.Msg) *dns.Msg {
	reply := new(dns.Msg).SetReply(query)
	q := query.Question[0]
	if strings.EqualFold(q.Name, dns.Fqdn(peer.Host)) {
		if q.Qclass == dns.ClassINET && (q.Qtype == dns.TypeA || q.Qtype == dns.TypeANY) {
			reply.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 30}, A: net.IP(homeServiceIP.AsSlice())}}
		}
		return reply
	}
	// Resolve ordinary addresses for clients that use the VPN DNS globally.
	if q.Qclass != dns.ClassINET || (q.Qtype != dns.TypeA && q.Qtype != dns.TypeAAAA) {
		return reply
	}
	network := "ip4"
	if q.Qtype == dns.TypeAAAA {
		network = "ip6"
	}
	resolve, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupNetIP(resolve, network, q.Name)
	if err != nil {
		reply.Rcode = dns.RcodeServerFailure
		if dnsErr, ok := err.(*net.DNSError); ok && dnsErr.IsNotFound {
			reply.Rcode = dns.RcodeNameError
		}
		return reply
	}
	for _, address := range addresses {
		h := dns.RR_Header{Name: q.Name, Rrtype: q.Qtype, Class: dns.ClassINET, Ttl: 30}
		if address.Is4() {
			reply.Answer = append(reply.Answer, &dns.A{Hdr: h, A: net.IP(address.AsSlice())})
		} else {
			reply.Answer = append(reply.Answer, &dns.AAAA{Hdr: h, AAAA: net.IP(address.AsSlice())})
		}
	}
	return reply
}
