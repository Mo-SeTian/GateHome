package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/semaphore"
)

type PortForward struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Protocol       string `json:"protocol"`
	ListenPort     int    `json:"listen_port"`
	Upstream       string `json:"upstream"`
	FirewallID     string `json:"firewall_id"`
	Enabled        bool   `json:"enabled"`
	UDPIdleSeconds int    `json:"udp_idle_seconds,omitempty"`
}

var errForwardLoop = errors.New("转发目标指向本机已启用的转发端口，请填写实际服务地址")

func localForwardTarget(address string, local map[netip.Addr]bool) (int, bool) {
	host, port, err := net.SplitHostPort(address)
	n, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil {
		return 0, false
	}
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return n, true
	}
	ip, err := netip.ParseAddr(host)
	return n, err == nil && (ip.Unmap().IsLoopback() || local[ip.Unmap()])
}

func forwardUDPTimeout(rule PortForward) time.Duration {
	if rule.UDPIdleSeconds == 0 {
		return 300 * time.Second
	}
	return time.Duration(rule.UDPIdleSeconds) * time.Second
}

func validatePortForwards(c Config) error {
	if len(c.Forwards) > 100 {
		return errors.New("最多支持 100 条端口转发规则")
	}
	ids, ports := map[string]bool{}, map[string]bool{}
	for _, f := range c.Forwards {
		if !idPattern.MatchString(f.ID) || ids[f.ID] || strings.TrimSpace(f.Name) == "" || len(f.Name) > 100 {
			return errors.New("端口转发名称或 ID 无效，ID 不能重复")
		}
		ids[f.ID] = true
		if f.Protocol != "tcp" && f.Protocol != "udp" {
			return errors.New("端口转发协议须为 TCP 或 UDP")
		}
		if f.UDPIdleSeconds != 0 && (f.Protocol != "udp" || f.UDPIdleSeconds < 30 || f.UDPIdleSeconds > 86400) {
			return errors.New("UDP 空闲超时须为 30–86400 秒，0 使用默认 300 秒；TCP 规则请留空")
		}
		key := net.JoinHostPort(f.Protocol, strconv.Itoa(f.ListenPort))
		if f.ListenPort < 1024 || f.ListenPort > 65535 || ports[key] {
			return errors.New("转发监听端口须为 1024–65535，同一协议的端口不能重复")
		}
		ports[key] = true
		host, port, err := net.SplitHostPort(f.Upstream)
		n, portErr := strconv.Atoi(port)
		targetError := errors.New("转发目标应为 主机:端口 或 [IPv6]:端口，端口范围 1–65535")
		if err != nil || portErr != nil || n < 1 || n > 65535 || host == "" || len(host) > 253 {
			return targetError
		}
		if ip, err := netip.ParseAddr(host); err == nil {
			ip = ip.Unmap()
			if ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
				return targetError
			}
		} else {
			for _, label := range strings.Split(strings.TrimSuffix(strings.ToLower(host), "."), ".") {
				if !labelPattern.MatchString(label) {
					return targetError
				}
			}
		}
		if f.FirewallID != "" {
			if _, ok := c.Firewall(f.FirewallID); !ok {
				return errors.New("端口转发引用了不存在的防火墙；删除前请移除引用")
			}
		}
		if f.Protocol == "tcp" {
			if c.SunPanel.Enabled && f.ListenPort == c.SunPanel.Port {
				return errors.New("TCP 转发端口不能与 Sun-Panel 端口相同")
			}
			for _, g := range c.Groups {
				if f.ListenPort == g.HTTPPort || f.ListenPort == g.HTTPSPort {
					return errors.New("TCP 转发端口不能与 HTTP / HTTPS 业务端口相同")
				}
			}
		}
	}
	if len(c.Forwards) > 0 {
		local := localAdminAddresses()
		for _, f := range c.Forwards {
			port, isLocal := localForwardTarget(f.Upstream, local)
			if !f.Enabled || !isLocal {
				continue
			}
			for _, target := range c.Forwards {
				if target.Enabled && target.Protocol == f.Protocol && target.ListenPort == port {
					return errForwardLoop
				}
			}
		}
	}
	return nil
}

const maxForwardSessions = 1024

type PortForwarder struct {
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	bindings  []*forwardBinding
	proxy     *Proxy
	failures  chan error
	slots     chan struct{}
	pending   *semaphore.Weighted
	closeOnce sync.Once
}

type forwardBinding struct {
	rule     PortForward
	tcp      net.Listener
	udp      *net.UDPConn
	mu       sync.Mutex
	sessions map[netip.AddrPort]*forwardUDPSession
}

type forwardUDPSession struct {
	queue chan []byte
}

// Bind all ports before serving. A failed bind releases every port opened here.
func StartPortForwards(ctx context.Context, c Config, proxy *Proxy) (*PortForwarder, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := Validate(c); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	p := &PortForwarder{ctx: ctx, cancel: cancel, proxy: proxy, failures: make(chan error, 1), slots: make(chan struct{}, maxForwardSessions), pending: semaphore.NewWeighted(32 << 20)}
	for _, rule := range c.Forwards {
		if !rule.Enabled {
			continue
		}
		b := &forwardBinding{rule: rule, sessions: map[netip.AddrPort]*forwardUDPSession{}}
		address := ":" + strconv.Itoa(rule.ListenPort)
		var err error
		if rule.Protocol == "tcp" {
			b.tcp, err = net.Listen("tcp", address)
		} else {
			b.udp, err = net.ListenUDP("udp", &net.UDPAddr{Port: rule.ListenPort})
		}
		if err != nil {
			p.Close()
			return nil, fmt.Errorf("无法监听 %s %d，请检查端口占用或权限", strings.ToUpper(rule.Protocol), rule.ListenPort)
		}
		p.bindings = append(p.bindings, b)
	}
	for _, b := range p.bindings {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			if b.tcp != nil {
				p.serveTCP(b)
			} else {
				p.serveUDP(b)
			}
		}()
	}
	context.AfterFunc(ctx, p.closeListeners)
	return p, nil
}

func (p *PortForwarder) Failures() <-chan error { return p.failures }

func (p *PortForwarder) Close() {
	p.closeListeners()
	p.wg.Wait()
}

func (p *PortForwarder) closeListeners() {
	p.closeOnce.Do(func() {
		p.cancel()
		for _, b := range p.bindings {
			if b.tcp != nil {
				b.tcp.Close()
			} else {
				b.udp.Close()
			}
		}
	})
}

func (p *PortForwarder) dial(rule PortForward) (net.Conn, error) {
	local := localAdminAddresses()
	dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second,
		ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
			port, isLocal := localForwardTarget(address, local)
			if isLocal {
				for _, b := range p.bindings {
					if b.rule.Protocol == rule.Protocol && b.rule.ListenPort == port {
						return errForwardLoop
					}
				}
			}
			return nil
		}}
	return dialer.DialContext(p.ctx, rule.Protocol, rule.Upstream)
}

func (p *PortForwarder) allowed(rule PortForward, remote string) bool {
	if rule.FirewallID == "" {
		return true
	}
	firewalls, _ := p.proxy.firewalls.Load().(map[string]*compiledFirewall)
	route := proxyRoute{route: Route{FirewallID: rule.FirewallID}, firewall: firewalls[rule.FirewallID]}
	allowed, _ := route.firewallDecision(remote)
	return allowed
}

func (p *PortForwarder) listenFailed(b *forwardBinding) {
	if p.ctx.Err() == nil {
		select {
		case p.failures <- fmt.Errorf("%s 转发端口 %d 意外停止", strings.ToUpper(b.rule.Protocol), b.rule.ListenPort):
		default:
		}
	}
}

func (p *PortForwarder) logDialFailure(rule PortForward, remote string, err error) {
	message := "无法连接 " + strings.ToUpper(rule.Protocol) + " 转发目标"
	if errors.Is(err, errForwardLoop) {
		message = errForwardLoop.Error()
	}
	p.proxy.logs.Add(LogEntry{Category: "forward", Action: strings.ToUpper(rule.Protocol) + " 转发", Target: rule.Name, Remote: remoteIP(remote), Rule: "forward/" + rule.ID, OK: false, Message: message})
}

func (p *PortForwarder) serveTCP(b *forwardBinding) {
	delay := time.Duration(0)
	for {
		client, err := b.tcp.Accept()
		if err != nil {
			if temporary, ok := err.(net.Error); ok && temporary.Temporary() {
				if delay == 0 {
					delay = 5 * time.Millisecond
				} else {
					delay = min(2*delay, time.Second)
				}
				select {
				case <-p.ctx.Done():
					return
				case <-time.After(delay):
					continue
				}
			}
			p.listenFailed(b)
			return
		}
		delay = 0
		if !p.allowed(b.rule, client.RemoteAddr().String()) {
			client.Close()
			continue
		}
		select {
		case p.slots <- struct{}{}:
			p.wg.Add(1)
			go func() {
				defer p.wg.Done()
				defer func() { <-p.slots }()
				p.forwardTCP(b.rule, client)
			}()
		default:
			client.Close()
		}
	}
}

func (p *PortForwarder) forwardTCP(rule PortForward, client net.Conn) {
	defer client.Close()
	stopClient := context.AfterFunc(p.ctx, func() { client.Close() })
	defer stopClient()
	upstream, err := p.dial(rule)
	if err != nil {
		if p.ctx.Err() == nil {
			p.logDialFailure(rule, client.RemoteAddr().String(), err)
		}
		return
	}
	defer upstream.Close()
	stopUpstream := context.AfterFunc(p.ctx, func() { upstream.Close() })
	defer stopUpstream()
	copyStream := func(dst, src net.Conn) {
		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			src.Close()
			return
		}
		dst.(*net.TCPConn).CloseWrite()
	}
	done := make(chan struct{})
	go func() { copyStream(upstream, client); close(done) }()
	copyStream(client, upstream)
	<-done
}

func (p *PortForwarder) serveUDP(b *forwardBinding) {
	buffer := make([]byte, 65535)
	for {
		n, remote, err := b.udp.ReadFromUDPAddrPort(buffer)
		if err != nil {
			p.listenFailed(b)
			return
		}
		if !p.allowed(b.rule, remote.String()) {
			continue
		}
		b.mu.Lock()
		session := b.sessions[remote]
		if session == nil {
			select {
			case p.slots <- struct{}{}:
				session = &forwardUDPSession{queue: make(chan []byte, 16)}
				b.sessions[remote] = session
				p.wg.Add(1)
				go func() {
					defer p.wg.Done()
					defer func() { <-p.slots }()
					p.forwardUDP(b, remote, session)
				}()
			default:
			}
		}
		if session != nil && p.pending.TryAcquire(int64(n)) {
			select {
			case session.queue <- append([]byte{}, buffer[:n]...):
			default:
				p.pending.Release(int64(n))
			}
		}
		b.mu.Unlock()
	}
}

func (p *PortForwarder) forwardUDP(b *forwardBinding, remote netip.AddrPort, session *forwardUDPSession) {
	defer func() {
		b.mu.Lock()
		delete(b.sessions, remote)
		for len(session.queue) > 0 {
			p.pending.Release(int64(len(<-session.queue)))
		}
		b.mu.Unlock()
	}()
	upstream, err := p.dial(b.rule)
	if err != nil {
		if p.ctx.Err() == nil {
			p.logDialFailure(b.rule, remote.String(), err)
		}
		return
	}
	defer upstream.Close()
	stop := context.AfterFunc(p.ctx, func() { upstream.Close() })
	defer stop()
	ctx, cancel := context.WithCancel(p.ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		buffer := make([]byte, 65535)
		for {
			upstream.SetReadDeadline(time.Now().Add(forwardUDPTimeout(b.rule)))
			n, err := upstream.Read(buffer)
			if err != nil {
				return
			}
			if p.allowed(b.rule, remote.String()) {
				if _, err := b.udp.WriteToUDPAddrPort(buffer[:n], remote); err != nil {
					return
				}
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			upstream.Close()
			<-done
			return
		case packet := <-session.queue:
			p.pending.Release(int64(len(packet)))
			upstream.SetWriteDeadline(time.Now().Add(5 * time.Second))
			upstream.SetReadDeadline(time.Now().Add(forwardUDPTimeout(b.rule)))
			if _, err := upstream.Write(packet); err != nil {
				upstream.Close()
				<-done
				return
			}
		}
	}
}
