package gateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func relativeDNSName(host, zone string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	zone = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(zone)), ".")
	if host == "" || host == zone {
		return "@"
	}
	suffix := "." + zone
	if strings.HasSuffix(host, suffix) {
		rel := strings.TrimSuffix(host, suffix)
		if rel != "" {
			return rel
		}
	}
	return host
}

func popPercentEncode(s string) string {
	encoded := url.QueryEscape(s)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return encoded
}

func hmacSHA256Bytes(key []byte, msg string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(msg))
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func defaultLogCategory(v string) string {
	if v != "" {
		return v
	}
	return "ddns"
}

// ============================================================================
// 1. 阿里云 DNS (AliDNS)
// ============================================================================

type AliDNS struct {
	AccessKeyID     string
	AccessKeySecret string
	BaseURL         string
	Client          *http.Client
	logs            *Logs
	logCategory     string
	now             func() time.Time
}

func NewAliDNS(accessKeyID, accessKeySecret string) *AliDNS {
	return &AliDNS{
		AccessKeyID:     accessKeyID,
		AccessKeySecret: accessKeySecret,
		BaseURL:         "https://alidns.aliyuncs.com",
		Client:          &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *AliDNS) Close() { closeClient(c.Client) }

func (c *AliDNS) currentTime() time.Time {
	if c.now != nil {
		return c.now().UTC()
	}
	return time.Now().UTC()
}

func (c *AliDNS) call(ctx context.Context, action string, params map[string]string, output any) (callErr error) {
	started := time.Now()
	status := 0
	category := defaultLogCategory(c.logCategory)
	defer func() {
		message := "阿里云 DNS API 调用成功"
		if callErr != nil {
			message = callErr.Error()
		}
		c.logs.Add(LogEntry{
			Category:   category,
			Action:     "阿里云 DNS API",
			Target:     c.BaseURL,
			Method:     http.MethodPost,
			Path:       "/" + action,
			Status:     status,
			OK:         callErr == nil,
			DurationMS: time.Since(started).Milliseconds(),
			Message:    message,
		})
	}()

	var nonceBytes [16]byte
	if _, err := rand.Read(nonceBytes[:]); err != nil {
		return errors.New("阿里云 DNS 签名随机数生成失败")
	}

	all := map[string]string{
		"Action":           action,
		"Format":           "JSON",
		"Version":          "2015-01-09",
		"AccessKeyId":      c.AccessKeyID,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":   hex.EncodeToString(nonceBytes[:]),
		"Timestamp":        c.currentTime().Format("2006-01-02T15:04:05Z"),
	}
	for k, v := range params {
		all[k] = v
	}

	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, popPercentEncode(k)+"="+popPercentEncode(all[k]))
	}
	canonicalQuery := strings.Join(parts, "&")
	stringToSign := "POST&%2F&" + popPercentEncode(canonicalQuery)

	mac := hmac.New(sha1.New, []byte(c.AccessKeySecret+"&"))
	mac.Write([]byte(stringToSign))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	formBody := canonicalQuery + "&Signature=" + popPercentEncode(sig)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/", strings.NewReader(formBody))
	if err != nil {
		return errors.New("阿里云 DNS 请求构建失败")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.Client.Do(req)
	if err != nil {
		return errors.New("阿里云 DNS 连接失败，请检查网络")
	}
	defer resp.Body.Close()
	status = resp.StatusCode

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return errors.New("阿里云 DNS 响应读取失败")
	}
	var errResp struct {
		Code string `json:"Code"`
	}
	_ = json.Unmarshal(raw, &errResp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || errResp.Code != "" {
		// Never surface response bodies: provider messages may expose request metadata.
		return fmt.Errorf("阿里云 DNS 请求失败（HTTP %d，错误码 %s），请检查 AccessKey 权限与域名", resp.StatusCode, errResp.Code)
	}
	if output != nil {
		if err := json.Unmarshal(raw, output); err != nil {
			return errors.New("阿里云 DNS 数据格式错误")
		}
	}
	return nil
}

func (c *AliDNS) ZoneID(ctx context.Context, zone string) (string, error) {
	var resp struct {
		DomainID   string `json:"DomainId"`
		DomainName string `json:"DomainName"`
	}
	if err := c.call(ctx, "DescribeDomainInfo", map[string]string{"DomainName": zone}, &resp); err != nil {
		return "", err
	}
	if resp.DomainID == "" || !strings.EqualFold(strings.TrimSuffix(resp.DomainName, "."), zone) {
		return "", errors.New("未找到匹配的阿里云 DNS 域名，请检查权限")
	}
	return strings.ToLower(strings.TrimSuffix(resp.DomainName, ".")), nil
}

type aliRecord struct {
	RecordID string `json:"RecordId"`
	RR       string `json:"RR"`
	Type     string `json:"Type"`
	Value    string `json:"Value"`
	Line     string `json:"Line"`
	TTL      int    `json:"TTL"`
}

func (c *AliDNS) SyncRecord(ctx context.Context, zoneID string, record DNSRecord, ip netip.Addr) (bool, error) {
	if !validPublicIP(ip, record.Type) {
		return false, errors.New("公网 IP 类型不匹配或不是公网地址")
	}
	rr := relativeDNSName(record.Host, zoneID)
	var listResp struct {
		TotalCount    int `json:"TotalCount"`
		DomainRecords struct {
			Record []aliRecord `json:"Record"`
		} `json:"DomainRecords"`
	}
	if err := c.call(ctx, "DescribeDomainRecords", map[string]string{
		"DomainName": zoneID,
		"RRKeyWord":  rr,
		"SearchMode": "EXACT",
		"PageSize":   "500",
	}, &listResp); err != nil {
		return false, err
	}
	if listResp.TotalCount > 500 {
		return false, errors.New("阿里云同名 DNS 记录过多，拒绝自动修改")
	}
	var matches []aliRecord
	for _, r := range listResp.DomainRecords.Record {
		if !strings.EqualFold(r.RR, rr) {
			continue
		}
		if strings.EqualFold(r.Type, "CNAME") {
			return false, errors.New("存在同名 CNAME，请先在阿里云 DNS 处理冲突")
		}
		if strings.EqualFold(r.Type, record.Type) {
			matches = append(matches, r)
		}
	}
	if len(matches) > 1 {
		return false, errors.New("存在多条同名同类型记录，拒绝覆盖，请先在阿里云 DNS 合并")
	}
	if len(matches) == 0 {
		err := c.call(ctx, "AddDomainRecord", map[string]string{
			"DomainName": zoneID,
			"RR":         rr,
			"Type":       record.Type,
			"Value":      ip.String(),
			"TTL":        "600",
		}, nil)
		return err == nil, err
	}
	r := matches[0]
	if old, err := netip.ParseAddr(strings.TrimSpace(r.Value)); err == nil && old == ip {
		return false, nil
	}
	if r.RecordID == "" {
		return false, errors.New("阿里云 DNS 记录缺少 RecordId")
	}
	params := map[string]string{
		"RecordId": r.RecordID,
		"RR":       rr,
		"Type":     record.Type,
		"Value":    ip.String(),
		"TTL":      "600",
	}
	if r.TTL >= 1 {
		params["TTL"] = strconv.Itoa(r.TTL)
	}
	err := c.call(ctx, "UpdateDomainRecord", params, nil)
	return err == nil, err
}

func (c *AliDNS) PresentTXT(ctx context.Context, zoneID, zone, fqdn, value string) (string, error) {
	rr := relativeDNSName(fqdn, zone)
	var resp struct {
		RecordID string `json:"RecordId"`
	}
	if err := c.call(ctx, "AddDomainRecord", map[string]string{
		"DomainName": zoneID,
		"RR":         rr,
		"Type":       "TXT",
		"Value":      value,
		"TTL":        "600",
	}, &resp); err != nil {
		return "", err
	}
	if resp.RecordID == "" {
		return "", errors.New("阿里云 DNS 未返回 TXT 记录 ID")
	}
	return resp.RecordID, nil
}

func (c *AliDNS) CleanUpTXT(ctx context.Context, zoneID, zone, fqdn, recordID, value string) error {
	if recordID != "" {
		return c.call(ctx, "DeleteDomainRecord", map[string]string{"RecordId": recordID}, nil)
	}
	rr := relativeDNSName(fqdn, zone)
	var listResp struct {
		DomainRecords struct {
			Record []aliRecord `json:"Record"`
		} `json:"DomainRecords"`
	}
	if err := c.call(ctx, "DescribeDomainRecords", map[string]string{
		"DomainName": zoneID,
		"RRKeyWord":  rr,
		"Type":       "TXT",
		"SearchMode": "EXACT",
		"PageSize":   "500",
	}, &listResp); err != nil {
		return err
	}
	for _, r := range listResp.DomainRecords.Record {
		if strings.EqualFold(r.RR, rr) && strings.EqualFold(r.Type, "TXT") && r.Value == value && r.RecordID != "" {
			if err := c.call(ctx, "DeleteDomainRecord", map[string]string{"RecordId": r.RecordID}, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// ============================================================================
// 2. 腾讯云 DNS / DNSPod (TencentCloudDNS)
// ============================================================================

type TencentCloudDNS struct {
	SecretID    string
	SecretKey   string
	BaseURL     string
	Client      *http.Client
	logs        *Logs
	logCategory string
	now         func() time.Time
}

func NewTencentCloudDNS(secretID, secretKey string) *TencentCloudDNS {
	return &TencentCloudDNS{
		SecretID:  secretID,
		SecretKey: secretKey,
		BaseURL:   "https://dnspod.tencentcloudapi.com",
		Client:    &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *TencentCloudDNS) Close() { closeClient(c.Client) }

func (c *TencentCloudDNS) currentTime() time.Time {
	if c.now != nil {
		return c.now().UTC()
	}
	return time.Now().UTC()
}

func (c *TencentCloudDNS) call(ctx context.Context, action string, payload any, output any, allowNoData bool) (noData bool, callErr error) {
	started := time.Now()
	status := 0
	category := defaultLogCategory(c.logCategory)
	defer func() {
		message := "腾讯云 DNS API 调用成功"
		if callErr != nil {
			message = callErr.Error()
		}
		c.logs.Add(LogEntry{
			Category:   category,
			Action:     "腾讯云 DNS API",
			Target:     c.BaseURL,
			Method:     http.MethodPost,
			Path:       "/" + action,
			Status:     status,
			OK:         callErr == nil,
			DurationMS: time.Since(started).Milliseconds(),
			Message:    message,
		})
	}()

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	targetURL := strings.TrimRight(c.BaseURL, "/") + "/"
	u, err := url.Parse(targetURL)
	if err != nil || u.Host == "" {
		return false, errors.New("腾讯云 DNS 地址无效")
	}

	now := c.currentTime()
	ts := strconv.FormatInt(now.Unix(), 10)
	date := now.Format("2006-01-02")
	host := u.Host

	contentType := "application/json; charset=utf-8"
	canonicalHeaders := "content-type:" + contentType + "\nhost:" + host + "\nx-tc-action:" + strings.ToLower(action) + "\n"
	signedHeaders := "content-type;host;x-tc-action"
	canonicalRequest := "POST\n/\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + sha256Hex(bodyBytes)

	credentialScope := date + "/dnspod/tc3_request"
	stringToSign := "TC3-HMAC-SHA256\n" + ts + "\n" + credentialScope + "\n" + sha256Hex([]byte(canonicalRequest))

	secretDate := hmacSHA256Bytes([]byte("TC3"+c.SecretKey), date)
	secretService := hmacSHA256Bytes(secretDate, "dnspod")
	secretSigning := hmacSHA256Bytes(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256Bytes(secretSigning, stringToSign))
	authorization := fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.SecretID, credentialScope, signedHeaders, signature)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return false, errors.New("腾讯云 DNS 请求构建失败")
	}
	req.Host = host
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", "2021-03-23")
	req.Header.Set("X-TC-Timestamp", ts)
	req.Header.Set("Authorization", authorization)

	resp, err := c.Client.Do(req)
	if err != nil {
		return false, errors.New("腾讯云 DNS 连接失败，请检查网络")
	}
	defer resp.Body.Close()
	status = resp.StatusCode

	var envelope struct {
		Response struct {
			Error *struct {
				Code string `json:"Code"`
			} `json:"Error"`
		} `json:"Response"`
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, errors.New("腾讯云 DNS 响应读取失败")
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return false, errors.New("腾讯云 DNS 返回了无效响应")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code := ""
		if envelope.Response.Error != nil {
			code = envelope.Response.Error.Code
		}
		return false, fmt.Errorf("腾讯云 DNS 请求失败（HTTP %d，错误码 %s），请检查 SecretId / SecretKey 权限与域名", resp.StatusCode, code)
	}
	if envelope.Response.Error != nil {
		if allowNoData && envelope.Response.Error.Code == "ResourceNotFound.NoDataOfRecord" {
			return true, nil
		}
		return false, fmt.Errorf("腾讯云 DNS 请求失败（错误码 %s），请检查 SecretId / SecretKey 权限与域名", envelope.Response.Error.Code)
	}
	if output != nil {
		var wrapper struct {
			Response json.RawMessage `json:"Response"`
		}
		if err := json.Unmarshal(raw, &wrapper); err != nil || json.Unmarshal(wrapper.Response, output) != nil {
			return false, errors.New("腾讯云 DNS 数据格式错误")
		}
	}
	return false, nil
}

func (c *TencentCloudDNS) ZoneID(ctx context.Context, zone string) (string, error) {
	var resp struct {
		DomainInfo struct {
			Domain string `json:"Domain"`
		} `json:"DomainInfo"`
	}
	if _, err := c.call(ctx, "DescribeDomain", map[string]any{"Domain": zone}, &resp, false); err != nil {
		return "", err
	}
	resolved := strings.TrimSuffix(strings.ToLower(resp.DomainInfo.Domain), ".")
	if resolved == "" || resolved != zone {
		return "", errors.New("未找到匹配的腾讯云 DNS 域名，请检查权限")
	}
	return resolved, nil
}

type tencentRecord struct {
	RecordID uint64 `json:"RecordId"`
	Name     string `json:"Name"`
	Type     string `json:"Type"`
	Value    string `json:"Value"`
	Line     string `json:"Line"`
	TTL      uint64 `json:"TTL"`
}

func (c *TencentCloudDNS) SyncRecord(ctx context.Context, zoneID string, record DNSRecord, ip netip.Addr) (bool, error) {
	if !validPublicIP(ip, record.Type) {
		return false, errors.New("公网 IP 类型不匹配或不是公网地址")
	}
	sub := relativeDNSName(record.Host, zoneID)
	var listResp struct {
		RecordCountInfo struct {
			TotalCount int `json:"TotalCount"`
		} `json:"RecordCountInfo"`
		RecordList []tencentRecord `json:"RecordList"`
	}
	noData, err := c.call(ctx, "DescribeRecordList", map[string]any{
		"Domain":    zoneID,
		"Subdomain": sub,
		"Limit":     3000,
	}, &listResp, true)
	if err != nil {
		return false, err
	}
	if !noData && listResp.RecordCountInfo.TotalCount > 3000 {
		return false, errors.New("腾讯云同名 DNS 记录过多，拒绝自动修改")
	}
	var matches []tencentRecord
	if !noData {
		for _, r := range listResp.RecordList {
			if !strings.EqualFold(r.Name, sub) {
				continue
			}
			if strings.EqualFold(r.Type, "CNAME") {
				return false, errors.New("存在同名 CNAME，请先在腾讯云 DNS 处理冲突")
			}
			if strings.EqualFold(r.Type, record.Type) {
				matches = append(matches, r)
			}
		}
	}
	if len(matches) > 1 {
		return false, errors.New("存在多条同名同类型记录，拒绝覆盖，请先在腾讯云 DNS 合并")
	}
	if len(matches) == 0 {
		_, err := c.call(ctx, "CreateRecord", map[string]any{
			"Domain":     zoneID,
			"SubDomain":  sub,
			"RecordType": record.Type,
			"RecordLine": "默认",
			"Value":      ip.String(),
			"TTL":        600,
		}, nil, false)
		return err == nil, err
	}
	r := matches[0]
	if old, err := netip.ParseAddr(strings.TrimSpace(r.Value)); err == nil && old == ip {
		return false, nil
	}
	if r.RecordID == 0 {
		return false, errors.New("腾讯云 DNS 记录缺少 RecordId")
	}
	line := r.Line
	if line == "" {
		line = "默认"
	}
	ttl := r.TTL
	if ttl == 0 {
		ttl = 600
	}
	_, err = c.call(ctx, "ModifyRecord", map[string]any{
		"Domain":     zoneID,
		"RecordId":   r.RecordID,
		"SubDomain":  sub,
		"RecordType": record.Type,
		"RecordLine": line,
		"Value":      ip.String(),
		"TTL":        ttl,
	}, nil, false)
	return err == nil, err
}

func (c *TencentCloudDNS) PresentTXT(ctx context.Context, zoneID, zone, fqdn, value string) (string, error) {
	sub := relativeDNSName(fqdn, zone)
	var resp struct {
		RecordID uint64 `json:"RecordId"`
	}
	if _, err := c.call(ctx, "CreateRecord", map[string]any{
		"Domain":     zoneID,
		"SubDomain":  sub,
		"RecordType": "TXT",
		"RecordLine": "默认",
		"Value":      value,
		"TTL":        600,
	}, &resp, false); err != nil {
		return "", err
	}
	if resp.RecordID == 0 {
		return "", errors.New("腾讯云 DNS 未返回 TXT 记录 ID")
	}
	return strconv.FormatUint(resp.RecordID, 10), nil
}

func (c *TencentCloudDNS) CleanUpTXT(ctx context.Context, zoneID, zone, fqdn, recordID, value string) error {
	if recordID != "" {
		id, err := strconv.ParseUint(recordID, 10, 64)
		if err != nil {
			return errors.New("腾讯云 DNS 记录 ID 无效")
		}
		_, err = c.call(ctx, "DeleteRecord", map[string]any{
			"Domain":   zoneID,
			"RecordId": id,
		}, nil, false)
		return err
	}
	sub := relativeDNSName(fqdn, zone)
	var listResp struct {
		RecordList []tencentRecord `json:"RecordList"`
	}
	noData, err := c.call(ctx, "DescribeRecordList", map[string]any{
		"Domain":     zoneID,
		"Subdomain":  sub,
		"RecordType": "TXT",
		"Limit":      3000,
	}, &listResp, true)
	if err != nil || noData {
		return err
	}
	for _, r := range listResp.RecordList {
		if strings.EqualFold(r.Name, sub) && strings.EqualFold(r.Type, "TXT") && r.Value == value && r.RecordID != 0 {
			if _, err := c.call(ctx, "DeleteRecord", map[string]any{
				"Domain":   zoneID,
				"RecordId": r.RecordID,
			}, nil, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// ============================================================================
// 3. 华为云 DNS (HuaweiCloudDNS)
// ============================================================================

type HuaweiCloudDNS struct {
	AccessKeyID     string
	SecretAccessKey string
	BaseURL         string
	Client          *http.Client
	logs            *Logs
	logCategory     string
	now             func() time.Time
	mu              sync.Mutex // Serialize multi-TXT RecordSet read-modify-write for ACME wildcards.
}

func NewHuaweiCloudDNS(accessKeyID, secretAccessKey string) *HuaweiCloudDNS {
	return &HuaweiCloudDNS{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		BaseURL:         "https://dns.myhuaweicloud.com",
		Client:          &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *HuaweiCloudDNS) Close() { closeClient(c.Client) }

func (c *HuaweiCloudDNS) currentTime() time.Time {
	if c.now != nil {
		return c.now().UTC()
	}
	return time.Now().UTC()
}

func huaweiCanonicalQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	vals, err := url.ParseQuery(rawQuery)
	if err != nil {
		return ""
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		items := append([]string(nil), vals[k]...)
		sort.Strings(items)
		ek := popPercentEncode(k)
		for _, item := range items {
			parts = append(parts, ek+"="+popPercentEncode(item))
		}
	}
	return strings.Join(parts, "&")
}

func (c *HuaweiCloudDNS) call(ctx context.Context, method, pathWithQuery string, input, output any) (callErr error) {
	started := time.Now()
	status := 0
	category := defaultLogCategory(c.logCategory)
	defer func() {
		message := "华为云 DNS API 调用成功"
		if callErr != nil {
			message = callErr.Error()
		}
		c.logs.Add(LogEntry{
			Category:   category,
			Action:     "华为云 DNS API",
			Target:     c.BaseURL,
			Method:     method,
			Path:       pathWithQuery,
			Status:     status,
			OK:         callErr == nil,
			DurationMS: time.Since(started).Milliseconds(),
			Message:    message,
		})
	}()

	var bodyBytes []byte
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return err
		}
		bodyBytes = b
	}
	fullURL := strings.TrimRight(c.BaseURL, "/") + pathWithQuery
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return errors.New("华为云 DNS 请求构建失败")
	}

	u := req.URL
	sdkDate := c.currentTime().Format("20060102T150405Z")
	req.Host = u.Host
	req.Header.Set("X-Sdk-Date", sdkDate)

	signedHeadersMap := map[string]string{
		"host":       u.Host,
		"x-sdk-date": sdkDate,
	}
	if len(bodyBytes) > 0 {
		req.Header.Set("Content-Type", "application/json")
		signedHeadersMap["content-type"] = "application/json"
	}

	canonURI := u.EscapedPath()
	if canonURI == "" {
		canonURI = "/"
	} else if !strings.HasSuffix(canonURI, "/") {
		canonURI += "/"
	}
	canonQuery := huaweiCanonicalQuery(u.RawQuery)

	headerKeys := make([]string, 0, len(signedHeadersMap))
	for k := range signedHeadersMap {
		headerKeys = append(headerKeys, k)
	}
	sort.Strings(headerKeys)
	var canonHeaders strings.Builder
	for _, k := range headerKeys {
		canonHeaders.WriteString(k)
		canonHeaders.WriteByte(':')
		canonHeaders.WriteString(strings.TrimSpace(signedHeadersMap[k]))
		canonHeaders.WriteByte('\n')
	}
	signedHeaders := strings.Join(headerKeys, ";")
	canonReq := method + "\n" + canonURI + "\n" + canonQuery + "\n" + canonHeaders.String() + "\n" + signedHeaders + "\n" + sha256Hex(bodyBytes)
	stringToSign := "SDK-HMAC-SHA256\n" + sdkDate + "\n" + sha256Hex([]byte(canonReq))
	sig := hex.EncodeToString(hmacSHA256Bytes([]byte(c.SecretAccessKey), stringToSign))
	auth := fmt.Sprintf("SDK-HMAC-SHA256 Access=%s, SignedHeaders=%s, Signature=%s", c.AccessKeyID, signedHeaders, sig)
	req.Header.Set("Authorization", auth)

	resp, err := c.Client.Do(req)
	if err != nil {
		return errors.New("华为云 DNS 连接失败，请检查网络")
	}
	defer resp.Body.Close()
	status = resp.StatusCode

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return errors.New("华为云 DNS 响应读取失败")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errBody struct {
			Code      string `json:"code"`
			ErrorCode string `json:"error_code"`
		}
		_ = json.Unmarshal(raw, &errBody)
		code := errBody.ErrorCode
		if code == "" {
			code = errBody.Code
		}
		return fmt.Errorf("华为云 DNS 请求失败（HTTP %d，错误码 %s），请检查 AK/SK 权限与域名", resp.StatusCode, code)
	}
	if output != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, output); err != nil {
			return errors.New("华为云 DNS 数据格式错误")
		}
	}
	return nil
}

func (c *HuaweiCloudDNS) ZoneID(ctx context.Context, zone string) (string, error) {
	var resp struct {
		Zones []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"zones"`
	}
	if err := c.call(ctx, http.MethodGet, "/v2/zones?type=public&name="+url.QueryEscape(zone+"."), nil, &resp); err != nil {
		return "", err
	}
	var matchedID string
	count := 0
	for _, z := range resp.Zones {
		if strings.TrimSuffix(strings.ToLower(z.Name), ".") == zone && z.ID != "" {
			matchedID = z.ID
			count++
		}
	}
	if count != 1 || matchedID == "" {
		return "", errors.New("未找到唯一匹配的华为云 DNS 域名，请检查权限")
	}
	return matchedID, nil
}

type huaweiRecordSet struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	TTL     int      `json:"ttl"`
	Records []string `json:"records"`
}

func (c *HuaweiCloudDNS) listRecordSets(ctx context.Context, zoneID, fqdn string) ([]huaweiRecordSet, int, error) {
	targetName := strings.TrimSuffix(strings.ToLower(fqdn), ".") + "."
	var resp struct {
		RecordSets []huaweiRecordSet `json:"recordsets"`
		Metadata   struct {
			TotalCount int `json:"total_count"`
		} `json:"metadata"`
	}
	path := "/v2/zones/" + url.PathEscape(zoneID) + "/recordsets?name=" + url.QueryEscape(targetName) + "&search_mode=equal&limit=500"
	if err := c.call(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, 0, err
	}
	return resp.RecordSets, resp.Metadata.TotalCount, nil
}

func (c *HuaweiCloudDNS) SyncRecord(ctx context.Context, zoneID string, record DNSRecord, ip netip.Addr) (bool, error) {
	if !validPublicIP(ip, record.Type) {
		return false, errors.New("公网 IP 类型不匹配或不是公网地址")
	}
	rows, totalCount, err := c.listRecordSets(ctx, zoneID, record.Host)
	if err != nil {
		return false, err
	}
	if totalCount > 500 {
		return false, errors.New("华为云同名 DNS 记录过多，拒绝自动修改")
	}
	var matches []huaweiRecordSet
	for _, r := range rows {
		if strings.TrimSuffix(strings.ToLower(r.Name), ".") != record.Host {
			continue
		}
		if strings.EqualFold(r.Type, "CNAME") {
			return false, errors.New("存在同名 CNAME，请先在华为云 DNS 处理冲突")
		}
		if strings.EqualFold(r.Type, record.Type) {
			matches = append(matches, r)
		}
	}
	if len(matches) > 1 {
		return false, errors.New("存在多条同名同类型记录，拒绝覆盖，请先在华为云 DNS 合并")
	}
	basePath := "/v2/zones/" + url.PathEscape(zoneID) + "/recordsets"
	fqdn := record.Host + "."
	if len(matches) == 0 {
		err := c.call(ctx, http.MethodPost, basePath, map[string]any{
			"name":    fqdn,
			"type":    record.Type,
			"ttl":     300,
			"records": []string{ip.String()},
		}, nil)
		return err == nil, err
	}
	r := matches[0]
	if len(r.Records) > 1 {
		return false, errors.New("存在多条解析值，拒绝覆盖，请先在华为云 DNS 清理")
	}
	if len(r.Records) == 1 {
		if old, err := netip.ParseAddr(strings.TrimSpace(r.Records[0])); err == nil && old == ip {
			return false, nil
		}
	}
	if r.ID == "" {
		return false, errors.New("华为云 DNS 记录缺少 ID")
	}
	ttl := r.TTL
	if ttl < 1 {
		ttl = 300
	}
	err = c.call(ctx, http.MethodPut, basePath+"/"+url.PathEscape(r.ID), map[string]any{
		"name":    fqdn,
		"type":    record.Type,
		"ttl":     ttl,
		"records": []string{ip.String()},
	}, nil)
	return err == nil, err
}

func (c *HuaweiCloudDNS) PresentTXT(ctx context.Context, zoneID, zone, fqdn, value string) (string, error) {
	_ = zone
	c.mu.Lock()
	defer c.mu.Unlock()

	targetHost := strings.TrimSuffix(strings.ToLower(fqdn), ".")
	targetFQDN := targetHost + "."
	quoted := strconv.Quote(value)

	rows, _, err := c.listRecordSets(ctx, zoneID, targetHost)
	if err != nil {
		return "", err
	}
	var existing *huaweiRecordSet
	for i := range rows {
		if strings.TrimSuffix(strings.ToLower(rows[i].Name), ".") == targetHost && strings.EqualFold(rows[i].Type, "TXT") {
			existing = &rows[i]
			break
		}
	}
	basePath := "/v2/zones/" + url.PathEscape(zoneID) + "/recordsets"
	if existing == nil {
		var created struct {
			ID string `json:"id"`
		}
		if err := c.call(ctx, http.MethodPost, basePath, map[string]any{
			"name":    targetFQDN,
			"type":    "TXT",
			"ttl":     300,
			"records": []string{quoted},
		}, &created); err != nil {
			return "", err
		}
		return created.ID, nil
	}
	for _, item := range existing.Records {
		if item == quoted || item == value {
			return existing.ID, nil
		}
	}
	updatedRecords := append(append([]string(nil), existing.Records...), quoted)
	ttl := existing.TTL
	if ttl < 1 {
		ttl = 300
	}
	if err := c.call(ctx, http.MethodPut, basePath+"/"+url.PathEscape(existing.ID), map[string]any{
		"name":    targetFQDN,
		"type":    "TXT",
		"ttl":     ttl,
		"records": updatedRecords,
	}, nil); err != nil {
		return "", err
	}
	return existing.ID, nil
}

func (c *HuaweiCloudDNS) CleanUpTXT(ctx context.Context, zoneID, zone, fqdn, recordID, value string) error {
	_ = zone
	_ = recordID
	c.mu.Lock()
	defer c.mu.Unlock()

	targetHost := strings.TrimSuffix(strings.ToLower(fqdn), ".")
	targetFQDN := targetHost + "."
	quoted := strconv.Quote(value)

	rows, _, err := c.listRecordSets(ctx, zoneID, targetHost)
	if err != nil {
		return err
	}
	var existing *huaweiRecordSet
	for i := range rows {
		if strings.TrimSuffix(strings.ToLower(rows[i].Name), ".") == targetHost && strings.EqualFold(rows[i].Type, "TXT") {
			existing = &rows[i]
			break
		}
	}
	if existing == nil || existing.ID == "" {
		return nil
	}
	remaining := make([]string, 0, len(existing.Records))
	removed := false
	for _, item := range existing.Records {
		if item == quoted || item == value {
			removed = true
			continue
		}
		remaining = append(remaining, item)
	}
	if !removed {
		return nil
	}
	itemPath := "/v2/zones/" + url.PathEscape(zoneID) + "/recordsets/" + url.PathEscape(existing.ID)
	if len(remaining) == 0 {
		return c.call(ctx, http.MethodDelete, itemPath, nil, nil)
	}
	ttl := existing.TTL
	if ttl < 1 {
		ttl = 300
	}
	return c.call(ctx, http.MethodPut, itemPath, map[string]any{
		"name":    targetFQDN,
		"type":    "TXT",
		"ttl":     ttl,
		"records": remaining,
	}, nil)
}

// ============================================================================
// 4. Porkbun (PorkbunDNS)
// ============================================================================

type PorkbunDNS struct {
	APIKey       string
	SecretAPIKey string
	BaseURL      string
	Client       *http.Client
	logs         *Logs
	logCategory  string
}

func NewPorkbunDNS(apiKey, secretAPIKey string) *PorkbunDNS {
	return &PorkbunDNS{
		APIKey:       apiKey,
		SecretAPIKey: secretAPIKey,
		BaseURL:      "https://api.porkbun.com/api/json/v3",
		Client:       &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *PorkbunDNS) Close() { closeClient(c.Client) }

func (c *PorkbunDNS) call(ctx context.Context, path string, fields map[string]any, output any) (callErr error) {
	started := time.Now()
	status := 0
	category := defaultLogCategory(c.logCategory)
	defer func() {
		message := "Porkbun API 调用成功"
		if callErr != nil {
			message = callErr.Error()
		}
		c.logs.Add(LogEntry{
			Category:   category,
			Action:     "Porkbun API",
			Target:     c.BaseURL,
			Method:     http.MethodPost,
			Path:       path,
			Status:     status,
			OK:         callErr == nil,
			DurationMS: time.Since(started).Milliseconds(),
			Message:    message,
		})
	}()

	payload := map[string]any{
		"apikey":       c.APIKey,
		"secretapikey": c.SecretAPIKey,
	}
	for k, v := range fields {
		payload[k] = v
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return errors.New("Porkbun 请求构建失败")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return errors.New("Porkbun 连接失败，请检查网络")
	}
	defer resp.Body.Close()
	status = resp.StatusCode

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return errors.New("Porkbun 响应读取失败")
	}
	var envelope struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return errors.New("Porkbun 返回了无效响应")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || envelope.Status != "SUCCESS" {
		return fmt.Errorf("Porkbun 请求失败（HTTP %d，状态 %s），请检查 API Key 权限与域名", resp.StatusCode, envelope.Status)
	}
	if output != nil {
		if err := json.Unmarshal(raw, output); err != nil {
			return errors.New("Porkbun 数据格式错误")
		}
	}
	return nil
}

func (c *PorkbunDNS) ZoneID(ctx context.Context, zone string) (string, error) {
	if err := c.call(ctx, "/dns/retrieve/"+url.PathEscape(zone), nil, nil); err != nil {
		return "", err
	}
	return zone, nil
}

type porkbunRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     string `json:"ttl"`
}

func porkbunSubName(host, zone string) string {
	rel := relativeDNSName(host, zone)
	if rel == "@" {
		return ""
	}
	return rel
}

func (c *PorkbunDNS) SyncRecord(ctx context.Context, zoneID string, record DNSRecord, ip netip.Addr) (bool, error) {
	if !validPublicIP(ip, record.Type) {
		return false, errors.New("公网 IP 类型不匹配或不是公网地址")
	}
	var listResp struct {
		Records []porkbunRecord `json:"records"`
	}
	if err := c.call(ctx, "/dns/retrieve/"+url.PathEscape(zoneID), nil, &listResp); err != nil {
		return false, err
	}
	var matches []porkbunRecord
	for _, r := range listResp.Records {
		if strings.TrimSuffix(strings.ToLower(r.Name), ".") != record.Host {
			continue
		}
		if strings.EqualFold(r.Type, "CNAME") {
			return false, errors.New("存在同名 CNAME，请先在 Porkbun 处理冲突")
		}
		if strings.EqualFold(r.Type, record.Type) {
			matches = append(matches, r)
		}
	}
	if len(matches) > 1 {
		return false, errors.New("存在多条同名同类型记录，拒绝覆盖，请先在 Porkbun 合并")
	}
	sub := porkbunSubName(record.Host, zoneID)
	if len(matches) == 0 {
		err := c.call(ctx, "/dns/create/"+url.PathEscape(zoneID), map[string]any{
			"name":    sub,
			"type":    record.Type,
			"content": ip.String(),
			"ttl":     "600",
		}, nil)
		return err == nil, err
	}
	r := matches[0]
	if old, err := netip.ParseAddr(strings.TrimSpace(r.Content)); err == nil && old == ip {
		return false, nil
	}
	if r.ID == "" {
		return false, errors.New("Porkbun 记录缺少 ID")
	}
	ttl := r.TTL
	if ttl == "" {
		ttl = "600"
	}
	err := c.call(ctx, "/dns/edit/"+url.PathEscape(zoneID)+"/"+url.PathEscape(r.ID), map[string]any{
		"name":    sub,
		"type":    record.Type,
		"content": ip.String(),
		"ttl":     ttl,
	}, nil)
	return err == nil, err
}

func parseFlexibleID(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil && str != "" {
		return str
	}
	var num uint64
	if json.Unmarshal(raw, &num) == nil && num > 0 {
		return strconv.FormatUint(num, 10)
	}
	return ""
}

func (c *PorkbunDNS) PresentTXT(ctx context.Context, zoneID, zone, fqdn, value string) (string, error) {
	sub := porkbunSubName(fqdn, zone)
	var resp struct {
		ID json.RawMessage `json:"id"`
	}
	if err := c.call(ctx, "/dns/create/"+url.PathEscape(zoneID), map[string]any{
		"name":    sub,
		"type":    "TXT",
		"content": value,
		"ttl":     "600",
	}, &resp); err != nil {
		return "", err
	}
	id := parseFlexibleID(resp.ID)
	if id == "" {
		return "", errors.New("Porkbun 未返回 TXT 记录 ID")
	}
	return id, nil
}

func (c *PorkbunDNS) CleanUpTXT(ctx context.Context, zoneID, zone, fqdn, recordID, value string) error {
	if recordID != "" {
		return c.call(ctx, "/dns/delete/"+url.PathEscape(zoneID)+"/"+url.PathEscape(recordID), nil, nil)
	}
	targetHost := strings.TrimSuffix(strings.ToLower(fqdn), ".")
	var listResp struct {
		Records []porkbunRecord `json:"records"`
	}
	if err := c.call(ctx, "/dns/retrieve/"+url.PathEscape(zoneID), nil, &listResp); err != nil {
		return err
	}
	for _, r := range listResp.Records {
		if strings.TrimSuffix(strings.ToLower(r.Name), ".") == targetHost && strings.EqualFold(r.Type, "TXT") && r.Content == value && r.ID != "" {
			if err := c.call(ctx, "/dns/delete/"+url.PathEscape(zoneID)+"/"+url.PathEscape(r.ID), nil, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
