package gateway

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Custom proxy groups, routing rules, rule-providers, DNS policies, and extra YAML overrides are stored
// once in config.json. Both per-tab editors and the unified custom.yaml view read/write this same source.

type customGroupYAML struct {
	Name      string         `yaml:"name"`
	Type      string         `yaml:"type"`
	Proxies   []string       `yaml:"proxies,omitempty"`
	Filter    string         `yaml:"filter,omitempty"`
	URL       string         `yaml:"url,omitempty"`
	Interval  int            `yaml:"interval,omitempty"`
	Tolerance int            `yaml:"tolerance,omitempty"`
	Extra     map[string]any `yaml:",inline"`
}

type customGroupsDoc struct {
	ProxyGroups []customGroupYAML `yaml:"proxy-groups"`
}

type customRuleOut struct {
	Rule    string `yaml:"rule"`
	Enabled bool   `yaml:"enabled"`
	Comment string `yaml:"comment,omitempty"`
}

type customRuleEntry struct {
	Rule    string
	Enabled bool
	Comment string
}

func (e *customRuleEntry) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		e.Rule = strings.TrimSpace(node.Value)
		e.Enabled = true
		return nil
	case yaml.MappingNode:
		var raw struct {
			Rule    string `yaml:"rule"`
			Enabled *bool  `yaml:"enabled"`
			Comment string `yaml:"comment"`
		}
		if err := node.Decode(&raw); err != nil {
			return err
		}
		e.Rule = strings.TrimSpace(raw.Rule)
		e.Enabled = raw.Enabled == nil || *raw.Enabled
		e.Comment = strings.TrimSpace(raw.Comment)
		return nil
	}
	return errors.New("规则条目须为字符串（如 DOMAIN-SUFFIX,example.com,节点选择）或含 rule 字段的映射")
}

type customRulesDoc struct {
	Rules []customRuleEntry `yaml:"rules"`
}

func encodeYAMLIndent2(v any) (string, error) {
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	_ = enc.Close()
	return out.String(), nil
}

func toCustomGroupYAML(g MihomoCustomGroup) customGroupYAML {
	extra, _ := normalizeMihomoExtra(g.Extra)
	return customGroupYAML{
		Name: g.Name, Type: g.Type, Proxies: g.Proxies, Filter: g.Filter,
		URL: g.URL, Interval: g.Interval, Tolerance: g.Tolerance,
		Extra: extra,
	}
}

func customGroupsToYAML(groups []MihomoCustomGroup) string {
	list := make([]customGroupYAML, 0, len(groups))
	for _, g := range groups {
		list = append(list, toCustomGroupYAML(g))
	}
	header := strings.Join([]string{
		"# GateHome 自定义策略组：与页面「策略组」同源，保存后合并进机场配置（同名则覆盖机场策略组）",
		"# 支持类型：select（手动选择）、url-test（自动测速）、fallback（故障转移）、load-balance（负载均衡）",
		"# 示例写法（去掉行首 # 即可生效）：",
		"# proxy-groups:",
		"#   - name: 节点选择",
		"#     type: select",
		"#     proxies: [自动测速, 香港节点, 日本节点, DIRECT]",
		"#   - name: 自动测速",
		"#     type: url-test",
		"#     filter: 香港|狮城|日本",
		"#     url: https://www.gstatic.com/generate_204",
		"#     interval: 300",
		"#     tolerance: 50",
		"#   - name: OpenAI",
		"#     type: fallback",
		"#     filter: 美国|日本|IEPL",
		"#     url: https://www.gstatic.com/generate_204",
		"#     interval: 300",
	}, "\n") + "\n"
	if len(list) == 0 {
		return header + "proxy-groups: []\n"
	}
	out, err := encodeYAMLIndent2(customGroupsDoc{ProxyGroups: list})
	if err != nil {
		return header + "proxy-groups: []\n"
	}
	return header + out
}

func validateAndBuildCustomGroups(items []customGroupYAML, existing []MihomoCustomGroup) ([]MihomoCustomGroup, error) {
	idByName := map[string]string{}
	for _, g := range existing {
		idByName[g.Name] = g.ID
	}
	seen := map[string]bool{}
	out := make([]MihomoCustomGroup, 0, len(items))
	for i, item := range items {
		extra, extraErr := normalizeMihomoExtra(item.Extra)
		if extraErr != nil {
			return nil, fmt.Errorf("第 %d 个策略组「%s」: %v", i+1, strings.TrimSpace(item.Name), extraErr)
		}
		g := MihomoCustomGroup{
			Name:      strings.TrimSpace(item.Name),
			Type:      strings.TrimSpace(item.Type),
			Proxies:   item.Proxies,
			Filter:    strings.TrimSpace(item.Filter),
			URL:       strings.TrimSpace(item.URL),
			Interval:  item.Interval,
			Tolerance: item.Tolerance,
			Extra:     extra,
		}
		if g.Type == "" {
			g.Type = "select"
		}
		if err := validateMihomoCustomGroup(g); err != nil {
			return nil, fmt.Errorf("第 %d 个策略组「%s」: %v", i+1, g.Name, err)
		}
		if seen[g.Name] {
			return nil, fmt.Errorf("策略组名称重复: %s", g.Name)
		}
		seen[g.Name] = true
		if id := idByName[g.Name]; id != "" {
			g.ID = id
		} else {
			g.ID = randomMihomoID("grp")
		}
		out = append(out, g)
	}
	return out, nil
}

func parseCustomGroupsYAML(raw []byte, existing []MihomoCustomGroup) ([]MihomoCustomGroup, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []MihomoCustomGroup{}, nil
	}
	var docNode yaml.Node
	if err := yaml.Unmarshal(raw, &docNode); err != nil {
		return nil, fmt.Errorf("YAML 解析失败: %v", err)
	}
	if docNode.Kind == 0 || len(docNode.Content) == 0 {
		return []MihomoCustomGroup{}, nil
	}
	root := docNode.Content[0]
	if root.Kind == yaml.ScalarNode && (root.Value == "" || root.Tag == "!!null") {
		return []MihomoCustomGroup{}, nil
	}
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("YAML 解析失败: 根节点必须为包含 proxy-groups 的映射对象")
	}
	var groupsNode *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := strings.TrimSpace(root.Content[i].Value)
		if key != "proxy-groups" {
			return nil, fmt.Errorf("YAML 解析失败: line %d: 仅支持 proxy-groups 字段，不支持顶层字段 %s", root.Content[i].Line, key)
		}
		groupsNode = root.Content[i+1]
	}
	if groupsNode == nil || (groupsNode.Kind == yaml.ScalarNode && (groupsNode.Value == "" || groupsNode.Tag == "!!null")) {
		return []MihomoCustomGroup{}, nil
	}
	if groupsNode.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("YAML 解析失败: line %d: proxy-groups 须为列表", groupsNode.Line)
	}
	var items []customGroupYAML
	if err := groupsNode.Decode(&items); err != nil {
		return nil, fmt.Errorf("YAML 解析失败: %v", err)
	}
	return validateAndBuildCustomGroups(items, existing)
}

func customRulesToYAML(rules []MihomoCustomRule) string {
	header := strings.Join([]string{
		"# GateHome 自定义分流规则：与页面「自定义分流」同源，自上而下优先级递减，且高于机场订阅自带规则",
		"# 条目可写成字符串，或用 {rule, enabled, comment} 映射表示停用/备注",
		"# 示例写法（去掉行首 # 即可生效）：",
		"# rules:",
		"#   - DOMAIN-SUFFIX,openai.com,OpenAI",
		"#   - DOMAIN-KEYWORD,anthropic,节点选择",
		"#   - IP-CIDR,192.168.0.0/16,DIRECT,no-resolve",
		"#   - DST-PORT,22,DIRECT",
		"#   - RULE-SET,openai,OpenAI",
		"#   - MATCH,节点选择",
		"#   - rule: DOMAIN-SUFFIX,example.com,DIRECT",
		"#     enabled: false",
		"#     comment: 临时停用规则备注",
	}, "\n") + "\n"
	if len(rules) == 0 {
		return header + "rules: []\n"
	}
	out, err := encodeYAMLIndent2(map[string]any{"rules": encodeCustomRuleItems(rules)})
	if err != nil {
		return header + "rules: []\n"
	}
	return header + out
}

func encodeCustomRuleItems(rules []MihomoCustomRule) []any {
	items := make([]any, 0, len(rules))
	for _, r := range rules {
		line := formatCustomRule(r)
		if line == "" {
			continue
		}
		if r.Enabled && strings.TrimSpace(r.Comment) == "" {
			items = append(items, line)
		} else {
			items = append(items, customRuleOut{Rule: line, Enabled: r.Enabled, Comment: strings.TrimSpace(r.Comment)})
		}
	}
	return items
}

func validateAndBuildCustomRules(entries []customRuleEntry, existing []MihomoCustomRule) ([]MihomoCustomRule, error) {
	idByLine := map[string]string{}
	for _, r := range existing {
		if line := formatCustomRule(r); line != "" {
			idByLine[line] = r.ID
		}
	}
	out := make([]MihomoCustomRule, 0, len(entries))
	for i, e := range entries {
		parts := strings.Split(e.Rule, ",")
		for j := range parts {
			parts[j] = strings.TrimSpace(parts[j])
		}
		var rule MihomoCustomRule
		if len(parts) > 0 && strings.EqualFold(parts[0], "MATCH") {
			var target string
			switch len(parts) {
			case 2:
				target = parts[1]
			case 3:
				if parts[1] != "" {
					return nil, fmt.Errorf("第 %d 条 MATCH 规则格式应为 MATCH,目标: %q", i+1, e.Rule)
				}
				target = parts[2]
			default:
				return nil, fmt.Errorf("第 %d 条 MATCH 规则格式应为 MATCH,目标: %q", i+1, e.Rule)
			}
			rule = MihomoCustomRule{
				Type:    "MATCH",
				Value:   "",
				Target:  target,
				Enabled: e.Enabled,
				Comment: e.Comment,
			}
		} else {
			if len(parts) < 3 {
				return nil, fmt.Errorf("第 %d 条规则格式应为 类型,内容,目标[,no-resolve]: %q", i+1, e.Rule)
			}
			rule = MihomoCustomRule{
				Type:    strings.ToUpper(parts[0]),
				Value:   parts[1],
				Target:  parts[2],
				Enabled: e.Enabled,
				Comment: e.Comment,
			}
			for _, extra := range parts[3:] {
				if strings.EqualFold(extra, "no-resolve") {
					rule.NoResolve = true
				} else if extra != "" {
					return nil, fmt.Errorf("第 %d 条规则含不支持的参数: %s", i+1, extra)
				}
			}
		}
		if err := validateMihomoCustomRule(rule); err != nil {
			return nil, fmt.Errorf("第 %d 条规则 %q: %v", i+1, e.Rule, err)
		}
		if id := idByLine[formatCustomRule(rule)]; id != "" {
			rule.ID = id
			delete(idByLine, formatCustomRule(rule))
		} else {
			rule.ID = randomMihomoID("rule")
		}
		out = append(out, rule)
	}
	return out, nil
}

func parseCustomRulesYAML(raw []byte, existing []MihomoCustomRule) ([]MihomoCustomRule, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []MihomoCustomRule{}, nil
	}
	var doc customRulesDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("YAML 解析失败: %v", err)
	}
	return validateAndBuildCustomRules(doc.Rules, existing)
}

func marshalYAMLNode(v any) (*yaml.Node, error) {
	b, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, err
	}
	return doc.Content[0], nil
}

func customAllToYAML(cfg MihomoConfig) string {
	header := strings.Join([]string{
		"# GateHome 自定义总配置 (custom.yaml)：展示合并机场订阅前的全部自定义配置",
		"# 与「策略组」「自定义分流」「规则集」「专线 DNS」双向同步；每次更新机场订阅均会保留此文件并重新合并",
		"# 如与机场订阅存在同名策略组、规则集、DNS 策略或其它配置键冲突，均以此配置覆盖机场订阅",
		"# 还可直接添加任意 Mihomo 覆写项（如 find-process-mode: strict / sniffer: / hosts: 等）",
	}, "\n") + "\n"

	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if strings.TrimSpace(cfg.CustomOverlayYAML) != "" {
		var overlayDoc yaml.Node
		if err := yaml.Unmarshal([]byte(cfg.CustomOverlayYAML), &overlayDoc); err == nil &&
			overlayDoc.Kind == yaml.DocumentNode && len(overlayDoc.Content) > 0 && overlayDoc.Content[0].Kind == yaml.MappingNode {
			mergeYAMLMappingOverlay(root, overlayDoc.Content[0], true)
		}
	}

	if len(cfg.CustomDNSPolicies) > 0 {
		dnsMap := yamlMapEnsureMapping(root, "dns")
		policyMap := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, p := range cfg.CustomDNSPolicies {
			domain := strings.TrimSpace(p.Domain)
			servers := normalizeDNSPolicyServers(p.Servers)
			if domain == "" || len(servers) == 0 {
				continue
			}
			if p.Enabled {
				seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
				for _, srv := range servers {
					seq.Content = append(seq.Content, yamlStrNode(srv))
				}
				yamlMapSet(policyMap, domain, seq)
			} else {
				entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
				for _, srv := range servers {
					seq.Content = append(seq.Content, yamlStrNode(srv))
				}
				yamlMapSet(entry, "servers", seq)
				yamlMapSet(entry, "enabled", yamlBoolNode(false))
				yamlMapSet(policyMap, domain, entry)
			}
		}
		yamlMapSet(dnsMap, "nameserver-policy", policyMap)
	}

	groupList := make([]customGroupYAML, 0, len(cfg.CustomGroups))
	for _, g := range cfg.CustomGroups {
		groupList = append(groupList, toCustomGroupYAML(g))
	}
	if groupsNode, err := marshalYAMLNode(groupList); err == nil {
		yamlMapSet(root, "proxy-groups", groupsNode)
	} else {
		yamlMapSet(root, "proxy-groups", &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"})
	}

	providersMap := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, rs := range cfg.CustomRuleSets {
		pNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		pType := "http"
		if rs.Source == "manual" {
			pType = "file"
		}
		yamlMapSet(pNode, "type", yamlStrNode(pType))
		yamlMapSet(pNode, "behavior", yamlStrNode(rs.Behavior))
		yamlMapSet(pNode, "format", yamlStrNode(rs.Format))
		if pType == "http" {
			yamlMapSet(pNode, "url", yamlStrNode(rs.URL))
			if rs.Interval > 0 {
				yamlMapSet(pNode, "interval", yamlIntNode(rs.Interval))
			}
		}
		yamlMapSet(pNode, "path", yamlStrNode(rs.Path))
		if !rs.Enabled {
			yamlMapSet(pNode, "enabled", yamlBoolNode(false))
		}
		yamlMapSet(providersMap, rs.Name, pNode)
	}
	yamlMapSet(root, "rule-providers", providersMap)

	ruleItems := encodeCustomRuleItems(cfg.CustomRules)
	if rulesNode, err := marshalYAMLNode(ruleItems); err == nil {
		yamlMapSet(root, "rules", rulesNode)
	} else {
		yamlMapSet(root, "rules", &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"})
	}

	out, err := encodeYAMLIndent2(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}})
	if err != nil {
		return header + "proxy-groups: []\nrule-providers: {}\nrules: []\n"
	}
	return header + out
}

func parseCustomRuleProvidersNode(node *yaml.Node, existing []MihomoRuleSet, baseDir string) ([]MihomoRuleSet, error) {
	if node == nil || (node.Kind == yaml.ScalarNode && (node.Value == "" || node.Tag == "!!null")) {
		return []MihomoRuleSet{}, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, errors.New("rule-providers 须为键值对映射对象")
	}
	existingByName := map[string]MihomoRuleSet{}
	for _, rs := range existing {
		existingByName[rs.Name] = rs
	}
	seen := map[string]bool{}
	out := make([]MihomoRuleSet, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := strings.TrimSpace(node.Content[i].Value)
		valNode := node.Content[i+1]
		if valNode.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("rule-providers.%s 须为映射对象", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("规则集名称重复: %s", name)
		}
		seen[name] = true

		var raw struct {
			Type     string   `yaml:"type"`
			Behavior string   `yaml:"behavior"`
			Format   string   `yaml:"format"`
			URL      string   `yaml:"url"`
			Path     string   `yaml:"path"`
			Interval int      `yaml:"interval"`
			Enabled  *bool    `yaml:"enabled"`
			Comment  string   `yaml:"comment"`
			Payload  []string `yaml:"payload"`
		}
		if err := valNode.Decode(&raw); err != nil {
			return nil, fmt.Errorf("rule-providers.%s 解析失败: %v", name, err)
		}
		prev, hasPrev := existingByName[name]
		rsType := strings.ToLower(strings.TrimSpace(raw.Type))
		source := "http"
		preset := ""
		content := ""
		comment := strings.TrimSpace(raw.Comment)
		if hasPrev && comment == "" {
			comment = prev.Comment
		}
		if rsType == "file" {
			source = "manual"
			if len(raw.Payload) > 0 {
				content = strings.Join(raw.Payload, "\n")
			} else if hasPrev && prev.Source == "manual" {
				content = prev.Content
			}
		} else if rsType == "" || rsType == "http" {
			if hasPrev && prev.Source == "builtin" && prev.URL == strings.TrimSpace(raw.URL) {
				source = "builtin"
				preset = prev.Preset
			}
		} else {
			return nil, fmt.Errorf("rule-providers.%s type 须为 http 或 file", name)
		}

		fmtVal := strings.ToLower(strings.TrimSpace(raw.Format))
		if fmtVal == "" {
			fmtVal = "yaml"
		}
		if fmtVal == "txt" {
			fmtVal = "text"
		}
		behaviorVal := strings.ToLower(strings.TrimSpace(raw.Behavior))
		if behaviorVal == "" {
			behaviorVal = "domain"
		}

		rs := MihomoRuleSet{
			Name:     name,
			Source:   source,
			Preset:   preset,
			Behavior: behaviorVal,
			Format:   fmtVal,
			URL:      strings.TrimSpace(raw.URL),
			Path:     strings.TrimSpace(raw.Path),
			Interval: raw.Interval,
			Content:  content,
			Enabled:  raw.Enabled == nil || *raw.Enabled,
			Comment:  comment,
		}
		if hasPrev && rs.Source == "manual" && rs.Content == "" && (rs.Format == "text" || rs.Format == "yaml") && baseDir != "" {
			normPath, pathErr := normalizeRuleSetRelativePath(rs.Path, rs.Name, rs.Format)
			if pathErr == nil {
				if diskPath, err := resolveRuleSetDiskPath(baseDir, normPath); err == nil {
					if b, err := os.ReadFile(diskPath); err == nil {
						rs.Content = string(b)
					}
				}
			}
		}
		if rs.Source == "manual" && rs.Content == "" && (rs.Format == "text" || rs.Format == "yaml") {
			return nil, fmt.Errorf("手动规则集「%s」需在「规则集」页面填写内容、在 YAML 中提供 payload 列表，或指向已存在的本地文件", name)
		}

		cleanRS, err := normalizeAndValidateMihomoRuleSet(rs)
		if err != nil {
			return nil, fmt.Errorf("规则集「%s」: %v", name, err)
		}
		if hasPrev && prev.ID != "" {
			cleanRS.ID = prev.ID
		} else {
			cleanRS.ID = randomMihomoID("rset")
		}
		out = append(out, cleanRS)
	}
	return out, nil
}

func parseCustomDNSPolicyNode(node *yaml.Node, existing []MihomoDNSPolicy) ([]MihomoDNSPolicy, error) {
	if node == nil || (node.Kind == yaml.ScalarNode && (node.Value == "" || node.Tag == "!!null")) {
		return []MihomoDNSPolicy{}, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, errors.New("dns.nameserver-policy 须为映射对象")
	}
	idByDomain := map[string]string{}
	for _, p := range existing {
		idByDomain[p.Domain] = p.ID
	}
	out := make([]MihomoDNSPolicy, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		domain := strings.TrimSpace(node.Content[i].Value)
		valNode := node.Content[i+1]
		var servers []string
		enabled := true
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
		case yaml.MappingNode:
			var raw struct {
				Servers []string `yaml:"servers"`
				Enabled *bool    `yaml:"enabled"`
			}
			if err := valNode.Decode(&raw); err != nil {
				return nil, fmt.Errorf("dns.nameserver-policy.%s 解析失败: %v", domain, err)
			}
			servers = normalizeDNSPolicyServers(raw.Servers)
			enabled = raw.Enabled == nil || *raw.Enabled
		default:
			return nil, fmt.Errorf("dns.nameserver-policy.%s 格式无效", domain)
		}
		p := MihomoDNSPolicy{
			Domain:  domain,
			Servers: normalizeDNSPolicyServers(servers),
			Enabled: enabled,
		}
		if err := validateMihomoDNSPolicy(p); err != nil {
			return nil, fmt.Errorf("DNS 策略「%s」: %v", domain, err)
		}
		if id := idByDomain[p.Domain]; id != "" {
			p.ID = id
		} else {
			p.ID = randomMihomoID("dns")
		}
		out = append(out, p)
	}
	return out, nil
}

func parseCustomAllYAML(raw []byte, existing MihomoConfig, baseDir string) (MihomoConfig, error) {
	next := existing
	if len(bytes.TrimSpace(raw)) == 0 {
		next.CustomGroups = []MihomoCustomGroup{}
		next.CustomRules = []MihomoCustomRule{}
		next.CustomRuleSets = []MihomoRuleSet{}
		next.CustomDNSPolicies = []MihomoDNSPolicy{}
		next.CustomOverlayYAML = ""
		return next, nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return next, fmt.Errorf("YAML 解析失败: %v", err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		next.CustomGroups = []MihomoCustomGroup{}
		next.CustomRules = []MihomoCustomRule{}
		next.CustomRuleSets = []MihomoRuleSet{}
		next.CustomDNSPolicies = []MihomoDNSPolicy{}
		next.CustomOverlayYAML = ""
		return next, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return next, errors.New("YAML 根节点必须为映射对象")
	}

	for _, forbidden := range []string{
		"external-controller", "secret", "authentication", "skip-auth-prefixes",
		"external-controller-tls", "external-controller-unix", "external-controller-pipe", "external-controller-cors",
		"external-ui", "external-ui-url", "external-ui-name",
		"mixed-port", "port", "socks-port", "allow-lan",
	} {
		if _, idx := yamlMapFind(root, forbidden); idx >= 0 {
			return next, fmt.Errorf("%s 由 GateHome 安全托管，请勿在 custom.yaml 中修改", forbidden)
		}
	}

	if groupsNode, idx := yamlMapFind(root, "proxy-groups"); idx >= 0 {
		if groupsNode.Kind == yaml.ScalarNode && (groupsNode.Value == "" || groupsNode.Tag == "!!null") {
			next.CustomGroups = []MihomoCustomGroup{}
		} else if groupsNode.Kind != yaml.SequenceNode {
			return next, errors.New("proxy-groups 须为列表")
		} else {
			var items []customGroupYAML
			if err := groupsNode.Decode(&items); err != nil {
				return next, fmt.Errorf("proxy-groups 解析失败: %v", err)
			}
			groups, err := validateAndBuildCustomGroups(items, existing.CustomGroups)
			if err != nil {
				return next, err
			}
			next.CustomGroups = groups
		}
	} else {
		next.CustomGroups = []MihomoCustomGroup{}
	}

	if rulesNode, idx := yamlMapFind(root, "rules"); idx >= 0 {
		if rulesNode.Kind == yaml.ScalarNode && (rulesNode.Value == "" || rulesNode.Tag == "!!null") {
			next.CustomRules = []MihomoCustomRule{}
		} else if rulesNode.Kind != yaml.SequenceNode {
			return next, errors.New("rules 须为列表")
		} else {
			var entries []customRuleEntry
			if err := rulesNode.Decode(&entries); err != nil {
				return next, fmt.Errorf("rules 解析失败: %v", err)
			}
			rules, err := validateAndBuildCustomRules(entries, existing.CustomRules)
			if err != nil {
				return next, err
			}
			next.CustomRules = rules
		}
	} else {
		next.CustomRules = []MihomoCustomRule{}
	}

	if rpNode, idx := yamlMapFind(root, "rule-providers"); idx >= 0 {
		rsets, err := parseCustomRuleProvidersNode(rpNode, existing.CustomRuleSets, baseDir)
		if err != nil {
			return next, err
		}
		next.CustomRuleSets = rsets
	} else {
		next.CustomRuleSets = []MihomoRuleSet{}
	}

	overlayRoot := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		val := root.Content[i+1]
		if key == "proxy-groups" || key == "rules" || key == "rule-providers" {
			continue
		}
		if key == "dns" {
			if val.Kind == yaml.ScalarNode && (val.Value == "" || val.Tag == "!!null") {
				next.CustomDNSPolicies = []MihomoDNSPolicy{}
				continue
			}
			if val.Kind != yaml.MappingNode {
				return next, errors.New("dns 须为映射对象")
			}
			if polNode, pidx := yamlMapFind(val, "nameserver-policy"); pidx >= 0 {
				policies, err := parseCustomDNSPolicyNode(polNode, existing.CustomDNSPolicies)
				if err != nil {
					return next, err
				}
				next.CustomDNSPolicies = policies
			} else {
				next.CustomDNSPolicies = []MihomoDNSPolicy{}
			}
			dnsRest := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			for j := 0; j+1 < len(val.Content); j += 2 {
				if val.Content[j].Value == "nameserver-policy" {
					continue
				}
				dnsRest.Content = append(dnsRest.Content, val.Content[j], val.Content[j+1])
			}
			if len(dnsRest.Content) > 0 {
				yamlMapSet(overlayRoot, "dns", dnsRest)
			}
			continue
		}
		yamlMapSet(overlayRoot, key, val)
	}
	if _, idx := yamlMapFind(root, "dns"); idx < 0 {
		next.CustomDNSPolicies = []MihomoDNSPolicy{}
	}

	if len(overlayRoot.Content) > 0 {
		out, err := encodeYAMLIndent2(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{overlayRoot}})
		if err != nil {
			return next, fmt.Errorf("自定义覆写配置序列化失败: %v", err)
		}
		next.CustomOverlayYAML = out
	} else {
		next.CustomOverlayYAML = ""
	}
	return next, nil
}

func (a *Admin) handleMihomoGetCustomYAML(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	m := a.mihomo
	m.mu.Lock()
	cfgSnapshot := m.cfg
	groups := append([]MihomoCustomGroup(nil), m.cfg.CustomGroups...)
	rules := append([]MihomoCustomRule(nil), m.cfg.CustomRules...)
	m.mu.Unlock()
	switch kind {
	case "groups":
		jsonResponse(w, 200, map[string]any{"kind": kind, "content": customGroupsToYAML(groups)})
	case "rules":
		jsonResponse(w, 200, map[string]any{"kind": kind, "content": customRulesToYAML(rules)})
	case "all":
		jsonResponse(w, 200, map[string]any{"kind": kind, "content": customAllToYAML(cfgSnapshot)})
	default:
		apiError(w, 404, "未知的配置类型")
	}
}

func (a *Admin) handleMihomoPutCustomYAML(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if kind != "groups" && kind != "rules" && kind != "all" {
		apiError(w, 404, "未知的配置类型")
		return
	}
	var input struct {
		Content      string `json:"content"`
		ValidateOnly bool   `json:"validate_only"`
	}
	if !decodeBodyLarge(w, r, &input, 2<<20) {
		return
	}

	m := a.mihomo
	m.mu.Lock()
	prev := m.cfg
	next := m.cfg
	var parseErr error
	switch kind {
	case "groups":
		var groups []MihomoCustomGroup
		groups, parseErr = parseCustomGroupsYAML([]byte(input.Content), m.cfg.CustomGroups)
		next.CustomGroups = groups
	case "rules":
		var rules []MihomoCustomRule
		rules, parseErr = parseCustomRulesYAML([]byte(input.Content), m.cfg.CustomRules)
		next.CustomRules = rules
	case "all":
		next, parseErr = parseCustomAllYAML([]byte(input.Content), m.cfg, m.dir)
	}
	if parseErr != nil {
		m.mu.Unlock()
		apiError(w, 400, parseErr.Error())
		return
	}
	// Dry-run the merge so a config that would break runtime.yaml is rejected before saving.
	source, _ := m.readSourceYAML(next)
	if _, err := synthesizeMihomoYAML(source, next); err != nil {
		m.mu.Unlock()
		apiError(w, 400, "合并机场配置失败: "+err.Error())
		return
	}
	if input.ValidateOnly {
		m.mu.Unlock()
		jsonResponse(w, 200, map[string]any{"ok": true, "valid": true})
		return
	}
	next.ManualOverride = false
	if err := saveMihomoConfig(m.dir, next); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	m.cfg = next
	if err := m.synthesizeAndWriteLocked(); err != nil {
		m.cfg = prev
		_ = saveMihomoConfig(m.dir, prev)
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	if kind == "all" {
		keptPaths := map[string]bool{}
		for _, rs := range next.CustomRuleSets {
			if rs.Source == "manual" {
				keptPaths[rs.Path] = true
			}
		}
		for _, oldRS := range prev.CustomRuleSets {
			if oldRS.Source == "manual" && !keptPaths[oldRS.Path] {
				removeManualRuleSetFile(m.dir, oldRS)
			}
		}
	}
	running := m.running
	m.mu.Unlock()
	if running {
		_ = m.Reload()
	}

	var content string
	switch kind {
	case "rules":
		content = customRulesToYAML(next.CustomRules)
	case "all":
		content = customAllToYAML(next)
	default:
		content = customGroupsToYAML(next.CustomGroups)
	}
	jsonResponse(w, 200, map[string]any{"ok": true, "content": content, "status": m.statusSnapshot()})
}
