package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type NetworkInterface struct {
	Name      string       `json:"name"`
	Up        bool         `json:"up"`
	Loopback  bool         `json:"loopback"`
	Addresses []netip.Addr `json:"addresses"`
}

type DetectedIP struct {
	Address      netip.Addr `json:"address"`
	Interface    string     `json:"interface"`
	LocalAddress string     `json:"local_address"`
	Endpoint     string     `json:"endpoint"`
	Attempts     int        `json:"attempts"`
	CheckedAt    time.Time  `json:"checked_at"`
	LastSuccess  time.Time  `json:"last_success"`
	Error        string     `json:"error"`
}

func networkInterfaces() ([]NetworkInterface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, errors.New("网卡列表读取失败")
	}
	result := []NetworkInterface{}
	for _, iface := range interfaces {
		item := NetworkInterface{Name: iface.Name, Up: iface.Flags&net.FlagUp != 0, Loopback: iface.Flags&net.FlagLoopback != 0, Addresses: []netip.Addr{}}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err == nil {
				item.Addresses = append(item.Addresses, prefix.Addr().Unmap())
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func ipSource(g DDNSGroup, kind string) string {
	source := g.IPv4Source
	if kind == "AAAA" {
		source = g.IPv6Source
	}
	if source == "" {
		return "url"
	}
	return source
}

func validateIPSources(g DDNSGroup) error {
	if len(g.Interface) > 64 || strings.ContainsAny(g.Interface, "\x00\r\n/") {
		return errors.New("网卡名称无效")
	}
	for _, kind := range []string{"A", "AAAA"} {
		source := ipSource(g, kind)
		if source != "url" && source != "interface" {
			return errors.New("IP 获取方式须为公网查询或读取网卡")
		}
		if source == "interface" && g.Interface == "" && (g.Mode == "dual" || (g.Mode == "ipv4" && kind == "A") || (g.Mode == "ipv6" && kind == "AAAA")) {
			return errors.New("直接读取 IP 时必须选择网卡")
		}
	}
	return nil
}

func selectInterfaceIP(addresses []netip.Addr, kind string, publicOnly bool) (netip.Addr, error) {
	for _, ip := range addresses {
		family := (kind == "A" && ip.Is4()) || (kind == "AAAA" && ip.Is6() && !ip.Is4In6())
		if family && ip.IsGlobalUnicast() && !ip.IsLoopback() && (!publicOnly || validPublicIP(ip, kind)) {
			return ip, nil
		}
	}
	return netip.Addr{}, errors.New("所选网卡没有可用的对应版本地址")
}

func detectIP(ctx context.Context, g DDNSGroup, kind string) (DetectedIP, error) {
	var iface *net.Interface
	if g.Interface != "" {
		selected, err := net.InterfaceByName(g.Interface)
		if err != nil || selected.Flags&net.FlagUp == 0 || selected.Flags&net.FlagLoopback != 0 {
			return DetectedIP{Interface: g.Interface}, errors.New("所选网卡不存在、未连接或为环回网卡")
		}
		iface = selected
		addresses, err := iface.Addrs()
		if err != nil {
			return DetectedIP{Interface: iface.Name}, errors.New("所选网卡地址读取失败")
		}
		ips := []netip.Addr{}
		for _, address := range addresses {
			if prefix, err := netip.ParsePrefix(address.String()); err == nil {
				ips = append(ips, prefix.Addr().Unmap())
			}
		}
		ip, err := selectInterfaceIP(ips, kind, ipSource(g, kind) == "interface")
		if err != nil {
			metadata := DetectedIP{Interface: iface.Name}
			if local, localErr := selectInterfaceIP(ips, kind, false); localErr == nil {
				metadata.LocalAddress = local.String()
			}
			if ipSource(g, kind) == "interface" {
				return metadata, errors.New("所选网卡没有对应版本的公网 IP，内网地址不会用于 DDNS")
			}
			return metadata, err
		}
		if ipSource(g, kind) == "interface" {
			return DetectedIP{Address: ip, Interface: iface.Name, LocalAddress: ip.String()}, nil
		}
	} else if ipSource(g, kind) == "interface" {
		return DetectedIP{}, errors.New("直接读取 IP 时必须选择网卡")
	}
	return queryPublicIPs(ctx, ipEndpoints(g, kind), kind, iface)
}

func queryPublicIP(ctx context.Context, endpoint, kind string, iface *net.Interface) (result DetectedIP, queryErr error) {
	result.Endpoint = safeLogURL(endpoint)
	var metadata DetectedIP
	var metadataMu sync.Mutex
	defer func() {
		metadataMu.Lock()
		result.Interface, result.LocalAddress = metadata.Interface, metadata.LocalAddress
		metadataMu.Unlock()
	}()
	network := "tcp4"
	if kind == "AAAA" {
		network = "tcp6"
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if iface != nil {
		bindIPInterface(dialer, *iface)
		metadata.Interface = iface.Name
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, addr)
		if err == nil {
			observed := DetectedIP{LocalAddress: conn.LocalAddr().(*net.TCPAddr).IP.String()}
			if iface != nil {
				observed.Interface = iface.Name
			}
			if iface == nil {
				local, _ := netip.ParseAddr(observed.LocalAddress)
				interfaces, _ := networkInterfaces()
				for _, item := range interfaces {
					for _, address := range item.Addresses {
						if address == local.Unmap() {
							observed.Interface = item.Name
						}
					}
				}
			}
			metadataMu.Lock()
			metadata = observed
			metadataMu.Unlock()
		}
		return conn, err
	}, TLSHandshakeTimeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 5 * time.Second, Transport: transport, CheckRedirect: func(r *http.Request, via []*http.Request) error { return errors.New("IP 查询不允许重定向") }}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return result, errors.New("公网 IP 查询地址无效")
	}
	resp, err := client.Do(req)
	if err != nil {
		return result, errors.New("公网 IP 查询失败，请检查对应版本网络、网卡路由及绑定权限")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result, errors.New("公网 IP 查询服务暂不可用")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil {
		return result, errors.New("公网 IP 响应读取失败")
	}
	if len(b) > 64<<10 {
		return result, errors.New("IP 接口响应超过 64 KiB")
	}
	ip, err := parsePublicIPResponse(b, kind)
	if err != nil {
		return result, err
	}
	result.Address = ip
	return result, nil
}
