package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
)

func defaultIPEndpoints(kind string) []string {
	if kind == "AAAA" {
		return []string{"http://v6.66666.host:66/ip", "http://myip6.ipip.net", "https://6.ipw.cn", "http://v6.666666.host:66/ip"}
	}
	return []string{"https://ddns.oray.com/checkip", "http://v4.66666.host:66/ip", "https://myip.ipip.net", "http://v4.666666.host:66/ip", "https://4.ipw.cn", "https://ip.3322.net"}
}

func ipEndpoints(g DDNSGroup, kind string) []string {
	endpoints, legacy := g.IPv4URLs, g.IPv4URL
	if kind == "AAAA" {
		endpoints, legacy = g.IPv6URLs, g.IPv6URL
	}
	if endpoints != nil {
		return endpoints
	}
	if legacy != "" {
		return []string{legacy}
	}
	return defaultIPEndpoints(kind)
}

func validateIPEndpoints(g DDNSGroup) error {
	for _, kind := range []string{"A", "AAAA"} {
		endpoints := ipEndpoints(g, kind)
		if len(endpoints) < 1 || len(endpoints) > 20 {
			return errors.New("IPv4 / IPv6 接口列表各须包含 1–20 个地址")
		}
		seen := map[string]bool{}
		for _, endpoint := range endpoints {
			u, err := url.Parse(endpoint)
			if len(endpoint) > 2048 || strings.TrimSpace(endpoint) != endpoint || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || seen[endpoint] {
				return errors.New("IP 接口须为不含账号和片段的 HTTP / HTTPS 地址，且不能重复")
			}
			seen[endpoint] = true
		}
	}
	return nil
}

func queryPublicIPs(ctx context.Context, endpoints []string, kind string, iface *net.Interface) (DetectedIP, error) {
	var sample DetectedIP
	var lastErr error
	for i, endpoint := range endpoints {
		if ctx.Err() != nil {
			return sample, errors.New("IP 接口检测已取消")
		}
		sample, lastErr = queryPublicIP(ctx, endpoint, kind, iface)
		sample.Attempts = i + 1
		if lastErr == nil {
			return sample, nil
		}
	}
	if lastErr == nil {
		return sample, errors.New("未配置 IP 获取接口")
	}
	return sample, fmt.Errorf("%d 个接口均未获取有效公网 IP：%w", len(endpoints), lastErr)
}

var ipInResponse = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b|[0-9a-fA-F]*:[0-9a-fA-F:.]+`)

func parsePublicIPResponse(data []byte, kind string) (netip.Addr, error) {
	text := strings.TrimSpace(string(data))
	if ip, err := netip.ParseAddr(text); err == nil && validPublicIP(ip, kind) {
		return ip, nil
	}
	var found netip.Addr
	for _, candidate := range ipInResponse.FindAllString(text, -1) {
		// A label such as "IP:2606:..." may contribute one leading colon.
		if strings.HasPrefix(candidate, ":") && !strings.HasPrefix(candidate, "::") {
			candidate = strings.TrimPrefix(candidate, ":")
		}
		ip, err := netip.ParseAddr(candidate)
		if err != nil || !validPublicIP(ip, kind) {
			continue
		}
		if found.IsValid() && found != ip {
			return netip.Addr{}, errors.New("接口返回多个不同的公网 IP，无法确定同步地址")
		}
		found = ip
	}
	if !found.IsValid() {
		return netip.Addr{}, errors.New("接口未返回有效的对应版本公网 IP")
	}
	return found, nil
}
