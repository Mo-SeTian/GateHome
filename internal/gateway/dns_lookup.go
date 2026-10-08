package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
)

func queryPublicDNS(ctx context.Context, client *http.Client, endpoint, family, host string) ([]netip.Addr, error) {
	kind, code := "A", 1
	if family == "ip6" {
		kind, code = "AAAA", 28
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"?"+url.Values{"name": {host}, "type": {kind}}.Encode(), nil)
	if err != nil {
		return nil, errors.New("DNS 查询地址无效")
	}
	req.Header.Set("Accept", "application/dns-json")
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("DNS 查询连接失败")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("DNS 查询服务不可用")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return nil, errors.New("DNS 查询响应无效")
	}
	var answer struct {
		Status *int
		TC     bool
		Answer []struct {
			Type int
			Data string
		}
	}
	if json.Unmarshal(data, &answer) != nil || answer.Status == nil || answer.TC {
		return nil, errors.New("DNS 查询响应无效")
	}
	if *answer.Status == 3 {
		return nil, &net.DNSError{IsNotFound: true}
	}
	if *answer.Status != 0 {
		return nil, errors.New("DNS 查询未成功")
	}
	ips := []netip.Addr{}
	for _, record := range answer.Answer {
		if record.Type != code {
			continue
		}
		ip, err := netip.ParseAddr(record.Data)
		if err != nil || (code == 1 && !ip.Is4()) || (code == 28 && (!ip.Is6() || ip.Is4In6())) {
			return nil, errors.New("DNS 地址记录无效")
		}
		ips = append(ips, ip)
	}
	return ips, nil
}
