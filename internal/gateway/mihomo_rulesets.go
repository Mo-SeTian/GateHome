package gateway

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func mihomoBuiltInRuleSetPresets() []MihomoRuleSetPreset {
	return []MihomoRuleSetPreset{
		{
			ID:          "cn-domain",
			Label:       "国内直连域名 (geosite:cn)",
			Name:        "cn-domain",
			Behavior:    "domain",
			Format:      "mrs",
			URL:         "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/cn.mrs",
			Path:        "./ruleset/cn-domain.mrs",
			Interval:    86400,
			Description: "中国大陆常用直连域名集合 (.mrs)",
		},
		{
			ID:          "cn-ip",
			Label:       "国内直连 IP (geoip:cn)",
			Name:        "cn-ip",
			Behavior:    "ipcidr",
			Format:      "mrs",
			URL:         "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geoip/cn.mrs",
			Path:        "./ruleset/cn-ip.mrs",
			Interval:    86400,
			Description: "中国大陆 IPv4/IPv6 CIDR 地址段 (.mrs)",
		},
		{
			ID:          "private-ip",
			Label:       "局域网私有 IP (private)",
			Name:        "private-ip",
			Behavior:    "ipcidr",
			Format:      "mrs",
			URL:         "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geoip/private.mrs",
			Path:        "./ruleset/private-ip.mrs",
			Interval:    86400,
			Description: "局域网、保留与内网地址段 (.mrs)",
		},
		{
			ID:          "ads",
			Label:       "广告与追踪拦截 (ads)",
			Name:        "ads",
			Behavior:    "domain",
			Format:      "mrs",
			URL:         "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/category-ads-all.mrs",
			Path:        "./ruleset/ads.mrs",
			Interval:    86400,
			Description: "常见广告、埋点与追踪域名集合 (.mrs)",
		},
		{
			ID:          "openai",
			Label:       "OpenAI / ChatGPT",
			Name:        "openai",
			Behavior:    "domain",
			Format:      "mrs",
			URL:         "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/openai.mrs",
			Path:        "./ruleset/openai.mrs",
			Interval:    86400,
			Description: "OpenAI、ChatGPT 及关联 API 域名 (.mrs)",
		},
		{
			ID:          "anthropic",
			Label:       "Anthropic / Claude",
			Name:        "anthropic",
			Behavior:    "domain",
			Format:      "mrs",
			URL:         "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/anthropic.mrs",
			Path:        "./ruleset/anthropic.mrs",
			Interval:    86400,
			Description: "Anthropic 与 Claude 相关域名 (.mrs)",
		},
		{
			ID:          "telegram-domain",
			Label:       "Telegram 域名",
			Name:        "telegram-domain",
			Behavior:    "domain",
			Format:      "mrs",
			URL:         "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/telegram.mrs",
			Path:        "./ruleset/telegram-domain.mrs",
			Interval:    86400,
			Description: "Telegram 客户端与网页端域名 (.mrs)",
		},
		{
			ID:          "telegram-ip",
			Label:       "Telegram IP 段",
			Name:        "telegram-ip",
			Behavior:    "ipcidr",
			Format:      "mrs",
			URL:         "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geoip/telegram.mrs",
			Path:        "./ruleset/telegram-ip.mrs",
			Interval:    86400,
			Description: "Telegram 数据中心 IP CIDR (.mrs)",
		},
		{
			ID:          "google",
			Label:       "Google 服务",
			Name:        "google",
			Behavior:    "domain",
			Format:      "mrs",
			URL:         "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/google.mrs",
			Path:        "./ruleset/google.mrs",
			Interval:    86400,
			Description: "Google 全系产品域名 (.mrs)",
		},
		{
			ID:          "youtube",
			Label:       "YouTube 视频",
			Name:        "youtube",
			Behavior:    "domain",
			Format:      "mrs",
			URL:         "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/youtube.mrs",
			Path:        "./ruleset/youtube.mrs",
			Interval:    86400,
			Description: "YouTube 播放与 CDN 域名 (.mrs)",
		},
		{
			ID:          "github",
			Label:       "GitHub 开发者服务",
			Name:        "github",
			Behavior:    "domain",
			Format:      "mrs",
			URL:         "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/github.mrs",
			Path:        "./ruleset/github.mrs",
			Interval:    86400,
			Description: "GitHub 及资源托管域名 (.mrs)",
		},
		{
			ID:          "netflix",
			Label:       "Netflix 流媒体",
			Name:        "netflix",
			Behavior:    "domain",
			Format:      "mrs",
			URL:         "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/netflix.mrs",
			Path:        "./ruleset/netflix.mrs",
			Interval:    86400,
			Description: "Netflix 流媒体域名 (.mrs)",
		},
		{
			ID:          "apple",
			Label:       "Apple 苹果服务",
			Name:        "apple",
			Behavior:    "domain",
			Format:      "mrs",
			URL:         "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/apple.mrs",
			Path:        "./ruleset/apple.mrs",
			Interval:    86400,
			Description: "Apple App Store 与系统更新域名 (.mrs)",
		},
		{
			ID:          "microsoft",
			Label:       "Microsoft 微软服务",
			Name:        "microsoft",
			Behavior:    "domain",
			Format:      "mrs",
			URL:         "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/microsoft.mrs",
			Path:        "./ruleset/microsoft.mrs",
			Interval:    86400,
			Description: "Microsoft / Windows / Office 域名 (.mrs)",
		},
	}
}

func findBuiltInRuleSetPreset(id string) (MihomoRuleSetPreset, bool) {
	id = strings.TrimSpace(id)
	for _, p := range mihomoBuiltInRuleSetPresets() {
		if p.ID == id {
			return p, true
		}
	}
	return MihomoRuleSetPreset{}, false
}

func ruleSetFormatExt(format string) string {
	switch format {
	case "text":
		return "txt"
	case "mrs":
		return "mrs"
	default:
		return "yaml"
	}
}

func defaultRuleSetPath(name, format string) string {
	safe := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	if safe == "" {
		safe = "ruleset"
	}
	return "./ruleset/" + safe + "." + ruleSetFormatExt(format)
}

func normalizeRuleSetRelativePath(raw, fallbackName, format string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		p = defaultRuleSetPath(fallbackName, format)
	}
	if strings.ContainsAny(p, "\r\n\x00\\:*?\"<>|") {
		return "", errors.New("规则集路径包含非法字符（如反斜杠、空字节、引号或换行）")
	}
	if strings.HasPrefix(p, "/") {
		return "", errors.New("规则集路径须为工作目录下的相对路径（如 ./ruleset/demo.mrs）")
	}
	if strings.HasSuffix(p, "/") {
		return "", errors.New("规则集路径须指向具体文件，不能为目录")
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", errors.New("规则集路径不能包含上级目录跳转 (..)")
	}
	cleaned = strings.TrimPrefix(cleaned, "./")
	if !strings.HasPrefix(cleaned, "ruleset/") || len(cleaned) <= len("ruleset/") {
		return "", errors.New("规则集路径必须位于 ./ruleset/ 目录下（如 ./ruleset/demo.mrs）")
	}
	switch strings.ToLower(path.Ext(cleaned)) {
	case ".yaml", ".yml", ".txt", ".mrs":
	default:
		return "", errors.New("规则集文件扩展名须为 .yaml、.yml、.txt 或 .mrs")
	}
	return "./" + cleaned, nil
}

func normalizeAndValidateMihomoRuleSet(rs MihomoRuleSet) (MihomoRuleSet, error) {
	rs.Name = strings.TrimSpace(rs.Name)
	rs.Source = strings.ToLower(strings.TrimSpace(rs.Source))
	rs.Preset = strings.TrimSpace(rs.Preset)
	rs.Behavior = strings.ToLower(strings.TrimSpace(rs.Behavior))
	rs.Format = strings.ToLower(strings.TrimSpace(rs.Format))
	rs.URL = strings.TrimSpace(rs.URL)
	rs.Comment = strings.TrimSpace(rs.Comment)

	if rs.Source == "" {
		rs.Source = "http"
	}
	if rs.Source == "builtin" && rs.Preset != "" {
		if preset, ok := findBuiltInRuleSetPreset(rs.Preset); ok {
			if rs.Name == "" {
				rs.Name = preset.Name
			}
			if rs.Behavior == "" {
				rs.Behavior = preset.Behavior
			}
			if rs.Format == "" {
				rs.Format = preset.Format
			}
			if rs.URL == "" {
				rs.URL = preset.URL
			}
			if strings.TrimSpace(rs.Path) == "" {
				rs.Path = preset.Path
			}
			if rs.Interval == 0 {
				rs.Interval = preset.Interval
			}
		}
	}

	switch rs.Source {
	case "http", "builtin", "manual":
	default:
		return rs, errors.New("规则集来源须为 http（远端）、builtin（内置）或 manual（手动编写）")
	}

	if rs.Name == "" || len(rs.Name) > 64 || strings.ContainsAny(rs.Name, ",/\r\n \t") {
		return rs, errors.New("规则集名称不能为空，不能包含逗号、斜杠或空白字符，且不超过 64 字符")
	}
	if strings.Contains(rs.Name, "..") {
		return rs, errors.New("规则集名称不能包含 ..")
	}

	switch rs.Behavior {
	case "domain", "ipcidr", "classical":
	default:
		return rs, errors.New("匹配模式须为 domain（域名）、ipcidr（IP 网段）或 classical（混合）")
	}

	if rs.Format == "txt" {
		rs.Format = "text"
	}
	switch rs.Format {
	case "yaml", "text", "mrs":
	default:
		return rs, errors.New("规则集格式须为 yaml、text (txt) 或 mrs")
	}

	if rs.Format == "mrs" && rs.Behavior == "classical" {
		return rs, errors.New("MRS 二进制格式仅支持 domain 或 ipcidr 匹配模式，不支持 classical 混合模式")
	}

	if rs.Interval < 0 || rs.Interval > 2592000 {
		return rs, errors.New("刷新间隔须为 0–2592000 秒（最长 30 天）")
	}

	if len(rs.Comment) > 256 || strings.ContainsAny(rs.Comment, "\r\n") {
		return rs, errors.New("规则集备注不能包含换行且不超过 256 字符")
	}

	cleanPath, err := normalizeRuleSetRelativePath(rs.Path, rs.Name, rs.Format)
	if err != nil {
		return rs, err
	}
	rs.Path = cleanPath

	if rs.Source == "http" || rs.Source == "builtin" {
		if rs.URL == "" || strings.ContainsAny(rs.URL, "\r\n") {
			return rs, errors.New("远端/内置规则集须填写规则集订阅 URL")
		}
		if err := validateMihomoSubscriptionURL(rs.URL); err != nil {
			return rs, fmt.Errorf("规则集 URL 无效: %v", err)
		}
		if rs.Interval == 0 {
			rs.Interval = 86400
		}
		rs.Content = ""
	} else {
		rs.URL = ""
		rs.Interval = 0
		if rs.Format == "text" || rs.Format == "yaml" {
			lines := normalizeManualRuleLines(rs.Content)
			if len(lines) == 0 {
				return rs, errors.New("手动编写的文本/YAML 规则集内容不能为空")
			}
			if rs.Format == "yaml" && strings.Contains(rs.Content, "payload:") {
				var doc struct {
					Payload []string `yaml:"payload"`
				}
				dec := yaml.NewDecoder(strings.NewReader(rs.Content))
				if err := dec.Decode(&doc); err != nil {
					return rs, fmt.Errorf("YAML 规则集解析失败: %v", err)
				}
				if len(doc.Payload) == 0 {
					return rs, errors.New("YAML payload 列表不能为空")
				}
			}
		}
	}

	return rs, nil
}

func normalizeManualRuleLines(content string) []string {
	rawLines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

func formatManualRuleSetFileBytes(rs MihomoRuleSet) ([]byte, error) {
	if rs.Format == "text" {
		lines := normalizeManualRuleLines(rs.Content)
		return []byte(strings.Join(lines, "\n") + "\n"), nil
	}
	if strings.Contains(rs.Content, "payload:") {
		trimmed := strings.TrimSpace(rs.Content) + "\n"
		return []byte(trimmed), nil
	}
	lines := normalizeManualRuleLines(rs.Content)
	encoded, err := encodeYAMLIndent2(map[string][]string{"payload": lines})
	if err != nil {
		return nil, err
	}
	return []byte(encoded), nil
}

func resolveRuleSetDiskPath(baseDir, relPath string) (string, error) {
	if strings.ContainsAny(relPath, "\r\n\x00\\") || strings.HasPrefix(strings.TrimSpace(relPath), "/") {
		return "", errors.New("规则集文件路径无效")
	}
	cleanRel := strings.TrimPrefix(path.Clean(strings.TrimSpace(relPath)), "./")
	if !strings.HasPrefix(cleanRel, "ruleset/") || len(cleanRel) <= len("ruleset/") || strings.Contains(cleanRel, "/../") || strings.HasPrefix(cleanRel, "../") {
		return "", errors.New("规则集文件路径必须位于 ./ruleset/ 目录下")
	}
	switch strings.ToLower(path.Ext(cleanRel)) {
	case ".yaml", ".yml", ".txt", ".mrs":
	default:
		return "", errors.New("规则集文件扩展名须为 .yaml、.yml、.txt 或 .mrs")
	}
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	rulesetRootAbs := filepath.Join(baseAbs, "ruleset")
	targetAbs, err := filepath.Abs(filepath.Join(baseAbs, filepath.FromSlash(cleanRel)))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(targetAbs, rulesetRootAbs+string(os.PathSeparator)) {
		return "", errors.New("规则集文件路径超出 ruleset 目录")
	}
	if info, err := os.Lstat(rulesetRootAbs); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errors.New("规则集根目录不能为符号链接或非目录")
		}
	}
	subParts := strings.Split(strings.TrimPrefix(cleanRel, "ruleset/"), "/")
	cur := rulesetRootAbs
	for _, seg := range subParts {
		cur = filepath.Join(cur, seg)
		if info, err := os.Lstat(cur); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", errors.New("规则集路径不允许使用符号链接")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	if realRoot, err := filepath.EvalSymlinks(rulesetRootAbs); err == nil {
		for anc := filepath.Dir(targetAbs); len(anc) >= len(rulesetRootAbs); anc = filepath.Dir(anc) {
			if _, statErr := os.Lstat(anc); statErr == nil {
				realAnc, evalErr := filepath.EvalSymlinks(anc)
				if evalErr != nil {
					return "", evalErr
				}
				if realAnc != realRoot && !strings.HasPrefix(realAnc, realRoot+string(os.PathSeparator)) {
					return "", errors.New("规则集路径解析后超出 ruleset 目录")
				}
				break
			}
			if anc == rulesetRootAbs {
				break
			}
		}
	}
	return targetAbs, nil
}

func (m *mihomoManager) writeManualRuleSetFilesLocked(cfg MihomoConfig) error {
	for _, rs := range cfg.CustomRuleSets {
		if !rs.Enabled || rs.Source != "manual" || (rs.Format != "text" && rs.Format != "yaml") {
			continue
		}
		targetPath, err := resolveRuleSetDiskPath(m.dir, rs.Path)
		if err != nil {
			return err
		}
		data, err := formatManualRuleSetFileBytes(rs)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0700); err != nil {
			return err
		}
		if err := atomicWrite(targetPath, data); err != nil {
			return err
		}
	}
	return nil
}

func removeManualRuleSetFile(baseDir string, rs MihomoRuleSet) {
	if rs.Source != "manual" || rs.Path == "" {
		return
	}
	targetPath, err := resolveRuleSetDiskPath(baseDir, rs.Path)
	if err != nil {
		return
	}
	_ = os.Remove(targetPath)
}
