package gateway

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"
)

// Custom proxy groups and routing rules are stored once (config.json) and can be edited either through
// the form UI or as Mihomo-style YAML; both views read and write this same single source of truth.

type customGroupYAML struct {
	Name      string   `yaml:"name"`
	Type      string   `yaml:"type"`
	Proxies   []string `yaml:"proxies,omitempty"`
	Filter    string   `yaml:"filter,omitempty"`
	URL       string   `yaml:"url,omitempty"`
	Interval  int      `yaml:"interval,omitempty"`
	Tolerance int      `yaml:"tolerance,omitempty"`
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

func customGroupsToYAML(groups []MihomoCustomGroup) string {
	list := make([]customGroupYAML, 0, len(groups))
	for _, g := range groups {
		list = append(list, customGroupYAML{
			Name: g.Name, Type: g.Type, Proxies: g.Proxies, Filter: g.Filter,
			URL: g.URL, Interval: g.Interval, Tolerance: g.Tolerance,
		})
	}
	header := "# GateHome 自定义策略组：与页面「策略组」同源，保存后合并进机场配置（同名则覆盖机场策略组）\n"
	if len(list) == 0 {
		return header + "proxy-groups: []\n"
	}
	out, err := encodeYAMLIndent2(customGroupsDoc{ProxyGroups: list})
	if err != nil {
		return header + "proxy-groups: []\n"
	}
	return header + out
}

func parseCustomGroupsYAML(raw []byte, existing []MihomoCustomGroup) ([]MihomoCustomGroup, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []MihomoCustomGroup{}, nil
	}
	var doc customGroupsDoc
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return []MihomoCustomGroup{}, nil
		}
		return nil, fmt.Errorf("YAML 解析失败: %v", err)
	}
	idByName := map[string]string{}
	for _, g := range existing {
		idByName[g.Name] = g.ID
	}
	seen := map[string]bool{}
	out := make([]MihomoCustomGroup, 0, len(doc.ProxyGroups))
	for i, item := range doc.ProxyGroups {
		g := MihomoCustomGroup{
			Name:      strings.TrimSpace(item.Name),
			Type:      strings.TrimSpace(item.Type),
			Proxies:   item.Proxies,
			Filter:    strings.TrimSpace(item.Filter),
			URL:       strings.TrimSpace(item.URL),
			Interval:  item.Interval,
			Tolerance: item.Tolerance,
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

func customRulesToYAML(rules []MihomoCustomRule) string {
	header := "# GateHome 自定义分流规则：与页面「自定义分流」同源，自上而下优先级递减，且高于机场订阅自带规则\n# 条目可写成字符串，或用 {rule, enabled, comment} 映射表示停用/备注\n"
	if len(rules) == 0 {
		return header + "rules: []\n"
	}
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
	out, err := encodeYAMLIndent2(map[string]any{"rules": items})
	if err != nil {
		return header + "rules: []\n"
	}
	return header + out
}

func parseCustomRulesYAML(raw []byte, existing []MihomoCustomRule) ([]MihomoCustomRule, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []MihomoCustomRule{}, nil
	}
	var doc customRulesDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("YAML 解析失败: %v", err)
	}
	idByLine := map[string]string{}
	for _, r := range existing {
		if line := formatCustomRule(r); line != "" {
			idByLine[line] = r.ID
		}
	}
	out := make([]MihomoCustomRule, 0, len(doc.Rules))
	for i, e := range doc.Rules {
		parts := strings.Split(e.Rule, ",")
		for j := range parts {
			parts[j] = strings.TrimSpace(parts[j])
		}
		if len(parts) < 3 {
			return nil, fmt.Errorf("第 %d 条规则格式应为 类型,内容,目标[,no-resolve]: %q", i+1, e.Rule)
		}
		rule := MihomoCustomRule{
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

func (a *Admin) handleMihomoGetCustomYAML(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	m := a.mihomo
	m.mu.Lock()
	groups := append([]MihomoCustomGroup(nil), m.cfg.CustomGroups...)
	rules := append([]MihomoCustomRule(nil), m.cfg.CustomRules...)
	m.mu.Unlock()
	switch kind {
	case "groups":
		jsonResponse(w, 200, map[string]any{"kind": kind, "content": customGroupsToYAML(groups)})
	case "rules":
		jsonResponse(w, 200, map[string]any{"kind": kind, "content": customRulesToYAML(rules)})
	default:
		apiError(w, 404, "未知的配置类型")
	}
}

func (a *Admin) handleMihomoPutCustomYAML(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if kind != "groups" && kind != "rules" {
		apiError(w, 404, "未知的配置类型")
		return
	}
	var input struct {
		Content      string `json:"content"`
		ValidateOnly bool   `json:"validate_only"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if !decodeBodyLarge(w, r, &input, 1<<20) {
		return
	}

	m := a.mihomo
	m.mu.Lock()
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
	prev := m.cfg
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
	running := m.running
	m.mu.Unlock()
	if running {
		_ = m.Reload()
	}

	content := customGroupsToYAML(next.CustomGroups)
	if kind == "rules" {
		content = customRulesToYAML(next.CustomRules)
	}
	jsonResponse(w, 200, map[string]any{"ok": true, "content": content, "status": m.statusSnapshot()})
}
