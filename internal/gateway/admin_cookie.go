package gateway

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

func localAdminAddresses() map[netip.Addr]bool {
	result := map[netip.Addr]bool{}
	addresses, _ := net.InterfaceAddrs()
	for _, address := range addresses {
		if prefix, err := netip.ParsePrefix(address.String()); err == nil {
			result[prefix.Addr().Unmap()] = true
		}
	}
	return result
}

func isAdminUpstream(u *url.URL, adminPort int, addresses map[netip.Addr]bool) bool {
	if adminPort < 1 || u.Port() != strconv.Itoa(adminPort) {
		return false
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil || ip.Zone() != "" || ip.IsUnspecified() {
		return false
	}
	ip = ip.Unmap()
	return ip == netip.MustParseAddr("127.0.0.1") || ip == netip.MustParseAddr("::1") || addresses[ip]
}

// Business upstreams must not receive or overwrite the management session.
func stripAdminCookies(r *http.Request) {
	values := []string{}
	for _, header := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			name, _, _ := strings.Cut(strings.TrimSpace(part), "=")
			if strings.TrimSpace(name) != "gatehouse_session" {
				values = append(values, strings.TrimSpace(part))
			}
		}
	}
	r.Header.Del("Cookie")
	if len(values) > 0 {
		r.Header.Set("Cookie", strings.Join(values, "; "))
	}
}

func stripAdminResponseCookies(response *http.Response) {
	values := response.Header.Values("Set-Cookie")
	response.Header.Del("Set-Cookie")
	for _, value := range values {
		name, _, _ := strings.Cut(strings.TrimSpace(value), "=")
		if strings.TrimSpace(name) != "gatehouse_session" {
			response.Header.Add("Set-Cookie", value)
		}
	}
}
