package gateway

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type mihomoYAMLValidationSummary struct {
	Proxies int `json:"proxies"`
	Groups  int `json:"groups"`
	Rules   int `json:"rules"`
}

type mihomoYAMLValidationResult struct {
	Valid         bool                        `json:"valid"`
	Error         string                      `json:"error,omitempty"`
	Warnings      []string                    `json:"warnings,omitempty"`
	CoreTested    bool                        `json:"core_tested"`
	FormattedYAML string                      `json:"formatted_yaml,omitempty"`
	Summary       mihomoYAMLValidationSummary `json:"summary"`
}

func (m *mihomoManager) ValidateYAML(raw []byte) mihomoYAMLValidationResult {
	res := mihomoYAMLValidationResult{Warnings: []string{}}
	if len(bytes.TrimSpace(raw)) == 0 {
		res.Error = "YAML 内容不能为空"
		return res
	}
	if len(raw) > 10<<20 {
		res.Error = "YAML 配置大小不能超过 10 MiB"
		return res
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		res.Error = fmt.Sprintf("YAML 语法错误: %v", err)
		return res
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		res.Error = "YAML 根节点必须为映射对象"
		return res
	}
	root := doc.Content[0]

	// Check common port fields if present.
	for _, portKey := range []string{"mixed-port", "port", "socks-port"} {
		if vNode, _ := yamlMapFind(root, portKey); vNode != nil {
			if vNode.Kind != yaml.ScalarNode {
				res.Error = fmt.Sprintf("%s 须为 0–65535 的整数", portKey)
				return res
			}
			p, err := strconv.Atoi(strings.TrimSpace(vNode.Value))
			if err != nil || p < 0 || p > 65535 {
				res.Error = fmt.Sprintf("%s 端口值无效 (%s)，须为 0–65535", portKey, vNode.Value)
				return res
			}
		}
	}

	proxySet := map[string]bool{}
	dupSeen := map[string]bool{}
	if proxiesNode, _ := yamlMapFind(root, "proxies"); proxiesNode != nil && proxiesNode.Kind == yaml.SequenceNode {
		for _, item := range proxiesNode.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			res.Summary.Proxies++
			if nameNode, _ := yamlMapFind(item, "name"); nameNode != nil {
				name := strings.TrimSpace(nameNode.Value)
				if name == "" {
					res.Error = "proxies 中存在缺少 name 的节点条目"
					return res
				}
				if proxySet[name] && !dupSeen[name] {
					dupSeen[name] = true
					res.Warnings = append(res.Warnings, fmt.Sprintf("存在重复节点名称: %s", name))
				}
				proxySet[name] = true
			}
		}
	}

	groupSet := map[string]bool{}
	if groupsNode, _ := yamlMapFind(root, "proxy-groups"); groupsNode != nil && groupsNode.Kind == yaml.SequenceNode {
		for _, item := range groupsNode.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			res.Summary.Groups++
			if nameNode, _ := yamlMapFind(item, "name"); nameNode != nil && strings.TrimSpace(nameNode.Value) != "" {
				groupSet[nameNode.Value] = true
			}
		}
		builtins := map[string]bool{"DIRECT": true, "REJECT": true, "PASS": true, "COMPATIBLE": true}
		for _, item := range groupsNode.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			groupName := "(未命名策略组)"
			if nameNode, _ := yamlMapFind(item, "name"); nameNode != nil && strings.TrimSpace(nameNode.Value) != "" {
				groupName = nameNode.Value
			}
			membersNode, _ := yamlMapFind(item, "proxies")
			if membersNode == nil || membersNode.Kind != yaml.SequenceNode {
				continue
			}
			for _, member := range membersNode.Content {
				val := strings.TrimSpace(member.Value)
				if val == "" {
					continue
				}
				if !builtins[val] && !proxySet[val] && !groupSet[val] {
					res.Error = fmt.Sprintf("策略组「%s」引用了不存在的节点或策略组「%s」", groupName, val)
					return res
				}
			}
		}
	}

	if rulesNode, _ := yamlMapFind(root, "rules"); rulesNode != nil && rulesNode.Kind == yaml.SequenceNode {
		res.Summary.Rules = len(rulesNode.Content)
	}

	formatted, err := encodeYAMLIndent2(&doc)
	if err != nil {
		res.Error = fmt.Sprintf("YAML 格式化失败: %v", err)
		return res
	}
	formattedBytes := []byte(formatted)

	m.mu.Lock()
	bin := m.resolveBinaryLocked()
	dir := m.dir
	cfgCopy := m.cfg
	m.mu.Unlock()

	if bin != "" && dir != "" {
		if absDir, err := filepath.Abs(dir); err == nil {
			_ = os.MkdirAll(absDir, 0700)
			if tmp, err := os.CreateTemp(absDir, ".validate-*.yaml"); err == nil {
				tmpPath := tmp.Name()
				_ = tmp.Close()
				defer os.Remove(tmpPath)
				testBytes, injErr := injectManagedKeys(formattedBytes, cfgCopy)
				if injErr != nil {
					res.Error = injErr.Error()
					return res
				}
				if os.WriteFile(tmpPath, testBytes, 0600) == nil {
					ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
					out, err := exec.CommandContext(ctx, bin, "-t", "-d", absDir, "-f", tmpPath).CombinedOutput()
					cancel()
					res.CoreTested = true
					if err != nil {
						msg := strings.TrimSpace(string(out))
						if msg == "" {
							msg = err.Error()
						}
						res.Error = "Mihomo 内核校验失败: " + msg
						return res
					}
				}
			}
		}
	}

	res.Valid = true
	res.FormattedYAML = maskYAMLSecret(formattedBytes)
	return res
}
