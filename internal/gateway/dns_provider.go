package gateway

import (
	"context"
	"errors"
	"net/netip"
	"time"
)

type DNSCredential struct {
	Token string `json:"token"`
}

func (s State) HasDNSToken() bool {
	if s.CloudflareToken != "" {
		return true
	}
	for _, c := range s.DNSCredentials {
		if c.Token != "" {
			return true
		}
	}
	for _, c := range s.CertificateCredentials {
		if c.Token != "" {
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

type cloudflareProvider struct{ *Cloudflare }

func (p cloudflareProvider) Close() { closeClient(p.Client) }

func newDNSProvider(s State, g DDNSGroup, logs *Logs) (dnsProvider, error) {
	switch g.Provider {
	case "cloudflare":
		token := s.DNSCredentials[g.ID].Token
		if token == "" {
			return nil, errors.New("此 DNS 组尚未配置 Cloudflare API Token")
		}
		cf := NewCloudflare(token)
		cf.Client = outboundClient(s, 20*time.Second)
		cf.logs = logs
		return cloudflareProvider{cf}, nil
	default:
		return nil, errors.New("此 DNS 服务商尚未支持")
	}
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
	if err := validateRouteCredentials(s); err != nil {
		return err
	}
	for _, credential := range s.DNSCredentials {
		if len(credential.Token) > 512 || containsCredentialNewline(credential.Token) {
			return errors.New("DNS 凭据格式无效")
		}
	}
	for _, credential := range s.CertificateCredentials {
		if len(credential.Token) > 512 || containsCredentialNewline(credential.Token) {
			return errors.New("证书 DNS 凭据格式无效")
		}
	}
	for _, g := range s.Config.DDNS.Groups {
		if g.Enabled && s.DNSCredentials[g.ID].Token == "" {
			return errors.New("请在启用的 DDNS 组中填写 API Token")
		}
	}
	if s.Config.ACME.Enabled {
		for _, request := range s.Config.ACME.Requests {
			if request.Enabled && s.CertificateCredentials[request.ID].Token == "" {
				return errors.New("启用的证书任务尚未配置独立 API Token")
			}
		}
	}
	return nil
}
