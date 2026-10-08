package gateway

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	legolog "github.com/go-acme/lego/v4/log"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/registration"
)

func init() { legolog.Logger = log.New(io.Discard, "", 0) }

type acmeAccount struct {
	Email        string                 `json:"email"`
	Registration *registration.Resource `json:"registration"`
	Key          []byte                 `json:"key"`
	privateKey   crypto.PrivateKey
}

func (a *acmeAccount) GetEmail() string                        { return a.Email }
func (a *acmeAccount) GetRegistration() *registration.Resource { return a.Registration }
func (a *acmeAccount) GetPrivateKey() crypto.PrivateKey        { return a.privateKey }

type savedCertificate struct {
	Certificate []byte `json:"certificate"`
	PrivateKey  []byte `json:"private_key"`
}

type CertificateStatus struct {
	ID       string    `json:"id"`
	Domains  []string  `json:"domains"`
	Host     string    `json:"host"`
	NotAfter time.Time `json:"not_after"`
	Staging  bool      `json:"staging"`
	Ready    bool      `json:"ready"`
}

type Certificates struct {
	logs     *Logs
	mu       sync.RWMutex
	dir      string
	store    *Store
	certs    map[string]*tls.Certificate
	requests map[string]*tls.Certificate
	obtain   func(State, CertificateRequest) (savedCertificate, error)
	staging  bool
}

func NewCertificates(dir string, store *Store) (*Certificates, error) {
	dir = filepath.Join(dir, "certificates")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	m := &Certificates{dir: dir, store: store}
	m.Reload()
	return m, nil
}

func environment(staging bool) string {
	if staging {
		return "staging"
	}
	return "production"
}

func (m *Certificates) path(host string, staging bool) string {
	return filepath.Join(m.dir, environment(staging)+"-"+host+".json")
}

func decodeCertificate(saved savedCertificate, host string) (*tls.Certificate, error) {
	pair, err := tls.X509KeyPair(saved.Certificate, saved.PrivateKey)
	if err != nil {
		return nil, errors.New("证书或私钥无效")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, errors.New("证书解析失败")
	}
	if !certificateCovers(leaf, host) {
		return nil, errors.New("证书域名不匹配")
	}
	pair.Leaf = leaf
	return &pair, nil
}

func certificateCovers(leaf *x509.Certificate, domain string) bool {
	if strings.HasPrefix(domain, "*.") {
		for _, name := range leaf.DNSNames {
			if strings.EqualFold(name, domain) {
				return true
			}
		}
		return false
	}
	return leaf.VerifyHostname(domain) == nil
}

func decodeCertificateRequest(saved savedCertificate, request CertificateRequest) (*tls.Certificate, error) {
	if len(request.Domains) == 0 {
		return nil, errors.New("证书任务缺少域名")
	}
	pair, err := decodeCertificate(saved, request.Domains[0])
	if err != nil {
		return nil, err
	}
	for _, domain := range request.Domains {
		if !certificateCovers(pair.Leaf, domain) {
			return nil, errors.New("证书未覆盖申请的全部域名")
		}
	}
	return pair, nil
}

func (m *Certificates) readRequest(request CertificateRequest, staging bool) savedCertificate {
	data, err := os.ReadFile(m.path("request-"+request.ID, staging))
	if errors.Is(err, os.ErrNotExist) && len(request.Domains) == 1 {
		data, _ = os.ReadFile(m.path(request.Domains[0], staging))
	}
	var saved savedCertificate
	_ = json.Unmarshal(data, &saved)
	return saved
}

func (m *Certificates) Reload() {
	// Serialize config snapshot and publication, so an older reload cannot win.
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.store.Snapshot().Config
	certs := map[string]*tls.Certificate{}
	requests := map[string]*tls.Certificate{}
	for _, request := range c.ACME.Requests {
		pair, err := decodeCertificateRequest(m.readRequest(request, c.ACME.Staging), request)
		if err != nil {
			continue
		}
		requests[request.ID] = pair
		for _, domain := range request.Domains {
			certs[domain] = pair
		}
	}
	m.certs, m.requests, m.staging = certs, requests, c.ACME.Staging
}

func (m *Certificates) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	host := strings.ToLower(strings.TrimSuffix(hello.ServerName, "."))
	names := []string{host}
	if _, suffix, ok := strings.Cut(host, "."); ok {
		names = append(names, "*."+suffix)
	}
	for _, name := range names {
		pair := m.certs[name]
		if pair != nil && !time.Now().Before(pair.Leaf.NotBefore) && time.Now().Before(pair.Leaf.NotAfter) && certificateCovers(pair.Leaf, host) {
			return pair, nil
		}
	}
	return nil, errors.New("此域名尚无有效证书")
}

func (m *Certificates) ForGroup(groupID string) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		host := strings.ToLower(strings.TrimSuffix(hello.ServerName, "."))
		c := m.store.Snapshot().Config
		for _, r := range c.Routes {
			if r.GroupID == groupID && r.Host == host && r.TLS && c.RouteActive(r) {
				return m.GetCertificate(hello)
			}
		}
		return nil, errors.New("此域名未配置在当前反代组")
	}
}

func renewalDue(pair *tls.Certificate, now time.Time) bool {
	if pair == nil {
		return true
	}
	lifetime := pair.Leaf.NotAfter.Sub(pair.Leaf.NotBefore)
	window := lifetime / 3
	if window > 30*24*time.Hour {
		window = 30 * 24 * time.Hour
	}
	return !now.Add(window).Before(pair.Leaf.NotAfter)
}

func (m *Certificates) Due(id string, staging bool) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pair := m.requests[id]
	request, exists := m.store.Snapshot().Config.CertificateRequest(id)
	return !exists || staging != m.staging || renewalDue(pair, time.Now()) || (pair != nil && !sameCertificateDomains(pair.Leaf.DNSNames, request.Domains))
}

func (m *Certificates) Status() []CertificateStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := []CertificateStatus{}
	for _, request := range m.store.Snapshot().Config.ACME.Requests {
		s := CertificateStatus{ID: request.ID, Domains: request.Domains, Host: request.Domains[0], Staging: m.staging}
		if p := m.requests[request.ID]; p != nil {
			s.NotAfter = p.Leaf.NotAfter
			s.Ready = time.Now().After(p.Leaf.NotBefore) && time.Now().Before(p.Leaf.NotAfter)
		}
		result = append(result, s)
	}
	return result
}

func (m *Certificates) Issue(state State, request CertificateRequest) error {
	obtain := m.obtain
	if obtain == nil {
		obtain = m.obtainACME
	}
	saved, err := obtain(state, request)
	if err != nil {
		return err
	}
	if _, err := decodeCertificateRequest(saved, request); err != nil {
		return err
	}
	current := m.store.Snapshot()
	active, exists := current.Config.CertificateRequest(request.ID)
	if !exists || !current.Config.ACME.Enabled || !active.Enabled || current.Config.ACME.Staging != state.Config.ACME.Staging || active.Provider != request.Provider || !slices.Equal(active.Domains, request.Domains) {
		return errors.New("证书任务已修改或停用，本次结果未应用")
	}
	if err := writeJSON(m.path("request-"+request.ID, state.Config.ACME.Staging), saved); err != nil {
		return errors.New("证书保存失败，现有证书保持不变")
	}
	m.Reload()
	return nil
}

func (m *Certificates) obtainACME(state State, request CertificateRequest) (savedCertificate, error) {
	credential := state.CertificateCredentials[request.ID]
	if request.Provider != "cloudflare" || credential.Token == "" {
		return savedCertificate{}, errors.New("证书 DNS 服务商不支持或此任务未配置独立 API Token")
	}
	a, accountPath, err := m.loadAccount(state.Config.ACME)
	if err != nil {
		return savedCertificate{}, errors.New("ACME 账户文件创建或读取失败")
	}
	cfg := lego.NewConfig(a)
	if state.Config.ACME.Staging {
		cfg.CADirURL = lego.LEDirectoryStaging
	}
	cfg.Certificate.KeyType = certcrypto.EC256
	cfg.Certificate.Timeout = 2 * time.Minute
	cfg.HTTPClient = instrumentClient(outboundClient(state, 2*time.Minute), m.logs, "certificates", "ACME HTTP API")
	defer closeClient(cfg.HTTPClient)
	client, err := lego.NewClient(cfg)
	if err != nil {
		return savedCertificate{}, errors.New("无法连接 Let's Encrypt，请检查网络和系统时间")
	}
	dnsHTTP := instrumentClient(outboundClient(state, 20*time.Second), m.logs, "certificates", "Cloudflare DNS 验证 API")
	defer closeClient(dnsHTTP)
	dnsCfg := &cloudflare.Config{AuthToken: credential.Token, TTL: 120, PropagationTimeout: 3 * time.Minute, PollingInterval: 5 * time.Second, HTTPClient: dnsHTTP}
	provider, err := cloudflare.NewDNSProviderConfig(dnsCfg)
	if err != nil {
		return savedCertificate{}, errors.New("Cloudflare DNS 验证配置失败")
	}
	if err := client.Challenge.SetDNS01Provider(provider); err != nil {
		return savedCertificate{}, errors.New("DNS-01 验证初始化失败")
	}
	if a.Registration == nil {
		a.Registration, err = client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: state.Config.ACME.AcceptTerms})
		if err != nil {
			return savedCertificate{}, errors.New("Let's Encrypt 账户注册失败，请检查邮箱、网络及服务条款")
		}
		if err := writeJSON(accountPath, a); err != nil {
			return savedCertificate{}, errors.New("ACME 账户保存失败")
		}
	}
	var resource *certificate.Resource
	previous := m.readRequest(request, state.Config.ACME.Staging)
	pair, checkErr := decodeCertificateRequest(previous, request)
	if checkErr == nil && sameCertificateDomains(pair.Leaf.DNSNames, request.Domains) {
		resource, err = client.Certificate.RenewWithOptions(certificate.Resource{Domain: request.Domains[0], Certificate: previous.Certificate}, &certificate.RenewOptions{Bundle: true})
	} else {
		resource, err = client.Certificate.Obtain(certificate.ObtainRequest{Domains: request.Domains, Bundle: true})
	}
	if err != nil {
		return savedCertificate{}, errors.New("证书签发失败，请检查独立 Token 的 Zone:Read / DNS:Edit 权限、权威 DNS 可达性及 CA 限流；5 分钟后可手动重试")
	}
	return savedCertificate{Certificate: resource.Certificate, PrivateKey: resource.PrivateKey}, nil
}

func sameCertificateDomains(actual, expected []string) bool {
	a, b := slices.Clone(actual), slices.Clone(expected)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func (m *Certificates) loadAccount(c ACMEConfig) (*acmeAccount, string, error) {
	hash := sha256.Sum256([]byte(c.Email))
	path := filepath.Join(m.dir, "account-"+environment(c.Staging)+"-"+hex.EncodeToString(hash[:8])+".json")
	a := &acmeAccount{Email: c.Email}
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, a); err != nil {
			return nil, path, err
		}
		block, _ := pem.Decode(a.Key)
		if block == nil {
			return nil, path, errors.New("账户密钥格式错误")
		}
		a.privateKey, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		return a, path, err
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, path, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, path, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, path, err
	}
	a.privateKey, a.Key = key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	return a, path, writeJSON(path, a)
}

func (m *Certificates) SetLogs(logs *Logs) { m.logs = logs }
