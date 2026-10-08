package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type Cloudflare struct {
	Token   string
	BaseURL string
	Client  *http.Client
	logs    *Logs
}

func NewCloudflare(token string) *Cloudflare {
	return &Cloudflare{Token: token, BaseURL: "https://api.cloudflare.com/client/v4", Client: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Cloudflare) call(ctx context.Context, method, path string, input, output any) (callErr error) {
	started := time.Now()
	status := 0
	defer func() {
		message := "Cloudflare API 调用成功"
		if callErr != nil {
			message = callErr.Error()
		}
		c.logs.Add(LogEntry{Category: "ddns", Action: "Cloudflare API", Target: c.BaseURL, Method: method, Path: path, Status: status, OK: callErr == nil, DurationMS: time.Since(started).Milliseconds(), Message: message})
	}()
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return errors.New("Cloudflare 请求构建失败")
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Client.Do(req)
	if err != nil {
		return errors.New("Cloudflare 连接失败，请检查网络")
	}
	defer resp.Body.Close()
	status = resp.StatusCode
	var envelope struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
		Errors  []struct {
			Code int `json:"code"`
		} `json:"errors"`
		ResultInfo struct {
			TotalCount int `json:"total_count"`
		} `json:"result_info"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope); err != nil {
		return errors.New("Cloudflare 返回了无效响应")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !envelope.Success {
		code := 0
		if len(envelope.Errors) > 0 {
			code = envelope.Errors[0].Code
		}
		// Never surface response bodies: provider messages may contain credentials.
		return fmt.Errorf("Cloudflare 请求失败（HTTP %d，错误码 %d），请检查 Token 权限和域名", resp.StatusCode, code)
	}
	if envelope.ResultInfo.TotalCount > 100 {
		return errors.New("同名 DNS 记录过多，拒绝自动修改")
	}
	if output != nil {
		if err := json.Unmarshal(envelope.Result, output); err != nil {
			return errors.New("Cloudflare 数据格式错误")
		}
	}
	return nil
}

func (c *Cloudflare) ZoneID(ctx context.Context, zone string) (string, error) {
	var zones []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := c.call(ctx, "GET", "/zones?name="+url.QueryEscape(zone)+"&per_page=100", nil, &zones); err != nil {
		return "", err
	}
	if len(zones) != 1 || zones[0].ID == "" || zones[0].Name != zone {
		return "", errors.New("未找到唯一匹配的 Cloudflare 域名，请检查 Zone:Read 权限")
	}
	return zones[0].ID, nil
}

type cfRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl,omitempty"`
	Proxied bool   `json:"proxied"`
}

// SyncRecord touches only the explicitly configured address record.
func (c *Cloudflare) SyncRecord(ctx context.Context, zoneID string, record DNSRecord, ip netip.Addr) (bool, error) {
	if !validPublicIP(ip, record.Type) {
		return false, errors.New("公网 IP 类型不匹配或不是公网地址")
	}
	path := "/zones/" + url.PathEscape(zoneID) + "/dns_records"
	var records []cfRecord
	if err := c.call(ctx, "GET", path+"?name="+url.QueryEscape(record.Host)+"&per_page=100", nil, &records); err != nil {
		return false, err
	}
	var matches []cfRecord
	for _, r := range records {
		if strings.TrimSuffix(strings.ToLower(r.Name), ".") != record.Host {
			continue
		}
		if r.Type == "CNAME" {
			return false, errors.New("存在同名 CNAME，请先在 Cloudflare 处理冲突")
		}
		if r.Type == record.Type {
			matches = append(matches, r)
		}
	}
	if len(matches) > 1 {
		return false, errors.New("存在多条同名同类型记录，拒绝覆盖，请先在 Cloudflare 合并")
	}
	if len(matches) == 0 {
		err := c.call(ctx, "POST", path, cfRecord{Type: record.Type, Name: record.Host, Content: ip.String(), TTL: 120, Proxied: false}, nil)
		return err == nil, err
	}
	r := matches[0]
	if r.Proxied {
		return false, errors.New("此记录已开启橙云代理，请先切换为仅 DNS（灰云）")
	}
	if old, err := netip.ParseAddr(r.Content); err == nil && old == ip {
		return false, nil
	}
	if r.ID == "" {
		return false, errors.New("Cloudflare 记录缺少 ID")
	}
	err := c.call(ctx, "PATCH", path+"/"+url.PathEscape(r.ID), map[string]string{"content": ip.String()}, nil)
	return err == nil, err
}

func validPublicIP(ip netip.Addr, kind string) bool {
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, network := range nonPublicNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return (kind == "A" && ip.Is4()) || (kind == "AAAA" && ip.Is6() && !ip.Is4In6())
}

var nonPublicNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("100::/64"),
}
