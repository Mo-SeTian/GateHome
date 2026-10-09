package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Access struct {
	Mode          string   `json:"mode"`
	CIDRs         []string `json:"cidrs"`
	Subscriptions []string `json:"subscriptions"`
}

type RouteAuthConfig struct {
	Enabled       bool   `json:"enabled"`
	Username      string `json:"username"`
	FailureLimit  int    `json:"failure_limit,omitempty"`
	FreezeSeconds int    `json:"freeze_seconds,omitempty"`
}

type Route struct {
	GroupID    string          `json:"group_id"`
	Name       string          `json:"name"`
	Host       string          `json:"host"`
	Upstream   string          `json:"upstream"`
	Image      string          `json:"image,omitempty"`
	Enabled    bool            `json:"enabled"`
	TLS        bool            `json:"tls"`
	FirewallID string          `json:"firewall_id"`
	Auth       RouteAuthConfig `json:"auth"`
	Access     *Access         `json:"access,omitempty"` // Original on-disk access policy, migrated on load.
}

type FirewallGroup struct {
	Name          string   `json:"name"`
	Match         string   `json:"match"`
	Action        string   `json:"action"`
	CIDRs         []string `json:"cidrs"`
	Subscriptions []string `json:"subscriptions"`
}

type Firewall struct {
	Protection    Protection      `json:"protection"`
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	DefaultAction string          `json:"default_action"`
	Groups        []FirewallGroup `json:"groups"`
}

type ProxyGroup struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	HTTPPort     int    `json:"http_port"`
	HTTPSPort    int    `json:"https_port"`
	DomainSuffix string `json:"domain_suffix,omitempty"`
	DDNSGroupID  string `json:"ddns_group_id,omitempty"`
}

type Subscription struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Enabled  bool   `json:"enabled"`
	Interval int    `json:"interval"`
}

type DNSRecord struct {
	Host string `json:"host"`
	Type string `json:"type"`
}

type DDNSConfig struct {
	Groups   []DDNSGroup `json:"groups"`
	Enabled  bool        `json:"enabled,omitempty"` // Legacy fields, migrated on load.
	Interval int         `json:"interval,omitempty"`
	IPv4URL  string      `json:"ipv4_url,omitempty"`
	IPv6URL  string      `json:"ipv6_url,omitempty"`
	Records  []DNSRecord `json:"records,omitempty"`
}

type DDNSGroup struct {
	Provider   string   `json:"provider"`
	Zone       string   `json:"zone"`
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Enabled    bool     `json:"enabled"`
	Interval   int      `json:"interval"`
	Mode       string   `json:"mode"`
	Hosts      []string `json:"hosts"`
	IPv4URL    string   `json:"ipv4_url,omitempty"` // Legacy single endpoints.
	IPv6URL    string   `json:"ipv6_url,omitempty"`
	IPv4URLs   []string `json:"ipv4_urls,omitempty"`
	IPv6URLs   []string `json:"ipv6_urls,omitempty"`
	Interface  string   `json:"interface,omitempty"`
	IPv4Source string   `json:"ipv4_source,omitempty"`
	IPv6Source string   `json:"ipv6_source,omitempty"`
}

func (g DDNSGroup) Records() []DNSRecord {
	records := []DNSRecord{}
	for _, host := range g.Hosts {
		if g.Mode == "ipv4" || g.Mode == "dual" {
			records = append(records, DNSRecord{Host: host, Type: "A"})
		}
		if g.Mode == "ipv6" || g.Mode == "dual" {
			records = append(records, DNSRecord{Host: host, Type: "AAAA"})
		}
	}
	return records
}

type OutboundProxyConfig struct {
	Enabled  bool   `json:"enabled"`
	URL      string `json:"url"`
	Username string `json:"username"`
}

type ACMEConfig struct {
	DNSGroups   map[string]string    `json:"dns_groups"`
	Requests    []CertificateRequest `json:"requests"`
	Enabled     bool                 `json:"enabled"`
	Email       string               `json:"email"`
	Staging     bool                 `json:"staging"`
	AcceptTerms bool                 `json:"accept_terms"`
}

type CertificateRequest struct {
	ID       string   `json:"id"`
	Provider string   `json:"provider"`
	Domains  []string `json:"domains"`
	Enabled  bool     `json:"enabled"`
}

func (c Config) CertificateRequest(id string) (CertificateRequest, bool) {
	for _, request := range c.ACME.Requests {
		if request.ID == id {
			return request, true
		}
	}
	return CertificateRequest{}, false
}

type Config struct {
	Homepage HomepageConfig `json:"homepage"`
	// Read-only compatibility fields for migrating the original on-disk format.
	HTTPPort      int                 `json:"http_port,omitempty"`
	HTTPSPort     int                 `json:"https_port,omitempty"`
	Groups        []ProxyGroup        `json:"groups"`
	Subscriptions []Subscription      `json:"subscriptions"`
	Firewalls     []Firewall          `json:"firewalls"`
	Zone          string              `json:"zone,omitempty"` // Legacy DNS account zone.
	Routes        []Route             `json:"routes"`
	DDNS          DDNSConfig          `json:"ddns"`
	ACME          ACMEConfig          `json:"acme"`
	OutboundProxy OutboundProxyConfig `json:"outbound_proxy"`
	AdminAccess   AdminAccessConfig   `json:"admin_access"`
	LogRetention  LogRetentionConfig  `json:"log_retention"`
	Dashboard     DashboardConfig     `json:"dashboard"`
}

type State struct {
	AdminUsername          string                   `json:"admin_username"`
	Config                 Config                   `json:"config"`
	PasswordHash           string                   `json:"password_hash"`
	CloudflareToken        string                   `json:"cloudflare_token"`
	Revision               int                      `json:"revision"`
	ProxyPassword          string                   `json:"proxy_password"`
	DNSCredentials         map[string]DNSCredential `json:"dns_credentials"`
	CertificateCredentials map[string]DNSCredential `json:"certificate_credentials"`
	RoutePasswordHashes    map[string]string        `json:"route_password_hashes,omitempty"`
	IPBlocks               map[string]IPBlock       `json:"ip_blocks,omitempty"`
}

type Store struct {
	mu    sync.RWMutex
	path  string
	paths StoragePaths
	state State
}

func DefaultConfig() Config {
	return Config{Homepage: defaultHomepage(), Groups: []ProxyGroup{{ID: "default", Name: "默认组", Enabled: true, HTTPPort: 18080, HTTPSPort: 18443}}, Subscriptions: []Subscription{}, Firewalls: []Firewall{}, Routes: []Route{},
		DDNS: DDNSConfig{Groups: []DDNSGroup{}}, LogRetention: defaultLogRetention,
		ACME: ACMEConfig{Staging: true, DNSGroups: map[string]string{}, Requests: []CertificateRequest{}}}
}

func OpenStore(dir string) (*Store, error) {
	return OpenStorePaths(legacyStorage(dir))
}

func OpenStorePaths(paths StoragePaths) (*Store, error) {
	dir := paths.Config
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	s := &Store{path: paths.file("state.json"), paths: paths}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.state.Config = DefaultConfig()
		s.state.AdminUsername = defaultAdminUsername
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.state); err != nil {
		return nil, errors.New("配置文件无法解析")
	}
	migrated := migrateState(&s.state)
	if err := ValidateAdminUsername(s.state.AdminUsername); err != nil {
		return nil, err
	}
	if err := Validate(s.state.Config); err != nil {
		return nil, err
	}
	if err := validateCredentials(s.state); err != nil {
		return nil, err
	}
	if migrated {
		s.state.Revision++
		if err := writeJSON(s.path, s.state); err != nil {
			return nil, errors.New("旧配置迁移保存失败")
		}
	}
	return s, nil
}

func containsCredentialNewline(value string) bool { return strings.ContainsAny(value, "\r\n") }

func migrateState(s *State) bool {
	changed := migrateConfig(&s.Config)
	if s.AdminUsername == "" {
		s.AdminUsername = defaultAdminUsername
		changed = true
	}
	if s.DNSCredentials == nil {
		s.DNSCredentials = map[string]DNSCredential{}
		changed = true
	}
	if s.Config.ACME.DNSGroups == nil {
		s.Config.ACME.DNSGroups = map[string]string{}
		changed = true
	}
	oldZone := s.Config.Zone
	legacyGroupID := ""
	for i := range s.Config.DDNS.Groups {
		g := &s.Config.DDNS.Groups[i]
		if g.Provider == "" {
			g.Provider = "cloudflare"
			g.Zone = oldZone
			if legacyGroupID == "" {
				legacyGroupID = g.ID
			}
			if s.CloudflareToken != "" {
				s.DNSCredentials[g.ID] = DNSCredential{Token: s.CloudflareToken}
			}
			changed = true
		}
	}
	if oldZone != "" {
		found := false
		for _, g := range s.Config.DDNS.Groups {
			if g.Zone == oldZone {
				found = true
			}
		}
		if !found && (s.CloudflareToken != "" || s.Config.ACME.Enabled) {
			id := "migrated-account"
			for {
				if _, exists := s.Config.DNSGroup(id); !exists {
					break
				}
				id += "-old"
			}
			s.Config.DDNS.Groups = append(s.Config.DDNS.Groups, DDNSGroup{ID: id, Name: "原 DNS 账户", Provider: "cloudflare", Zone: oldZone, Mode: "dual", Hosts: []string{oldZone}, Interval: 300, IPv4URL: "https://api4.ipify.org", IPv6URL: "https://api6.ipify.org"})
			legacyGroupID = id
			if s.CloudflareToken != "" {
				s.DNSCredentials[id] = DNSCredential{Token: s.CloudflareToken}
			}
		}
		// Existing certificates used the global account, including when several groups shared it.
		if legacyGroupID != "" {
			for _, r := range s.Config.Routes {
				if r.TLS && inZone(r.Host, oldZone) && s.Config.ACME.DNSGroups[r.Host] == "" {
					s.Config.ACME.DNSGroups[r.Host] = legacyGroupID
				}
			}
		}
		s.Config.Zone = ""
		s.CloudflareToken = ""
		changed = true
	}
	if s.CertificateCredentials == nil {
		s.CertificateCredentials = map[string]DNSCredential{}
		changed = true
	}
	if s.Config.ACME.Requests == nil {
		s.Config.ACME.Requests = []CertificateRequest{}
		hosts := map[string]bool{}
		for _, route := range s.Config.Routes {
			if route.TLS {
				hosts[route.Host] = hosts[route.Host] || s.Config.RouteActive(route)
			}
		}
		names := make([]string, 0, len(hosts))
		for host := range hosts {
			names = append(names, host)
		}
		sort.Strings(names)
		for i, host := range names {
			request := CertificateRequest{ID: fmt.Sprintf("migrated-certificate-%d", i), Provider: "cloudflare", Domains: []string{host}, Enabled: hosts[host]}
			if group, err := s.Config.CertificateDNSGroup(host); err == nil {
				request.Provider = group.Provider
				s.CertificateCredentials[request.ID] = s.DNSCredentials[group.ID]
			}
			s.Config.ACME.Requests = append(s.Config.ACME.Requests, request)
		}
		changed = true
	}
	return changed
}

func migrateConfig(c *Config) bool {
	migrated := false
	if c.Homepage.Port == 0 && !c.Homepage.Enabled && len(c.Homepage.Groups) == 0 && c.Homepage.CustomCSS == "" && c.Homepage.Background == "" && c.Homepage.SearchEngines == nil {
		c.Homepage = defaultHomepage()
		migrated = true
	}
	if c.Homepage.SearchEngines == nil {
		c.Homepage.SearchEngines = defaultHomepage().SearchEngines
		migrated = true
	}
	if c.LogRetention == (LogRetentionConfig{}) {
		c.LogRetention = defaultLogRetention
		migrated = true
	}
	if c.DDNS.Groups == nil {
		old := c.DDNS
		c.DDNS = DDNSConfig{Groups: []DDNSGroup{}}
		families := map[string]string{}
		order := []string{}
		for _, r := range old.Records {
			if _, ok := families[r.Host]; !ok {
				order = append(order, r.Host)
			}
			mode := "ipv4"
			if r.Type == "AAAA" {
				mode = "ipv6"
			} else if r.Type != "A" {
				c.DDNS = old
				return false
			}
			if previous, ok := families[r.Host]; ok && previous != mode {
				mode = "dual"
			}
			families[r.Host] = mode
		}
		for _, mode := range []string{"ipv4", "ipv6", "dual"} {
			g := DDNSGroup{ID: "migrated-" + mode, Name: "原 DDNS · " + mode, Enabled: old.Enabled, Interval: old.Interval, Mode: mode, Hosts: []string{}, IPv4URL: old.IPv4URL, IPv6URL: old.IPv6URL}
			if g.Interval == 0 {
				g.Interval = 300
			}
			if g.IPv4URL == "" {
				g.IPv4URL = "https://api4.ipify.org"
			}
			if g.IPv6URL == "" {
				g.IPv6URL = "https://api6.ipify.org"
			}
			for _, host := range order {
				if families[host] == mode {
					g.Hosts = append(g.Hosts, host)
				}
			}
			if len(g.Hosts) > 0 {
				c.DDNS.Groups = append(c.DDNS.Groups, g)
			}
		}
		migrated = true
	}
	if c.Groups == nil {
		httpPort, httpsPort := c.HTTPPort, c.HTTPSPort
		if httpPort == 0 {
			httpPort = 18080
		}
		if httpsPort == 0 {
			httpsPort = 18443
		}
		c.Groups = []ProxyGroup{{ID: "default", Name: "默认组", Enabled: true, HTTPPort: httpPort, HTTPSPort: httpsPort}}
		for i := range c.Routes {
			c.Routes[i].GroupID = "default"
		}
		c.HTTPPort, c.HTTPSPort = 0, 0
		migrated = true
	}
	if c.Subscriptions == nil {
		c.Subscriptions = []Subscription{}
		migrated = true
	}
	if c.Firewalls == nil {
		c.Firewalls = []Firewall{}
		migrated = true
	}
	for i := range c.Routes {
		r := &c.Routes[i]
		if r.Access == nil || r.FirewallID != "" {
			continue
		}
		a := r.Access
		if validateAccess(*a) != nil {
			continue // Validation rejects an invalid legacy policy instead of weakening it.
		}
		if a.Mode != "public" {
			id := fmt.Sprintf("migrated-%d", i+1)
			for {
				if _, exists := c.Firewall(id); !exists {
					break
				}
				id += "-old"
			}
			f := Firewall{ID: id, Name: r.Name, DefaultAction: "allow", Groups: []FirewallGroup{}}
			if f.Name == "" {
				f.Name = fmt.Sprintf("迁移防火墙 %d", i+1)
			}
			if a.Mode == "allow" {
				f.DefaultAction = "deny"
			}
			if len(a.CIDRs)+len(a.Subscriptions) > 0 {
				f.Groups = append(f.Groups, FirewallGroup{Name: "原 IP 列表", Match: "include", Action: a.Mode, CIDRs: a.CIDRs, Subscriptions: a.Subscriptions})
			}
			c.Firewalls = append(c.Firewalls, f)
			r.FirewallID = id
		}
		r.Access = nil
		migrated = true
	}
	return migrated
}

func (c Config) Firewall(id string) (Firewall, bool) {
	for _, f := range c.Firewalls {
		if f.ID == id {
			return f, true
		}
	}
	return Firewall{}, false
}

func (c Config) Group(id string) (ProxyGroup, bool) {
	for _, g := range c.Groups {
		if g.ID == id {
			return g, true
		}
	}
	return ProxyGroup{}, false
}

func (c Config) DDNSEnabled() bool {
	for _, g := range c.DDNS.Groups {
		if g.Enabled {
			return true
		}
	}
	return false
}

func (c Config) RouteActive(r Route) bool {
	g, ok := c.Group(r.GroupID)
	return ok && g.Enabled && r.Enabled
}

func (c Config) TLSHosts() []string {
	seen := map[string]bool{}
	for _, r := range c.Routes {
		if c.RouteActive(r) && r.TLS {
			seen[r.Host] = true
		}
	}
	hosts := make([]string, 0, len(seen))
	for host := range seen {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts
}

func listenerSignature(c Config) string {
	parts := []string{}
	if c.Homepage.Enabled {
		parts = append(parts, fmt.Sprintf("homepage:%d", c.Homepage.Port))
	}
	for _, g := range c.Groups {
		if g.Enabled {
			parts = append(parts, fmt.Sprintf("%s:%d:%d", g.ID, g.HTTPPort, g.HTTPSPort))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "/")
}

func validateAdminPort(c Config, port int) error {
	if c.Homepage.Enabled && c.Homepage.Port == port {
		return errors.New("首页端口不能与管理端口相同")
	}
	for _, g := range c.Groups {
		if g.HTTPPort == port || g.HTTPSPort == port {
			return errors.New("业务端口不能与管理端口相同")
		}
	}
	return nil
}

// Snapshots are immutable after publication; callers must not modify slices.
func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *Store) Update(c Config, token *string, revision int) error {
	return s.UpdateWithProxy(c, token, nil, revision)
}

func (s *Store) UpdateWithProxy(c Config, token, proxyPassword *string, revision int) error {
	return s.UpdateDNS(c, nil, token, proxyPassword, revision)
}

func (s *Store) UpdateDNS(c Config, tokens map[string]*string, legacyToken, proxyPassword *string, revision int) error {
	return s.UpdateCredentials(c, tokens, nil, legacyToken, proxyPassword, revision)
}

func (s *Store) UpdateCredentials(c Config, tokens, certificateTokens map[string]*string, legacyToken, proxyPassword *string, revision int) error {
	return s.UpdateRouteCredentials(c, tokens, certificateTokens, legacyToken, proxyPassword, nil, revision)
}

func (s *Store) UpdateRouteCredentials(c Config, tokens, certificateTokens map[string]*string, legacyToken, proxyPassword *string, routePasswords map[string]*string, revision int) error {
	c = includeProxyDNSHosts(c)
	if err := Validate(c); err != nil {
		return err
	}
	for _, route := range c.Routes {
		if route.Image != "" {
			if _, err := readRouteImage(s.paths.Data, route.Image); err != nil {
				return errors.New("反代图片不存在或已失效，请重新选择图片")
			}
		}
	}
	for id := range homepageImages(c.Homepage) {
		if _, err := readRouteImage(s.paths.Data, id); err != nil {
			return errors.New("首页链接图片不存在，请重新选择")
		}
	}
	if c.Homepage.Background != "" {
		if _, err := readHomepageBackground(s.paths.Data, c.Homepage.Background); err != nil {
			return errors.New("首页背景不存在，请重新选择")
		}
	}
	hashes, err := hashRoutePasswords(c, routePasswords)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if revision != s.state.Revision {
		return errors.New("配置已被修改，请刷新页面后重试")
	}
	next := s.state
	next.Config = c
	next.RoutePasswordHashes = map[string]string{}
	for _, route := range c.Routes {
		key := routeKey(route)
		for _, old := range s.state.Config.Routes {
			if routeKey(old) == key && old.Auth.Username == route.Auth.Username {
				next.RoutePasswordHashes[key] = s.state.RoutePasswordHashes[key]
				break
			}
		}
		if hash, changed := hashes[key]; changed {
			next.RoutePasswordHashes[key] = hash
		}
	}
	if legacyToken != nil {
		next.CloudflareToken = strings.TrimSpace(*legacyToken)
	}
	next.DNSCredentials = map[string]DNSCredential{}
	for _, g := range c.DDNS.Groups {
		old, exists := s.state.Config.DNSGroup(g.ID)
		if exists && old.Provider == g.Provider {
			next.DNSCredentials[g.ID] = s.state.DNSCredentials[g.ID]
		}
		if value, ok := tokens[g.ID]; ok && value != nil {
			next.DNSCredentials[g.ID] = DNSCredential{Token: strings.TrimSpace(*value)}
		}
	}
	for id := range tokens {
		if _, exists := c.DNSGroup(id); !exists {
			return errors.New("凭据引用了不存在的 DNS 组")
		}
	}
	next.CertificateCredentials = map[string]DNSCredential{}
	for _, request := range c.ACME.Requests {
		if old, exists := s.state.Config.CertificateRequest(request.ID); exists && old.Provider == request.Provider {
			next.CertificateCredentials[request.ID] = s.state.CertificateCredentials[request.ID]
		}
		if value, ok := certificateTokens[request.ID]; ok && value != nil {
			next.CertificateCredentials[request.ID] = DNSCredential{Token: strings.TrimSpace(*value)}
		}
	}
	for id := range certificateTokens {
		if _, exists := c.CertificateRequest(id); !exists {
			return errors.New("凭据引用了不存在的证书任务")
		}
	}
	if proxyPassword != nil {
		next.ProxyPassword = *proxyPassword
	}
	if next.Config.OutboundProxy.URL != s.state.Config.OutboundProxy.URL || next.Config.OutboundProxy.Username != s.state.Config.OutboundProxy.Username {
		if proxyPassword == nil {
			next.ProxyPassword = ""
		}
	}
	if err := validateCredentials(next); err != nil {
		return err
	}
	next.Revision++
	if err := writeJSON(s.path, next); err != nil {
		return errors.New("配置写入失败，请检查数据目录权限")
	}
	s.state = next
	return nil
}

func (s *Store) SetPassword(hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state
	next.PasswordHash = hash
	if next.AdminUsername == "" {
		next.AdminUsername = defaultAdminUsername
	}
	if err := writeJSON(s.path, next); err != nil {
		return err
	}
	s.state = next
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, b)
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var idPattern = regexp.MustCompile(`^[a-zA-Z0-9-]{1,64}$`)

func validDomain(s string) bool {
	if len(s) > 253 || !strings.Contains(s, ".") || net.ParseIP(s) != nil {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if !labelPattern.MatchString(label) {
			return false
		}
	}
	return true
}

func inZone(host, zone string) bool { return host == zone || strings.HasSuffix(host, "."+zone) }

func Validate(c Config) error {
	if err := validateHomepage(c.Homepage, c.Groups); err != nil {
		return err
	}
	if err := validateDashboard(c.Dashboard); err != nil {
		return err
	}
	if err := validateLogRetention(c.LogRetention); err != nil {
		return err
	}
	if err := validateAdminAccess(c.AdminAccess); err != nil {
		return err
	}
	if c.HTTPPort != 0 || c.HTTPSPort != 0 {
		return errors.New("旧版监听配置已迁移，请刷新后使用反代组配置")
	}
	if c.Firewalls == nil {
		return errors.New("缺少防火墙配置，请刷新页面后重试；无防火墙时使用空列表")
	}
	if len(c.Groups) > 100 || len(c.Subscriptions) > 100 || len(c.Firewalls) > 100 {
		return errors.New("最多支持 100 个反代组、100 个订阅和 100 个防火墙")
	}
	groups, ports := map[string]bool{}, map[int]bool{}
	for _, g := range c.Groups {
		if !idPattern.MatchString(g.ID) || groups[g.ID] || strings.TrimSpace(g.Name) == "" || len(g.Name) > 100 {
			return errors.New("反代组名称或 ID 无效，ID 不能重复")
		}
		groups[g.ID] = true
		if g.DomainSuffix != "" && !validDomain(g.DomainSuffix) {
			return errors.New("反代组域名后缀无效，请填写 example.com")
		}
		if g.DDNSGroupID != "" {
			dns, exists := c.DNSGroup(g.DDNSGroupID)
			if !exists || g.DomainSuffix == "" || !inZone(g.DomainSuffix, dns.Zone) {
				return errors.New("反代组须填写绑定 DDNS 组根域名范围内的后缀；删除 DDNS 组前请先解除绑定")
			}
			for _, route := range c.Routes {
				if route.GroupID == g.ID && !inZone(route.Host, dns.Zone) {
					return errors.New("反代组中有域名不属于绑定 DDNS 组的根域名")
				}
			}
		}
		if g.Enabled && g.HTTPPort == 0 && g.HTTPSPort == 0 {
			return errors.New("启用的反代组至少需要一个监听端口")
		}
		for _, port := range []int{g.HTTPPort, g.HTTPSPort} {
			if port == 0 {
				continue
			}
			if port < 1024 || port > 65535 || ports[port] {
				return errors.New("所有组的监听端口须为不同的 1024–65535 端口；0 表示关闭该协议")
			}
			ports[port] = true
		}
	}
	subscriptions := map[string]Subscription{}
	for _, s := range c.Subscriptions {
		if !idPattern.MatchString(s.ID) || strings.TrimSpace(s.Name) == "" || len(s.Name) > 100 {
			return errors.New("订阅名称或 ID 无效")
		}
		if _, exists := subscriptions[s.ID]; exists {
			return errors.New("订阅 ID 不能重复")
		}
		u, err := url.Parse(s.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(s.URL) > 4096 {
			return errors.New("订阅地址须为 HTTPS 链接，不含账号或片段")
		}
		if s.Interval < 300 || s.Interval > 604800 {
			return errors.New("订阅更新间隔应为 300–604800 秒")
		}
		subscriptions[s.ID] = s
	}
	firewalls := map[string]bool{}
	for _, f := range c.Firewalls {
		if !idPattern.MatchString(f.ID) || firewalls[f.ID] || strings.TrimSpace(f.Name) == "" || len(f.Name) > 100 {
			return errors.New("防火墙名称或 ID 无效，ID 不能重复")
		}
		if err := validateProtection(f.Protection); err != nil {
			return fmt.Errorf("防火墙「%s」：%w", f.Name, err)
		}
		firewalls[f.ID] = true
		if f.DefaultAction != "allow" && f.DefaultAction != "deny" {
			return errors.New("防火墙默认动作须为允许或禁止")
		}
		if len(f.Groups) > 100 {
			return errors.New("每个防火墙最多支持 100 个 IP 规则组")
		}
		for _, group := range f.Groups {
			if strings.TrimSpace(group.Name) == "" || len(group.Name) > 100 || (group.Match != "include" && group.Match != "exclude") || (group.Action != "allow" && group.Action != "deny") {
				return errors.New("防火墙组须有名称，匹配方式为包含或排除，动作为允许或禁止")
			}
			if len(group.CIDRs)+len(group.Subscriptions) == 0 || len(group.CIDRs) > 200 {
				return errors.New("每个防火墙组至少需要一个 IP / CIDR 或订阅，手动条目最多 200 个")
			}
			for _, cidr := range group.CIDRs {
				if _, err := parsePrefix(cidr); err != nil {
					return errors.New("防火墙组的 IP / CIDR 格式错误")
				}
			}
			refs := map[string]bool{}
			for _, id := range group.Subscriptions {
				s, ok := subscriptions[id]
				if !ok || !s.Enabled || refs[id] {
					return errors.New("防火墙组引用的订阅须存在、已启用且不重复；停用或删除前请移除引用")
				}
				refs[id] = true
			}
		}
	}
	if c.Zone != "" && !validDomain(c.Zone) {
		return errors.New("根域名格式错误，请使用小写 ASCII 域名（国际化域名使用 Punycode）")
	}
	if len(c.Routes) > 100 {
		return errors.New("第一版最多支持 100 条代理及 DNS 记录")
	}
	seen := map[string]bool{}
	for _, r := range c.Routes {
		key := r.GroupID + "/" + r.Host
		if r.Image != "" && !routeImageID.MatchString(r.Image) {
			return errors.New("反代图片引用无效，请重新选择图片")
		}
		if !groups[r.GroupID] {
			return errors.New("代理规则引用了不存在的反代组")
		}
		if !validDomain(r.Host) || seen[key] {
			return errors.New("代理域名格式错误或在组内重复")
		}
		seen[key] = true
		g, _ := c.Group(r.GroupID)
		if r.TLS && r.Enabled && g.Enabled && g.HTTPSPort == 0 {
			return errors.New("HTTPS 代理规则需要所属组开启 HTTPS 监听端口")
		}
		if !r.TLS && r.Enabled && g.Enabled && g.HTTPPort == 0 {
			return errors.New("HTTP 代理规则需要所属组开启 HTTP 监听端口")
		}
		u, err := url.Parse(r.Upstream)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("后端地址应为 http(s)://主机:端口，不包含账号、路径、查询参数或片段")
		}
		if port := u.Port(); port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return errors.New("后端端口必须是 1–65535")
			}
		}
		if err := validateRouteAuth(r.Auth); err != nil {
			return err
		}
		if len(r.Name) > 100 {
			return errors.New("服务名称过长")
		}
		if r.Access != nil {
			return errors.New("旧版访问策略已迁移，请刷新后选择防火墙")
		}
		if r.FirewallID != "" && !firewalls[r.FirewallID] {
			return errors.New("代理规则引用了不存在的防火墙；删除前请移除代理引用")
		}

	}
	if c.DDNS.Groups == nil || c.DDNS.Enabled || c.DDNS.Interval != 0 || c.DDNS.IPv4URL != "" || c.DDNS.IPv6URL != "" || len(c.DDNS.Records) > 0 {
		return errors.New("DDNS 配置已升级，请刷新后使用任务组")
	}
	if len(c.DDNS.Groups) > 100 {
		return errors.New("最多支持 100 个 DDNS 组")
	}
	seen = map[string]bool{}
	ids := map[string]bool{}
	recordCount := 0
	for _, g := range c.DDNS.Groups {
		if !idPattern.MatchString(g.ID) || ids[g.ID] || strings.TrimSpace(g.Name) == "" || len(g.Name) > 100 {
			return errors.New("DDNS 组名称或 ID 无效，ID 不能重复")
		}
		ids[g.ID] = true
		if g.Provider != "cloudflare" {
			return errors.New("请选择已支持的 DNS 服务商，当前支持 Cloudflare")
		}
		if !validDomain(g.Zone) {
			return errors.New("请在 DNS 组中填写有效的根域名")
		}
		if g.Interval < 60 || g.Interval > 86400 {
			return errors.New("DDNS 组检查间隔应为 60–86400 秒")
		}
		if g.Mode != "ipv4" && g.Mode != "ipv6" && g.Mode != "dual" {
			return errors.New("DDNS 同步类型须为 IPv4、IPv6 或双栈")
		}
		if err := validateIPSources(g); err != nil {
			return err
		}
		if len(g.Hosts) == 0 || len(g.Hosts) > 100 {
			return errors.New("每个 DDNS 组须填写 1–100 个域名")
		}
		if err := validateIPEndpoints(g); err != nil {
			return err
		}
		for _, r := range g.Records() {
			key := r.Host + "/" + r.Type
			if !validDomain(r.Host) || !inZone(r.Host, g.Zone) || seen[key] {
				return errors.New("DDNS 域名须属于所属组的根域名，同一 A / AAAA 记录不能在组内或跨组重复")
			}
			seen[key] = true
			recordCount++
		}
	}
	if recordCount > 1000 {
		return errors.New("DDNS 最多同步 1000 条 A / AAAA 记录")
	}

	if err := validateOutboundProxy(c.OutboundProxy); err != nil {
		return err
	}
	if len(c.ACME.Requests) > 100 {
		return errors.New("最多配置 100 个证书任务")
	}
	requestIDs, requestedDomains := map[string]bool{}, map[string]bool{}
	for _, request := range c.ACME.Requests {
		if !idPattern.MatchString(request.ID) || requestIDs[request.ID] || request.Provider != "cloudflare" {
			return errors.New("证书任务 ID 重复、无效或 DNS 服务商不支持")
		}
		requestIDs[request.ID] = true
		if len(request.Domains) == 0 || len(request.Domains) > 100 {
			return errors.New("每个证书任务须包含 1–100 个域名")
		}
		for _, domain := range request.Domains {
			if !validDomain(strings.TrimPrefix(domain, "*.")) || requestedDomains[domain] {
				return errors.New("证书域名无效或重复；泛域名应填写 *.example.com")
			}
			requestedDomains[domain] = true
		}
	}
	if c.ACME.Enabled {
		a, err := mail.ParseAddress(c.ACME.Email)
		if err != nil || a.Address != c.ACME.Email {
			return errors.New("请填写有效的 ACME 联系邮箱")
		}
		if !c.ACME.AcceptTerms {
			return errors.New("启用证书前需同意 Let's Encrypt 服务条款")
		}
	}
	return nil
}

func validateAccess(a Access) error {
	if a.Mode != "public" && a.Mode != "allow" && a.Mode != "deny" {
		return errors.New("无效的访问控制模式")
	}
	if a.Mode == "allow" && len(a.CIDRs) == 0 && len(a.Subscriptions) == 0 {
		return errors.New("白名单不能为空，以免封锁所有访问")
	}
	if len(a.CIDRs) > 200 {
		return errors.New("每条规则最多支持 200 个 IP / CIDR")
	}
	if a.Mode == "public" && len(a.Subscriptions) > 0 {
		return errors.New("订阅仅可用于黑名单或白名单策略")
	}
	for _, v := range a.CIDRs {
		if _, err := parsePrefix(v); err != nil {
			return fmt.Errorf("IP / CIDR 格式错误：%s", v)
		}
	}
	return nil
}

func parsePrefix(s string) (netip.Prefix, error) {
	if p, err := netip.ParsePrefix(s); err == nil {
		if p.Addr().Is4In6() {
			if p.Bits() < 96 {
				return netip.Prefix{}, errors.New("无效的 IPv4 映射 CIDR")
			}
			return netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96).Masked(), nil
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}
