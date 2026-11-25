package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const mihomoBackupFormat = "gatehome-mihomo-backup-v1"

var (
	mihomoSafeSubFilePattern    = regexp.MustCompile(`^[A-Za-z0-9_-]+\.yaml$`)
	mihomoSafeManualFilePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+\.(yaml|yml|txt)$`)
)

// safeMihomoBackupFile validates relative backup paths under "mihomo/".
// Only declarative config files, subscription YAML caches, and manual rulesets are included;
// runtime binaries, UI assets, GeoIP databases, caches, and remote rulesets are excluded.
func safeMihomoBackupFile(name string) bool {
	if !strings.HasPrefix(name, "mihomo/") || path.Clean(name) != name || strings.ContainsAny(name, "\\\x00:") {
		return false
	}
	if name == "mihomo/config.json" || name == "mihomo/runtime.yaml" {
		return true
	}
	if rel, ok := strings.CutPrefix(name, "mihomo/subscriptions/"); ok {
		return !strings.Contains(rel, "/") && !strings.Contains(rel, "..") && mihomoSafeSubFilePattern.MatchString(rel)
	}
	if rel, ok := strings.CutPrefix(name, "mihomo/ruleset/manual/"); ok {
		return !strings.Contains(rel, "/") && !strings.Contains(rel, "..") && mihomoSafeManualFilePattern.MatchString(rel)
	}
	if rel, ok := strings.CutPrefix(name, "mihomo/ruleset/"); ok {
		return !strings.Contains(rel, "/") && !strings.Contains(rel, "..") && mihomoSafeManualFilePattern.MatchString(rel)
	}
	return false
}

func mihomoFileCount(files map[string][]byte) int {
	n := 0
	for name := range files {
		if strings.HasPrefix(name, "mihomo/") {
			n++
		}
	}
	return n
}

func mihomoBackupNames(paths StoragePaths) ([]string, error) {
	root := paths.directory("mihomo")
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("Mihomo 配置目录无效")
	}

	var names []string
	manualPaths := map[string]bool{}
	for _, single := range []string{"config.json", "runtime.yaml"} {
		full := filepath.Join(root, single)
		fi, err := os.Lstat(full)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
			return nil, errors.New("Mihomo 配置文件状态异常或包含符号链接")
		}
		names = append(names, "mihomo/"+single)
		if single == "config.json" {
			if rawCfg, err := os.ReadFile(full); err == nil {
				var parsedCfg MihomoConfig
				if json.Unmarshal(rawCfg, &parsedCfg) == nil {
					for _, rs := range parsedCfg.CustomRuleSets {
						if rs.Source == "manual" && rs.Path != "" {
							rel := strings.TrimPrefix(path.Clean(filepath.ToSlash(rs.Path)), "./")
							manualPaths["mihomo/"+rel] = true
						}
					}
				}
			}
		}
	}

	walkSubdir := func(relDir string) error {
		dirPath := filepath.Join(root, filepath.FromSlash(relDir))
		di, err := os.Lstat(dirPath)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil || !di.IsDir() || di.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Mihomo 目录 %s 无效", relDir)
		}
		return filepath.WalkDir(dirPath, func(p string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if p == dirPath {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("Mihomo 目录 %s 不允许符号链接", relDir)
			}
			if entry.IsDir() {
				return fmt.Errorf("Mihomo 目录 %s 包含未预期的子目录", relDir)
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			name := "mihomo/" + filepath.ToSlash(rel)
			if !entry.Type().IsRegular() || !safeMihomoBackupFile(name) {
				return fmt.Errorf("Mihomo 目录包含不支持的备份文件: %s", name)
			}
			names = append(names, name)
			return nil
		})
	}
	if err := walkSubdir("subscriptions"); err != nil {
		return nil, err
	}
	if err := walkSubdir("ruleset/manual"); err != nil {
		return nil, err
	}
	for mp := range manualPaths {
		if strings.HasPrefix(mp, "mihomo/ruleset/manual/") || !safeMihomoBackupFile(mp) {
			continue
		}
		diskPath := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(mp, "mihomo/")))
		fi, err := os.Lstat(diskPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
			return nil, errors.New("Mihomo 手动规则集文件状态异常")
		}
		names = append(names, mp)
	}
	return names, nil
}

func validateMihomoBackupFile(name string, data []byte) error {
	if !safeMihomoBackupFile(name) {
		return errors.New("备份含不支持的 Mihomo 文件路径")
	}
	switch {
	case name == "mihomo/config.json":
		if len(data) == 0 || len(data) > 2<<20 {
			return errors.New("备份中的 mihomo/config.json 大小无效")
		}
		var cfg MihomoConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return errors.New("备份中的 mihomo/config.json 格式无效")
		}
	case name == "mihomo/runtime.yaml" || strings.HasPrefix(name, "mihomo/subscriptions/"):
		if len(data) > 10<<20 {
			return errors.New("备份中的 Mihomo YAML 文件超过 10 MiB 上限")
		}
		if len(bytes.TrimSpace(data)) > 0 {
			if err := validateMihomoYAML(data); err != nil {
				return fmt.Errorf("备份中的 %s 无效: %v", name, err)
			}
		}
	case strings.HasPrefix(name, "mihomo/ruleset/manual/"), strings.HasPrefix(name, "mihomo/ruleset/"):
		if len(data) > 4<<20 {
			return errors.New("备份中的 Mihomo 手动规则集文件过大")
		}
	}
	return nil
}

type mihomoStandaloneBackup struct {
	Format             string            `json:"format"`
	Version            string            `json:"version"`
	CreatedAt          time.Time         `json:"created_at"`
	Config             MihomoConfig      `json:"config"`
	RuntimeYAML        string            `json:"runtime_yaml,omitempty"`
	Subscriptions      map[string]string `json:"subscriptions,omitempty"`
	ManualRuleSetFiles map[string]string `json:"manual_ruleset_files,omitempty"`
}

func (m *mihomoManager) ExportStandaloneBackup() (mihomoStandaloneBackup, error) {
	m.mu.Lock()
	cfgCopy := m.cfg
	cfgCopy.Subscriptions = append([]MihomoSubscription(nil), m.cfg.Subscriptions...)
	cfgCopy.CustomGroups = append([]MihomoCustomGroup(nil), m.cfg.CustomGroups...)
	cfgCopy.CustomRules = append([]MihomoCustomRule(nil), m.cfg.CustomRules...)
	cfgCopy.CustomRuleSets = append([]MihomoRuleSet(nil), m.cfg.CustomRuleSets...)
	cfgCopy.CustomDNSPolicies = append([]MihomoDNSPolicy(nil), m.cfg.CustomDNSPolicies...)
	dir := m.dir
	rtPath := m.runtimePath()
	m.mu.Unlock()

	out := mihomoStandaloneBackup{
		Format:             mihomoBackupFormat,
		Version:            Version,
		CreatedAt:          time.Now().UTC(),
		Config:             cfgCopy,
		Subscriptions:      map[string]string{},
		ManualRuleSetFiles: map[string]string{},
	}

	if rtBytes, err := os.ReadFile(rtPath); err == nil {
		out.RuntimeYAML = string(rtBytes)
	}
	subDir := filepath.Join(dir, "subscriptions")
	if entries, err := os.ReadDir(subDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || e.Type()&os.ModeSymlink != 0 || !mihomoSafeSubFilePattern.MatchString(e.Name()) {
				continue
			}
			if data, err := os.ReadFile(filepath.Join(subDir, e.Name())); err == nil && len(data) <= 10<<20 {
				out.Subscriptions[e.Name()] = string(data)
			}
		}
	}
	manualDir := filepath.Join(dir, "ruleset", "manual")
	if entries, err := os.ReadDir(manualDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || e.Type()&os.ModeSymlink != 0 || !mihomoSafeManualFilePattern.MatchString(e.Name()) {
				continue
			}
			if data, err := os.ReadFile(filepath.Join(manualDir, e.Name())); err == nil && len(data) <= 4<<20 {
				out.ManualRuleSetFiles["ruleset/manual/"+e.Name()] = string(data)
			}
		}
	}
	for _, rs := range cfgCopy.CustomRuleSets {
		if rs.Source != "manual" || rs.Path == "" {
			continue
		}
		rel := strings.TrimPrefix(path.Clean(filepath.ToSlash(rs.Path)), "./")
		if _, exists := out.ManualRuleSetFiles[rel]; exists {
			continue
		}
		if !safeMihomoBackupFile("mihomo/" + rel) {
			continue
		}
		if data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel))); err == nil && len(data) <= 4<<20 {
			out.ManualRuleSetFiles[rel] = string(data)
		}
	}
	return out, nil
}

func (m *mihomoManager) RestoreStandaloneBackup(b mihomoStandaloneBackup, gwCfg Config, adminPort int) (map[string]int, error) {
	if b.Format != mihomoBackupFormat {
		return nil, errors.New("不支持的 Mihomo 备份文件格式")
	}

	next := b.Config
	normalizeMihomoConfig(&next)
	if next.BinaryPath != "" {
		if info, err := os.Stat(next.BinaryPath); err != nil || info.IsDir() {
			next.BinaryPath = ""
		}
	}
	if err := validateMihomoSettings(next, gwCfg, adminPort); err != nil {
		return nil, err
	}
	for i := range next.CustomGroups {
		if extra, err := normalizeMihomoExtra(next.CustomGroups[i].Extra); err != nil {
			return nil, fmt.Errorf("备份中的策略组「%s」无效: %v", next.CustomGroups[i].Name, err)
		} else {
			next.CustomGroups[i].Extra = extra
		}
		if err := validateMihomoCustomGroup(next.CustomGroups[i]); err != nil {
			return nil, fmt.Errorf("备份中的策略组「%s」无效: %v", next.CustomGroups[i].Name, err)
		}
	}
	for _, r := range next.CustomRules {
		if err := validateMihomoCustomRule(r); err != nil {
			return nil, fmt.Errorf("备份中的分流规则无效: %v", err)
		}
	}
	for i := range next.CustomRuleSets {
		rs, err := normalizeAndValidateMihomoRuleSet(next.CustomRuleSets[i])
		if err != nil {
			return nil, fmt.Errorf("备份中的规则集无效: %v", err)
		}
		next.CustomRuleSets[i] = rs
	}
	for _, p := range next.CustomDNSPolicies {
		if err := validateMihomoDNSPolicy(p); err != nil {
			return nil, fmt.Errorf("备份中的 DNS 策略无效: %v", err)
		}
	}
	for _, s := range next.Subscriptions {
		if s.URL != "" {
			if err := validateMihomoSubscriptionURL(s.URL); err != nil {
				return nil, fmt.Errorf("备份中的订阅「%s」链接无效: %v", s.Name, err)
			}
		}
		if err := validateMihomoSubscriptionFilters(s.IncludeFilter, s.ExcludeFilter, s.NodeSuffix); err != nil {
			return nil, fmt.Errorf("备份中的订阅「%s」筛选规则无效: %v", s.Name, err)
		}
	}

	for name, content := range b.Subscriptions {
		if !mihomoSafeSubFilePattern.MatchString(name) || strings.Contains(name, "..") {
			return nil, fmt.Errorf("备份含非法的订阅文件名: %s", name)
		}
		if len(content) > 10<<20 {
			return nil, fmt.Errorf("订阅文件 %s 超过 10 MiB 上限", name)
		}
		if strings.TrimSpace(content) != "" {
			if err := validateMihomoYAML([]byte(content)); err != nil {
				return nil, fmt.Errorf("订阅文件 %s YAML 无效: %v", name, err)
			}
		}
	}
	for rel, content := range b.ManualRuleSetFiles {
		normalized := strings.TrimPrefix(path.Clean(filepath.ToSlash(rel)), "./")
		if !strings.HasPrefix(normalized, "ruleset/") {
			normalized = "ruleset/manual/" + normalized
		}
		if !safeMihomoBackupFile("mihomo/" + normalized) {
			return nil, fmt.Errorf("备份含非法的手动规则集文件路径: %s", rel)
		}
		if len(content) > 4<<20 {
			return nil, fmt.Errorf("手动规则集文件 %s 超过 4 MiB 上限", rel)
		}
	}

	if next.ManualOverride {
		if strings.TrimSpace(b.RuntimeYAML) == "" {
			next.ManualOverride = false
		} else if err := validateMihomoYAML([]byte(b.RuntimeYAML)); err != nil {
			return nil, fmt.Errorf("备份中的 runtime.yaml 无效: %v", err)
		}
	}

	m.mu.Lock()
	dir := m.dir
	prev := m.cfg
	wasRunning := m.running
	portsChanged := prev.ControllerPort != next.ControllerPort ||
		prev.ControllerSecret != next.ControllerSecret ||
		prev.Settings.MixedPort != next.Settings.MixedPort ||
		prev.Settings.HTTPPort != next.Settings.HTTPPort ||
		prev.Settings.SocksPort != next.Settings.SocksPort ||
		prev.Settings.DNSListenPort != next.Settings.DNSListenPort

	subDir := filepath.Join(dir, "subscriptions")
	if err := os.MkdirAll(subDir, 0700); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	// Remove stale subscription YAML files not in the restored backup
	if existingSubs, err := os.ReadDir(subDir); err == nil {
		for _, e := range existingSubs {
			if e.IsDir() {
				continue
			}
			if _, keep := b.Subscriptions[e.Name()]; !keep && mihomoSafeSubFilePattern.MatchString(e.Name()) {
				_ = os.Remove(filepath.Join(subDir, e.Name()))
			}
		}
	}
	for name, content := range b.Subscriptions {
		if err := atomicWrite(filepath.Join(subDir, name), []byte(content)); err != nil {
			m.mu.Unlock()
			return nil, err
		}
	}

	manualDir := filepath.Join(dir, "ruleset", "manual")
	if err := os.MkdirAll(manualDir, 0700); err == nil {
		if existingManuals, err := os.ReadDir(manualDir); err == nil {
			for _, e := range existingManuals {
				if e.IsDir() {
					continue
				}
				_ = os.Remove(filepath.Join(manualDir, e.Name()))
			}
		}
	}
	for rel, content := range b.ManualRuleSetFiles {
		normalized := strings.TrimPrefix(path.Clean(filepath.ToSlash(rel)), "./")
		if !strings.HasPrefix(normalized, "ruleset/") {
			normalized = "ruleset/manual/" + normalized
		}
		diskPath := filepath.Join(dir, filepath.FromSlash(normalized))
		if err := os.MkdirAll(filepath.Dir(diskPath), 0700); err != nil {
			m.mu.Unlock()
			return nil, err
		}
		if err := atomicWrite(diskPath, []byte(content)); err != nil {
			m.mu.Unlock()
			return nil, err
		}
	}

	m.cfg = next
	if err := saveMihomoConfig(dir, next); err != nil {
		m.cfg = prev
		m.mu.Unlock()
		return nil, err
	}
	if err := m.writeManualRuleSetFilesLocked(next); err != nil {
		m.cfg = prev
		_ = saveMihomoConfig(dir, prev)
		m.mu.Unlock()
		return nil, err
	}
	if next.ManualOverride {
		finalBytes, err := injectManagedKeys([]byte(b.RuntimeYAML), next)
		if err != nil {
			m.cfg = prev
			_ = saveMihomoConfig(dir, prev)
			m.mu.Unlock()
			return nil, err
		}
		if err := atomicWrite(m.runtimePath(), finalBytes); err != nil {
			m.cfg = prev
			_ = saveMihomoConfig(dir, prev)
			m.mu.Unlock()
			return nil, err
		}
	} else {
		if err := m.synthesizeAndWriteLocked(); err != nil {
			m.cfg = prev
			_ = saveMihomoConfig(dir, prev)
			m.mu.Unlock()
			return nil, err
		}
	}
	m.mu.Unlock()

	if wasRunning {
		if portsChanged {
			_ = m.Restart()
		} else {
			_ = m.Reload()
		}
	}

	summary := map[string]int{
		"subscriptions":    len(next.Subscriptions),
		"custom_groups":    len(next.CustomGroups),
		"custom_rules":     len(next.CustomRules),
		"custom_rule_sets": len(next.CustomRuleSets),
		"custom_dns":       len(next.CustomDNSPolicies),
	}
	return summary, nil
}
