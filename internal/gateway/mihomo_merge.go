package gateway

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const mihomoHeaderComment = "# Managed by GateHome (synthesized runtime.yaml)\n"

func buildColdStartYAML(cfg MihomoConfig) string {
	var b strings.Builder
	b.WriteString("# GateHome Mihomo cold-start baseline\n")
	if cfg.Settings.MixedPort > 0 {
		fmt.Fprintf(&b, "mixed-port: %d\n", cfg.Settings.MixedPort)
	}
	if cfg.Settings.HTTPPort > 0 {
		fmt.Fprintf(&b, "port: %d\n", cfg.Settings.HTTPPort)
	}
	if cfg.Settings.SocksPort > 0 {
		fmt.Fprintf(&b, "socks-port: %d\n", cfg.Settings.SocksPort)
	}
	fmt.Fprintf(&b, "allow-lan: %t\n", cfg.Settings.AllowLAN)
	fmt.Fprintf(&b, "mode: %s\n", cfg.Settings.Mode)
	fmt.Fprintf(&b, "log-level: %s\n", cfg.Settings.LogLevel)
	fmt.Fprintf(&b, "ipv6: %t\n", cfg.Settings.IPv6)
	fmt.Fprintf(&b, "unified-delay: %t\n", cfg.Settings.UnifiedDelay)
	fmt.Fprintf(&b, "tcp-concurrent: %t\n", cfg.Settings.TCPConcurrent)
	fmt.Fprintf(&b, "external-controller: 127.0.0.1:%d\n", cfg.ControllerPort)
	fmt.Fprintf(&b, "secret: %q\n", cfg.ControllerSecret)
	b.WriteString("dns:\n")
	b.WriteString("  enable: true\n")
	fmt.Fprintf(&b, "  ipv6: %t\n", cfg.Settings.IPv6)
	coldDNSMode := cfg.Settings.DNSEnhancedMode
	if coldDNSMode == "" || coldDNSMode == "keep" {
		coldDNSMode = "fake-ip"
	}
	fmt.Fprintf(&b, "  enhanced-mode: %s\n", coldDNSMode)
	b.WriteString("  fake-ip-range: 198.18.0.1/16\n")
	b.WriteString("  nameserver:\n")
	b.WriteString("    - 223.5.5.5\n")
	b.WriteString("    - 119.29.29.29\n")
	b.WriteString("proxies: []\n")
	b.WriteString("proxy-groups:\n")
	b.WriteString("  - name: 节点选择\n")
	b.WriteString("    type: select\n")
	b.WriteString("    proxies:\n")
	b.WriteString("      - DIRECT\n")
	b.WriteString("rules:\n")
	b.WriteString("  - MATCH,DIRECT\n")
	return b.String()
}

func synthesizeMihomoYAML(sourceYAML []byte, cfg MihomoConfig) ([]byte, error) {
	normalizeMihomoConfig(&cfg)
	trimmed := bytes.TrimSpace(sourceYAML)
	if len(trimmed) == 0 {
		trimmed = []byte(buildColdStartYAML(cfg))
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(trimmed, &doc); err != nil {
		return nil, fmt.Errorf("YAML 解析失败: %v", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("订阅或配置 YAML 根节点必须为映射对象")
	}
	root := doc.Content[0]

	// Layer 2: Core overrides (ports, LAN, IPv6, DNS, controller/secret)
	setIntOrDelete := func(key string, port int) {
		if port > 0 {
			yamlMapSet(root, key, yamlIntNode(port))
		} else {
			yamlMapDelete(root, key)
		}
	}
	setIntOrDelete("mixed-port", cfg.Settings.MixedPort)
	setIntOrDelete("port", cfg.Settings.HTTPPort)
	setIntOrDelete("socks-port", cfg.Settings.SocksPort)
	yamlMapSet(root, "allow-lan", yamlBoolNode(cfg.Settings.AllowLAN))
	yamlMapSet(root, "mode", yamlStrNode(cfg.Settings.Mode))
	yamlMapSet(root, "log-level", yamlStrNode(cfg.Settings.LogLevel))
	yamlMapSet(root, "ipv6", yamlBoolNode(cfg.Settings.IPv6))
	yamlMapSet(root, "unified-delay", yamlBoolNode(cfg.Settings.UnifiedDelay))
	yamlMapSet(root, "tcp-concurrent", yamlBoolNode(cfg.Settings.TCPConcurrent))
	yamlMapSet(root, "external-controller", yamlStrNode(fmt.Sprintf("127.0.0.1:%d", cfg.ControllerPort)))
	yamlMapSet(root, "secret", yamlStrNode(cfg.ControllerSecret))
	sanitizeDangerousMihomoKeys(root)
	if cfg.Settings.AuthEnabled && cfg.Settings.AuthUsername != "" && cfg.Settings.AuthPassword != "" {
		authSeq := &yaml.Node{
			Kind:    yaml.SequenceNode,
			Tag:     "!!seq",
			Content: []*yaml.Node{yamlStrNode(cfg.Settings.AuthUsername + ":" + cfg.Settings.AuthPassword)},
		}
		skipSeq := &yaml.Node{
			Kind:    yaml.SequenceNode,
			Tag:     "!!seq",
			Content: []*yaml.Node{yamlStrNode("127.0.0.1/32"), yamlStrNode("::1/128")},
		}
		yamlMapSet(root, "authentication", authSeq)
		yamlMapSet(root, "skip-auth-prefixes", skipSeq)
	} else {
		yamlMapDelete(root, "authentication")
		yamlMapDelete(root, "skip-auth-prefixes")
	}

	dnsNode := yamlMapEnsureMapping(root, "dns")
	yamlMapSet(dnsNode, "ipv6", yamlBoolNode(cfg.Settings.IPv6))
	if cfg.Settings.DNSEnhancedMode != "" && cfg.Settings.DNSEnhancedMode != "keep" {
		if v, _ := yamlMapFind(dnsNode, "enable"); v == nil {
			yamlMapSet(dnsNode, "enable", yamlBoolNode(true))
		}
		yamlMapSet(dnsNode, "enhanced-mode", yamlStrNode(cfg.Settings.DNSEnhancedMode))
		if v, _ := yamlMapFind(dnsNode, "nameserver"); v == nil {
			ns := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{
				yamlStrNode("223.5.5.5"),
				yamlStrNode("119.29.29.29"),
			}}
			yamlMapSet(dnsNode, "nameserver", ns)
		}
		if cfg.Settings.DNSEnhancedMode == "fake-ip" {
			if v, _ := yamlMapFind(dnsNode, "fake-ip-range"); v == nil {
				yamlMapSet(dnsNode, "fake-ip-range", yamlStrNode("198.18.0.1/16"))
			}
		}
	}
	if cfg.Settings.DNSListenPort > 0 {
		listenHost := "127.0.0.1"
		if cfg.Settings.AllowLAN {
			listenHost = "0.0.0.0"
		}
		yamlMapSet(dnsNode, "enable", yamlBoolNode(true))
		yamlMapSet(dnsNode, "listen", yamlStrNode(fmt.Sprintf("%s:%d", listenHost, cfg.Settings.DNSListenPort)))
	} else {
		yamlMapDelete(dnsNode, "listen")
	}
	hasEnabledCustomDNS := false
	for _, p := range cfg.CustomDNSPolicies {
		if p.Enabled && strings.TrimSpace(p.Domain) != "" && len(normalizeDNSPolicyServers(p.Servers)) > 0 {
			hasEnabledCustomDNS = true
			break
		}
	}
	if hasEnabledCustomDNS {
		policyNode := yamlMapEnsureMapping(dnsNode, "nameserver-policy")
		for _, p := range cfg.CustomDNSPolicies {
			if !p.Enabled {
				continue
			}
			domain := strings.TrimSpace(p.Domain)
			servers := normalizeDNSPolicyServers(p.Servers)
			if domain == "" || len(servers) == 0 {
				continue
			}
			seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for _, srv := range servers {
				seq.Content = append(seq.Content, yamlStrNode(srv))
			}
			yamlMapSet(policyNode, domain, seq)
		}
	}

	// Layer 2.5: Custom YAML overlay (arbitrary top-level/nested overrides from custom.yaml)
	if strings.TrimSpace(cfg.CustomOverlayYAML) != "" {
		var overlayDoc yaml.Node
		if err := yaml.Unmarshal([]byte(cfg.CustomOverlayYAML), &overlayDoc); err != nil {
			return nil, fmt.Errorf("自定义配置覆写层解析失败: %v", err)
		}
		if overlayDoc.Kind == yaml.DocumentNode && len(overlayDoc.Content) > 0 && overlayDoc.Content[0].Kind == yaml.MappingNode {
			mergeYAMLMappingOverlay(root, overlayDoc.Content[0], true)
		}
	}

	// Apply active subscription node filters (include/exclude regex) and suffix renaming before collecting proxies.
	if activeSub, ok := activeSubscription(cfg); ok {
		applySubscriptionNodeTransform(root, activeSub)
	}

	// Collect available proxy node names from proxies:
	proxyNames := collectProxyNames(root)

	// Layer 3: Custom proxy groups
	groupsSeq := yamlMapEnsureSequence(root, "proxy-groups")
	if len(cfg.CustomGroups) > 0 {
		existingByName := map[string]int{}
		for idx, item := range groupsSeq.Content {
			if item.Kind == yaml.MappingNode {
				if nameNode, _ := yamlMapFind(item, "name"); nameNode != nil && nameNode.Value != "" {
					existingByName[nameNode.Value] = idx
				}
			}
		}
		var prepended []*yaml.Node
		for _, cg := range cfg.CustomGroups {
			groupNode := buildCustomGroupNode(cg, proxyNames)
			if existingIdx, ok := existingByName[cg.Name]; ok {
				groupsSeq.Content[existingIdx] = groupNode
			} else {
				prepended = append(prepended, groupNode)
			}
		}
		if len(prepended) > 0 {
			groupsSeq.Content = append(prepended, groupsSeq.Content...)
		}
	}
	if len(groupsSeq.Content) == 0 {
		groupsSeq.Content = append(groupsSeq.Content, buildCustomGroupNode(MihomoCustomGroup{
			Name:    "节点选择",
			Type:    "select",
			Proxies: []string{"DIRECT"},
		}, proxyNames))
	}

	// Layer 3.5: Custom rule-providers (remote / built-in / manual file)
	if len(cfg.CustomRuleSets) > 0 {
		var enabledSets []MihomoRuleSet
		for _, rs := range cfg.CustomRuleSets {
			if rs.Enabled {
				enabledSets = append(enabledSets, rs)
			}
		}
		if len(enabledSets) > 0 {
			providersMap := yamlMapEnsureMapping(root, "rule-providers")
			for _, rs := range enabledSets {
				providerNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				providerType := "http"
				if rs.Source == "manual" {
					providerType = "file"
				}
				yamlMapSet(providerNode, "type", yamlStrNode(providerType))
				yamlMapSet(providerNode, "behavior", yamlStrNode(rs.Behavior))
				yamlMapSet(providerNode, "format", yamlStrNode(rs.Format))
				yamlMapSet(providerNode, "path", yamlStrNode(rs.Path))
				if providerType == "http" {
					yamlMapSet(providerNode, "url", yamlStrNode(rs.URL))
					if rs.Interval > 0 {
						yamlMapSet(providerNode, "interval", yamlIntNode(rs.Interval))
					}
				}
				yamlMapSet(providersMap, rs.Name, providerNode)
			}
		}
	}

	// Layer 4: Prepend enabled custom routing rules
	rulesSeq := yamlMapEnsureSequence(root, "rules")
	var customRuleNodes []*yaml.Node
	for _, r := range cfg.CustomRules {
		if !r.Enabled {
			continue
		}
		formatted := formatCustomRule(r)
		if formatted != "" {
			customRuleNodes = append(customRuleNodes, yamlStrNode(formatted))
		}
	}
	if len(customRuleNodes) > 0 {
		rulesSeq.Content = append(customRuleNodes, rulesSeq.Content...)
	}
	if len(rulesSeq.Content) == 0 {
		rulesSeq.Content = append(rulesSeq.Content, yamlStrNode("MATCH,DIRECT"))
	}

	rendered, err := encodeYAMLIndent2(&doc)
	if err != nil {
		return nil, fmt.Errorf("YAML 序列化失败: %v", err)
	}
	outBytes := []byte(mihomoHeaderComment + rendered)

	if err := validateMihomoYAML(outBytes); err != nil {
		return nil, err
	}
	return outBytes, nil
}

func activeSubscription(cfg MihomoConfig) (MihomoSubscription, bool) {
	if cfg.ActiveSubscriptionID == "" {
		return MihomoSubscription{}, false
	}
	for _, s := range cfg.Subscriptions {
		if s.ID == cfg.ActiveSubscriptionID {
			return s, true
		}
	}
	return MihomoSubscription{}, false
}

func applySubscriptionNodeTransform(root *yaml.Node, sub MihomoSubscription) {
	incPat := strings.TrimSpace(sub.IncludeFilter)
	excPat := strings.TrimSpace(sub.ExcludeFilter)
	suffix := sub.NodeSuffix
	if incPat == "" && excPat == "" && suffix == "" {
		return
	}
	var incRe, excRe *mihomoRegex
	if incPat != "" {
		var err error
		if incRe, err = compileMihomoRegex(incPat); err != nil {
			return
		}
	}
	if excPat != "" {
		var err error
		if excRe, err = compileMihomoRegex(excPat); err != nil {
			return
		}
	}

	proxiesNode, _ := yamlMapFind(root, "proxies")
	if proxiesNode == nil || proxiesNode.Kind != yaml.SequenceNode {
		return
	}

	removed := map[string]bool{}
	renamed := map[string]string{}
	kept := make([]*yaml.Node, 0, len(proxiesNode.Content))

	for _, item := range proxiesNode.Content {
		if item.Kind != yaml.MappingNode {
			kept = append(kept, item)
			continue
		}
		nameNode, _ := yamlMapFind(item, "name")
		if nameNode == nil {
			kept = append(kept, item)
			continue
		}
		origName := nameNode.Value
		keep := true
		if incRe != nil && !incRe.MatchString(origName) {
			keep = false
		}
		if keep && excRe != nil && excRe.MatchString(origName) {
			keep = false
		}
		if !keep {
			if origName != "" {
				removed[origName] = true
			}
			continue
		}
		if suffix != "" && origName != "" {
			newName := origName + suffix
			nameNode.Value = newName
			renamed[origName] = newName
		}
		kept = append(kept, item)
	}
	proxiesNode.Content = kept

	if len(removed) == 0 && len(renamed) == 0 {
		return
	}
	groupsNode, _ := yamlMapFind(root, "proxy-groups")
	if groupsNode == nil || groupsNode.Kind != yaml.SequenceNode {
		return
	}
	for _, groupItem := range groupsNode.Content {
		if groupItem.Kind != yaml.MappingNode {
			continue
		}
		membersSeq, _ := yamlMapFind(groupItem, "proxies")
		if membersSeq == nil || membersSeq.Kind != yaml.SequenceNode {
			continue
		}
		nextMembers := make([]*yaml.Node, 0, len(membersSeq.Content))
		for _, m := range membersSeq.Content {
			val := m.Value
			if removed[val] {
				continue
			}
			if newName, ok := renamed[val]; ok {
				m.Value = newName
			}
			nextMembers = append(nextMembers, m)
		}
		if len(nextMembers) == 0 {
			useNode, _ := yamlMapFind(groupItem, "use")
			if useNode == nil || (useNode.Kind == yaml.SequenceNode && len(useNode.Content) == 0) {
				nextMembers = append(nextMembers, yamlStrNode("DIRECT"))
			}
		}
		membersSeq.Content = nextMembers
	}
}

func formatCustomRule(r MihomoCustomRule) string {
	ruleType := strings.ToUpper(strings.TrimSpace(r.Type))
	value := strings.TrimSpace(r.Value)
	target := strings.TrimSpace(r.Target)
	if ruleType == "" || value == "" || target == "" {
		return ""
	}
	line := ruleType + "," + value + "," + target
	if r.NoResolve && (ruleType == "IP-CIDR" || ruleType == "IP-CIDR6" || ruleType == "SRC-IP-CIDR" || ruleType == "GEOIP" || ruleType == "RULE-SET") {
		line += ",no-resolve"
	}
	return line
}

func mihomoExtraHasDynamicSource(extra map[string]any) bool {
	if len(extra) == 0 {
		return false
	}
	for _, k := range []string{"include-all", "include-all-proxies", "include-all-providers"} {
		if b, ok := extra[k].(bool); ok && b {
			return true
		}
	}
	if useVal, ok := extra["use"]; ok && useVal != nil {
		switch u := useVal.(type) {
		case []any:
			if len(u) > 0 {
				return true
			}
		case []string:
			if len(u) > 0 {
				return true
			}
		case string:
			if strings.TrimSpace(u) != "" {
				return true
			}
		}
	}
	return false
}

func buildCustomGroupNode(cg MihomoCustomGroup, allProxies []string) *yaml.Node {
	hasDynamic := mihomoExtraHasDynamicSource(cg.Extra)
	members := make([]string, 0, len(cg.Proxies)+8)
	seen := map[string]bool{}
	appendUnique := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			members = append(members, name)
		}
	}
	for _, p := range cg.Proxies {
		appendUnique(p)
	}
	if !hasDynamic && strings.TrimSpace(cg.Filter) != "" {
		if re, err := compileMihomoRegex(cg.Filter); err == nil {
			for _, candidate := range allProxies {
				if re.MatchString(candidate) {
					appendUnique(candidate)
				}
			}
		}
	}
	if !hasDynamic && len(members) == 0 {
		members = append(members, "DIRECT")
	}

	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	yamlMapSet(m, "name", yamlStrNode(cg.Name))
	yamlMapSet(m, "type", yamlStrNode(cg.Type))
	if len(members) > 0 {
		proxiesSeq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, member := range members {
			proxiesSeq.Content = append(proxiesSeq.Content, yamlStrNode(member))
		}
		yamlMapSet(m, "proxies", proxiesSeq)
	}
	if hasDynamic && strings.TrimSpace(cg.Filter) != "" {
		yamlMapSet(m, "filter", yamlStrNode(cg.Filter))
	}

	if cg.Type != "select" && cg.Type != "relay" {
		testURL := strings.TrimSpace(cg.URL)
		if testURL == "" {
			testURL = "https://www.gstatic.com/generate_204"
		}
		interval := cg.Interval
		if interval <= 0 {
			interval = 300
		}
		yamlMapSet(m, "url", yamlStrNode(testURL))
		yamlMapSet(m, "interval", yamlIntNode(interval))
		if cg.Tolerance > 0 {
			yamlMapSet(m, "tolerance", yamlIntNode(cg.Tolerance))
		}
	}

	if len(cg.Extra) > 0 {
		keys := make([]string, 0, len(cg.Extra))
		for k := range cg.Extra {
			if !mihomoGroupCoreKeys[k] {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		for _, k := range keys {
			if node, err := marshalYAMLNode(normalizeMihomoValueForYAML(cg.Extra[k])); err == nil && node != nil {
				yamlMapSet(m, k, node)
			}
		}
	}
	return m
}

func collectProxyNames(root *yaml.Node) []string {
	proxiesNode, _ := yamlMapFind(root, "proxies")
	if proxiesNode == nil || proxiesNode.Kind != yaml.SequenceNode {
		return nil
	}
	names := make([]string, 0, len(proxiesNode.Content))
	for _, item := range proxiesNode.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if nameNode, _ := yamlMapFind(item, "name"); nameNode != nil && nameNode.Value != "" {
			names = append(names, nameNode.Value)
		}
	}
	return names
}

func countYAMLProxies(raw []byte) int {
	var parsed struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return 0
	}
	return len(parsed.Proxies)
}

func validateMihomoYAML(raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("YAML 内容不能为空")
	}
	if len(raw) > 10<<20 {
		return errors.New("YAML 配置大小不能超过 10 MiB")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("YAML 语法错误: %v", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return errors.New("YAML 根节点必须为映射对象")
	}
	return nil
}

// MihomoGroupInfo summarizes a proxy group for UI display (live or fallback from runtime.yaml).
type MihomoGroupInfo struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Now     string   `json:"now"`
	All     []string `json:"all"`
	Custom  bool     `json:"custom"`
	GroupID string   `json:"group_id,omitempty"`
}

func extractYAMLGroupsAndNodes(raw []byte, customGroups []MihomoCustomGroup) ([]MihomoGroupInfo, []string) {
	customByName := map[string]string{}
	for _, cg := range customGroups {
		customByName[cg.Name] = cg.ID
	}
	var parsed struct {
		Proxies []struct {
			Name string `yaml:"name"`
		} `yaml:"proxies"`
		ProxyGroups []struct {
			Name    string   `yaml:"name"`
			Type    string   `yaml:"type"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, nil
	}
	nodes := make([]string, 0, len(parsed.Proxies))
	for _, p := range parsed.Proxies {
		if p.Name != "" {
			nodes = append(nodes, p.Name)
		}
	}
	groups := make([]MihomoGroupInfo, 0, len(parsed.ProxyGroups))
	for _, g := range parsed.ProxyGroups {
		if g.Name == "" {
			continue
		}
		now := ""
		if len(g.Proxies) > 0 {
			now = g.Proxies[0]
		}
		id, isCustom := customByName[g.Name]
		groups = append(groups, MihomoGroupInfo{
			Name:    g.Name,
			Type:    g.Type,
			Now:     now,
			All:     slices.Clone(g.Proxies),
			Custom:  isCustom,
			GroupID: id,
		})
	}
	return groups, nodes
}

func extractInheritedDNSPolicies(raw []byte) []MihomoInheritedDNSPolicy {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []MihomoInheritedDNSPolicy{}
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return []MihomoInheritedDNSPolicy{}
	}
	dnsNode, _ := yamlMapFind(doc.Content[0], "dns")
	policyNode, _ := yamlMapFind(dnsNode, "nameserver-policy")
	if policyNode == nil || policyNode.Kind != yaml.MappingNode {
		return []MihomoInheritedDNSPolicy{}
	}
	out := make([]MihomoInheritedDNSPolicy, 0, len(policyNode.Content)/2)
	for i := 0; i+1 < len(policyNode.Content); i += 2 {
		domain := strings.TrimSpace(policyNode.Content[i].Value)
		valNode := policyNode.Content[i+1]
		if domain == "" {
			continue
		}
		var servers []string
		switch valNode.Kind {
		case yaml.ScalarNode:
			if v := strings.TrimSpace(valNode.Value); v != "" {
				servers = []string{v}
			}
		case yaml.SequenceNode:
			for _, item := range valNode.Content {
				if item.Kind == yaml.ScalarNode {
					if v := strings.TrimSpace(item.Value); v != "" {
						servers = append(servers, v)
					}
				}
			}
		}
		if len(servers) > 0 {
			out = append(out, MihomoInheritedDNSPolicy{Domain: domain, Servers: servers})
		}
	}
	slices.SortFunc(out, func(a, b MihomoInheritedDNSPolicy) int {
		return strings.Compare(a.Domain, b.Domain)
	})
	return out
}

func yamlMapFind(m *yaml.Node, key string) (*yaml.Node, int) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, -1
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1], i
		}
	}
	return nil, -1
}

func yamlMapSet(m *yaml.Node, key string, val *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	if _, idx := yamlMapFind(m, key); idx >= 0 {
		m.Content[idx+1] = val
		return
	}
	m.Content = append(m.Content, yamlStrNode(key), val)
}

func yamlMapDelete(m *yaml.Node, key string) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	if _, idx := yamlMapFind(m, key); idx >= 0 {
		m.Content = append(m.Content[:idx], m.Content[idx+2:]...)
	}
}

func yamlMapEnsureMapping(m *yaml.Node, key string) *yaml.Node {
	if v, _ := yamlMapFind(m, key); v != nil && v.Kind == yaml.MappingNode {
		return v
	}
	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	yamlMapSet(m, key, child)
	return child
}

func yamlMapEnsureSequence(m *yaml.Node, key string) *yaml.Node {
	if v, _ := yamlMapFind(m, key); v != nil && v.Kind == yaml.SequenceNode {
		return v
	}
	child := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	yamlMapSet(m, key, child)
	return child
}

func yamlStrNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

func yamlIntNode(v int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(v)}
}

func yamlBoolNode(v bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}
}

var reservedOverlayKeys = map[string]bool{
	"external-controller":      true,
	"secret":                   true,
	"authentication":           true,
	"skip-auth-prefixes":       true,
	"external-controller-tls":  true,
	"external-controller-unix": true,
	"external-controller-pipe": true,
	"external-controller-cors": true,
	"external-ui":              true,
	"external-ui-url":          true,
	"external-ui-name":         true,
	"mixed-port":               true,
	"port":                     true,
	"socks-port":               true,
	"allow-lan":                true,
	"proxy-groups":             true,
	"rule-providers":           true,
	"rules":                    true,
}

var dangerousControllerKeys = []string{
	"external-controller-tls",
	"external-controller-unix",
	"external-controller-pipe",
	"external-controller-cors",
	"external-ui",
	"external-ui-url",
	"external-ui-name",
}

func sanitizeDangerousMihomoKeys(root *yaml.Node) {
	for _, k := range dangerousControllerKeys {
		yamlMapDelete(root, k)
	}
}

func validateProxyProviderRelativePath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if strings.ContainsAny(p, "\r\n\x00\\:*?\"<>|") {
		return "", errors.New("proxy-providers 路径包含非法字符")
	}
	if strings.HasPrefix(p, "/") {
		return "", errors.New("proxy-providers 路径须为相对路径")
	}
	if strings.HasSuffix(p, "/") {
		return "", errors.New("proxy-providers 路径须指向具体文件")
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", errors.New("proxy-providers 路径不能包含上级目录跳转 (..)")
	}
	cleaned = strings.TrimPrefix(cleaned, "./")
	if (!strings.HasPrefix(cleaned, "proxies/") || len(cleaned) <= len("proxies/")) &&
		(!strings.HasPrefix(cleaned, "ruleset/") || len(cleaned) <= len("ruleset/")) {
		return "", errors.New("proxy-providers 路径必须位于 ./proxies/ 或 ./ruleset/ 目录下")
	}
	return "./" + cleaned, nil
}

func validateManagedProviderPaths(root *yaml.Node) error {
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	if rpNode, idx := yamlMapFind(root, "rule-providers"); idx >= 0 && rpNode.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(rpNode.Content); i += 2 {
			name := strings.TrimSpace(rpNode.Content[i].Value)
			entry := rpNode.Content[i+1]
			if entry.Kind != yaml.MappingNode {
				continue
			}
			pathNode, pidx := yamlMapFind(entry, "path")
			if pidx < 0 || strings.TrimSpace(pathNode.Value) == "" {
				continue
			}
			fmtVal := "yaml"
			if fmtNode, fidx := yamlMapFind(entry, "format"); fidx >= 0 && strings.TrimSpace(fmtNode.Value) != "" {
				fmtVal = strings.ToLower(strings.TrimSpace(fmtNode.Value))
				if fmtVal == "txt" {
					fmtVal = "text"
				}
			}
			cleaned, err := normalizeRuleSetRelativePath(pathNode.Value, name, fmtVal)
			if err != nil {
				return fmt.Errorf("rule-providers.%s path 无效: %v", name, err)
			}
			pathNode.Value = cleaned
		}
	}
	if ppNode, idx := yamlMapFind(root, "proxy-providers"); idx >= 0 && ppNode.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(ppNode.Content); i += 2 {
			name := strings.TrimSpace(ppNode.Content[i].Value)
			entry := ppNode.Content[i+1]
			if entry.Kind != yaml.MappingNode {
				continue
			}
			pathNode, pidx := yamlMapFind(entry, "path")
			if pidx < 0 || strings.TrimSpace(pathNode.Value) == "" {
				continue
			}
			cleaned, err := validateProxyProviderRelativePath(pathNode.Value)
			if err != nil {
				return fmt.Errorf("proxy-providers.%s: %v", name, err)
			}
			pathNode.Value = cleaned
		}
	}
	return nil
}

func mergeYAMLMappingOverlay(dst, src *yaml.Node, topLevel bool) {
	if dst == nil || dst.Kind != yaml.MappingNode || src == nil || src.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(src.Content); i += 2 {
		key := src.Content[i].Value
		if topLevel && reservedOverlayKeys[key] {
			continue
		}
		val := src.Content[i+1]
		if existing, idx := yamlMapFind(dst, key); idx >= 0 && existing.Kind == yaml.MappingNode && val.Kind == yaml.MappingNode {
			mergeYAMLMappingOverlay(existing, val, false)
			continue
		}
		yamlMapSet(dst, key, val)
	}
}
