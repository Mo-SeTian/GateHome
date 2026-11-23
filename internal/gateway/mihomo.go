package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// MihomoCoreSettings defines GateHome-managed mihomo core runtime and port overrides.
type MihomoCoreSettings struct {
	MixedPort       int    `json:"mixed_port"`
	HTTPPort        int    `json:"http_port"`
	SocksPort       int    `json:"socks_port"`
	DNSListenPort   int    `json:"dns_listen_port"`
	AllowLAN        bool   `json:"allow_lan"`
	Mode            string `json:"mode"`
	LogLevel        string `json:"log_level"`
	IPv6            bool   `json:"ipv6"`
	UnifiedDelay    bool   `json:"unified_delay"`
	TCPConcurrent   bool   `json:"tcp_concurrent"`
	DNSEnhancedMode string `json:"dns_enhanced_mode"`
	AuthEnabled     bool   `json:"auth_enabled"`
	AuthUsername    string `json:"auth_username,omitempty"`
	AuthPassword    string `json:"auth_password,omitempty"`
}

// MihomoSubscription represents a managed proxy subscription source.
type MihomoSubscription struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	URL           string    `json:"url,omitempty"`
	IncludeFilter string    `json:"include_filter,omitempty"`
	ExcludeFilter string    `json:"exclude_filter,omitempty"`
	NodeSuffix    string    `json:"node_suffix,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
	NodeCount     int       `json:"node_count"`
	LastError     string    `json:"last_error,omitempty"`
	Upload        int64     `json:"upload,omitempty"`
	Download      int64     `json:"download,omitempty"`
	Total         int64     `json:"total,omitempty"`
	Expire        int64     `json:"expire,omitempty"`
}

// MihomoSubscriptionView exposes subscription metadata without leaking credentials or query tokens.
type MihomoSubscriptionView struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	URLHost       string    `json:"url_host,omitempty"`
	URLMasked     string    `json:"url_masked,omitempty"`
	HasURL        bool      `json:"has_url"`
	IncludeFilter string    `json:"include_filter,omitempty"`
	ExcludeFilter string    `json:"exclude_filter,omitempty"`
	NodeSuffix    string    `json:"node_suffix,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
	NodeCount     int       `json:"node_count"`
	LastError     string    `json:"last_error,omitempty"`
	Upload        int64     `json:"upload,omitempty"`
	Download      int64     `json:"download,omitempty"`
	Total         int64     `json:"total,omitempty"`
	Expire        int64     `json:"expire,omitempty"`
}

// MihomoCustomGroup represents a user-defined proxy group layered into runtime.yaml.
type MihomoCustomGroup struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Proxies   []string `json:"proxies"`
	Filter    string   `json:"filter,omitempty"`
	URL       string   `json:"url,omitempty"`
	Interval  int      `json:"interval,omitempty"`
	Tolerance int      `json:"tolerance,omitempty"`
}

// MihomoCustomRule represents a high-priority custom routing rule prepended to rules.
type MihomoCustomRule struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Value     string `json:"value"`
	Target    string `json:"target"`
	NoResolve bool   `json:"no_resolve,omitempty"`
	Enabled   bool   `json:"enabled"`
	Comment   string `json:"comment,omitempty"`
}

// MihomoDNSPolicy represents an incremental custom dns.nameserver-policy entry merged into runtime.yaml.
type MihomoDNSPolicy struct {
	ID      string   `json:"id"`
	Domain  string   `json:"domain"`
	Servers []string `json:"servers"`
	Enabled bool     `json:"enabled"`
}

// MihomoInheritedDNSPolicy summarizes a nameserver-policy entry read from the active subscription source.
type MihomoInheritedDNSPolicy struct {
	Domain  string   `json:"domain"`
	Servers []string `json:"servers"`
}

// MihomoConfig holds the persisted mihomo configuration managed by GateHome.
type MihomoConfig struct {
	Enabled              bool                 `json:"enabled"`
	TakeoverOutbound     bool                 `json:"takeover_outbound"`
	BinaryPath           string               `json:"binary_path,omitempty"`
	ControllerPort       int                  `json:"controller_port"`
	ControllerSecret     string               `json:"controller_secret,omitempty"`
	Settings             MihomoCoreSettings   `json:"settings"`
	ActiveSubscriptionID string               `json:"active_subscription_id,omitempty"`
	Subscriptions        []MihomoSubscription `json:"subscriptions"`
	CustomGroups         []MihomoCustomGroup  `json:"custom_groups"`
	CustomRules          []MihomoCustomRule   `json:"custom_rules"`
	CustomDNSPolicies    []MihomoDNSPolicy    `json:"custom_dns_policies"`
	ManualOverride       bool                 `json:"manual_override"`
}

var validMihomoModes = map[string]bool{
	"rule":   true,
	"global": true,
	"direct": true,
}

var validMihomoLogLevels = map[string]bool{
	"silent":  true,
	"error":   true,
	"warning": true,
	"info":    true,
	"debug":   true,
}

var validMihomoDNSModes = map[string]bool{
	"fake-ip":    true,
	"redir-host": true,
	"keep":       true,
}

var validMihomoGroupTypes = map[string]bool{
	"select":       true,
	"url-test":     true,
	"fallback":     true,
	"load-balance": true,
}

var validMihomoRuleTypes = map[string]bool{
	"DOMAIN-SUFFIX":  true,
	"DOMAIN-KEYWORD": true,
	"DOMAIN":         true,
	"IP-CIDR":        true,
	"IP-CIDR6":       true,
	"DST-PORT":       true,
	"SRC-IP-CIDR":    true,
	"GEOSITE":        true,
	"GEOIP":          true,
	"PROCESS-NAME":   true,
}

func randomMihomoSecret() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("gh-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func randomMihomoID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b)
}

func defaultMihomoConfig() MihomoConfig {
	return MihomoConfig{
		ControllerPort:   19090,
		ControllerSecret: randomMihomoSecret(),
		Settings: MihomoCoreSettings{
			MixedPort:       7890,
			AllowLAN:        true,
			Mode:            "rule",
			LogLevel:        "info",
			IPv6:            true,
			UnifiedDelay:    true,
			TCPConcurrent:   true,
			DNSEnhancedMode: "keep",
		},
		Subscriptions:     []MihomoSubscription{},
		CustomGroups:      []MihomoCustomGroup{},
		CustomRules:       []MihomoCustomRule{},
		CustomDNSPolicies: []MihomoDNSPolicy{},
	}
}

func normalizeMihomoConfig(cfg *MihomoConfig) bool {
	changed := false
	if cfg.ControllerPort == 0 {
		cfg.ControllerPort = 19090
		changed = true
	}
	if cfg.ControllerSecret == "" {
		cfg.ControllerSecret = randomMihomoSecret()
		changed = true
	}
	if cfg.Settings.MixedPort == 0 && cfg.Settings.HTTPPort == 0 && cfg.Settings.SocksPort == 0 {
		cfg.Settings.MixedPort = 7890
		changed = true
	}
	if cfg.Settings.Mode == "" {
		cfg.Settings.Mode = "rule"
		changed = true
	}
	if cfg.Settings.LogLevel == "" {
		cfg.Settings.LogLevel = "info"
		changed = true
	}
	if cfg.Settings.DNSEnhancedMode == "" {
		cfg.Settings.DNSEnhancedMode = "keep"
		changed = true
	}
	if cfg.Subscriptions == nil {
		cfg.Subscriptions = []MihomoSubscription{}
	}
	if cfg.CustomGroups == nil {
		cfg.CustomGroups = []MihomoCustomGroup{}
	}
	if cfg.CustomRules == nil {
		cfg.CustomRules = []MihomoCustomRule{}
	}
	if cfg.CustomDNSPolicies == nil {
		cfg.CustomDNSPolicies = []MihomoDNSPolicy{}
	}
	return changed
}

func validateMihomoSettings(cfg MihomoConfig, gwConfig Config, adminPort int) error {
	for _, p := range []struct {
		name string
		val  int
	}{
		{"混合代理端口", cfg.Settings.MixedPort},
		{"HTTP 代理端口", cfg.Settings.HTTPPort},
		{"SOCKS5 代理端口", cfg.Settings.SocksPort},
		{"DNS 监听端口", cfg.Settings.DNSListenPort},
	} {
		if p.val < 0 || p.val > 65535 {
			return fmt.Errorf("%s须为 0（禁用）或 1–65535", p.name)
		}
	}
	if cfg.Settings.MixedPort == 0 && cfg.Settings.HTTPPort == 0 && cfg.Settings.SocksPort == 0 {
		return errors.New("混合端口、HTTP 端口与 SOCKS5 端口不能同时为 0，请至少启用一个")
	}
	if cfg.ControllerPort < 1 || cfg.ControllerPort > 65535 {
		return errors.New("内部控制端口须为 1–65535")
	}
	seenPorts := map[int]string{}
	addUniquePort := func(port int, label string) error {
		if port == 0 {
			return nil
		}
		if prev, exists := seenPorts[port]; exists {
			return fmt.Errorf("%s (%d) 与%s冲突", label, port, prev)
		}
		seenPorts[port] = label
		return nil
	}
	if err := addUniquePort(cfg.Settings.MixedPort, "混合代理端口"); err != nil {
		return err
	}
	if err := addUniquePort(cfg.Settings.HTTPPort, "HTTP 代理端口"); err != nil {
		return err
	}
	if err := addUniquePort(cfg.Settings.SocksPort, "SOCKS5 代理端口"); err != nil {
		return err
	}
	if err := addUniquePort(cfg.Settings.DNSListenPort, "DNS 监听端口"); err != nil {
		return err
	}
	if err := addUniquePort(cfg.ControllerPort, "内部控制端口"); err != nil {
		return err
	}
	if adminPort > 0 {
		if label, exists := seenPorts[adminPort]; exists {
			return fmt.Errorf("%s (%d) 不能与管理端口相同", label, adminPort)
		}
	}
	if gwConfig.SunPanel.Enabled && gwConfig.SunPanel.Port > 0 {
		if label, exists := seenPorts[gwConfig.SunPanel.Port]; exists {
			return fmt.Errorf("%s (%d) 与 Sun-Panel 端口冲突", label, gwConfig.SunPanel.Port)
		}
	}
	for _, g := range gwConfig.Groups {
		if !g.Enabled {
			continue
		}
		if g.HTTPPort > 0 {
			if label, exists := seenPorts[g.HTTPPort]; exists {
				return fmt.Errorf("%s (%d) 与反代组「%s」HTTP 端口冲突", label, g.HTTPPort, g.Name)
			}
		}
		if g.HTTPSPort > 0 {
			if label, exists := seenPorts[g.HTTPSPort]; exists {
				return fmt.Errorf("%s (%d) 与反代组「%s」HTTPS 端口冲突", label, g.HTTPSPort, g.Name)
			}
		}
	}
	for _, f := range gwConfig.Forwards {
		if !f.Enabled {
			continue
		}
		if cfg.Settings.DNSListenPort > 0 && f.ListenPort == cfg.Settings.DNSListenPort {
			return fmt.Errorf("DNS 监听端口 (%d) 与端口转发「%s」冲突", f.ListenPort, f.Name)
		}
		if f.Protocol == "udp" {
			continue
		}
		if label, exists := seenPorts[f.ListenPort]; exists && f.ListenPort != cfg.Settings.DNSListenPort {
			return fmt.Errorf("%s (%d) 与端口转发「%s」冲突", label, f.ListenPort, f.Name)
		}
	}
	if !validMihomoModes[cfg.Settings.Mode] {
		return errors.New("代理模式须为 rule、global 或 direct")
	}
	if !validMihomoLogLevels[cfg.Settings.LogLevel] {
		return errors.New("日志级别无效")
	}
	if !validMihomoDNSModes[cfg.Settings.DNSEnhancedMode] {
		return errors.New("DNS 增强模式须为 fake-ip、redir-host 或 keep")
	}
	if cfg.Settings.AuthEnabled {
		user := strings.TrimSpace(cfg.Settings.AuthUsername)
		if user == "" || len(user) > 64 || strings.ContainsAny(user, ":\r\n \t") {
			return errors.New("局域网代理账号不能为空，不能包含冒号、空白或换行，且不超过 64 字符")
		}
		pass := cfg.Settings.AuthPassword
		if pass == "" || len(pass) > 128 || strings.ContainsAny(pass, "\r\n") {
			return errors.New("局域网代理密码不能为空，不能包含换行，且不超过 128 字符")
		}
	}
	return nil
}

func validateMihomoCustomGroup(g MihomoCustomGroup) error {
	name := strings.TrimSpace(g.Name)
	if name == "" || len(name) > 128 || strings.ContainsAny(name, ",\r\n") {
		return errors.New("策略组名称不能为空，不能包含逗号或换行，且不超过 128 字符")
	}
	if !validMihomoGroupTypes[g.Type] {
		return errors.New("策略组类型须为 select、url-test、fallback 或 load-balance")
	}
	if strings.TrimSpace(g.Filter) != "" {
		if _, err := regexp.Compile(g.Filter); err != nil {
			return fmt.Errorf("节点筛选正则无效: %v", err)
		}
	}
	if g.Interval < 0 || g.Interval > 86400 {
		return errors.New("测速间隔须为 0–86400 秒")
	}
	if g.Tolerance < 0 || g.Tolerance > 10000 {
		return errors.New("测速容差须为 0–10000 ms")
	}
	if g.URL != "" {
		u, err := url.Parse(g.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || strings.ContainsAny(g.URL, "\r\n") {
			return errors.New("测速 URL 须为有效的 http/https 地址")
		}
	}
	for _, p := range g.Proxies {
		if strings.TrimSpace(p) == "" || strings.ContainsAny(p, "\r\n") {
			return errors.New("成员节点名称无效")
		}
	}
	return nil
}

func validateMihomoCustomRule(r MihomoCustomRule) error {
	ruleType := strings.ToUpper(strings.TrimSpace(r.Type))
	if !validMihomoRuleTypes[ruleType] {
		return fmt.Errorf("不支持的规则类型: %s", r.Type)
	}
	value := strings.TrimSpace(r.Value)
	if value == "" || len(value) > 512 || strings.ContainsAny(value, ",\r\n ") {
		return errors.New("匹配内容不能为空，且不能包含逗号、空格或换行")
	}
	target := strings.TrimSpace(r.Target)
	if target == "" || len(target) > 128 || strings.ContainsAny(target, ",\r\n") {
		return errors.New("出站目标不能为空，且不能包含逗号或换行")
	}
	if ruleType == "DST-PORT" {
		if port, err := strconv.Atoi(value); err != nil || port < 1 || port > 65535 {
			return errors.New("DST-PORT 须填写 1–65535 的端口号")
		}
	}
	if ruleType == "IP-CIDR" || ruleType == "IP-CIDR6" || ruleType == "SRC-IP-CIDR" {
		if _, err := netip.ParsePrefix(value); err != nil {
			return fmt.Errorf("%s 须为有效的 CIDR 网段（如 192.168.1.0/24）", ruleType)
		}
	}
	if len(r.Comment) > 256 || strings.ContainsAny(r.Comment, "\r\n") {
		return errors.New("规则备注不能包含换行且不超过 256 字符")
	}
	return nil
}

func normalizeDNSPolicyServers(servers []string) []string {
	cleaned := make([]string, 0, len(servers))
	for _, s := range servers {
		s = strings.TrimSpace(s)
		if s != "" {
			cleaned = append(cleaned, s)
		}
	}
	return cleaned
}

func validateMihomoDNSPolicy(p MihomoDNSPolicy) error {
	domain := strings.TrimSpace(p.Domain)
	if domain == "" || len(domain) > 253 || strings.ContainsAny(domain, " \t\r\n,") {
		return errors.New("DNS 匹配域名不能为空，不能包含空白、逗号或换行，且不超过 253 字符")
	}
	servers := normalizeDNSPolicyServers(p.Servers)
	if len(servers) == 0 {
		return errors.New("请至少填写一个 DNS 服务器地址")
	}
	for _, srv := range servers {
		if len(srv) > 256 || strings.ContainsAny(srv, "\r\n") {
			return errors.New("DNS 服务器地址无效，不能包含换行且不超过 256 字符")
		}
	}
	return nil
}

func validateMihomoSubscriptionURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 4096 || strings.ContainsAny(raw, " \t\r\n") {
		return errors.New("订阅地址无效")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return errors.New("订阅地址须为有效 HTTP/HTTPS 链接，且不含嵌入账号密码")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return errors.New("订阅地址端口无效")
		}
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil {
		if ip.Zone() != "" || !allowedServiceIP(ip.Unmap()) {
			return errors.New("订阅地址不允许使用受限或元数据 IP")
		}
	}
	return nil
}

func validateMihomoSubscriptionFilters(includeFilter, excludeFilter, nodeSuffix string) error {
	if inc := strings.TrimSpace(includeFilter); inc != "" {
		if len(inc) > 512 {
			return errors.New("保留节点正则不能超过 512 字符")
		}
		if _, err := regexp.Compile(inc); err != nil {
			return fmt.Errorf("保留节点正则无效: %v", err)
		}
	}
	if exc := strings.TrimSpace(excludeFilter); exc != "" {
		if len(exc) > 512 {
			return errors.New("排除节点正则不能超过 512 字符")
		}
		if _, err := regexp.Compile(exc); err != nil {
			return fmt.Errorf("排除节点正则无效: %v", err)
		}
	}
	if nodeSuffix != "" {
		if len(nodeSuffix) > 64 || strings.ContainsAny(nodeSuffix, ",\r\n") {
			return errors.New("节点统一后缀不能包含逗号或换行，且不超过 64 字符")
		}
	}
	return nil
}

func activeOutboundURL(s MihomoCoreSettings) string {
	if s.MixedPort > 0 {
		return "http://127.0.0.1:" + strconv.Itoa(s.MixedPort)
	}
	if s.HTTPPort > 0 {
		return "http://127.0.0.1:" + strconv.Itoa(s.HTTPPort)
	}
	if s.SocksPort > 0 {
		return "socks5://127.0.0.1:" + strconv.Itoa(s.SocksPort)
	}
	return ""
}

func maskSubscriptionURL(raw string) (host string, masked string, hasURL bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", "https://***", true
	}
	host = u.Host
	masked = u.Scheme + "://" + u.Host + "/***"
	return host, masked, true
}

func toSubscriptionViews(subs []MihomoSubscription) []MihomoSubscriptionView {
	out := make([]MihomoSubscriptionView, 0, len(subs))
	for _, s := range subs {
		host, masked, hasURL := maskSubscriptionURL(s.URL)
		out = append(out, MihomoSubscriptionView{
			ID:            s.ID,
			Name:          s.Name,
			URLHost:       host,
			URLMasked:     masked,
			HasURL:        hasURL,
			IncludeFilter: s.IncludeFilter,
			ExcludeFilter: s.ExcludeFilter,
			NodeSuffix:    s.NodeSuffix,
			UpdatedAt:     s.UpdatedAt,
			NodeCount:     s.NodeCount,
			LastError:     s.LastError,
			Upload:        s.Upload,
			Download:      s.Download,
			Total:         s.Total,
			Expire:        s.Expire,
		})
	}
	return out
}

func detectLANIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	var candidates []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			p, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			ip := p.Addr().Unmap()
			if !ip.Is4() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				continue
			}
			if ip.IsPrivate() {
				candidates = append(candidates, ip.String())
			}
		}
	}
	if len(candidates) > 0 {
		slices.Sort(candidates)
		return candidates[0]
	}
	return "127.0.0.1"
}

func loadMihomoConfig(dir string) (MihomoConfig, error) {
	path := filepath.Join(dir, "config.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg := defaultMihomoConfig()
		return cfg, nil
	}
	if err != nil {
		return MihomoConfig{}, err
	}
	var cfg MihomoConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return MihomoConfig{}, errors.New("Mihomo 配置文件损坏")
	}
	if normalizeMihomoConfig(&cfg) {
		_ = saveMihomoConfig(dir, cfg)
	}
	return cfg, nil
}

func saveMihomoConfig(dir string, cfg MihomoConfig) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0700)
	normalizeMihomoConfig(&cfg)
	return writeJSON(filepath.Join(dir, "config.json"), cfg)
}

func parseSubscriptionUserInfo(header string) (upload, download, total, expire int64) {
	for _, part := range strings.Split(header, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "upload":
			upload = n
		case "download":
			download = n
		case "total":
			total = n
		case "expire":
			expire = n
		}
	}
	return
}
