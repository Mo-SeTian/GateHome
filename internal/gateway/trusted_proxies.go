package gateway

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func parseTrustedProxies(values []string) ([]netip.Prefix, error) {
	if len(values) > 32 {
		return nil, errors.New("可信前置代理最多 32 个 IP 或 CIDR")
	}
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			addr, parseErr := netip.ParseAddr(value)
			if parseErr != nil || addr.Zone() != "" {
				return nil, errors.New("可信前置代理须为 IP 或 CIDR，不包含协议、端口或网卡区域")
			}
			addr = addr.Unmap()
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		if prefix.Addr().Is4In6() {
			if prefix.Bits() < 96 {
				return nil, errors.New("IPv4 映射代理网段范围无效")
			}
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		if prefix.Bits() == 0 || prefix.Addr().IsMulticast() || prefix.Addr().IsUnspecified() {
			return nil, errors.New("请填写明确的可信代理地址，不能信任全部来源、未指定地址或组播地址")
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func proxySourceRequest(r *http.Request, trusted []netip.Prefix) *http.Request {
	isTrusted := func(ip netip.Addr) bool {
		for _, prefix := range trusted {
			if prefix.Contains(ip) {
				return true
			}
		}
		return false
	}
	peer, port, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || len(trusted) == 0 {
		return r
	}
	current, err := netip.ParseAddr(peer)
	if err != nil || !isTrusted(current.Unmap()) {
		return r
	}
	current = current.Unmap()
	remaining := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	// Only a trusted hop can identify the previous hop. Ignore client-supplied
	// entries to the left of the first untrusted address, including invalid ones.
	for hops := 0; isTrusted(current); hops++ {
		if remaining == "" || hops == 32 {
			return r
		}
		index := strings.LastIndexByte(remaining, ',')
		value := remaining[index+1:]
		next, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil || next.Zone() != "" || next.IsUnspecified() || next.IsMulticast() {
			return r
		}
		current = next.Unmap()
		if index < 0 {
			break
		}
		remaining = remaining[:index]
	}
	request := r.Clone(r.Context())
	request.RemoteAddr = net.JoinHostPort(current.String(), port)
	return request
}
