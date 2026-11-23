package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func decodeBodyLarge(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	controller := http.NewResponseController(w)
	if controller.SetReadDeadline(time.Now().Add(15*time.Second)) == nil {
		defer controller.SetReadDeadline(time.Time{})
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if decoder.Decode(v) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		apiError(w, 400, "请求 JSON 格式错误或内容过大")
		return false
	}
	return true
}

const (
	managedSecretPlaceholder = "__GATEHOME_MANAGED_SECRET__"
	managedAuthPlaceholder   = "__GATEHOME_MANAGED_AUTH__"
)

type mihomoStatusResponse struct {
	Running                bool                       `json:"running"`
	PID                    int                        `json:"pid,omitempty"`
	CoreInstalled          bool                       `json:"core_installed"`
	CoreVersion            string                     `json:"core_version,omitempty"`
	BinaryPath             string                     `json:"binary_path,omitempty"`
	ZashboardInstalled     bool                       `json:"zashboard_installed"`
	ZashboardVersion       string                     `json:"zashboard_version,omitempty"`
	LANIP                  string                     `json:"lan_ip"`
	OutboundTakeoverActive bool                       `json:"outbound_takeover_active"`
	OutboundProxyURL       string                     `json:"outbound_proxy_url,omitempty"`
	ManualOverride         bool                       `json:"manual_override"`
	LastError              string                     `json:"last_error,omitempty"`
	RecentLogs             []string                   `json:"recent_logs"`
	Enabled                bool                       `json:"enabled"`
	TakeoverOutbound       bool                       `json:"takeover_outbound"`
	ControllerPort         int                        `json:"controller_port"`
	Settings               MihomoCoreSettings         `json:"settings"`
	AuthPasswordConfigured bool                       `json:"auth_password_configured"`
	ActiveSubscriptionID   string                     `json:"active_subscription_id,omitempty"`
	Subscriptions          []MihomoSubscriptionView   `json:"subscriptions"`
	CustomGroups           []MihomoCustomGroup        `json:"custom_groups"`
	CustomRules            []MihomoCustomRule         `json:"custom_rules"`
	CustomDNSPolicies      []MihomoDNSPolicy          `json:"custom_dns_policies"`
	InheritedDNSPolicies   []MihomoInheritedDNSPolicy `json:"inherited_dns_policies"`
}

func (m *mihomoManager) statusSnapshot() mihomoStatusResponse {
	m.mu.Lock()
	defer m.mu.Unlock()

	bin := m.resolveBinaryLocked()
	ver := m.version
	if ver == "" && bin != "" {
		ver = m.probeBinaryVersion(bin)
		m.version = ver
	}
	zashInstalled, zashVersion := m.zashboardStatus()
	outboundURL := ""
	takeoverActive := false
	if m.running && m.cfg.TakeoverOutbound {
		outboundURL = activeOutboundURL(m.cfg.Settings)
		takeoverActive = outboundURL != ""
	}
	sourceRaw, _ := m.readSourceYAML(m.cfg)
	inheritedDNS := extractInheritedDNSPolicies(sourceRaw)
	sanitizedSettings := m.cfg.Settings
	authPwdConfigured := sanitizedSettings.AuthPassword != ""
	sanitizedSettings.AuthPassword = ""
	return mihomoStatusResponse{
		Running:                m.running,
		PID:                    m.pid,
		CoreInstalled:          bin != "",
		CoreVersion:            ver,
		BinaryPath:             bin,
		ZashboardInstalled:     zashInstalled,
		ZashboardVersion:       zashVersion,
		LANIP:                  detectLANIP(),
		OutboundTakeoverActive: takeoverActive,
		OutboundProxyURL:       outboundURL,
		ManualOverride:         m.cfg.ManualOverride,
		LastError:              m.lastError,
		RecentLogs:             m.logs.snapshot(),
		Enabled:                m.cfg.Enabled,
		TakeoverOutbound:       m.cfg.TakeoverOutbound,
		ControllerPort:         m.cfg.ControllerPort,
		Settings:               sanitizedSettings,
		AuthPasswordConfigured: authPwdConfigured,
		ActiveSubscriptionID:   m.cfg.ActiveSubscriptionID,
		Subscriptions:          toSubscriptionViews(m.cfg.Subscriptions),
		CustomGroups:           slices.Clone(m.cfg.CustomGroups),
		CustomRules:            slices.Clone(m.cfg.CustomRules),
		CustomDNSPolicies:      slices.Clone(m.cfg.CustomDNSPolicies),
		InheritedDNSPolicies:   inheritedDNS,
	}
}

func (a *Admin) mihomoRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/mihomo/status", a.requireAuth(a.handleMihomoStatus))
	mux.HandleFunc("PUT /api/mihomo/settings", a.requireAuth(a.handleMihomoSettings))
	mux.HandleFunc("POST /api/mihomo/start", a.requireAuth(a.handleMihomoStart))
	mux.HandleFunc("POST /api/mihomo/stop", a.requireAuth(a.handleMihomoStop))
	mux.HandleFunc("POST /api/mihomo/restart", a.requireAuth(a.handleMihomoRestart))
	mux.HandleFunc("POST /api/mihomo/reload", a.requireAuth(a.handleMihomoReload))
	mux.HandleFunc("PATCH /api/mihomo/mode", a.requireAuth(a.handleMihomoMode))

	mux.HandleFunc("GET /api/mihomo/groups", a.requireAuth(a.handleMihomoGroups))
	mux.HandleFunc("POST /api/mihomo/groups", a.requireAuth(a.handleMihomoSaveGroup))
	mux.HandleFunc("DELETE /api/mihomo/groups/{id}", a.requireAuth(a.handleMihomoDeleteGroup))
	mux.HandleFunc("PUT /api/mihomo/select", a.requireAuth(a.handleMihomoSelectProxyBody))
	mux.HandleFunc("PUT /api/mihomo/groups/{name}/select", a.requireAuth(a.handleMihomoSelectProxy))
	mux.HandleFunc("POST /api/mihomo/delay", a.requireAuth(a.handleMihomoGroupDelayBody))
	mux.HandleFunc("POST /api/mihomo/groups/{name}/delay", a.requireAuth(a.handleMihomoGroupDelay))

	mux.HandleFunc("POST /api/mihomo/rules", a.requireAuth(a.handleMihomoAddRule))
	mux.HandleFunc("PUT /api/mihomo/rules", a.requireAuth(a.handleMihomoSaveRules))
	mux.HandleFunc("DELETE /api/mihomo/rules/{id}", a.requireAuth(a.handleMihomoDeleteRule))

	mux.HandleFunc("POST /api/mihomo/dns-policies", a.requireAuth(a.handleMihomoAddDNSPolicy))
	mux.HandleFunc("PUT /api/mihomo/dns-policies", a.requireAuth(a.handleMihomoSaveDNSPolicies))
	mux.HandleFunc("DELETE /api/mihomo/dns-policies/{id}", a.requireAuth(a.handleMihomoDeleteDNSPolicy))

	mux.HandleFunc("POST /api/mihomo/subscriptions", a.requireAuth(a.handleMihomoAddSubscription))
	mux.HandleFunc("PUT /api/mihomo/subscriptions/{id}", a.requireAuth(a.handleMihomoUpdateSubscription))
	mux.HandleFunc("PUT /api/mihomo/subscriptions/{id}/activate", a.requireAuth(a.handleMihomoActivateSubscription))
	mux.HandleFunc("POST /api/mihomo/subscriptions/{id}/refresh", a.requireAuth(a.handleMihomoRefreshSubscription))
	mux.HandleFunc("DELETE /api/mihomo/subscriptions/{id}", a.requireAuth(a.handleMihomoDeleteSubscription))

	mux.HandleFunc("GET /api/mihomo/config/yaml", a.requireAuth(a.handleMihomoGetYAML))
	mux.HandleFunc("PUT /api/mihomo/config/yaml", a.requireAuth(a.handleMihomoPutYAML))
	mux.HandleFunc("POST /api/mihomo/config/validate", a.requireAuth(a.handleMihomoValidateYAML))
	mux.HandleFunc("POST /api/mihomo/config/resync", a.requireAuth(a.handleMihomoResyncYAML))

	mux.HandleFunc("POST /api/mihomo/core/download", a.requireAuth(a.handleMihomoDownloadCore))
	mux.HandleFunc("POST /api/mihomo/core/upload", a.requireAuth(a.handleMihomoUploadCore))
	mux.HandleFunc("POST /api/mihomo/zashboard/download", a.requireAuth(a.handleMihomoDownloadZashboard))
}

func (a *Admin) handleMihomoStatus(w http.ResponseWriter, r *http.Request) {
	m := a.mihomo
	m.mu.Lock()
	if _, err := os.Stat(m.runtimePath()); errors.Is(err, os.ErrNotExist) {
		_ = m.synthesizeAndWriteLocked()
	}
	m.mu.Unlock()
	jsonResponse(w, 200, m.statusSnapshot())
}

func (a *Admin) handleMihomoSettings(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled          bool               `json:"enabled"`
		TakeoverOutbound bool               `json:"takeover_outbound"`
		ControllerPort   int                `json:"controller_port"`
		Settings         MihomoCoreSettings `json:"settings"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	m := a.mihomo
	gwCfg := a.store.Snapshot().Config

	m.mu.Lock()
	next := m.cfg
	next.Enabled = input.Enabled
	next.TakeoverOutbound = input.TakeoverOutbound
	if input.ControllerPort > 0 {
		next.ControllerPort = input.ControllerPort
	}
	input.Settings.AuthUsername = strings.TrimSpace(input.Settings.AuthUsername)
	if input.Settings.AuthPassword == "" {
		input.Settings.AuthPassword = m.cfg.Settings.AuthPassword
	}
	next.Settings = input.Settings
	if next.Settings.Mode == "" {
		next.Settings.Mode = "rule"
	}
	if next.Settings.LogLevel == "" {
		next.Settings.LogLevel = "info"
	}
	if next.Settings.DNSEnhancedMode == "" {
		next.Settings.DNSEnhancedMode = "keep"
	}

	if err := validateMihomoSettings(next, gwCfg, a.adminPort); err != nil {
		m.mu.Unlock()
		apiError(w, 400, err.Error())
		return
	}

	portsOrCtrlChanged := next.ControllerPort != m.cfg.ControllerPort ||
		next.Settings.MixedPort != m.cfg.Settings.MixedPort ||
		next.Settings.HTTPPort != m.cfg.Settings.HTTPPort ||
		next.Settings.SocksPort != m.cfg.Settings.SocksPort ||
		next.Settings.DNSListenPort != m.cfg.Settings.DNSListenPort ||
		next.Settings.AllowLAN != m.cfg.Settings.AllowLAN ||
		next.Settings.AuthEnabled != m.cfg.Settings.AuthEnabled ||
		next.Settings.AuthUsername != m.cfg.Settings.AuthUsername ||
		next.Settings.AuthPassword != m.cfg.Settings.AuthPassword

	next.ManualOverride = false
	prev := m.cfg
	m.cfg = next
	if err := saveMihomoConfig(m.dir, m.cfg); err != nil {
		m.cfg = prev
		m.mu.Unlock()
		apiError(w, 500, "保存配置失败: "+err.Error())
		return
	}
	if err := m.synthesizeAndWriteLocked(); err != nil {
		m.mu.Unlock()
		apiError(w, 500, "生成 runtime.yaml 失败: "+err.Error())
		return
	}
	running := m.running
	m.mu.Unlock()

	if running {
		var applyErr error
		if portsOrCtrlChanged {
			applyErr = m.Restart()
		} else {
			applyErr = m.Reload()
		}
		if applyErr != nil {
			apiError(w, 500, "配置已保存，但内核应用失败: "+applyErr.Error())
			return
		}
	}
	jsonResponse(w, 200, m.statusSnapshot())
}

func (a *Admin) handleMihomoStart(w http.ResponseWriter, r *http.Request) {
	if err := a.mihomo.Start(); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, a.mihomo.statusSnapshot())
}

func (a *Admin) handleMihomoStop(w http.ResponseWriter, r *http.Request) {
	if err := a.mihomo.Stop(); err != nil {
		apiError(w, 500, err.Error())
		return
	}
	jsonResponse(w, 200, a.mihomo.statusSnapshot())
}

func (a *Admin) handleMihomoRestart(w http.ResponseWriter, r *http.Request) {
	if err := a.mihomo.Restart(); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, a.mihomo.statusSnapshot())
}

func (a *Admin) handleMihomoReload(w http.ResponseWriter, r *http.Request) {
	m := a.mihomo
	m.mu.Lock()
	if !m.cfg.ManualOverride {
		_ = m.synthesizeAndWriteLocked()
	}
	m.mu.Unlock()
	if err := m.Reload(); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, m.statusSnapshot())
}

func (a *Admin) handleMihomoMode(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Mode string `json:"mode"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	if err := a.mihomo.PatchMode(input.Mode); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, a.mihomo.statusSnapshot())
}

func (a *Admin) handleMihomoGroups(w http.ResponseWriter, r *http.Request) {
	groups, delays, allNodes, live, err := a.mihomo.GetGroupsAndDelays()
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	if groups == nil {
		groups = []MihomoGroupInfo{}
	}
	if allNodes == nil {
		allNodes = []string{}
	}
	jsonResponse(w, 200, map[string]any{
		"groups":    groups,
		"delays":    delays,
		"all_nodes": allNodes,
		"live":      live,
		"error":     errMsg,
	})
}

func (a *Admin) handleMihomoSaveGroup(w http.ResponseWriter, r *http.Request) {
	var input MihomoCustomGroup
	if !decodeBody(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Filter = strings.TrimSpace(input.Filter)
	input.URL = strings.TrimSpace(input.URL)
	if input.Type == "" {
		input.Type = "select"
	}
	if err := validateMihomoCustomGroup(input); err != nil {
		apiError(w, 400, err.Error())
		return
	}

	m := a.mihomo
	m.mu.Lock()
	next := m.cfg
	next.CustomGroups = slices.Clone(m.cfg.CustomGroups)
	updatedIdx := -1
	for i, existing := range next.CustomGroups {
		if input.ID != "" && existing.ID == input.ID {
			updatedIdx = i
		} else if existing.Name == input.Name {
			m.mu.Unlock()
			apiError(w, 400, fmt.Sprintf("自定义策略组「%s」已存在", input.Name))
			return
		}
	}
	if input.ID == "" || updatedIdx < 0 {
		input.ID = randomMihomoID("grp")
		next.CustomGroups = append(next.CustomGroups, input)
	} else {
		next.CustomGroups[updatedIdx] = input
	}
	next.ManualOverride = false
	if err := saveMihomoConfig(m.dir, next); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	m.cfg = next
	if err := m.synthesizeAndWriteLocked(); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	running := m.running
	m.mu.Unlock()

	if running {
		_ = m.Reload()
	}
	jsonResponse(w, 200, m.statusSnapshot())
}

func (a *Admin) handleMihomoDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		apiError(w, 400, "缺少策略组 ID")
		return
	}
	m := a.mihomo
	m.mu.Lock()
	idx := slices.IndexFunc(m.cfg.CustomGroups, func(g MihomoCustomGroup) bool { return g.ID == id })
	if idx < 0 {
		m.mu.Unlock()
		apiError(w, 404, "未找到该自定义策略组")
		return
	}
	m.cfg.CustomGroups = append(m.cfg.CustomGroups[:idx], m.cfg.CustomGroups[idx+1:]...)
	m.cfg.ManualOverride = false
	if err := saveMihomoConfig(m.dir, m.cfg); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	_ = m.synthesizeAndWriteLocked()
	running := m.running
	m.mu.Unlock()

	if running {
		_ = m.Reload()
	}
	jsonResponse(w, 200, m.statusSnapshot())
}

func (a *Admin) applySelectProxy(w http.ResponseWriter, group, node string) {
	if group == "" || node == "" {
		apiError(w, 400, "策略组名称和节点名称不能为空")
		return
	}
	if err := a.mihomo.SelectProxy(group, node); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]any{"ok": true})
}

func (a *Admin) handleMihomoSelectProxyBody(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Group string `json:"group"`
		Node  string `json:"node"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	a.applySelectProxy(w, input.Group, input.Node)
}

func (a *Admin) handleMihomoSelectProxy(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Node string `json:"node"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	a.applySelectProxy(w, r.PathValue("name"), input.Node)
}

func (a *Admin) applyGroupDelay(w http.ResponseWriter, group, testURL string, timeout int) {
	if group == "" {
		apiError(w, 400, "策略组名称不能为空")
		return
	}
	delays, err := a.mihomo.TestGroupDelay(group, testURL, timeout)
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]any{"delays": delays})
}

func (a *Admin) handleMihomoGroupDelayBody(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Group   string `json:"group"`
		URL     string `json:"url"`
		Timeout int    `json:"timeout"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	a.applyGroupDelay(w, input.Group, input.URL, input.Timeout)
}

func (a *Admin) handleMihomoGroupDelay(w http.ResponseWriter, r *http.Request) {
	var input struct {
		URL     string `json:"url"`
		Timeout int    `json:"timeout"`
	}
	if r.ContentLength > 0 {
		if !decodeBody(w, r, &input) {
			return
		}
	}
	a.applyGroupDelay(w, r.PathValue("name"), input.URL, input.Timeout)
}

func (a *Admin) handleMihomoAddRule(w http.ResponseWriter, r *http.Request) {
	var input MihomoCustomRule
	if !decodeBody(w, r, &input) {
		return
	}
	input.Type = strings.ToUpper(strings.TrimSpace(input.Type))
	input.Value = strings.TrimSpace(input.Value)
	input.Target = strings.TrimSpace(input.Target)
	input.Comment = strings.TrimSpace(input.Comment)
	if input.ID == "" {
		input.ID = randomMihomoID("rule")
	}
	if err := validateMihomoCustomRule(input); err != nil {
		apiError(w, 400, err.Error())
		return
	}

	m := a.mihomo
	m.mu.Lock()
	next := m.cfg
	next.CustomRules = append([]MihomoCustomRule{input}, m.cfg.CustomRules...)
	next.ManualOverride = false
	if err := saveMihomoConfig(m.dir, next); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	m.cfg = next
	if err := m.synthesizeAndWriteLocked(); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	running := m.running
	m.mu.Unlock()

	if running {
		_ = m.Reload()
	}
	jsonResponse(w, 200, m.statusSnapshot())
}

func (a *Admin) handleMihomoSaveRules(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Rules []MihomoCustomRule `json:"rules"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	clean := make([]MihomoCustomRule, 0, len(input.Rules))
	for _, rule := range input.Rules {
		rule.Type = strings.ToUpper(strings.TrimSpace(rule.Type))
		rule.Value = strings.TrimSpace(rule.Value)
		rule.Target = strings.TrimSpace(rule.Target)
		rule.Comment = strings.TrimSpace(rule.Comment)
		if rule.ID == "" {
			rule.ID = randomMihomoID("rule")
		}
		if err := validateMihomoCustomRule(rule); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		clean = append(clean, rule)
	}

	m := a.mihomo
	m.mu.Lock()
	next := m.cfg
	next.CustomRules = clean
	next.ManualOverride = false
	if err := saveMihomoConfig(m.dir, next); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	m.cfg = next
	if err := m.synthesizeAndWriteLocked(); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	running := m.running
	m.mu.Unlock()

	if running {
		_ = m.Reload()
	}
	jsonResponse(w, 200, m.statusSnapshot())
}

func (a *Admin) handleMihomoDeleteRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		apiError(w, 400, "缺少规则 ID")
		return
	}
	m := a.mihomo
	m.mu.Lock()
	idx := slices.IndexFunc(m.cfg.CustomRules, func(item MihomoCustomRule) bool { return item.ID == id })
	if idx < 0 {
		m.mu.Unlock()
		apiError(w, 404, "未找到该规则")
		return
	}
	m.cfg.CustomRules = append(m.cfg.CustomRules[:idx], m.cfg.CustomRules[idx+1:]...)
	m.cfg.ManualOverride = false
	if err := saveMihomoConfig(m.dir, m.cfg); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	_ = m.synthesizeAndWriteLocked()
	running := m.running
	m.mu.Unlock()

	if running {
		_ = m.Reload()
	}
	jsonResponse(w, 200, m.statusSnapshot())
}

func (a *Admin) applyDNSPoliciesMutation(w http.ResponseWriter, mutate func([]MihomoDNSPolicy) ([]MihomoDNSPolicy, int, string)) {
	m := a.mihomo
	m.mu.Lock()
	nextList, statusCode, errMsg := mutate(m.cfg.CustomDNSPolicies)
	if errMsg != "" {
		m.mu.Unlock()
		apiError(w, statusCode, errMsg)
		return
	}
	next := m.cfg
	next.CustomDNSPolicies = nextList
	next.ManualOverride = false
	if err := saveMihomoConfig(m.dir, next); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	m.cfg = next
	if err := m.synthesizeAndWriteLocked(); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	running := m.running
	m.mu.Unlock()

	if running {
		_ = m.Reload()
	}
	jsonResponse(w, 200, m.statusSnapshot())
}

func (a *Admin) handleMihomoAddDNSPolicy(w http.ResponseWriter, r *http.Request) {
	var input MihomoDNSPolicy
	if !decodeBody(w, r, &input) {
		return
	}
	input.Domain = strings.TrimSpace(input.Domain)
	input.Servers = normalizeDNSPolicyServers(input.Servers)
	if input.ID == "" {
		input.ID = randomMihomoID("dns")
	}
	if err := validateMihomoDNSPolicy(input); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	a.applyDNSPoliciesMutation(w, func(curr []MihomoDNSPolicy) ([]MihomoDNSPolicy, int, string) {
		next := append(slices.Clone(curr), input)
		return next, 0, ""
	})
}

func (a *Admin) handleMihomoSaveDNSPolicies(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Policies []MihomoDNSPolicy `json:"policies"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	clean := make([]MihomoDNSPolicy, 0, len(input.Policies))
	for _, p := range input.Policies {
		p.Domain = strings.TrimSpace(p.Domain)
		p.Servers = normalizeDNSPolicyServers(p.Servers)
		if p.ID == "" {
			p.ID = randomMihomoID("dns")
		}
		if err := validateMihomoDNSPolicy(p); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		clean = append(clean, p)
	}
	a.applyDNSPoliciesMutation(w, func([]MihomoDNSPolicy) ([]MihomoDNSPolicy, int, string) {
		return clean, 0, ""
	})
}

func (a *Admin) handleMihomoDeleteDNSPolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		apiError(w, 400, "缺少 DNS 策略 ID")
		return
	}
	a.applyDNSPoliciesMutation(w, func(curr []MihomoDNSPolicy) ([]MihomoDNSPolicy, int, string) {
		idx := slices.IndexFunc(curr, func(item MihomoDNSPolicy) bool { return item.ID == id })
		if idx < 0 {
			return nil, 404, "未找到该 DNS 策略"
		}
		next := slices.Clone(curr)
		next = append(next[:idx], next[idx+1:]...)
		return next, 0, ""
	})
}
func (a *Admin) handleMihomoAddSubscription(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name          string `json:"name"`
		URL           string `json:"url"`
		YAMLContent   string `json:"yaml_content"`
		IncludeFilter string `json:"include_filter"`
		ExcludeFilter string `json:"exclude_filter"`
		NodeSuffix    string `json:"node_suffix"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	name := strings.TrimSpace(input.Name)
	subURL := strings.TrimSpace(input.URL)
	rawYAML := strings.TrimSpace(input.YAMLContent)
	incFilter := strings.TrimSpace(input.IncludeFilter)
	excFilter := strings.TrimSpace(input.ExcludeFilter)
	nodeSuffix := input.NodeSuffix
	if name == "" || len(name) > 100 {
		apiError(w, 400, "订阅名称不能为空且不超过 100 字符")
		return
	}
	if err := validateMihomoSubscriptionFilters(incFilter, excFilter, nodeSuffix); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	if subURL == "" && rawYAML == "" {
		apiError(w, 400, "请填写订阅链接或直接粘贴 YAML 配置")
		return
	}
	if subURL != "" {
		if err := validateMihomoSubscriptionURL(subURL); err != nil {
			apiError(w, 400, err.Error())
			return
		}
	}

	m := a.mihomo
	id := randomMihomoID("sub")
	sub := MihomoSubscription{
		ID:            id,
		Name:          name,
		URL:           subURL,
		IncludeFilter: incFilter,
		ExcludeFilter: excFilter,
		NodeSuffix:    nodeSuffix,
	}

	var data []byte
	var up, down, total, exp int64
	if rawYAML != "" {
		data = []byte(rawYAML)
		if err := validateMihomoYAML(data); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		sub.UpdatedAt = time.Now()
		sub.NodeCount = countYAMLProxies(data)
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		fetched, u, d, tot, ex, err := m.fetchSubscriptionYAML(ctx, subURL)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		data, up, down, total, exp = fetched, u, d, tot, ex
		sub.UpdatedAt = time.Now()
		sub.NodeCount = countYAMLProxies(data)
		sub.Upload, sub.Download, sub.Total, sub.Expire = up, down, total, exp
	}

	subDir := filepath.Join(m.dir, "subscriptions")
	if err := os.MkdirAll(subDir, 0700); err != nil {
		apiError(w, 500, err.Error())
		return
	}
	if err := atomicWrite(m.subscriptionFile(id), data); err != nil {
		apiError(w, 500, err.Error())
		return
	}

	m.mu.Lock()
	m.cfg.Subscriptions = append(m.cfg.Subscriptions, sub)
	if m.cfg.ActiveSubscriptionID == "" {
		m.cfg.ActiveSubscriptionID = id
		m.cfg.ManualOverride = false
		_ = m.synthesizeAndWriteLocked()
	}
	if err := saveMihomoConfig(m.dir, m.cfg); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	running := m.running && m.cfg.ActiveSubscriptionID == id
	m.mu.Unlock()

	if running {
		_ = m.Reload()
	}
	jsonResponse(w, 200, m.statusSnapshot())
}

func (a *Admin) handleMihomoUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input struct {
		Name          string `json:"name"`
		URL           string `json:"url"`
		IncludeFilter string `json:"include_filter"`
		ExcludeFilter string `json:"exclude_filter"`
		NodeSuffix    string `json:"node_suffix"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	name := strings.TrimSpace(input.Name)
	subURL := strings.TrimSpace(input.URL)
	incFilter := strings.TrimSpace(input.IncludeFilter)
	excFilter := strings.TrimSpace(input.ExcludeFilter)
	nodeSuffix := input.NodeSuffix
	if name == "" || len(name) > 100 {
		apiError(w, 400, "订阅名称不能为空且不超过 100 字符")
		return
	}
	if subURL != "" {
		if err := validateMihomoSubscriptionURL(subURL); err != nil {
			apiError(w, 400, err.Error())
			return
		}
	}
	if err := validateMihomoSubscriptionFilters(incFilter, excFilter, nodeSuffix); err != nil {
		apiError(w, 400, err.Error())
		return
	}

	m := a.mihomo
	m.mu.Lock()
	idx := slices.IndexFunc(m.cfg.Subscriptions, func(s MihomoSubscription) bool { return s.ID == id })
	if idx < 0 {
		m.mu.Unlock()
		apiError(w, 404, "未找到该订阅")
		return
	}
	next := m.cfg
	next.Subscriptions = slices.Clone(m.cfg.Subscriptions)
	next.Subscriptions[idx].Name = name
	if subURL != "" {
		next.Subscriptions[idx].URL = subURL
	}
	next.Subscriptions[idx].IncludeFilter = incFilter
	next.Subscriptions[idx].ExcludeFilter = excFilter
	next.Subscriptions[idx].NodeSuffix = nodeSuffix

	isActive := next.ActiveSubscriptionID == id
	if isActive {
		next.ManualOverride = false
	}
	if err := saveMihomoConfig(m.dir, next); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	m.cfg = next
	if isActive {
		if err := m.synthesizeAndWriteLocked(); err != nil {
			m.mu.Unlock()
			apiError(w, 500, err.Error())
			return
		}
	}
	running := m.running && isActive
	m.mu.Unlock()

	if running {
		if err := m.Reload(); err != nil {
			apiError(w, 500, "订阅配置已更新，但热重载失败: "+err.Error())
			return
		}
	}
	jsonResponse(w, 200, m.statusSnapshot())
}

func (a *Admin) handleMihomoActivateSubscription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m := a.mihomo
	m.mu.Lock()
	if !slices.ContainsFunc(m.cfg.Subscriptions, func(s MihomoSubscription) bool { return s.ID == id }) {
		m.mu.Unlock()
		apiError(w, 404, "未找到该订阅")
		return
	}
	m.cfg.ActiveSubscriptionID = id
	m.cfg.ManualOverride = false
	if err := saveMihomoConfig(m.dir, m.cfg); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	if err := m.synthesizeAndWriteLocked(); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	running := m.running
	m.mu.Unlock()

	if running {
		if err := m.Reload(); err != nil {
			apiError(w, 500, "已切换订阅，但热重载失败: "+err.Error())
			return
		}
	}
	jsonResponse(w, 200, m.statusSnapshot())
}

func (a *Admin) handleMihomoRefreshSubscription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m := a.mihomo
	m.mu.Lock()
	idx := slices.IndexFunc(m.cfg.Subscriptions, func(s MihomoSubscription) bool { return s.ID == id })
	if idx < 0 {
		m.mu.Unlock()
		apiError(w, 404, "未找到该订阅")
		return
	}
	sub := m.cfg.Subscriptions[idx]
	m.mu.Unlock()

	if sub.URL == "" {
		apiError(w, 400, "本地导入配置无远程链接，无法刷新")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	data, up, down, total, exp, err := m.fetchSubscriptionYAML(ctx, sub.URL)
	if err != nil {
		m.mu.Lock()
		if idx < len(m.cfg.Subscriptions) && m.cfg.Subscriptions[idx].ID == id {
			m.cfg.Subscriptions[idx].LastError = err.Error()
			_ = saveMihomoConfig(m.dir, m.cfg)
		}
		m.mu.Unlock()
		apiError(w, 400, err.Error())
		return
	}

	subDir := filepath.Join(m.dir, "subscriptions")
	_ = os.MkdirAll(subDir, 0700)
	if err := atomicWrite(m.subscriptionFile(id), data); err != nil {
		apiError(w, 500, err.Error())
		return
	}

	m.mu.Lock()
	if idx < len(m.cfg.Subscriptions) && m.cfg.Subscriptions[idx].ID == id {
		m.cfg.Subscriptions[idx].UpdatedAt = time.Now()
		m.cfg.Subscriptions[idx].NodeCount = countYAMLProxies(data)
		m.cfg.Subscriptions[idx].LastError = ""
		m.cfg.Subscriptions[idx].Upload = up
		m.cfg.Subscriptions[idx].Download = down
		m.cfg.Subscriptions[idx].Total = total
		m.cfg.Subscriptions[idx].Expire = exp
	}
	isActive := m.cfg.ActiveSubscriptionID == id
	if isActive {
		m.cfg.ManualOverride = false
		_ = m.synthesizeAndWriteLocked()
	}
	_ = saveMihomoConfig(m.dir, m.cfg)
	running := m.running && isActive
	m.mu.Unlock()

	if running {
		_ = m.Reload()
	}
	jsonResponse(w, 200, m.statusSnapshot())
}

func (a *Admin) handleMihomoDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m := a.mihomo
	m.mu.Lock()
	idx := slices.IndexFunc(m.cfg.Subscriptions, func(s MihomoSubscription) bool { return s.ID == id })
	if idx < 0 {
		m.mu.Unlock()
		apiError(w, 404, "未找到该订阅")
		return
	}
	m.cfg.Subscriptions = append(m.cfg.Subscriptions[:idx], m.cfg.Subscriptions[idx+1:]...)
	_ = os.Remove(m.subscriptionFile(id))
	wasActive := m.cfg.ActiveSubscriptionID == id
	if wasActive {
		m.cfg.ActiveSubscriptionID = ""
		if len(m.cfg.Subscriptions) > 0 {
			m.cfg.ActiveSubscriptionID = m.cfg.Subscriptions[0].ID
		}
		m.cfg.ManualOverride = false
		_ = m.synthesizeAndWriteLocked()
	}
	_ = saveMihomoConfig(m.dir, m.cfg)
	running := m.running && wasActive
	m.mu.Unlock()

	if running {
		_ = m.Reload()
	}
	jsonResponse(w, 200, m.statusSnapshot())
}

func maskYAMLSecret(raw []byte) string {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err == nil && doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 && doc.Content[0].Kind == yaml.MappingNode {
		root := doc.Content[0]
		changed := false
		if _, idx := yamlMapFind(root, "secret"); idx >= 0 {
			root.Content[idx+1] = yamlStrNode(managedSecretPlaceholder)
			changed = true
		}
		if authNode, idx := yamlMapFind(root, "authentication"); idx >= 0 && authNode.Kind == yaml.SequenceNode {
			for _, item := range authNode.Content {
				if item.Kind != yaml.ScalarNode {
					continue
				}
				if user, _, ok := strings.Cut(item.Value, ":"); ok {
					item.Value = user + ":" + managedAuthPlaceholder
				} else {
					item.Value = managedAuthPlaceholder
				}
				item.Tag = "!!str"
				changed = true
			}
		}
		if changed {
			var out bytes.Buffer
			enc := yaml.NewEncoder(&out)
			enc.SetIndent(2)
			if enc.Encode(&doc) == nil {
				_ = enc.Close()
				return mihomoHeaderComment + out.String()
			}
		}
	}
	return string(raw)
}

func injectManagedKeys(raw []byte, cfg MihomoConfig) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("YAML 语法错误: %v", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("YAML 根节点必须为映射对象")
	}
	root := doc.Content[0]
	yamlMapSet(root, "external-controller", yamlStrNode(fmt.Sprintf("127.0.0.1:%d", cfg.ControllerPort)))
	yamlMapSet(root, "secret", yamlStrNode(cfg.ControllerSecret))
	if authNode, idx := yamlMapFind(root, "authentication"); idx >= 0 && authNode.Kind == yaml.SequenceNode {
		hasPlaceholder := false
		for _, item := range authNode.Content {
			if item.Kind == yaml.ScalarNode && (item.Value == managedAuthPlaceholder || strings.HasSuffix(item.Value, ":"+managedAuthPlaceholder)) {
				hasPlaceholder = true
				break
			}
		}
		if hasPlaceholder {
			if cfg.Settings.AuthEnabled && cfg.Settings.AuthUsername != "" && cfg.Settings.AuthPassword != "" {
				authNode.Content = []*yaml.Node{yamlStrNode(cfg.Settings.AuthUsername + ":" + cfg.Settings.AuthPassword)}
			} else {
				yamlMapDelete(root, "authentication")
			}
		}
	}

	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	_ = enc.Close()
	return out.Bytes(), nil
}

func (a *Admin) handleMihomoGetYAML(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "runtime"
	}
	m := a.mihomo
	m.mu.Lock()
	defer m.mu.Unlock()

	if kind == "source" {
		if m.cfg.ActiveSubscriptionID == "" {
			jsonResponse(w, 200, map[string]any{
				"kind":            "source",
				"content":         "# 暂无激活的机场订阅，当前使用 GateHome 冷启动默认基线\n",
				"empty":           true,
				"manual_override": m.cfg.ManualOverride,
			})
			return
		}
		raw, err := m.readSourceYAML(m.cfg)
		if err != nil {
			apiError(w, 500, "读取订阅源文件失败: "+err.Error())
			return
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			jsonResponse(w, 200, map[string]any{
				"kind":            "source",
				"content":         "# 订阅源文件为空\n",
				"empty":           true,
				"manual_override": m.cfg.ManualOverride,
			})
			return
		}
		jsonResponse(w, 200, map[string]any{
			"kind":            "source",
			"content":         string(raw),
			"empty":           false,
			"manual_override": m.cfg.ManualOverride,
		})
		return
	}

	if _, err := os.Stat(m.runtimePath()); errors.Is(err, os.ErrNotExist) {
		if err := m.synthesizeAndWriteLocked(); err != nil {
			apiError(w, 500, err.Error())
			return
		}
	}
	raw, err := os.ReadFile(m.runtimePath())
	if err != nil {
		apiError(w, 500, "读取运行配置失败: "+err.Error())
		return
	}
	jsonResponse(w, 200, map[string]any{
		"kind":            "runtime",
		"content":         maskYAMLSecret(raw),
		"empty":           false,
		"manual_override": m.cfg.ManualOverride,
	})
}

func (a *Admin) handleMihomoValidateYAML(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Content string `json:"content"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if !decodeBodyLarge(w, r, &input, 4<<20) {
		return
	}
	res := a.mihomo.ValidateYAML([]byte(input.Content))
	jsonResponse(w, 200, res)
}

func (a *Admin) handleMihomoPutYAML(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Content string `json:"content"`
	}
	// Limit manual YAML uploads up to 4 MiB via custom MaxBytesReader
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if !decodeBodyLarge(w, r, &input, 4<<20) {
		return
	}
	raw := []byte(input.Content)
	valRes := a.mihomo.ValidateYAML(raw)
	if !valRes.Valid {
		apiError(w, 400, valRes.Error)
		return
	}

	m := a.mihomo
	m.mu.Lock()
	finalYAML, err := injectManagedKeys(raw, m.cfg)
	if err != nil {
		m.mu.Unlock()
		apiError(w, 400, err.Error())
		return
	}
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	if err := atomicWrite(m.runtimePath(), finalYAML); err != nil {
		m.mu.Unlock()
		apiError(w, 500, "写入配置文件失败: "+err.Error())
		return
	}
	m.cfg.ManualOverride = true
	_ = saveMihomoConfig(m.dir, m.cfg)
	running := m.running
	m.mu.Unlock()

	if running {
		if err := m.Reload(); err != nil {
			apiError(w, 400, "文件已保存，但内核热重载失败: "+err.Error())
			return
		}
	}
	jsonResponse(w, 200, map[string]any{
		"ok":              true,
		"manual_override": true,
		"content":         maskYAMLSecret(finalYAML),
	})
}

func (a *Admin) handleMihomoResyncYAML(w http.ResponseWriter, r *http.Request) {
	m := a.mihomo
	m.mu.Lock()
	m.cfg.ManualOverride = false
	if err := saveMihomoConfig(m.dir, m.cfg); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	if err := m.synthesizeAndWriteLocked(); err != nil {
		m.mu.Unlock()
		apiError(w, 500, err.Error())
		return
	}
	raw, _ := os.ReadFile(m.runtimePath())
	running := m.running
	m.mu.Unlock()

	if running {
		if err := m.Reload(); err != nil {
			apiError(w, 500, "已重新合成，但热重载失败: "+err.Error())
			return
		}
	}
	jsonResponse(w, 200, map[string]any{
		"ok":              true,
		"manual_override": false,
		"content":         maskYAMLSecret(raw),
	})
}

func (a *Admin) handleMihomoDownloadCore(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	ver, err := a.mihomo.DownloadCore(ctx, a.store.Snapshot())
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]any{
		"ok":      true,
		"version": ver,
		"status":  a.mihomo.statusSnapshot(),
	})
}

func (a *Admin) handleMihomoUploadCore(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMihomoBinaryBytes+64<<10)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		apiError(w, 400, "上传请求无效或文件过大")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		apiError(w, 400, "请选择要上传的 mihomo 内核文件")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxMihomoBinaryBytes+1))
	if err != nil {
		apiError(w, 400, "读取上传文件失败")
		return
	}
	if int64(len(raw)) > maxMihomoBinaryBytes {
		apiError(w, 400, "内核文件不能超过 128 MiB")
		return
	}
	ver, err := a.mihomo.installBinaryData(raw)
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]any{
		"ok":      true,
		"version": ver,
		"status":  a.mihomo.statusSnapshot(),
	})
}

func (a *Admin) handleMihomoDownloadZashboard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	ver, err := a.mihomo.DownloadZashboard(ctx, a.store.Snapshot())
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]any{
		"ok":      true,
		"version": ver,
		"status":  a.mihomo.statusSnapshot(),
	})
}

func (a *Admin) handleZashboardUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.Contains(r.URL.Path, "..") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; font-src 'self' data:; connect-src 'self' ws: wss:; worker-src 'self' blob:; frame-ancestors 'self'; base-uri 'self'")

	installed, _ := a.mihomo.zashboardStatus()
	if !installed {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "<!doctype html><meta charset=\"utf-8\"><title>Zashboard 未安装</title><body style=\"font-family:system-ui,sans-serif;padding:2rem\"><h2>Zashboard 面板尚未下载</h2><p>请返回 GateHome「代理分流 → 端口与核心」下载面板后再打开。</p></body>")
		return
	}
	http.StripPrefix("/zashboard/", http.FileServer(http.Dir(a.mihomo.zashboardDir()))).ServeHTTP(w, r)
}

func (a *Admin) handleMihomoControllerProxy(w http.ResponseWriter, r *http.Request) {
	m := a.mihomo
	m.mu.Lock()
	running := m.running || m.ctrlBaseURL != ""
	ctrlURL, secret := m.controllerURLAndSecretLocked()
	m.mu.Unlock()
	if !running {
		apiError(w, 503, "mihomo 内核未运行")
		return
	}
	targetURL, err := url.Parse(ctrlURL)
	if err != nil {
		apiError(w, 500, "内部控制地址无效")
		return
	}

	trimmedPath := strings.TrimPrefix(r.URL.Path, "/api/mihomo/ctrl")
	if trimmedPath == "" {
		trimmedPath = "/"
	}
	rawEscaped := strings.TrimPrefix(r.URL.EscapedPath(), "/api/mihomo/ctrl")
	if rawEscaped == "" {
		rawEscaped = "/"
	}
	isWS := strings.EqualFold(r.Header.Get("Upgrade"), "websocket")

	proxy := &httputil.ReverseProxy{
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(targetURL)
			pr.Out.URL.Path = trimmedPath
			pr.Out.URL.RawPath = rawEscaped
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("X-Gatehouse-Request")
			if secret != "" {
				pr.Out.Header.Set("Authorization", "Bearer "+secret)
				if isWS {
					q := pr.Out.URL.Query()
					q.Set("token", secret)
					pr.Out.URL.RawQuery = q.Encode()
				}
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			if resp.StatusCode != http.StatusSwitchingProtocols {
				resp.Header.Set("Cache-Control", "no-store")
				resp.Header.Set("X-Content-Type-Options", "nosniff")
			}
			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
			apiError(rw, 502, "无法连接 Mihomo 控制接口: "+proxyErr.Error())
		},
	}
	proxy.ServeHTTP(w, r)
}
