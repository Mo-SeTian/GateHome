package gateway

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
)

type DNSCredential struct {
	KeyID string `json:"key_id,omitempty"`
	Token string `json:"token"`
}

type DNSCredentialPatch struct {
	KeyID *string `json:"key_id,omitempty"`
	Token *string `json:"token,omitempty"`
}

type DNSProviderMeta struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RequiresKeyID bool   `json:"requires_key_id"`
	KeyIDLabel    string `json:"key_id_label,omitempty"`
	TokenLabel    string `json:"token_label"`
}

var dnsProvidersCatalogList = []DNSProviderMeta{
	{ID: "cloudflare", Name: "Cloudflare", RequiresKeyID: false, TokenLabel: "API Token"},
	{ID: "alidns", Name: "阿里云 DNS", RequiresKeyID: true, KeyIDLabel: "AccessKey ID", TokenLabel: "AccessKey Secret"},
	{ID: "tencentcloud", Name: "腾讯云 DNS (DNSPod)", RequiresKeyID: true, KeyIDLabel: "SecretId", TokenLabel: "SecretKey"},
	{ID: "huaweicloud", Name: "华为云 DNS", RequiresKeyID: true, KeyIDLabel: "Access Key ID", TokenLabel: "Secret Access Key"},
	{ID: "porkbun", Name: "Porkbun", RequiresKeyID: true, KeyIDLabel: "API Key", TokenLabel: "Secret API Key"},
}

func dnsProvidersCatalog() []DNSProviderMeta {
	out := make([]DNSProviderMeta, len(dnsProvidersCatalogList))
	copy(out, dnsProvidersCatalogList)
	return out
}

func findDNSProviderMeta(id string) (DNSProviderMeta, bool) {
	for _, meta := range dnsProvidersCatalogList {
		if meta.ID == id {
			return meta, true
		}
	}
	return DNSProviderMeta{}, false
}

func supportedDNSProvider(id string) bool {
	_, ok := findDNSProviderMeta(id)
	return ok
}

func dnsProviderRequiresKeyID(id string) bool {
	meta, ok := findDNSProviderMeta(id)
	return ok && meta.RequiresKeyID
}

func dnsProviderName(id string) string {
	if meta, ok := findDNSProviderMeta(id); ok {
		return meta.Name
	}
	return id
}

func (c DNSCredential) HasAny() bool {
	return c.KeyID != "" || c.Token != ""
}

func (c DNSCredential) ConfiguredFor(provider string) bool {
	if c.Token == "" {
		return false
	}
	if dnsProviderRequiresKeyID(provider) {
		return c.KeyID != ""
	}
	return true
}

func validateDNSCredentialValue(v string) bool {
	return len(v) <= 512 && !containsCredentialNewline(v)
}

func (s State) HasDNSToken() bool {
	if s.CloudflareToken != "" {
		return true
	}
	for _, c := range s.DNSCredentials {
		if c.HasAny() {
			return true
		}
	}
	for _, c := range s.CertificateCredentials {
		if c.HasAny() {
			return true
		}
	}
	return false
}

type dnsProvider interface {
	ZoneID(context.Context, string) (string, error)
	SyncRecord(context.Context, string, DNSRecord, netip.Addr) (bool, error)
	Close()
}

type acmeTXTProvider interface {
	ZoneID(context.Context, string) (string, error)
	PresentTXT(ctx context.Context, zoneID, zone, fqdn, value string) (string, error)
	CleanUpTXT(ctx context.Context, zoneID, zone, fqdn, recordID, value string) error
}

type cloudflareProvider struct{ *Cloudflare }

func (p cloudflareProvider) Close() { closeClient(p.Client) }

func newDNSProvider(s State, g DDNSGroup, logs *Logs) (dnsProvider, error) {
	cred := s.DNSCredentials[g.ID]
	switch g.Provider {
	case "cloudflare":
		if !cred.ConfiguredFor(g.Provider) {
			return nil, errors.New("此 DNS 组尚未配置 Cloudflare API Token")
		}
		cf := NewCloudflare(cred.Token)
		cf.Client = outboundClient(s, 20*time.Second)
		cf.logs = logs
		return cloudflareProvider{cf}, nil
	case "alidns":
		if !cred.ConfiguredFor(g.Provider) {
			return nil, errors.New("此 DNS 组尚未配置完整的阿里云 AccessKey 凭据")
		}
		p := NewAliDNS(cred.KeyID, cred.Token)
		p.Client = outboundClient(s, 20*time.Second)
		p.logs = logs
		p.logCategory = "ddns"
		return p, nil
	case "tencentcloud":
		if !cred.ConfiguredFor(g.Provider) {
			return nil, errors.New("此 DNS 组尚未配置完整的腾讯云 SecretId / SecretKey")
		}
		p := NewTencentCloudDNS(cred.KeyID, cred.Token)
		p.Client = outboundClient(s, 20*time.Second)
		p.logs = logs
		p.logCategory = "ddns"
		return p, nil
	case "huaweicloud":
		if !cred.ConfiguredFor(g.Provider) {
			return nil, errors.New("此 DNS 组尚未配置完整的华为云 Access Key 凭据")
		}
		p := NewHuaweiCloudDNS(cred.KeyID, cred.Token)
		p.Client = outboundClient(s, 20*time.Second)
		p.logs = logs
		p.logCategory = "ddns"
		return p, nil
	case "porkbun":
		if !cred.ConfiguredFor(g.Provider) {
			return nil, errors.New("此 DNS 组尚未配置完整的 Porkbun API Key 凭据")
		}
		p := NewPorkbunDNS(cred.KeyID, cred.Token)
		p.Client = outboundClient(s, 20*time.Second)
		p.logs = logs
		p.logCategory = "ddns"
		return p, nil
	default:
		return nil, errors.New("此 DNS 服务商尚未支持")
	}
}

func newACMETXTProvider(provider string, cred DNSCredential, client *http.Client, logs *Logs) (acmeTXTProvider, func(), error) {
	switch provider {
	case "alidns":
		p := NewAliDNS(cred.KeyID, cred.Token)
		p.Client = client
		p.logs = logs
		p.logCategory = "certificates"
		return p, p.Close, nil
	case "tencentcloud":
		p := NewTencentCloudDNS(cred.KeyID, cred.Token)
		p.Client = client
		p.logs = logs
		p.logCategory = "certificates"
		return p, p.Close, nil
	case "huaweicloud":
		p := NewHuaweiCloudDNS(cred.KeyID, cred.Token)
		p.Client = client
		p.logs = logs
		p.logCategory = "certificates"
		return p, p.Close, nil
	case "porkbun":
		p := NewPorkbunDNS(cred.KeyID, cred.Token)
		p.Client = client
		p.logs = logs
		p.logCategory = "certificates"
		return p, p.Close, nil
	default:
		return nil, nil, errors.New("证书 DNS 服务商不支持")
	}
}

type acmeTXTRecord struct {
	zoneID   string
	zone     string
	recordID string
}

type dns01ChallengeAdapter struct {
	provider acmeTXTProvider
	mu       sync.Mutex
	records  map[string]acmeTXTRecord
	findZone func(fqdn string) (string, error)
}

var _ challenge.ProviderTimeout = (*dns01ChallengeAdapter)(nil)

func newDNS01ChallengeAdapter(provider acmeTXTProvider) *dns01ChallengeAdapter {
	return &dns01ChallengeAdapter{
		provider: provider,
		records:  map[string]acmeTXTRecord{},
	}
}

func (a *dns01ChallengeAdapter) Timeout() (time.Duration, time.Duration) {
	return 3 * time.Minute, 5 * time.Second
}

func (a *dns01ChallengeAdapter) resolveZone(ctx context.Context, fqdn string) (zone, zoneID string, err error) {
	if a.findZone != nil {
		raw, err := a.findZone(fqdn)
		if err != nil {
			return "", "", err
		}
		zone = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
		if zone == "" {
			return "", "", errors.New("无法识别 DNS 根域名")
		}
		zoneID, err = a.provider.ZoneID(ctx, zone)
		if err != nil {
			return "", "", err
		}
		return zone, zoneID, nil
	}
	if authZone, findErr := dns01.FindZoneByFqdn(fqdn); findErr == nil {
		candidate := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(authZone)), ".")
		if candidate != "" {
			if id, idErr := a.provider.ZoneID(ctx, candidate); idErr == nil {
				return candidate, id, nil
			}
		}
	}
	trimmed := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	labels := strings.Split(trimmed, ".")
	var lastErr error
	for i := 0; i <= len(labels)-2; i++ {
		if strings.HasPrefix(labels[i], "_") {
			continue
		}
		candidate := strings.Join(labels[i:], ".")
		id, err := a.provider.ZoneID(ctx, candidate)
		if err == nil {
			return candidate, id, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", "", lastErr
	}
	return "", "", errors.New("无法匹配 DNS 根域名")
}

func (a *dns01ChallengeAdapter) Present(domain, token, keyAuth string) error {
	domain = strings.TrimPrefix(strings.TrimSpace(domain), "*.")
	info := dns01.GetChallengeInfo(domain, keyAuth)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	zone, zoneID, err := a.resolveZone(ctx, info.EffectiveFQDN)
	if err != nil {
		return err
	}
	recordID, err := a.provider.PresentTXT(ctx, zoneID, zone, info.EffectiveFQDN, info.Value)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.records == nil {
		a.records = map[string]acmeTXTRecord{}
	}
	a.records[info.EffectiveFQDN+"\x00"+info.Value] = acmeTXTRecord{
		zoneID:   zoneID,
		zone:     zone,
		recordID: recordID,
	}
	a.mu.Unlock()
	return nil
}

func (a *dns01ChallengeAdapter) CleanUp(domain, token, keyAuth string) error {
	domain = strings.TrimPrefix(strings.TrimSpace(domain), "*.")
	info := dns01.GetChallengeInfo(domain, keyAuth)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	key := info.EffectiveFQDN + "\x00" + info.Value
	a.mu.Lock()
	rec, ok := a.records[key]
	if ok {
		delete(a.records, key)
	}
	a.mu.Unlock()
	if !ok {
		zone, zoneID, err := a.resolveZone(ctx, info.EffectiveFQDN)
		if err != nil {
			return err
		}
		rec = acmeTXTRecord{zoneID: zoneID, zone: zone}
	}
	return a.provider.CleanUpTXT(ctx, rec.zoneID, rec.zone, info.EffectiveFQDN, rec.recordID, info.Value)
}

func (c Config) DNSGroup(id string) (DDNSGroup, bool) {
	for _, g := range c.DDNS.Groups {
		if g.ID == id {
			return g, true
		}
	}
	return DDNSGroup{}, false
}

func (c Config) CertificateDNSGroup(host string) (DDNSGroup, error) {
	if id := c.ACME.DNSGroups[host]; id != "" {
		g, ok := c.DNSGroup(id)
		if !ok || !inZone(host, g.Zone) {
			return DDNSGroup{}, errors.New("证书选用的 DNS 组不存在或根域名不匹配")
		}
		return g, nil
	}
	var selected DDNSGroup
	count := 0
	for _, g := range c.DDNS.Groups {
		if !inZone(host, g.Zone) {
			continue
		}
		if len(g.Zone) > len(selected.Zone) {
			selected = g
			count = 1
		} else if len(g.Zone) == len(selected.Zone) {
			count++
		}
	}
	if count == 0 {
		return DDNSGroup{}, errors.New("请为此证书域名创建对应的 DNS 组")
	}
	if count > 1 {
		return DDNSGroup{}, errors.New("此证书域名匹配多个 DNS 组，请在证书页面指定使用的组")
	}
	return selected, nil
}

func validateCredentials(s State) error {
	if err := s.WxPusher.validate(); err != nil {
		return err
	}
	if err := s.Alerts.validate(); err != nil {
		return err
	}
	if !validStoredMCPToken(s.MCPToken, s.MCPTokenHash) {
		return errors.New("MCP 令牌原值与摘要不一致")
	}
	for _, client := range s.MCPClients {
		if !validStoredMCPToken(client.Token, client.Hash) {
			return errors.New("MCP 客户端令牌原值与摘要不一致")
		}
	}
	if err := validateWebDAVBackupState(s.WebDAVBackup); err != nil {
		return err
	}
	if s.MCPTokenHash != "" {
		decoded, err := hex.DecodeString(s.MCPTokenHash)
		if err != nil || len(decoded) != 32 {
			return errors.New("MCP 令牌摘要格式无效")
		}
	}
	if s.MCP.Enabled && (s.MCPTokenHash == "" || !s.MCP.hasPermissions()) {
		return errors.New("MCP 授权配置无效")
	}
	if err := validateGitHubToken(s.GitHubToken); err != nil {
		return err
	}
	if err := validateRouteCredentials(s); err != nil {
		return err
	}
	for _, credential := range s.DNSCredentials {
		if !validateDNSCredentialValue(credential.KeyID) || !validateDNSCredentialValue(credential.Token) {
			return errors.New("DNS 凭据格式无效")
		}
	}
	for _, credential := range s.CertificateCredentials {
		if !validateDNSCredentialValue(credential.KeyID) || !validateDNSCredentialValue(credential.Token) {
			return errors.New("证书 DNS 凭据格式无效")
		}
	}
	for _, g := range s.Config.DDNS.Groups {
		if g.Enabled && !s.DNSCredentials[g.ID].ConfiguredFor(g.Provider) {
			return errors.New("请在启用的 DDNS 组中填写完整的 DNS 凭据")
		}
	}
	if s.Config.ACME.Enabled {
		for _, request := range s.Config.ACME.Requests {
			if request.Enabled && !s.CertificateCredentials[request.ID].ConfiguredFor(request.Provider) {
				return errors.New("启用的证书任务尚未配置完整的独立 DNS 凭据")
			}
		}
	}
	if s.Assistant.Base != "" || s.Assistant.Model != "" || s.Assistant.Name != "" || s.Assistant.Key != "" || len(s.Assistant.Models) > 0 {
		if _, err := (assistantSettings{assistantEndpoint: s.Assistant}).endpoint(s.Assistant); err != nil {
			return err
		}
	}
	if len(s.AssistantProviders) > 19 {
		return errors.New("模型接入点数量超出上限")
	}
	seenProviders := map[string]bool{}
	for _, provider := range s.AssistantProviders {
		if provider.ID == "" || provider.ID == "new" || provider.ID == "default" || seenProviders[provider.ID] {
			return errors.New("模型接入点 ID 无效或重复")
		}
		seenProviders[provider.ID] = true
		if _, err := (assistantSettings{assistantEndpoint: provider}).endpoint(provider); err != nil {
			return err
		}
	}
	return nil
}
