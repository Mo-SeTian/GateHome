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

func urlEffectivePort(u *url.URL) int {
	if u == nil {
		return 0
	}
	if portText := u.Port(); portText != "" {
		port, err := strconv.Atoi(portText)
		if err != nil {
			return 0
		}
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return 80
	case "https":
		return 443
	default:
		return 0
	}
}

func isAdminUpstream(u *url.URL, adminPort int, addresses map[netip.Addr]bool) bool {
	if u == nil || adminPort < 1 || urlEffectivePort(u) != adminPort {
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
	return ip.IsLoopback() || addresses[ip]
}

// isAdminAccessBlockedTarget includes unspecified addresses (0.0.0.0 / ::) that
// are untrusted for session cookies but still connect locally on Linux/BSD hosts.
func isAdminAccessBlockedTarget(u *url.URL, adminPort int, addresses map[netip.Addr]bool) bool {
	if isAdminUpstream(u, adminPort, addresses) {
		return true
	}
	if u == nil || adminPort < 1 || urlEffectivePort(u) != adminPort {
		return false
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil || ip.Zone() != "" {
		return false
	}
	return ip.IsUnspecified()
}

// Business upstreams must not receive or overwrite the management session.
func stripAdminCookies(r *http.Request) {
	stripSessionCookie(r, "gatehouse_session")
	stripSessionCookie(r, passkeyCookie)
}

func stripSessionCookie(r *http.Request, protected string) {
	values := []string{}
	for _, header := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			name, _, _ := strings.Cut(strings.TrimSpace(part), "=")
			if strings.TrimSpace(name) != protected {
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
	stripSessionResponseCookie(response, "gatehouse_session")
	stripSessionResponseCookie(response, passkeyCookie)
}

func stripSessionResponseCookie(response *http.Response, protected string) {
	values := response.Header.Values("Set-Cookie")
	response.Header.Del("Set-Cookie")
	for _, value := range values {
		name, _, _ := strings.Cut(strings.TrimSpace(value), "=")
		if strings.TrimSpace(name) != protected {
			response.Header.Add("Set-Cookie", value)
		}
	}
}
