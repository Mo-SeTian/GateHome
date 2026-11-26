package gateway

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestMihomoColdStartAndMergeEngine(t *testing.T) {
	cfg := defaultMihomoConfig()
	cfg.ControllerPort = 19190
	cfg.ControllerSecret = "TEST_ONLY_CTRL_SECRET"

	cold, err := synthesizeMihomoYAML(nil, cfg)
	if err != nil {
		t.Fatalf("cold start synthesize failed: %v", err)
	}
	var parsedCold map[string]any
	if err := yaml.Unmarshal(cold, &parsedCold); err != nil {
		t.Fatalf("unmarshal cold start yaml: %v", err)
	}
	if parsedCold["mixed-port"] != 7890 {
		t.Fatalf("expected mixed-port 7890, got %v", parsedCold["mixed-port"])
	}
	if _, hasHTTP := parsedCold["port"]; hasHTTP {
		t.Fatalf("expected disabled http port key deleted, got %v", parsedCold["port"])
	}
	if parsedCold["external-controller"] != "127.0.0.1:19190" {
		t.Fatalf("unexpected external-controller: %v", parsedCold["external-controller"])
	}
	if parsedCold["secret"] != "TEST_ONLY_CTRL_SECRET" {
		t.Fatalf("unexpected secret: %v", parsedCold["secret"])
	}

	// Now merge an airport source.yaml preserving unknown fields while applying layers 2, 3, 4.
	sourceYAML := []byte(`
mixed-port: 9999
port: 9998
socks-port: 9997
allow-lan: false
mode: direct
ipv6: false
sniffer:
  enable: true
  sniff:
    HTTP:
      ports: [80, 8080]
proxy-providers:
  airport-main:
    type: http
    url: https://example.test/provider
dns:
  enable: true
  ipv6: false
  nameserver:
    - 1.1.1.1
proxies:
  - { name: "🇭🇰 香港 01", type: ss, server: 1.2.3.4, port: 443 }
  - { name: "🇭🇰 香港 02", type: ss, server: 1.2.3.5, port: 443 }
  - { name: "🇺🇸 美国 01", type: ss, server: 1.2.3.6, port: 443 }
  - { name: "🇯🇵 日本 01", type: ss, server: 1.2.3.7, port: 443 }
proxy-groups:
  - name: 节点选择
    type: select
    proxies:
      - 🇺🇸 美国 01
rules:
  - DOMAIN-SUFFIX,example.com,节点选择
  - MATCH,节点选择
`)

	cfg.Settings = MihomoCoreSettings{
		MixedPort:       0, // disable mixed-port, verify deletion
		HTTPPort:        17891,
		SocksPort:       17892,
		AllowLAN:        true,
		Mode:            "rule",
		LogLevel:        "warning",
		IPv6:            true,
		UnifiedDelay:    true,
		TCPConcurrent:   false,
		DNSEnhancedMode: "redir-host",
	}
	cfg.CustomGroups = []MihomoCustomGroup{
		{
			ID:        "grp-hk",
			Name:      "香港自动",
			Type:      "url-test",
			Filter:    "香港|HK",
			URL:       "https://www.gstatic.com/generate_204",
			Interval:  120,
			Tolerance: 50,
		},
		{
			ID:      "grp-empty",
			Name:    "空过滤组",
			Type:    "select",
			Filter:  "不存在的区域",
			Proxies: nil,
		},
		{
			ID:      "grp-replace",
			Name:    "节点选择",
			Type:    "select",
			Proxies: []string{"香港自动", "🇯🇵 日本 01", "DIRECT"},
		},
	}
	cfg.CustomRules = []MihomoCustomRule{
		{
			ID:      "r1",
			Type:    "DOMAIN-SUFFIX",
			Value:   "openai.com",
			Target:  "香港自动",
			Enabled: true,
		},
		{
			ID:        "r2",
			Type:      "IP-CIDR",
			Value:     "10.10.0.0/16",
			Target:    "DIRECT",
			NoResolve: true,
			Enabled:   true,
		},
		{
			ID:      "r3",
			Type:    "DOMAIN",
			Value:   "disabled.example",
			Target:  "REJECT",
			Enabled: false,
		},
	}

	out, err := synthesizeMihomoYAML(sourceYAML, cfg)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	var merged map[string]any
	if err := yaml.Unmarshal(out, &merged); err != nil {
		t.Fatalf("unmarshal merged yaml: %v", err)
	}

	if _, hasMixed := merged["mixed-port"]; hasMixed {
		t.Fatalf("expected mixed-port removed when 0, got %v", merged["mixed-port"])
	}
	if merged["port"] != 17891 || merged["socks-port"] != 17892 {
		t.Fatalf("expected custom ports 17891/17892, got port=%v socks=%v", merged["port"], merged["socks-port"])
	}
	if merged["ipv6"] != true || merged["allow-lan"] != true || merged["mode"] != "rule" || merged["log-level"] != "warning" {
		t.Fatalf("core overrides not applied: %v", merged)
	}
	// Unknown top-level blocks preserved
	if merged["sniffer"] == nil || merged["proxy-providers"] == nil {
		t.Fatalf("expected sniffer and proxy-providers preserved")
	}
	// DNS overrides & preserved nameserver
	dns, _ := merged["dns"].(map[string]any)
	if dns["ipv6"] != true || dns["enhanced-mode"] != "redir-host" {
		t.Fatalf("dns overrides mismatch: %v", dns)
	}
	if ns, _ := dns["nameserver"].([]any); len(ns) != 1 || ns[0] != "1.1.1.1" {
		t.Fatalf("expected airport dns.nameserver preserved, got %v", dns["nameserver"])
	}

	// Custom groups
	groups, allNodes := extractYAMLGroupsAndNodes(out, cfg.CustomGroups)
	if len(allNodes) != 4 {
		t.Fatalf("expected 4 proxy nodes, got %d", len(allNodes))
	}
	groupByName := map[string]MihomoGroupInfo{}
	for _, g := range groups {
		groupByName[g.Name] = g
	}
	hk, ok := groupByName["香港自动"]
	if !ok || len(hk.All) != 2 || hk.All[0] != "🇭🇰 香港 01" || hk.All[1] != "🇭🇰 香港 02" {
		t.Fatalf("regex filter custom group mismatch: %+v", hk)
	}
	emptyGrp := groupByName["空过滤组"]
	if len(emptyGrp.All) != 1 || emptyGrp.All[0] != "DIRECT" {
		t.Fatalf("expected empty custom group fallback to DIRECT, got %+v", emptyGrp)
	}
	replaced := groupByName["节点选择"]
	if len(replaced.All) != 3 || replaced.All[0] != "香港自动" {
		t.Fatalf("expected replaced 节点选择 group, got %+v", replaced)
	}

	// Custom rules prepended cleanly
	rules, _ := merged["rules"].([]any)
	if len(rules) != 4 {
		t.Fatalf("expected 4 rules (2 enabled custom + 2 source), got %v", rules)
	}
	if rules[0] != "DOMAIN-SUFFIX,openai.com,香港自动" || rules[1] != "IP-CIDR,10.10.0.0/16,DIRECT,no-resolve" {
		t.Fatalf("custom rules order/format mismatch: %v", rules)
	}
}

func TestMihomoPortValidationAndConflicts(t *testing.T) {
	gw := DefaultConfig()
	gw.SunPanel.Enabled = true
	gw.SunPanel.Port = 16680
	gw.Forwards = []PortForward{
		{ID: "f1", Name: "ssh", Enabled: true, Protocol: "tcp", ListenPort: 2222, Upstream: "127.0.0.1:22"},
		{ID: "f2", Name: "udp-only", Enabled: true, Protocol: "udp", ListenPort: 5353, Upstream: "127.0.0.1:53"},
	}

	validCfg := defaultMihomoConfig()
	if err := validateMihomoSettings(validCfg, gw, 16666); err != nil {
		t.Fatalf("expected default valid, got %v", err)
	}

	// All three ports 0
	bad := validCfg
	bad.Settings.MixedPort, bad.Settings.HTTPPort, bad.Settings.SocksPort = 0, 0, 0
	if validateMihomoSettings(bad, gw, 16666) == nil {
		t.Fatal("expected error when all three ports are 0")
	}

	// Duplicate ports between mixed and http
	bad = validCfg
	bad.Settings.MixedPort, bad.Settings.HTTPPort = 7890, 7890
	if validateMihomoSettings(bad, gw, 16666) == nil {
		t.Fatal("expected error when mixed_port == http_port")
	}

	// Duplicate with controller port
	bad = validCfg
	bad.Settings.SocksPort = bad.ControllerPort
	if validateMihomoSettings(bad, gw, 16666) == nil {
		t.Fatal("expected error when socks_port == controller_port")
	}

	// Collision with GateHome adminPort
	bad = validCfg
	bad.Settings.MixedPort = 16666
	if validateMihomoSettings(bad, gw, 16666) == nil {
		t.Fatal("expected error colliding with adminPort")
	}

	// Collision with enabled ProxyGroup HTTPPort (18080)
	bad = validCfg
	bad.Settings.HTTPPort = 18080
	if validateMihomoSettings(bad, gw, 16666) == nil {
		t.Fatal("expected error colliding with proxy group port")
	}

	// Collision with enabled TCP Forward (2222)
	bad = validCfg
	bad.Settings.SocksPort = 2222
	if validateMihomoSettings(bad, gw, 16666) == nil {
		t.Fatal("expected error colliding with TCP forward port")
	}

	// UDP Forward port should NOT collide with Mihomo TCP listener
	okUDP := validCfg
	okUDP.Settings.SocksPort = 5353
	if err := validateMihomoSettings(okUDP, gw, 16666); err != nil {
		t.Fatalf("UDP forward port should not block TCP mihomo port: %v", err)
	}

	// Collision with SunPanel port (16680)
	bad = validCfg
	bad.Settings.MixedPort = 16680
	if validateMihomoSettings(bad, gw, 16666) == nil {
		t.Fatal("expected error colliding with SunPanel port")
	}
}

func TestMihomoOutboundThreeTierPriority(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "DIRECT_HIT")
	}))
	defer target.Close()

	var tier1Calls, tier2Calls atomic.Int32
	tier1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tier1Calls.Add(1)
		_, _ = io.WriteString(w, "TIER1_MIHOMO")
	}))
	defer tier1.Close()

	tier2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tier2Calls.Add(1)
		_, _ = io.WriteString(w, "TIER2_SYSTEM")
	}))
	defer tier2.Close()

	var tier1URL atomic.Value
	tier1URL.Store(tier1.URL)
	restore := SetMihomoOutboundResolver(func() string {
		if v, ok := tier1URL.Load().(string); ok {
			return v
		}
		return ""
	})
	defer restore()

	readBody := func(s State) string {
		c := outboundClient(s, 2*time.Second)
		defer closeClient(c)
		resp, err := c.Get(target.URL)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}

	// Case 1: Both Tier 1 and Tier 2 active -> Tier 1 wins
	s := State{Config: DefaultConfig()}
	s.Config.OutboundProxy = OutboundProxyConfig{Enabled: true, URL: tier2.URL}
	if got := readBody(s); got != "TIER1_MIHOMO" {
		t.Fatalf("expected TIER1_MIHOMO, got %q", got)
	}
	if tier1Calls.Load() != 1 || tier2Calls.Load() != 0 {
		t.Fatalf("unexpected proxy hit counts: t1=%d t2=%d", tier1Calls.Load(), tier2Calls.Load())
	}

	// Case 2: Tier 1 stops -> falls back to Tier 2
	tier1URL.Store("")
	if got := readBody(s); got != "TIER2_SYSTEM" {
		t.Fatalf("expected TIER2_SYSTEM, got %q", got)
	}
	if tier2Calls.Load() != 1 {
		t.Fatalf("expected tier2Calls == 1, got %d", tier2Calls.Load())
	}

	// Case 3: Both off -> direct target
	s.Config.OutboundProxy.Enabled = false
	if got := readBody(s); got != "DIRECT_HIT" {
		t.Fatalf("expected DIRECT_HIT, got %q", got)
	}
}

func TestMihomoControllerClientAndActions(t *testing.T) {
	const testSecret = "SECRET_CTRL_KEY"
	var selectedNode string
	var patchedMode string
	var reloadPath string

	mux := http.NewServeMux()
	authCheck := func(r *http.Request) bool {
		return r.Header.Get("Authorization") == "Bearer "+testSecret
	}
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		if !authCheck(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		jsonResponse(w, 200, map[string]string{"version": "Meta v1.19.0"})
	})
	mux.HandleFunc("GET /proxies", func(w http.ResponseWriter, r *http.Request) {
		if !authCheck(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		jsonResponse(w, 200, map[string]any{
			"proxies": map[string]any{
				"节点选择": map[string]any{
					"name": "节点选择",
					"type": "Selector",
					"now":  "香港 01",
					"all":  []string{"香港 01", "日本 01", "DIRECT"},
				},
				"香港 01": map[string]any{
					"name":    "香港 01",
					"type":    "Shadowsocks",
					"history": []map[string]any{{"delay": 68}},
				},
			},
		})
	})
	mux.HandleFunc("PUT /proxies/{name}", func(w http.ResponseWriter, r *http.Request) {
		if !authCheck(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		selectedNode = r.PathValue("name") + "->" + body.Name
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /group/{name}/delay", func(w http.ResponseWriter, r *http.Request) {
		if !authCheck(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		jsonResponse(w, 200, map[string]int{"香港 01": 45, "日本 01": 89})
	})
	mux.HandleFunc("PATCH /configs", func(w http.ResponseWriter, r *http.Request) {
		if !authCheck(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		var body struct {
			Mode string `json:"mode"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		patchedMode = body.Mode
		w.WriteHeader(204)
	})
	mux.HandleFunc("PUT /configs", func(w http.ResponseWriter, r *http.Request) {
		if !authCheck(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		var body struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		reloadPath = body.Path
		w.WriteHeader(204)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	mgr := &mihomoManager{
		dir:         dir,
		cfg:         defaultMihomoConfig(),
		ctrlBaseURL: srv.URL,
		ctrlSecret:  testSecret,
	}
	ver, err := fetchMihomoControllerVersion(mihomoLocalClient(time.Second), srv.URL, testSecret)
	if err != nil || ver != "Meta v1.19.0" {
		t.Fatalf("unexpected controller version: ver=%q err=%v", ver, err)
	}

	groups, delays, allNodes, live, err := mgr.GetGroupsAndDelays()
	if err != nil || !live || len(groups) != 1 || groups[0].Now != "香港 01" || delays["香港 01"] != 68 || len(allNodes) != 1 {
		t.Fatalf("GetGroupsAndDelays mismatch: groups=%+v delays=%+v nodes=%v live=%v err=%v", groups, delays, allNodes, live, err)
	}

	if err := mgr.SelectProxy("节点选择", "日本 01"); err != nil {
		t.Fatalf("SelectProxy failed: %v", err)
	}
	if selectedNode != "节点选择->日本 01" {
		t.Fatalf("unexpected selectedNode: %q", selectedNode)
	}

	tested, err := mgr.TestGroupDelay("节点选择", "", 0)
	if err != nil || tested["香港 01"] != 45 || tested["日本 01"] != 89 {
		t.Fatalf("TestGroupDelay mismatch: tested=%+v err=%v", tested, err)
	}

	mgr.running = true
	if err := mgr.PatchMode("global"); err != nil {
		t.Fatalf("PatchMode failed: %v", err)
	}
	if patchedMode != "global" {
		t.Fatalf("expected global mode, got %q", patchedMode)
	}
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if !strings.HasSuffix(reloadPath, "runtime.yaml") {
		t.Fatalf("unexpected reload path: %q", reloadPath)
	}
}

func TestMihomoHelperProcess(t *testing.T) {
	if os.Getenv("GATEHOME_MIHOMO_HELPER") != "1" {
		return
	}
	for _, arg := range os.Args {
		if arg == "-v" {
			fmt.Println("Mihomo Meta v1.19.0 test")
			os.Exit(0)
		}
		if arg == "-t" {
			os.Exit(0)
		}
	}
	runtimePath := ""
	for i := 0; i < len(os.Args)-1; i++ {
		if os.Args[i] == "-f" {
			runtimePath = os.Args[i+1]
		}
	}
	raw, err := os.ReadFile(runtimePath)
	if err != nil {
		os.Exit(2)
	}
	var parsed struct {
		Controller string `yaml:"external-controller"`
		Secret     string `yaml:"secret"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		os.Exit(3)
	}
	ln, err := net.Listen("tcp", parsed.Controller)
	if err != nil {
		os.Exit(4)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+parsed.Secret {
			http.Error(w, "unauthorized", 401)
			return
		}
		jsonResponse(w, 200, map[string]string{"version": "Mihomo Meta v1.19.0 test"})
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
	<-sig
	_ = srv.Close()
	os.Exit(0)
}

func TestMihomoSubprocessLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell wrapper helper only on unix")
	}
	testBin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(binDir, "mihomo")
	script := fmt.Sprintf("#!/bin/sh\nGATEHOME_MIHOMO_HELPER=1 exec %q -test.run=^TestMihomoHelperProcess$ -- \"$@\"\n", testBin)
	if err := os.WriteFile(wrapper, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	// Pick free ephemeral ports so the test never collides in CI.
	freePort := func() int {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		p := l.Addr().(*net.TCPAddr).Port
		l.Close()
		return p
	}

	cfg := defaultMihomoConfig()
	cfg.ControllerPort = freePort()
	cfg.Settings.MixedPort = freePort()
	cfg.TakeoverOutbound = true

	mgr := &mihomoManager{
		dir: dir,
		cfg: cfg,
	}
	if err := mgr.Start(); err != nil {
		t.Fatalf("mgr.Start failed: %v", err)
	}
	defer func() { _ = mgr.Stop() }()

	if !mgr.running || mgr.pid == 0 || !strings.Contains(mgr.version, "Mihomo") {
		t.Fatalf("unexpected manager running state: running=%v pid=%d version=%q", mgr.running, mgr.pid, mgr.version)
	}
	if got := mgr.OutboundProxyURL(); got != fmt.Sprintf("http://127.0.0.1:%d", cfg.Settings.MixedPort) {
		t.Fatalf("unexpected outbound proxy url: %q", got)
	}
	if listeners := mgr.ActiveListeners(); len(listeners) != 2 {
		t.Fatalf("expected 2 active listeners (mixed + controller), got %+v", listeners)
	}

	if err := mgr.Stop(); err != nil {
		t.Fatalf("mgr.Stop failed: %v", err)
	}
	if mgr.running || mgr.OutboundProxyURL() != "" {
		t.Fatalf("expected stopped state after Stop()")
	}
}

func TestMihomoAdminAPIEndpoints(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	// Unauthenticated request rejected
	if adminRequest(h, "GET", "/api/mihomo/status", nil, nil, "").Code != 401 {
		t.Fatal("expected 401 without auth cookie")
	}

	// Initial status & masked secret check
	rec := adminRequest(h, "GET", "/api/mihomo/status", nil, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("GET /api/mihomo/status failed: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "controller_secret") {
		t.Fatal("controller_secret must never be serialized to API response")
	}

	// Configure 3 independent ports + IPv6 + TakeoverOutbound
	settingsPayload := map[string]any{
		"enabled":           true,
		"takeover_outbound": true,
		"controller_port":   19199,
		"settings": map[string]any{
			"mixed_port":        17890,
			"http_port":         17891,
			"socks_port":        17892,
			"allow_lan":         true,
			"mode":              "rule",
			"log_level":         "info",
			"ipv6":              false,
			"unified_delay":     true,
			"tcp_concurrent":    true,
			"dns_enhanced_mode": "fake-ip",
		},
	}
	rec = adminRequest(h, "PUT", "/api/mihomo/settings", settingsPayload, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("PUT /api/mihomo/settings failed: %d %s", rec.Code, rec.Body.String())
	}

	// Import local YAML subscription
	subYAML := `
proxies:
  - { name: "HK Node", type: ss, server: 1.2.3.4, port: 443 }
  - { name: "US Node", type: ss, server: 1.2.3.5, port: 443 }
proxy-groups:
  - name: "机场默认"
    type: select
    proxies: ["HK Node", "US Node"]
rules:
  - "MATCH,机场默认"
`
	rec = adminRequest(h, "POST", "/api/mihomo/subscriptions", map[string]any{
		"name":         "本地测试订阅",
		"yaml_content": subYAML,
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("POST /api/mihomo/subscriptions failed: %d %s", rec.Code, rec.Body.String())
	}
	var st mihomoStatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if len(st.Subscriptions) != 1 || st.Subscriptions[0].NodeCount != 2 || st.ActiveSubscriptionID == "" {
		t.Fatalf("unexpected subscription state: %+v", st.Subscriptions)
	}

	// Add custom group
	rec = adminRequest(h, "POST", "/api/mihomo/groups", map[string]any{
		"name":     "HK 专线",
		"type":     "url-test",
		"filter":   "HK",
		"interval": 180,
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("POST /api/mihomo/groups failed: %d %s", rec.Code, rec.Body.String())
	}

	// Add custom rule
	rec = adminRequest(h, "POST", "/api/mihomo/rules", map[string]any{
		"type":    "DOMAIN-SUFFIX",
		"value":   "anthropic.com",
		"target":  "HK 专线",
		"enabled": true,
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("POST /api/mihomo/rules failed: %d %s", rec.Code, rec.Body.String())
	}

	// Inspect synthesized runtime.yaml (secret masked) vs source.yaml (raw subscription)
	rec = adminRequest(h, "GET", "/api/mihomo/config/yaml?kind=runtime", nil, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("GET runtime yaml failed: %d", rec.Code)
	}
	var yamlRes struct {
		Kind           string `json:"kind"`
		Content        string `json:"content"`
		ManualOverride bool   `json:"manual_override"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &yamlRes)
	if !strings.Contains(yamlRes.Content, managedSecretPlaceholder) {
		t.Fatal("expected secret masked with managed placeholder in API YAML response")
	}
	if strings.Contains(yamlRes.Content, a.mihomo.cfg.ControllerSecret) {
		t.Fatal("real controller secret leaked in YAML API response")
	}
	if !strings.Contains(yamlRes.Content, "17890") || !strings.Contains(yamlRes.Content, "17891") || !strings.Contains(yamlRes.Content, "17892") ||
		!strings.Contains(yamlRes.Content, "DOMAIN-SUFFIX,anthropic.com,HK 专线") || !strings.Contains(yamlRes.Content, "HK 专线") {
		t.Fatalf("synthesized runtime.yaml missing expected layers:\n%s", yamlRes.Content)
	}

	rec = adminRequest(h, "GET", "/api/mihomo/config/yaml?kind=source", nil, cookie, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "机场默认") {
		t.Fatalf("expected source.yaml content, got %d %s", rec.Code, rec.Body.String())
	}

	// Manual YAML edit sets manual_override = true and preserves real secret on disk
	manualContent := "mixed-port: 27890\nmode: rule\nproxies: []\nproxy-groups: [{name: 节点选择, type: select, proxies: [DIRECT]}]\nrules: [MATCH,DIRECT]\n"
	rec = adminRequest(h, "PUT", "/api/mihomo/config/yaml", map[string]any{"content": manualContent}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("PUT runtime yaml failed: %d %s", rec.Code, rec.Body.String())
	}
	diskBytes, _ := os.ReadFile(a.mihomo.runtimePath())
	if !strings.Contains(string(diskBytes), a.mihomo.cfg.ControllerSecret) {
		t.Fatal("expected real controller secret injected into disk runtime.yaml")
	}

	// Resync clears manual_override and restores synthesized 5-layer YAML
	rec = adminRequest(h, "POST", "/api/mihomo/config/resync", map[string]any{}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("POST resync failed: %d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &yamlRes)
	if yamlRes.ManualOverride || !strings.Contains(yamlRes.Content, "17890") {
		t.Fatalf("expected manual_override cleared and layers restored: %+v", yamlRes)
	}
}

func TestMihomoSelectAssetAndGzipUpload(t *testing.T) {
	assets := []githubReleaseAsset{
		{Name: "mihomo-linux-amd64-v1.19.0.gz", URL: "https://github.com/1"},
		{Name: "mihomo-linux-amd64-compatible-v1.19.0.gz", URL: "https://github.com/2"},
		{Name: "mihomo-darwin-arm64-v1.19.0.gz", URL: "https://github.com/3"},
	}
	a, ok := selectMihomoAsset(assets, "linux", "amd64")
	if !ok || a.Name != "mihomo-linux-amd64-compatible-v1.19.0.gz" {
		t.Fatalf("expected linux amd64 compatible asset, got %+v ok=%v", a, ok)
	}
	a, ok = selectMihomoAsset(assets, "darwin", "arm64")
	if !ok || a.Name != "mihomo-darwin-arm64-v1.19.0.gz" {
		t.Fatalf("expected darwin arm64 asset, got %+v ok=%v", a, ok)
	}

	if runtime.GOOS != "windows" {
		dir := t.TempDir()
		mgr := &mihomoManager{dir: dir, cfg: defaultMihomoConfig()}
		padding := strings.Repeat("# padding\n", 120)
		script := "#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo \"Mihomo Meta v1.19.0\"; exit 0; fi\n" + padding
		if _, err := mgr.installBinaryData([]byte(script)); err == nil {
			t.Fatalf("expected shebang script upload to be rejected by installBinaryData")
		}

		// Compile a real native executable binary to test gzipped binary installation end-to-end.
		srcDir := t.TempDir()
		srcFile := filepath.Join(srcDir, "main.go")
		outBin := filepath.Join(srcDir, "mihomo-fake")
		code := "package main\nimport \"fmt\"\nfunc main() { fmt.Println(\"Mihomo Meta v1.19.0\") }\n"
		if err := os.WriteFile(srcFile, []byte(code), 0600); err != nil {
			t.Fatalf("write fake binary source: %v", err)
		}
		goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
		buildCmd := exec.Command(goTool, "build", "-o", outBin, srcFile)
		buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := buildCmd.CombinedOutput(); err != nil {
			t.Fatalf("compile fake binary failed: %v\n%s", err, string(out))
		}
		binBytes, err := os.ReadFile(outBin)
		if err != nil {
			t.Fatalf("read compiled fake binary: %v", err)
		}
		var gzBuf bytes.Buffer
		zw := gzip.NewWriter(&gzBuf)
		_, _ = zw.Write(binBytes)
		_ = zw.Close()

		ver, err := mgr.installBinaryData(gzBuf.Bytes())
		if err != nil || !strings.Contains(ver, "Mihomo Meta") {
			t.Fatalf("installBinaryData gzip failed: ver=%q err=%v", ver, err)
		}
		if _, err := os.Stat(mgr.managedBinaryPath()); err != nil {
			t.Fatalf("expected installed binary on disk: %v", err)
		}
	}
}

func TestMihomoAirportDNSKeepAndIncrementalPolicy(t *testing.T) {
	cfg := defaultMihomoConfig()
	if cfg.Settings.DNSEnhancedMode != "keep" {
		t.Fatalf("expected default DNSEnhancedMode == keep, got %q", cfg.Settings.DNSEnhancedMode)
	}
	cold := buildColdStartYAML(cfg)
	if !strings.Contains(cold, "enhanced-mode: fake-ip") {
		t.Fatalf("expected cold-start fallback fake-ip even when setting is keep:\n%s", cold)
	}

	airportSource := []byte(`
dns:
  enable: true
  ipv6: true
  enhanced-mode: fake-ip
  use-hosts: true
  fake-ip-range: 11.18.0.1/16
  nameserver: [119.29.29.29, 223.5.5.5, 'https://dns.alidns.com/dns-query']
  default-nameserver: [119.29.29.29, 223.5.5.5, 114.114.114.114]
  nameserver-policy:
    cdn-p1-hk.bblink88.com: ['dn-hk.appzhuanyong.com:1066', 'https://dn-jp.appzhuanyong.com/dns-query']
    ieplqz.bblink88.com: ['dn-hk.appzhuanyong.com:1066']
    cdn-p1-sg.bblink88.com: ['dn-hk.appzhuanyong.com:1066']
    cdn-p1-jp.bblink88.com: ['dn-hk.appzhuanyong.com:1066']
    cdn-p1-us.bblink88.com: ['dn-hk.appzhuanyong.com:1066']
    cdn-p1-lm.bblink88.com: ['dn-hk.appzhuanyong.com:1066']
    cdn-p1-kr.bblink88.com: ['dn-hk.appzhuanyong.com:1066']
    cdn-p1-tw.bblink88.com: ['dn-hk.appzhuanyong.com:1066']
proxies:
  - { name: "专线 HK", type: ss, server: cdn-p1-hk.bblink88.com, port: 443 }
proxy-groups:
  - name: "节点选择"
    type: select
    proxies: ["专线 HK"]
rules:
  - "MATCH,节点选择"
`)

	inherited := extractInheritedDNSPolicies(airportSource)
	if len(inherited) != 8 {
		t.Fatalf("expected 8 inherited DNS policies, got %d: %+v", len(inherited), inherited)
	}

	cfg.Settings.DNSListenPort = 17853
	cfg.CustomDNSPolicies = []MihomoDNSPolicy{
		{ID: "d1", Domain: "+.corp.internal", Servers: []string{"10.0.0.53", "tcp://10.0.0.54"}, Enabled: true},
		{ID: "d2", Domain: "ieplqz.bblink88.com", Servers: []string{"9.9.9.9"}, Enabled: true},
		{ID: "d3", Domain: "disabled.example", Servers: []string{"8.8.8.8"}, Enabled: false},
	}

	mergedBytes, err := synthesizeMihomoYAML(airportSource, cfg)
	if err != nil {
		t.Fatalf("synthesize airport dns failed: %v", err)
	}
	var merged map[string]any
	if err := yaml.Unmarshal(mergedBytes, &merged); err != nil {
		t.Fatal(err)
	}
	dnsMap, _ := merged["dns"].(map[string]any)
	if dnsMap["use-hosts"] != true || dnsMap["fake-ip-range"] != "11.18.0.1/16" || dnsMap["enhanced-mode"] != "fake-ip" {
		t.Fatalf("airport dns settings altered under keep mode: %+v", dnsMap)
	}
	if _, hasProxyServerNS := dnsMap["proxy-server-nameserver"]; hasProxyServerNS {
		t.Fatal("proxy-server-nameserver must NOT be injected automatically")
	}
	if dnsMap["listen"] != "0.0.0.0:17853" {
		t.Fatalf("expected dns.listen 0.0.0.0:17853, got %v", dnsMap["listen"])
	}
	if defNS, _ := dnsMap["default-nameserver"].([]any); len(defNS) != 3 {
		t.Fatalf("expected default-nameserver preserved, got %v", dnsMap["default-nameserver"])
	}
	policyMap, _ := dnsMap["nameserver-policy"].(map[string]any)
	if len(policyMap) != 9 {
		t.Fatalf("expected 9 nameserver-policy entries (7 preserved + 1 overridden + 1 added), got %d: %+v", len(policyMap), policyMap)
	}
	if _, exists := policyMap["cdn-p1-hk.bblink88.com"]; !exists {
		t.Fatal("airport cdn-p1-hk.bblink88.com policy was lost")
	}
	if _, exists := policyMap["disabled.example"]; exists {
		t.Fatal("disabled custom dns policy must not appear in runtime.yaml")
	}
	if corp, _ := policyMap["+.corp.internal"].([]any); len(corp) != 2 || corp[0] != "10.0.0.53" {
		t.Fatalf("unexpected custom +.corp.internal policy: %v", policyMap["+.corp.internal"])
	}

	// Verify loopback bind when AllowLAN is false.
	cfg.Settings.AllowLAN = false
	loopBytes, err := synthesizeMihomoYAML(airportSource, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(loopBytes), "listen: 127.0.0.1:17853") {
		t.Fatalf("expected loopback listen binding when AllowLAN=false:\n%s", string(loopBytes))
	}
}

func TestMihomoDNSListenPortValidationAndListeners(t *testing.T) {
	gw := DefaultConfig()
	gw.Forwards = []PortForward{
		{ID: "f-udp", Name: "dns-udp", Enabled: true, Protocol: "udp", ListenPort: 5353, Upstream: "127.0.0.1:53"},
	}

	cfg := defaultMihomoConfig()
	cfg.Settings.DNSListenPort = 5353
	if validateMihomoSettings(cfg, gw, 16666) == nil {
		t.Fatal("expected DNSListenPort to conflict with enabled UDP forward port")
	}
	cfg.Settings.DNSListenPort = 7890
	if validateMihomoSettings(cfg, gw, 16666) == nil {
		t.Fatal("expected DNSListenPort to conflict with MixedPort")
	}
	cfg.Settings.DNSListenPort = 17853
	if err := validateMihomoSettings(cfg, gw, 16666); err != nil {
		t.Fatalf("expected valid DNSListenPort, got %v", err)
	}

	mgr := &mihomoManager{cfg: cfg, running: true}
	listeners := mgr.ActiveListeners()
	var sawUDP, sawTCP bool
	for _, l := range listeners {
		if l.Port == 17853 && l.Protocol == "UDP" {
			sawUDP = true
		}
		if l.Port == 17853 && l.Protocol == "TCP" {
			sawTCP = true
		}
	}
	if !sawUDP || !sawTCP {
		t.Fatalf("expected both UDP and TCP DNS listeners reported when DNSListenPort active, got %+v", listeners)
	}
}

func TestMihomoDNSPolicyAPIEndpoints(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	subYAML := `
dns:
  enable: true
  enhanced-mode: fake-ip
  nameserver-policy:
    'node.example.com': ['119.29.29.29', 'https://dns.pub/dns-query']
proxies:
  - { name: "N1", type: ss, server: node.example.com, port: 443 }
proxy-groups:
  - name: "节点选择"
    type: select
    proxies: ["N1"]
rules:
  - "MATCH,节点选择"
`
	rec := adminRequest(h, "POST", "/api/mihomo/subscriptions", map[string]any{
		"name":         "专线订阅",
		"yaml_content": subYAML,
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("add subscription failed: %d %s", rec.Code, rec.Body.String())
	}

	var st mihomoStatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if len(st.InheritedDNSPolicies) != 1 || st.InheritedDNSPolicies[0].Domain != "node.example.com" {
		t.Fatalf("expected inherited_dns_policies in status response, got %+v", st.InheritedDNSPolicies)
	}

	// Add custom DNS policy
	rec = adminRequest(h, "POST", "/api/mihomo/dns-policies", map[string]any{
		"domain":  "+.lan.example",
		"servers": []string{"192.168.1.1"},
		"enabled": true,
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("POST dns-policies failed: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if len(st.CustomDNSPolicies) != 1 || st.CustomDNSPolicies[0].Domain != "+.lan.example" {
		t.Fatalf("unexpected custom_dns_policies: %+v", st.CustomDNSPolicies)
	}
	policyID := st.CustomDNSPolicies[0].ID

	// Toggle policy disabled via PUT
	st.CustomDNSPolicies[0].Enabled = false
	rec = adminRequest(h, "PUT", "/api/mihomo/dns-policies", map[string]any{
		"policies": st.CustomDNSPolicies,
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("PUT dns-policies failed: %d %s", rec.Code, rec.Body.String())
	}
	rtBytes, _ := os.ReadFile(a.mihomo.runtimePath())
	if strings.Contains(string(rtBytes), "+.lan.example") || !strings.Contains(string(rtBytes), "node.example.com") {
		t.Fatalf("unexpected runtime.yaml after disabling custom policy:\n%s", string(rtBytes))
	}

	// Delete custom policy
	rec = adminRequest(h, "DELETE", "/api/mihomo/dns-policies/"+policyID, nil, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("DELETE dns-policies failed: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if len(st.CustomDNSPolicies) != 0 {
		t.Fatalf("expected empty custom_dns_policies after delete, got %+v", st.CustomDNSPolicies)
	}

	// Invalid inputs
	if adminRequest(h, "POST", "/api/mihomo/dns-policies", map[string]any{"domain": "", "servers": []string{"1.1.1.1"}, "enabled": true}, cookie, "").Code != 400 {
		t.Fatal("expected 400 on empty domain")
	}
	if adminRequest(h, "DELETE", "/api/mihomo/dns-policies/missing", nil, cookie, "").Code != 404 {
		t.Fatal("expected 404 on deleting missing dns policy")
	}
}

func TestMihomoSubscriptionFilterAndSuffixMerge(t *testing.T) {
	sourceYAML := []byte(`
proxies:
  - { name: "剩余流量 10GB", type: ss, server: 1.1.1.1, port: 443 }
  - { name: "HK IEPL 01", type: ss, server: 1.1.1.2, port: 443 }
  - { name: "HK BGP 02", type: ss, server: 1.1.1.3, port: 443 }
  - { name: "JP IEPL 01", type: ss, server: 1.1.1.4, port: 443 }
  - { name: "US 普通 01", type: ss, server: 1.1.1.5, port: 443 }
proxy-groups:
  - name: "节点选择"
    type: select
    proxies: ["自动选择", "剩余流量 10GB", "HK IEPL 01", "JP IEPL 01", "US 普通 01", "DIRECT"]
  - name: "自动选择"
    type: url-test
    proxies: ["HK IEPL 01", "HK BGP 02"]
  - name: "全被排除组"
    type: select
    proxies: ["剩余流量 10GB", "US 普通 01"]
  - name: "Provider组"
    type: select
    use: ["main-provider"]
rules:
  - "MATCH,节点选择"
`)

	cfg := defaultMihomoConfig()
	cfg.ActiveSubscriptionID = "sub-1"
	cfg.Subscriptions = []MihomoSubscription{{
		ID:            "sub-1",
		Name:          "测试机场",
		IncludeFilter: "HK|JP",
		ExcludeFilter: "BGP|剩余流量",
		NodeSuffix:    " [专线]",
	}}
	cfg.CustomGroups = []MihomoCustomGroup{{
		ID:     "grp-hk",
		Name:   "香港节点",
		Type:   "select",
		Filter: `HK .*\[专线\]`,
	}}

	out, err := synthesizeMihomoYAML(sourceYAML, cfg)
	if err != nil {
		t.Fatalf("synthesize with subscription filter failed: %v", err)
	}
	groups, nodes := extractYAMLGroupsAndNodes(out, cfg.CustomGroups)
	if len(nodes) != 2 || nodes[0] != "HK IEPL 01 [专线]" || nodes[1] != "JP IEPL 01 [专线]" {
		t.Fatalf("expected only filtered+suffixed HK IEPL 01 & JP IEPL 01, got %v", nodes)
	}

	groupMap := map[string]MihomoGroupInfo{}
	for _, g := range groups {
		groupMap[g.Name] = g
	}
	if got := groupMap["节点选择"].All; len(got) != 4 || got[0] != "自动选择" || got[1] != "HK IEPL 01 [专线]" || got[2] != "JP IEPL 01 [专线]" || got[3] != "DIRECT" {
		t.Fatalf("expected 节点选择 synced without excluded nodes and preserving group ref, got %v", got)
	}
	if got := groupMap["自动选择"].All; len(got) != 1 || got[0] != "HK IEPL 01 [专线]" {
		t.Fatalf("expected 自动选择 synced, got %v", got)
	}
	if got := groupMap["全被排除组"].All; len(got) != 1 || got[0] != "DIRECT" {
		t.Fatalf("expected emptied group fallback to DIRECT, got %v", got)
	}
	if got := groupMap["Provider组"].All; len(got) != 0 {
		t.Fatalf("expected use-provider group left empty instead of DIRECT, got %v", got)
	}
	if got := groupMap["香港节点"].All; len(got) != 1 || got[0] != "HK IEPL 01 [专线]" {
		t.Fatalf("expected custom group filter to match transformed nodes, got %v", got)
	}
}

func TestMihomoSubscriptionFilterValidationAndAPI(t *testing.T) {
	if validateMihomoSubscriptionFilters("[unclosed", "", "") == nil {
		t.Fatal("expected invalid include_filter regex error")
	}
	if validateMihomoSubscriptionFilters("", "[unclosed", "") == nil {
		t.Fatal("expected invalid exclude_filter regex error")
	}
	if validateMihomoSubscriptionFilters("", "", "bad,suffix") == nil {
		t.Fatal("expected comma in node_suffix rejected")
	}

	_, h := testAdmin(t)
	cookie := loginForTest(t, h)

	subYAML := `
proxies:
  - { name: "公告: 到期提醒", type: ss, server: 1.2.3.4, port: 443 }
  - { name: "SG-01", type: ss, server: 1.2.3.5, port: 443 }
  - { name: "US-01", type: ss, server: 1.2.3.6, port: 443 }
proxy-groups:
  - name: "节点选择"
    type: select
    proxies: ["公告: 到期提醒", "SG-01", "US-01"]
rules:
  - "MATCH,节点选择"
`
	rec := adminRequest(h, "POST", "/api/mihomo/subscriptions", map[string]any{
		"name":           "机场A",
		"yaml_content":   subYAML,
		"include_filter": "",
		"exclude_filter": "公告",
		"node_suffix":    " [A]",
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("POST subscription with filters failed: %d %s", rec.Code, rec.Body.String())
	}
	var st mihomoStatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if len(st.Subscriptions) != 1 || st.Subscriptions[0].ExcludeFilter != "公告" || st.Subscriptions[0].NodeSuffix != " [A]" {
		t.Fatalf("unexpected subscription view: %+v", st.Subscriptions)
	}
	subID := st.Subscriptions[0].ID

	// Check runtime.yaml filtered & suffixed while source.yaml untouched
	rtRes := adminRequest(h, "GET", "/api/mihomo/config/yaml?kind=runtime", nil, cookie, "")
	if rtRes.Code != 200 || strings.Contains(rtRes.Body.String(), "到期提醒") || !strings.Contains(rtRes.Body.String(), "SG-01 [A]") {
		t.Fatalf("runtime.yaml filter/suffix mismatch: %s", rtRes.Body.String())
	}
	srcRes := adminRequest(h, "GET", "/api/mihomo/config/yaml?kind=source", nil, cookie, "")
	if srcRes.Code != 200 || !strings.Contains(srcRes.Body.String(), "到期提醒") {
		t.Fatalf("source.yaml should remain untouched: %s", srcRes.Body.String())
	}

	// Update subscription filters via PUT (also test include_filter narrowing)
	rec = adminRequest(h, "PUT", "/api/mihomo/subscriptions/"+subID, map[string]any{
		"name":           "机场A-精选",
		"include_filter": "SG",
		"exclude_filter": "公告",
		"node_suffix":    " [新]",
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("PUT subscription failed: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st.Subscriptions[0].Name != "机场A-精选" || st.Subscriptions[0].IncludeFilter != "SG" || st.Subscriptions[0].NodeSuffix != " [新]" {
		t.Fatalf("unexpected updated subscription: %+v", st.Subscriptions[0])
	}
	rtRes2 := adminRequest(h, "GET", "/api/mihomo/config/yaml?kind=runtime", nil, cookie, "")
	if strings.Contains(rtRes2.Body.String(), "US-01") || !strings.Contains(rtRes2.Body.String(), "SG-01 [新]") {
		t.Fatalf("runtime.yaml after subscription update mismatch: %s", rtRes2.Body.String())
	}

	if adminRequest(h, "PUT", "/api/mihomo/subscriptions/missing", map[string]any{"name": "x"}, cookie, "").Code != 404 {
		t.Fatal("expected 404 for missing subscription")
	}
}

func TestMihomoDelaySpecialGroupFallbackAndEditGroup(t *testing.T) {
	const groupName = "🇭🇰 香港 / 专线"
	mux := http.NewServeMux()
	var selected string
	mux.HandleFunc("GET /proxies", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{
			"proxies": map[string]any{
				groupName: map[string]any{
					"name": groupName,
					"type": "Selector",
					"now":  "HK-01",
					"all":  []string{"NestedAuto", "HK-01", "HK-TIMEOUT", "DIRECT"},
				},
				"NestedAuto": map[string]any{"name": "NestedAuto", "type": "URLTest", "all": []string{"HK-01"}},
				"HK-01":      map[string]any{"name": "HK-01", "type": "Shadowsocks"},
				"HK-TIMEOUT": map[string]any{"name": "HK-TIMEOUT", "type": "Shadowsocks"},
			},
		})
	})
	// Simulate /group/.../delay failing so fallback per-node /proxies/.../delay runs
	mux.HandleFunc("GET /proxies/{name}/delay", func(w http.ResponseWriter, r *http.Request) {
		node := r.PathValue("name")
		switch node {
		case "HK-01":
			jsonResponse(w, 200, map[string]int{"delay": 52})
			return
		case "NestedAuto":
			jsonResponse(w, 200, map[string]int{"delay": 64})
			return
		}
		http.Error(w, `{"message":"timeout"}`, 504)
	})
	mux.HandleFunc("PUT /proxies/", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		selected = body.Name
		w.WriteHeader(204)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a, h := testAdmin(t)
	a.mihomo.ctrlBaseURL = srv.URL
	cookie := loginForTest(t, h)

	// Test body-based POST /api/mihomo/delay with slash+emoji group name
	rec := adminRequest(h, "POST", "/api/mihomo/delay", map[string]any{"group": groupName}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("POST /api/mihomo/delay failed: %d %s", rec.Code, rec.Body.String())
	}
	var delayRes struct {
		Delays map[string]int `json:"delays"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &delayRes)
	if delayRes.Delays["HK-01"] != 52 || delayRes.Delays["NestedAuto"] != 64 {
		t.Fatalf("expected HK-01=52 and NestedAuto=64, got %+v", delayRes.Delays)
	}
	if val, ok := delayRes.Delays["HK-TIMEOUT"]; !ok || val != 0 {
		t.Fatalf("expected timed-out HK-TIMEOUT explicitly marked as 0, got %+v", delayRes.Delays)
	}
	if _, hasDirect := delayRes.Delays["DIRECT"]; hasDirect {
		t.Fatal("DIRECT builtin should not be in tested delays")
	}

	// Test single-node delay POST /api/mihomo/node-delay (reachable, timeout, invalid)
	rec = adminRequest(h, "POST", "/api/mihomo/node-delay", map[string]any{"node": "HK-01"}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("POST /api/mihomo/node-delay reachable failed: %d %s", rec.Code, rec.Body.String())
	}
	var nodeRes struct {
		Node  string `json:"node"`
		Delay int    `json:"delay"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &nodeRes)
	if nodeRes.Node != "HK-01" || nodeRes.Delay != 52 {
		t.Fatalf("expected HK-01 delay 52, got %+v", nodeRes)
	}
	rec = adminRequest(h, "POST", "/api/mihomo/node-delay", map[string]any{"node": "HK-TIMEOUT"}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("POST /api/mihomo/node-delay timeout failed: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &nodeRes)
	if nodeRes.Node != "HK-TIMEOUT" || nodeRes.Delay != 0 {
		t.Fatalf("expected HK-TIMEOUT delay 0, got %+v", nodeRes)
	}
	if adminRequest(h, "POST", "/api/mihomo/node-delay", map[string]any{"node": ""}, cookie, "").Code != 400 {
		t.Fatal("expected 400 on empty node in node-delay")
	}

	// Test body-based PUT /api/mihomo/select with slash+emoji group name
	rec = adminRequest(h, "PUT", "/api/mihomo/select", map[string]any{"group": groupName, "node": "HK-TIMEOUT"}, cookie, "")
	if rec.Code != 200 || selected != "HK-TIMEOUT" {
		t.Fatalf("PUT /api/mihomo/select failed: code=%d selected=%q", rec.Code, selected)
	}

	// Test creating and editing an existing custom proxy group via id
	rec = adminRequest(h, "POST", "/api/mihomo/groups", map[string]any{
		"name":   "香港自定义",
		"type":   "select",
		"filter": "HK",
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("create group failed: %d %s", rec.Code, rec.Body.String())
	}
	var st mihomoStatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	grpID := st.CustomGroups[0].ID

	rec = adminRequest(h, "POST", "/api/mihomo/groups", map[string]any{
		"id":       grpID,
		"name":     "香港测速",
		"type":     "url-test",
		"filter":   "HK|SG",
		"interval": 150,
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("edit existing group failed: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if len(st.CustomGroups) != 1 || st.CustomGroups[0].ID != grpID || st.CustomGroups[0].Name != "香港测速" || st.CustomGroups[0].Interval != 150 {
		t.Fatalf("expected updated custom group preserving id, got %+v", st.CustomGroups)
	}
}

func TestMihomoYAMLValidateAndSaveGate(t *testing.T) {
	_, h := testAdmin(t)
	cookie := loginForTest(t, h)

	validYAML := `
mixed-port: 7890
secret: "should-be-masked"
proxies:
  - { name: "N1", type: ss, server: 1.2.3.4, port: 443 }
proxy-groups:
  - name: "节点选择"
    type: select
    proxies: ["N1", "DIRECT"]
rules:
  - "MATCH,节点选择"
`
	rec := adminRequest(h, "POST", "/api/mihomo/config/validate", map[string]any{"content": validYAML}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("POST /api/mihomo/config/validate failed: %d", rec.Code)
	}
	var valRes mihomoYAMLValidationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &valRes)
	if !valRes.Valid || valRes.Summary.Proxies != 1 || valRes.Summary.Groups != 1 || valRes.Summary.Rules != 1 || !strings.Contains(valRes.FormattedYAML, managedSecretPlaceholder) {
		t.Fatalf("unexpected valid YAML result: %+v", valRes)
	}

	// Broken syntax rejected
	rec = adminRequest(h, "POST", "/api/mihomo/config/validate", map[string]any{"content": "proxies:\n  - [broken"}, cookie, "")
	_ = json.Unmarshal(rec.Body.Bytes(), &valRes)
	if valRes.Valid || valRes.Error == "" {
		t.Fatalf("expected syntax error flagged invalid: %+v", valRes)
	}

	// Missing member reference in proxy-groups rejected on validate & PUT save
	missingRefYAML := `
mixed-port: 7890
proxies:
  - { name: "N1", type: ss, server: 1.2.3.4, port: 443 }
proxy-groups:
  - name: "节点选择"
    type: select
    proxies: ["MISSING-NODE"]
rules:
  - "MATCH,节点选择"
`
	rec = adminRequest(h, "POST", "/api/mihomo/config/validate", map[string]any{"content": missingRefYAML}, cookie, "")
	_ = json.Unmarshal(rec.Body.Bytes(), &valRes)
	if valRes.Valid || !strings.Contains(valRes.Error, "MISSING-NODE") {
		t.Fatalf("expected missing member reference error, got %+v", valRes)
	}
	if adminRequest(h, "PUT", "/api/mihomo/config/yaml", map[string]any{"content": missingRefYAML}, cookie, "").Code != 400 {
		t.Fatal("expected PUT /api/mihomo/config/yaml to reject invalid member reference")
	}

	// Duplicate proxy names flagged in warnings
	dupYAML := `
proxies:
  - { name: "DUP", type: ss, server: 1.2.3.4, port: 443 }
  - { name: "DUP", type: ss, server: 1.2.3.5, port: 443 }
proxy-groups:
  - name: "节点选择"
    type: select
    proxies: ["DUP"]
rules:
  - "MATCH,节点选择"
`
	rec = adminRequest(h, "POST", "/api/mihomo/config/validate", map[string]any{"content": dupYAML}, cookie, "")
	_ = json.Unmarshal(rec.Body.Bytes(), &valRes)
	if !valRes.Valid || len(valRes.Warnings) != 1 || !strings.Contains(valRes.Warnings[0], "DUP") {
		t.Fatalf("expected duplicate proxy warning, got %+v", valRes)
	}
}

func TestMihomoLANAuthYAMLMaskingAndSettingsAPI(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	// Invalid username with colon rejected
	rec := adminRequest(h, "PUT", "/api/mihomo/settings", map[string]any{
		"enabled":           true,
		"takeover_outbound": false,
		"controller_port":   19090,
		"settings": map[string]any{
			"mixed_port":        7890,
			"allow_lan":         true,
			"mode":              "rule",
			"log_level":         "info",
			"dns_enhanced_mode": "keep",
			"auth_enabled":      true,
			"auth_username":     "bad:user",
			"auth_password":     "s3cret!",
		},
	}, cookie, "")
	if rec.Code != 400 {
		t.Fatalf("expected 400 on colon in auth_username, got %d %s", rec.Code, rec.Body.String())
	}

	// Save valid LAN proxy auth
	rec = adminRequest(h, "PUT", "/api/mihomo/settings", map[string]any{
		"enabled":           true,
		"takeover_outbound": true,
		"controller_port":   19090,
		"settings": map[string]any{
			"mixed_port":        17890,
			"allow_lan":         true,
			"mode":              "rule",
			"log_level":         "info",
			"dns_enhanced_mode": "keep",
			"auth_enabled":      true,
			"auth_username":     "lanuser",
			"auth_password":     "s3cret!",
		},
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("PUT /api/mihomo/settings with LAN auth failed: %d %s", rec.Code, rec.Body.String())
	}

	var st mihomoStatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if !st.Settings.AuthEnabled || st.Settings.AuthUsername != "lanuser" || !st.AuthPasswordConfigured || st.Settings.AuthPassword != "" {
		t.Fatalf("unexpected auth status snapshot (must not leak password): %+v auth_pwd_configured=%v", st.Settings, st.AuthPasswordConfigured)
	}

	// Runtime YAML on disk contains real credentials + loopback bypass
	rtDisk, err := os.ReadFile(a.mihomo.runtimePath())
	if err != nil {
		t.Fatalf("read runtime.yaml failed: %v", err)
	}
	if !strings.Contains(string(rtDisk), "lanuser:s3cret!") || !strings.Contains(string(rtDisk), "127.0.0.1/32") {
		t.Fatalf("expected real authentication and skip-auth-prefixes in runtime.yaml, got:\n%s", string(rtDisk))
	}

	// GET /api/mihomo/config/yaml masks auth password
	yamlRes := adminRequest(h, "GET", "/api/mihomo/config/yaml?kind=runtime", nil, cookie, "")
	if yamlRes.Code != 200 {
		t.Fatalf("GET yaml failed: %d", yamlRes.Code)
	}
	var yamlPayload struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(yamlRes.Body.Bytes(), &yamlPayload)
	if strings.Contains(yamlPayload.Content, "s3cret!") || !strings.Contains(yamlPayload.Content, "lanuser:"+managedAuthPlaceholder) {
		t.Fatalf("expected masked authentication in YAML response, got:\n%s", yamlPayload.Content)
	}

	// Saving masked YAML re-injects real credentials on disk while keeping API response masked
	putYamlRes := adminRequest(h, "PUT", "/api/mihomo/config/yaml", map[string]any{"content": yamlPayload.Content + "\n# keep"}, cookie, "")
	if putYamlRes.Code != 200 {
		t.Fatalf("PUT masked yaml failed: %d %s", putYamlRes.Code, putYamlRes.Body.String())
	}
	if strings.Contains(putYamlRes.Body.String(), "s3cret!") {
		t.Fatalf("PUT yaml response leaked plaintext proxy password: %s", putYamlRes.Body.String())
	}
	rtAfterPut, _ := os.ReadFile(a.mihomo.runtimePath())
	if !strings.Contains(string(rtAfterPut), "lanuser:s3cret!") {
		t.Fatalf("expected real credentials re-injected on disk, got:\n%s", string(rtAfterPut))
	}

	// Updating settings with blank auth_password preserves existing password
	rec = adminRequest(h, "PUT", "/api/mihomo/settings", map[string]any{
		"enabled":           true,
		"takeover_outbound": true,
		"controller_port":   19090,
		"settings": map[string]any{
			"mixed_port":        17890,
			"allow_lan":         true,
			"mode":              "rule",
			"log_level":         "info",
			"dns_enhanced_mode": "keep",
			"auth_enabled":      true,
			"auth_username":     "lanuser2",
			"auth_password":     "",
		},
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("update username preserving password failed: %d %s", rec.Code, rec.Body.String())
	}
	rtUpdated, _ := os.ReadFile(a.mihomo.runtimePath())
	if !strings.Contains(string(rtUpdated), "lanuser2:s3cret!") {
		t.Fatalf("expected preserved password with updated username, got:\n%s", string(rtUpdated))
	}

	// Disabling auth removes authentication & skip-auth-prefixes
	rec = adminRequest(h, "PUT", "/api/mihomo/settings", map[string]any{
		"enabled":           true,
		"takeover_outbound": true,
		"controller_port":   19090,
		"settings": map[string]any{
			"mixed_port":        17890,
			"allow_lan":         true,
			"mode":              "rule",
			"log_level":         "info",
			"dns_enhanced_mode": "keep",
			"auth_enabled":      false,
		},
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("disable auth failed: %d %s", rec.Code, rec.Body.String())
	}
	rtDisabled, _ := os.ReadFile(a.mihomo.runtimePath())
	if strings.Contains(string(rtDisabled), "authentication:") || strings.Contains(string(rtDisabled), "skip-auth-prefixes:") {
		t.Fatalf("expected authentication keys removed when disabled, got:\n%s", string(rtDisabled))
	}
}

func TestMihomoZashboardInstallServeAndControllerProxy(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	// ZipSlip attempt must fail
	var badZipBuf bytes.Buffer
	zw := zip.NewWriter(&badZipBuf)
	fw, _ := zw.Create("../evil.txt")
	_, _ = fw.Write([]byte("pwned"))
	_ = zw.Close()
	if _, err := a.mihomo.installZashboardZip(badZipBuf.Bytes(), "bad"); err == nil {
		t.Fatal("expected ZipSlip archive rejected")
	}

	// Valid dist/ archive strips top prefix cleanly
	var goodZipBuf bytes.Buffer
	zw = zip.NewWriter(&goodZipBuf)
	idxW, _ := zw.Create("dist/index.html")
	_, _ = idxW.Write([]byte("<html><head><title>Zashboard</title></head><body>ok</body></html>"))
	jsW, _ := zw.Create("dist/assets/app.js")
	_, _ = jsW.Write([]byte("console.log('zashboard');"))
	_ = zw.Close()

	ver, err := a.mihomo.installZashboardZip(goodZipBuf.Bytes(), "v1.2.3")
	if err != nil || ver != "v1.2.3" {
		t.Fatalf("install valid zashboard zip failed: ver=%q err=%v", ver, err)
	}
	installed, gotVer := a.mihomo.zashboardStatus()
	if !installed || gotVer != "v1.2.3" {
		t.Fatalf("expected zashboard installed v1.2.3, got installed=%v ver=%q", installed, gotVer)
	}

	// Unauthenticated /zashboard/ and /api/mihomo/ctrl/ rejected with 401
	unauthUI := httptest.NewRequest("GET", "http://localhost:16666/zashboard/", nil)
	unauthRec := httptest.NewRecorder()
	h.ServeHTTP(unauthRec, unauthUI)
	if unauthRec.Code != 401 {
		t.Fatalf("expected 401 for unauthenticated /zashboard/, got %d", unauthRec.Code)
	}

	unauthCtrl := httptest.NewRequest("GET", "http://localhost:16666/api/mihomo/ctrl/version", nil)
	unauthCtrlRec := httptest.NewRecorder()
	h.ServeHTTP(unauthCtrlRec, unauthCtrl)
	if unauthCtrlRec.Code != 401 {
		t.Fatalf("expected 401 for unauthenticated /api/mihomo/ctrl/, got %d", unauthCtrlRec.Code)
	}

	// Authenticated /zashboard/ serves index.html with Zashboard CSP
	authUI := httptest.NewRequest("GET", "http://localhost:16666/zashboard/", nil)
	authUI.AddCookie(cookie)
	authRec := httptest.NewRecorder()
	h.ServeHTTP(authRec, authUI)
	if authRec.Code != 200 || !strings.Contains(authRec.Body.String(), "<title>Zashboard</title>") {
		t.Fatalf("expected /zashboard/ 200 with index.html, got %d %s", authRec.Code, authRec.Body.String())
	}
	if !strings.Contains(authRec.Header().Get("Content-Security-Policy"), "connect-src 'self' ws: wss:") {
		t.Fatalf("expected Zashboard CSP header, got %q", authRec.Header().Get("Content-Security-Policy"))
	}

	// Mock Mihomo External Controller behind GateHome reverse proxy
	var seenAuth, seenCookie, seenPath string
	ctrlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenCookie = r.Header.Get("Cookie")
		seenPath = r.URL.RequestURI()
		jsonResponse(w, 200, map[string]any{"ok": true, "path": seenPath})
	}))
	defer ctrlSrv.Close()

	a.mihomo.ctrlBaseURL = ctrlSrv.URL
	a.mihomo.ctrlSecret = "top-secret-token"

	// Simulate real Zashboard PUT request (with session cookie & same Origin, WITHOUT X-Gatehouse-Request)
	proxyReq := httptest.NewRequest("PUT", "http://localhost:16666/api/mihomo/ctrl/proxies/HK%20Node", strings.NewReader(`{"name":"HK-01"}`))
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("Origin", "http://localhost:16666")
	proxyReq.AddCookie(cookie)
	proxyRec := httptest.NewRecorder()
	h.ServeHTTP(proxyRec, proxyReq)
	if proxyRec.Code != 200 {
		t.Fatalf("expected 200 from controller proxy without X-Gatehouse-Request, got %d %s", proxyRec.Code, proxyRec.Body.String())
	}
	if seenAuth != "Bearer top-secret-token" {
		t.Fatalf("expected injected Bearer token, got %q", seenAuth)
	}
	if seenCookie != "" {
		t.Fatalf("expected GateHome session cookie stripped before upstream controller, got %q", seenCookie)
	}
	if seenPath != "/proxies/HK%20Node" {
		t.Fatalf("expected preserved escaped controller path, got %q", seenPath)
	}

	// Cross-site request to controller proxy blocked even with cookie
	crossReq := httptest.NewRequest("GET", "http://localhost:16666/api/mihomo/ctrl/version", nil)
	crossReq.Header.Set("Sec-Fetch-Site", "cross-site")
	crossReq.AddCookie(cookie)
	crossRec := httptest.NewRecorder()
	h.ServeHTTP(crossRec, crossReq)
	if crossRec.Code != 403 {
		t.Fatalf("expected 403 for cross-site request to controller proxy, got %d", crossRec.Code)
	}
}

func TestMihomoLogsAPIAndClear(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	a.mihomo.logs.add(`time="2026-11-23T22:50:00+08:00" level=info msg="Mihomo started"`)
	a.mihomo.logs.add(`time="2026-11-23T22:50:01+08:00" level=warning msg="sample warning"`)

	rec := adminRequest(h, "GET", "/api/mihomo/logs", nil, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("GET /api/mihomo/logs failed: %d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Lines   []string `json:"lines"`
		Running bool     `json:"running"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	if len(payload.Lines) != 2 || !strings.Contains(payload.Lines[0], "Mihomo started") {
		t.Fatalf("unexpected logs payload: %+v", payload)
	}

	// Clear logs
	rec = adminRequest(h, "DELETE", "/api/mihomo/logs", map[string]any{}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("DELETE /api/mihomo/logs failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = adminRequest(h, "GET", "/api/mihomo/logs", nil, cookie, "")
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	if len(payload.Lines) != 0 {
		t.Fatalf("expected logs cleared, got %+v", payload.Lines)
	}
}

func TestMihomoMMDBUploadValidationAndStatus(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	upload := func(data []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("file", "country.mmdb")
		_, _ = fw.Write(data)
		_ = mw.Close()

		req := httptest.NewRequest("POST", "http://localhost:16666/api/mihomo/geodata/upload", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Gatehouse-Request", "1")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// Reject corrupt/invalid bytes
	badRec := upload([]byte("not an mmdb file"))
	if badRec.Code != 400 {
		t.Fatalf("expected 400 for invalid mmdb upload, got %d %s", badRec.Code, badRec.Body.String())
	}

	// Accept valid MMDB with MaxMind metadata marker in trailing bytes
	validMMDB := make([]byte, 2048)
	copy(validMMDB[len(validMMDB)-len(mmdbMetadataMarker)-4:], mmdbMetadataMarker)
	okRec := upload(validMMDB)
	if okRec.Code != 200 {
		t.Fatalf("expected 200 for valid mmdb upload, got %d %s", okRec.Code, okRec.Body.String())
	}

	var res struct {
		OK     bool                 `json:"ok"`
		Size   int64                `json:"size"`
		Status mihomoStatusResponse `json:"status"`
	}
	_ = json.Unmarshal(okRec.Body.Bytes(), &res)
	if !res.OK || res.Size != 2048 || !res.Status.MMDBInstalled || res.Status.MMDBSize != 2048 {
		t.Fatalf("unexpected upload status: %+v", res)
	}

	// Check GET /api/mihomo/status reflects mmdb_installed
	stRec := adminRequest(h, "GET", "/api/mihomo/status", nil, cookie, "")
	var st mihomoStatusResponse
	_ = json.Unmarshal(stRec.Body.Bytes(), &st)
	if !st.MMDBInstalled || st.MMDBSize != 2048 {
		t.Fatalf("expected status mmdb_installed=true size=2048, got %+v", st)
	}
	if _, err := os.Stat(a.mihomo.mmdbPath()); err != nil {
		t.Fatalf("country.mmdb missing on disk: %v", err)
	}
}

func TestMihomoCustomGroupsRulesYAMLRoundTrip(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	// Create group + rule via the form APIs, then read them back as YAML (single source of truth).
	if rec := adminRequest(h, "POST", "/api/mihomo/groups", map[string]any{"name": "AI", "type": "select", "proxies": []string{"DIRECT"}}, cookie, ""); rec.Code != 200 {
		t.Fatalf("create group: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminRequest(h, "POST", "/api/mihomo/rules", map[string]any{"type": "DOMAIN-SUFFIX", "value": "openai.com", "target": "AI", "enabled": true}, cookie, ""); rec.Code != 200 {
		t.Fatalf("create rule: %d %s", rec.Code, rec.Body.String())
	}
	rec := adminRequest(h, "GET", "/api/mihomo/custom/groups/yaml", nil, cookie, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "name: AI") {
		t.Fatalf("groups yaml missing form-created group: %d %s", rec.Code, rec.Body.String())
	}
	rec = adminRequest(h, "GET", "/api/mihomo/custom/rules/yaml", nil, cookie, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "DOMAIN-SUFFIX,openai.com,AI") {
		t.Fatalf("rules yaml missing form-created rule: %d %s", rec.Code, rec.Body.String())
	}

	// Edit via YAML: invalid content is rejected without touching state.
	if rec := adminRequest(h, "PUT", "/api/mihomo/custom/rules/yaml", map[string]any{"content": "rules:\n  - BAD,x\n"}, cookie, ""); rec.Code != 400 {
		t.Fatalf("expected 400 for bad rule yaml, got %d", rec.Code)
	}
	if rec := adminRequest(h, "PUT", "/api/mihomo/custom/groups/yaml", map[string]any{"content": "proxy-groups:\n  - name: X\n    type: bogus\n"}, cookie, ""); rec.Code != 400 {
		t.Fatalf("expected 400 for bad group yaml, got %d", rec.Code)
	}

	// Valid YAML edits replace the list and appear in the UI status + runtime.yaml (custom rules above airport ones).
	groupsYAML := "proxy-groups:\n  - name: AI\n    type: fallback\n    proxies: [DIRECT]\n    interval: 120\n  - name: 流媒体\n    type: select\n    proxies: [DIRECT]\n"
	if rec := adminRequest(h, "PUT", "/api/mihomo/custom/groups/yaml", map[string]any{"content": groupsYAML}, cookie, ""); rec.Code != 200 {
		t.Fatalf("put groups yaml: %d %s", rec.Code, rec.Body.String())
	}
	rulesYAML := "rules:\n  - DOMAIN-SUFFIX,openai.com,AI\n  - rule: DOMAIN-SUFFIX,netflix.com,流媒体\n    enabled: false\n    comment: 暂停\n"
	rec = adminRequest(h, "PUT", "/api/mihomo/custom/rules/yaml", map[string]any{"content": rulesYAML}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("put rules yaml: %d %s", rec.Code, rec.Body.String())
	}
	var st struct {
		Status mihomoStatusResponse `json:"status"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if len(st.Status.CustomGroups) != 2 || st.Status.CustomGroups[0].Type != "fallback" || len(st.Status.CustomRules) != 2 || st.Status.CustomRules[1].Enabled {
		t.Fatalf("UI status not reflecting YAML edits: %+v %+v", st.Status.CustomGroups, st.Status.CustomRules)
	}
	rt, _ := os.ReadFile(a.mihomo.runtimePath())
	if !strings.Contains(string(rt), "DOMAIN-SUFFIX,openai.com,AI") || strings.Contains(string(rt), "netflix.com") || !strings.Contains(string(rt), "流媒体") {
		t.Fatalf("runtime.yaml not merged from YAML edits:\n%s", rt)
	}
}

func TestMihomoConnectionsAPIAndClose(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	// When core is stopped, endpoint returns 200 with running=false and empty connections list.
	rec := adminRequest(h, "GET", "/api/mihomo/connections", nil, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("stopped connections expected 200, got %d %s", rec.Code, rec.Body.String())
	}
	var stoppedSnap MihomoConnectionsSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &stoppedSnap); err != nil {
		t.Fatalf("decode stopped connections: %v", err)
	}
	if stoppedSnap.Running || len(stoppedSnap.Connections) != 0 {
		t.Fatalf("expected stopped empty snapshot, got %+v", stoppedSnap)
	}

	const testSecret = "CONN_CTRL_SECRET"
	var closedID string
	var closedAll bool
	mux := http.NewServeMux()
	authCheck := func(r *http.Request) bool {
		return r.Header.Get("Authorization") == "Bearer "+testSecret
	}
	mux.HandleFunc("GET /connections", func(w http.ResponseWriter, r *http.Request) {
		if !authCheck(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		jsonResponse(w, 200, map[string]any{
			"downloadTotal": 20480,
			"uploadTotal":   4096,
			"memory":        1048576,
			"connections": []map[string]any{
				{
					"id":          "conn-1",
					"upload":      1024,
					"download":    8192,
					"start":       "2026-11-24T08:00:00Z",
					"chains":      []string{"JPN 日本 01|绿叶", "节点选择"},
					"rule":        "RuleSet",
					"rulePayload": "TelegramIP",
					"metadata": map[string]any{
						"network":         "tcp",
						"type":            "Mixed",
						"sourceIP":        "192.168.2.2",
						"sourcePort":      "55645",
						"destinationIP":   "149.154.166.110",
						"destinationPort": "443",
						"host":            "api.telegram.org",
					},
				},
			},
		})
	})
	mux.HandleFunc("DELETE /connections/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authCheck(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		closedID = r.PathValue("id")
		w.WriteHeader(204)
	})
	mux.HandleFunc("DELETE /connections", func(w http.ResponseWriter, r *http.Request) {
		if !authCheck(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		closedAll = true
		w.WriteHeader(204)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a.mihomo.mu.Lock()
	a.mihomo.running = true
	a.mihomo.ctrlBaseURL = srv.URL
	a.mihomo.ctrlSecret = testSecret
	a.mihomo.mu.Unlock()

	liveRec := adminRequest(h, "GET", "/api/mihomo/connections", nil, cookie, "")
	if liveRec.Code != 200 {
		t.Fatalf("live connections expected 200, got %d %s", liveRec.Code, liveRec.Body.String())
	}
	var liveSnap MihomoConnectionsSnapshot
	if err := json.Unmarshal(liveRec.Body.Bytes(), &liveSnap); err != nil {
		t.Fatalf("decode live connections: %v", err)
	}
	if !liveSnap.Running || len(liveSnap.Connections) != 1 {
		t.Fatalf("unexpected live snapshot: %+v", liveSnap)
	}
	c := liveSnap.Connections[0]
	if c.Metadata.SourceIP != "192.168.2.2" || c.Metadata.Host != "api.telegram.org" || c.Rule != "RuleSet" || c.RulePayload != "TelegramIP" || len(c.Chains) != 2 {
		t.Fatalf("unexpected connection item: %+v", c)
	}

	delOne := adminRequest(h, "DELETE", "/api/mihomo/connections/conn-1", map[string]any{}, cookie, "")
	if delOne.Code != 200 || closedID != "conn-1" {
		t.Fatalf("expected close conn-1 200, got code=%d closedID=%q body=%s", delOne.Code, closedID, delOne.Body.String())
	}

	delAll := adminRequest(h, "DELETE", "/api/mihomo/connections", map[string]any{}, cookie, "")
	if delAll.Code != 200 || !closedAll {
		t.Fatalf("expected close all 200, got code=%d closedAll=%v body=%s", delAll.Code, closedAll, delAll.Body.String())
	}
}

func TestMihomoRuleSetsMergeAndAPI(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	// Reject MRS + classical combination and path traversal.
	badClassical := adminRequest(h, "POST", "/api/mihomo/rule-sets", map[string]any{
		"name":     "bad-mrs",
		"source":   "http",
		"behavior": "classical",
		"format":   "mrs",
		"url":      "https://example.com/rules.mrs",
	}, cookie, "")
	if badClassical.Code != 400 {
		t.Fatalf("expected 400 for mrs+classical, got %d", badClassical.Code)
	}
	badPath := adminRequest(h, "POST", "/api/mihomo/rule-sets", map[string]any{
		"name":     "bad-path",
		"source":   "manual",
		"behavior": "domain",
		"format":   "txt",
		"path":     "../../etc/passwd",
		"content":  ".example.com",
	}, cookie, "")
	if badPath.Code != 400 {
		t.Fatalf("expected 400 for path traversal, got %d", badPath.Code)
	}

	// 1. Add built-in preset rule-set with auto RULE-SET creation.
	builtinRec := adminRequest(h, "POST", "/api/mihomo/rule-sets", map[string]any{
		"source":          "builtin",
		"preset":          "cn-ip",
		"name":            "cn-ip",
		"behavior":        "ipcidr",
		"format":          "mrs",
		"path":            "ruleset/custom-cn-ip.mrs",
		"interval":        43200,
		"create_rule":     true,
		"rule_target":     "DIRECT",
		"rule_no_resolve": true,
	}, cookie, "")
	if builtinRec.Code != 200 {
		t.Fatalf("builtin save failed: %d %s", builtinRec.Code, builtinRec.Body.String())
	}

	// 2. Add remote http rule-set (yaml).
	httpRec := adminRequest(h, "POST", "/api/mihomo/rule-sets", map[string]any{
		"name":     "my-remote",
		"source":   "http",
		"behavior": "classical",
		"format":   "yaml",
		"url":      "https://rules.example.com/custom.yaml",
		"path":     "./ruleset/my-remote.yaml",
		"interval": 3600,
		"comment":  "远端混合规则集",
	}, cookie, "")
	if httpRec.Code != 200 {
		t.Fatalf("http save failed: %d %s", httpRec.Code, httpRec.Body.String())
	}

	// 3. Add manual txt rule-set + manual yaml rule-set.
	manualTxtRec := adminRequest(h, "POST", "/api/mihomo/rule-sets", map[string]any{
		"name":            "local-domains",
		"source":          "manual",
		"behavior":        "domain",
		"format":          "txt",
		"path":            "./ruleset/local-domains.txt",
		"content":         "# comment\n+.internal.lan\n.example.org",
		"create_rule":     true,
		"rule_target":     "DIRECT",
		"rule_no_resolve": false,
	}, cookie, "")
	if manualTxtRec.Code != 200 {
		t.Fatalf("manual txt save failed: %d %s", manualTxtRec.Code, manualTxtRec.Body.String())
	}

	manualYamlRec := adminRequest(h, "POST", "/api/mihomo/rule-sets", map[string]any{
		"name":     "local-yaml",
		"source":   "manual",
		"behavior": "classical",
		"format":   "yaml",
		"content":  "DOMAIN-SUFFIX,corp.example,DIRECT\nIP-CIDR,10.20.0.0/16,DIRECT",
	}, cookie, "")
	if manualYamlRec.Code != 200 {
		t.Fatalf("manual yaml save failed: %d %s", manualYamlRec.Code, manualYamlRec.Body.String())
	}

	// Verify local manual files were materialized cleanly inside m.dir.
	txtBytes, err := os.ReadFile(filepath.Join(a.mihomo.dir, "ruleset", "local-domains.txt"))
	if err != nil || string(txtBytes) != "+.internal.lan\n.example.org\n" {
		t.Fatalf("unexpected local-domains.txt: err=%v content=%q", err, string(txtBytes))
	}
	yamlBytes, err := os.ReadFile(filepath.Join(a.mihomo.dir, "ruleset", "local-yaml.yaml"))
	if err != nil || !strings.Contains(string(yamlBytes), "payload:") || !strings.Contains(string(yamlBytes), "DOMAIN-SUFFIX,corp.example,DIRECT") {
		t.Fatalf("unexpected local-yaml.yaml: err=%v content=%q", err, string(yamlBytes))
	}

	// Verify runtime.yaml contains rule-providers + RULE-SET rules.
	rtRaw, err := os.ReadFile(a.mihomo.runtimePath())
	if err != nil {
		t.Fatalf("read runtime.yaml: %v", err)
	}
	rt := string(rtRaw)
	for _, want := range []string{
		"rule-providers:",
		"cn-ip:",
		"path: ./ruleset/custom-cn-ip.mrs",
		"interval: 43200",
		"my-remote:",
		"behavior: classical",
		"local-domains:",
		"type: file",
		"format: text",
		"RULE-SET,cn-ip,DIRECT,no-resolve",
		"RULE-SET,local-domains,DIRECT",
	} {
		if !strings.Contains(rt, want) {
			t.Fatalf("runtime.yaml missing %q:\n%s", want, rt)
		}
	}

	// Verify status exposes presets and all 4 rule-sets, then toggle and delete one manual rule-set.
	var st mihomoStatusResponse
	_ = json.Unmarshal(manualYamlRec.Body.Bytes(), &st)
	if len(st.RuleSetPresets) < 10 || len(st.CustomRuleSets) != 4 {
		t.Fatalf("unexpected status rule_sets=%d presets=%d", len(st.CustomRuleSets), len(st.RuleSetPresets))
	}
	targetID := st.CustomRuleSets[2].ID // local-domains
	togRec := adminRequest(h, "PUT", "/api/mihomo/rule-sets/"+targetID+"/toggle", map[string]any{}, cookie, "")
	if togRec.Code != 200 {
		t.Fatalf("toggle rule-set failed: %d %s", togRec.Code, togRec.Body.String())
	}
	rtAfterToggle, _ := os.ReadFile(a.mihomo.runtimePath())
	if strings.Contains(string(rtAfterToggle), "local-domains:") {
		t.Fatalf("disabled rule-set should be omitted from rule-providers:\n%s", rtAfterToggle)
	}
	delRec := adminRequest(h, "DELETE", "/api/mihomo/rule-sets/"+targetID, map[string]any{}, cookie, "")
	if delRec.Code != 200 {
		t.Fatalf("delete rule-set failed: %d %s", delRec.Code, delRec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(a.mihomo.dir, "ruleset", "local-domains.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected manual file removed after delete, got err=%v", err)
	}
}

func TestMihomoCustomAllYAMLSyncAndOverride(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	// Add an airport subscription with conflicting keys, group, provider, and rules.
	airportSub1 := `
find-process-mode: off
proxies:
  - { name: "机场HK", type: ss, server: 1.2.3.4, port: 443 }
proxy-groups:
  - name: "节点选择"
    type: select
    proxies: ["DIRECT"]
rule-providers:
  shared-set:
    type: http
    behavior: domain
    format: yaml
    url: https://airport.invalid/old.yaml
    path: ./ruleset/airport-old.yaml
    interval: 600
rules:
  - "MATCH,节点选择"
`
	addSub := adminRequest(h, "POST", "/api/mihomo/subscriptions", map[string]any{
		"name":         "测试机场",
		"yaml_content": airportSub1,
	}, cookie, "")
	if addSub.Code != 200 {
		t.Fatalf("add subscription failed: %d %s", addSub.Code, addSub.Body.String())
	}

	// Reject reserved keys in custom.yaml.
	badRec := adminRequest(h, "PUT", "/api/mihomo/custom/all/yaml", map[string]any{
		"content": "secret: hacked\nproxy-groups: []\nrules: []\n",
	}, cookie, "")
	if badRec.Code != 400 {
		t.Fatalf("expected 400 for reserved secret key, got %d", badRec.Code)
	}

	// Edit unified custom.yaml with groups, rules, rule-providers, dns policy, and extra overrides.
	customDoc := `
find-process-mode: strict
sniffer:
  enable: true
dns:
  nameserver-policy:
    "+.corp.internal":
      - 10.0.0.53
proxy-groups:
  - name: 节点选择
    type: select
    proxies: [机场HK, DIRECT]
rule-providers:
  shared-set:
    type: http
    behavior: domain
    format: text
    url: https://custom.example.com/shared.txt
    path: ./ruleset/shared.txt
    interval: 7200
  local-inline:
    type: file
    behavior: domain
    format: text
    path: ./ruleset/local-inline.txt
    payload:
      - "+.myhome.lan"
rules:
  - RULE-SET,shared-set,节点选择
  - RULE-SET,local-inline,DIRECT
`
	putRec := adminRequest(h, "PUT", "/api/mihomo/custom/all/yaml", map[string]any{
		"content": customDoc,
	}, cookie, "")
	if putRec.Code != 200 {
		t.Fatalf("put custom all yaml failed: %d %s", putRec.Code, putRec.Body.String())
	}
	var putRes struct {
		OK      bool                 `json:"ok"`
		Content string               `json:"content"`
		Status  mihomoStatusResponse `json:"status"`
	}
	_ = json.Unmarshal(putRec.Body.Bytes(), &putRes)
	if !putRes.OK || len(putRes.Status.CustomGroups) != 1 || len(putRes.Status.CustomRuleSets) != 2 || len(putRes.Status.CustomRules) != 2 || len(putRes.Status.CustomDNSPolicies) != 1 {
		t.Fatalf("structured state not synced from custom.yaml: %+v", putRes.Status)
	}

	// GET /api/mihomo/custom/all/yaml returns unified custom.yaml with all sections.
	getRec := adminRequest(h, "GET", "/api/mihomo/custom/all/yaml", nil, cookie, "")
	if getRec.Code != 200 || !strings.Contains(getRec.Body.String(), "find-process-mode: strict") || !strings.Contains(getRec.Body.String(), "shared-set") {
		t.Fatalf("get custom all yaml unexpected: %d %s", getRec.Code, getRec.Body.String())
	}

	// Verify runtime.yaml overrides conflicting airport items.
	rt1Bytes, _ := os.ReadFile(a.mihomo.runtimePath())
	rt1 := string(rt1Bytes)
	for _, want := range []string{
		"find-process-mode: strict",
		"enable: true",
		"10.0.0.53",
		"https://custom.example.com/shared.txt",
		"RULE-SET,shared-set,节点选择",
		"RULE-SET,local-inline,DIRECT",
	} {
		if !strings.Contains(rt1, want) {
			t.Fatalf("runtime.yaml missing %q:\n%s", want, rt1)
		}
	}
	if strings.Contains(rt1, "https://airport.invalid/old.yaml") || strings.Contains(rt1, "find-process-mode: off") {
		t.Fatalf("airport conflicting values should be overridden by custom.yaml:\n%s", rt1)
	}

	// Simulate an airport subscription update overwriting source.yaml with new conflicting settings.
	airportSub2 := `
find-process-mode: off
proxies:
  - { name: "机场HK", type: ss, server: 1.2.3.4, port: 443 }
  - { name: "机场SG", type: ss, server: 1.2.3.6, port: 443 }
proxy-groups:
  - name: "节点选择"
    type: select
    proxies: ["REJECT"]
rules:
  - "MATCH,节点选择"
`
	if err := atomicWrite(a.mihomo.subscriptionFile(a.mihomo.cfg.ActiveSubscriptionID), []byte(airportSub2)); err != nil {
		t.Fatalf("write updated subscription source: %v", err)
	}
	a.mihomo.mu.Lock()
	if err := a.mihomo.synthesizeAndWriteLocked(); err != nil {
		a.mihomo.mu.Unlock()
		t.Fatalf("resynthesize after subscription update: %v", err)
	}
	a.mihomo.mu.Unlock()

	rt2Bytes, _ := os.ReadFile(a.mihomo.runtimePath())
	rt2 := string(rt2Bytes)
	if !strings.Contains(rt2, "find-process-mode: strict") || strings.Contains(rt2, "find-process-mode: off") || !strings.Contains(rt2, "机场SG") || !strings.Contains(rt2, "https://custom.example.com/shared.txt") {
		t.Fatalf("custom.yaml overlay lost or failed to override after subscription update:\n%s", rt2)
	}
}

func TestMihomoRuleSetPathConfinement(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	badPaths := []string{
		"./config.json",
		"runtime.yaml",
		"./ui/zashboard/index.html",
		"../secret.yaml",
		"./ruleset",
		"./ruleset/",
		"./ruleset/evil.html",
		`.\ruleset\evil.yaml`,
	}
	for _, p := range badPaths {
		if _, err := normalizeRuleSetRelativePath(p, "demo", "yaml"); err == nil {
			t.Fatalf("expected normalizeRuleSetRelativePath to reject %q", p)
		}
		if _, err := resolveRuleSetDiskPath(a.mihomo.dir, p); err == nil {
			t.Fatalf("expected resolveRuleSetDiskPath to reject %q", p)
		}
	}

	// Ensure symlinks inside ruleset/ cannot escape mihomo dir.
	if runtime.GOOS != "windows" {
		outside := t.TempDir()
		rulesetDir := filepath.Join(a.mihomo.dir, "ruleset")
		if err := os.MkdirAll(rulesetDir, 0700); err != nil {
			t.Fatalf("mkdir ruleset: %v", err)
		}
		if err := os.Symlink(outside, filepath.Join(rulesetDir, "escape")); err == nil {
			if _, err := resolveRuleSetDiskPath(a.mihomo.dir, "./ruleset/escape/leak.yaml"); err == nil {
				t.Fatalf("expected resolveRuleSetDiskPath to reject symlink escape")
			}
		}
	}

	// Attempting to read ./config.json via custom.yaml file rule-provider without payload must fail.
	leakYAML := `
rule-providers:
  leak:
    type: file
    behavior: domain
    format: yaml
    path: ./config.json
rules:
  - MATCH,DIRECT
`
	rec := adminRequest(h, "PUT", "/api/mihomo/custom/all/yaml", map[string]any{"content": leakYAML}, cookie, "")
	if rec.Code == 200 {
		t.Fatalf("expected PUT custom/all/yaml with ./config.json rule-provider to fail, got 200")
	}
}

func TestMihomoHardeningOverlaySanitizationErrorRedactionAndLifecycle(t *testing.T) {
	// 1. Subscription fetch errors must redact token/query URLs.
	rawSubURL := "https://airport.example.test/api/v1/client/subscribe?token=SUPER_SECRET_TOKEN_123"
	uErr := &url.Error{Op: "Get", URL: rawSubURL, Err: errors.New("dial tcp: connection refused for " + rawSubURL)}
	safeMsg := sanitizeFetchError(uErr)
	if strings.Contains(safeMsg, "SUPER_SECRET_TOKEN_123") {
		t.Fatalf("sanitizeFetchError leaked subscription token: %q", safeMsg)
	}

	// 2. Managed security keys & provider paths cannot be bypassed in custom.yaml or injectManagedKeys.
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	for _, badKeyYAML := range []string{
		"skip-auth-prefixes:\n  - 0.0.0.0/0\n",
		"external-controller-tls: 0.0.0.0:9443\n",
		"external-controller-unix: /tmp/mihomo.sock\n",
		"external-ui-url: https://evil.example/ui.zip\n",
		"mixed-port: 9999\n",
	} {
		rec := adminRequest(h, "PUT", "/api/mihomo/custom/all/yaml", map[string]any{"content": badKeyYAML + "rules:\n  - MATCH,DIRECT\n"}, cookie, "")
		if rec.Code != 400 {
			t.Fatalf("expected 400 for managed key in custom.yaml %q, got %d", badKeyYAML, rec.Code)
		}
	}

	// Also verify reservedOverlayKeys blocks overlay bypass directly during synthesis.
	cfg := defaultMihomoConfig()
	cfg.CustomOverlayYAML = "skip-auth-prefixes: [0.0.0.0/0]\nexternal-controller-tls: 0.0.0.0:9999\nmixed-port: 12345\n"
	outBytes, err := synthesizeMihomoYAML(nil, cfg)
	if err != nil {
		t.Fatalf("synthesizeMihomoYAML unexpected err: %v", err)
	}
	outStr := string(outBytes)
	if strings.Contains(outStr, "0.0.0.0/0") || strings.Contains(outStr, "external-controller-tls") || strings.Contains(outStr, "12345") {
		t.Fatalf("reserved overlay keys leaked into runtime.yaml:\n%s", outStr)
	}

	// Manual YAML injectManagedKeys must strip dangerous controller keys and reject escaping provider paths.
	manualWithDanger := []byte("mixed-port: 7890\nskip-auth-prefixes: [0.0.0.0/0]\nexternal-controller-unix: /tmp/ctrl.sock\nexternal-ui: ./ui\n")
	injected, err := injectManagedKeys(manualWithDanger, cfg)
	if err != nil {
		t.Fatalf("injectManagedKeys failed on strippable keys: %v", err)
	}
	if strings.Contains(string(injected), "external-controller-unix") || strings.Contains(string(injected), "external-ui") || strings.Contains(string(injected), "0.0.0.0/0") {
		t.Fatalf("injectManagedKeys did not strip dangerous keys:\n%s", string(injected))
	}
	for _, badManual := range []string{
		"mixed-port: 7890\nrule-providers:\n  evil:\n    type: file\n    path: ./config.json\n",
		"mixed-port: 7890\nproxy-providers:\n  evil:\n    type: file\n    path: ../secret.yaml\n",
	} {
		if _, err := injectManagedKeys([]byte(badManual), cfg); err == nil {
			t.Fatalf("expected injectManagedKeys to reject escaping provider path in %q", badManual)
		}
	}

	// 3. /api/mihomo/ctrl mutating/WS requests must reject cross-site/same-site origins.
	a.mihomo.mu.Lock()
	a.mihomo.running = true
	a.mihomo.ctrlBaseURL = "http://127.0.0.1:1"
	a.mihomo.mu.Unlock()
	req := httptest.NewRequest("PUT", "/api/mihomo/ctrl/configs", strings.NewReader(`{}`))
	req.AddCookie(cookie)
	req.Header.Set("Sec-Fetch-Site", "same-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("expected same-site PUT /api/mihomo/ctrl/configs to be blocked with 403, got %d", w.Code)
	}
	a.mihomo.mu.Lock()
	a.mihomo.running = false
	a.mihomo.ctrlBaseURL = ""
	a.mihomo.mu.Unlock()

	// 4. Concurrent Start() calls must serialize cleanly without spawning orphan processes.
	if runtime.GOOS != "windows" {
		testBin, binErr := os.Executable()
		if binErr != nil {
			t.Fatal(binErr)
		}
		dir := t.TempDir()
		binDir := filepath.Join(dir, "bin")
		_ = os.MkdirAll(binDir, 0700)
		wrapper := filepath.Join(binDir, "mihomo")
		script := fmt.Sprintf("#!/bin/sh\nGATEHOME_MIHOMO_HELPER=1 exec %q -test.run=^TestMihomoHelperProcess$ -- \"$@\"\n", testBin)
		if err := os.WriteFile(wrapper, []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
		freePort := func() int {
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			p := l.Addr().(*net.TCPAddr).Port
			_ = l.Close()
			return p
		}
		lcCfg := defaultMihomoConfig()
		lcCfg.ControllerPort = freePort()
		lcCfg.Settings.MixedPort = freePort()
		mgr := &mihomoManager{dir: dir, cfg: lcCfg}
		defer func() { _ = mgr.Stop() }()

		var wg sync.WaitGroup
		errs := make([]error, 3)
		for i := range 3 {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				errs[idx] = mgr.Start()
			}(i)
		}
		wg.Wait()
		for i, e := range errs {
			if e != nil {
				t.Fatalf("concurrent Start #%d failed: %v", i, e)
			}
		}
		if err := mgr.Stop(); err != nil {
			t.Fatalf("Stop after concurrent Start failed: %v", err)
		}
	}

	// 5. DNSListenPort == 0 must delete inherited dns.listen; maskYAMLSecret must not duplicate header comments.
	dnsCfg := defaultMihomoConfig()
	dnsCfg.Settings.DNSListenPort = 0
	srcWithDNSListen := []byte("dns:\n  enable: true\n  listen: 0.0.0.0:5353\n  nameserver:\n    - 1.1.1.1\n")
	mergedDNS, err := synthesizeMihomoYAML(srcWithDNSListen, dnsCfg)
	if err != nil {
		t.Fatalf("synthesizeMihomoYAML dns test failed: %v", err)
	}
	if strings.Contains(string(mergedDNS), "5353") {
		t.Fatalf("expected dns.listen removed when DNSListenPort == 0:\n%s", string(mergedDNS))
	}
	masked1 := maskYAMLSecret(mergedDNS)
	masked2 := maskYAMLSecret([]byte(masked1))
	if strings.Count(masked2, mihomoHeaderComment) > 1 {
		t.Fatalf("maskYAMLSecret duplicated header comment:\n%s", masked2)
	}
}

func TestMihomoLogRingRedactsSensitivePatterns(t *testing.T) {
	var ring mihomoLogRing
	ring.add("normal proxy connection ready")
	ring.add("header Authorization: Basic abcdef")
	ring.add("token header Bearer super-secret-jwt")
	ring.add("upstream config password=my-secret")
	ring.add("socks auth pass=hunter2")
	ring.add("url query secret=topsecret")
	ring.add("url query token=xyz123")

	snap := ring.snapshot()
	if len(snap) != 7 {
		t.Fatalf("expected 7 log lines, got %d", len(snap))
	}
	if snap[0] != "normal proxy connection ready" {
		t.Fatalf("expected normal line intact, got %q", snap[0])
	}
	for i := 1; i < len(snap); i++ {
		if snap[i] != "[redacted sensitive log line]" {
			t.Fatalf("expected redacted placeholder at index %d, got %q", i, snap[i])
		}
	}
}

func TestMihomoExtendedGroupFieldsAndPCRERegex(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	// 1. PCRE lookahead/lookbehind filter regex works directly.
	re, err := compileMihomoRegex(`^(?=.*(?<!深)(港|HK|(?i)Hong))(?!(.*(台|本|坡|美))).*$`)
	if err != nil {
		t.Fatalf("compileMihomoRegex PCRE failed: %v", err)
	}
	if !re.MatchString("HK-01") || !re.MatchString("HongKong-IPLC") || re.MatchString("深港专线") || re.MatchString("香港转日本") {
		t.Fatalf("unexpected PCRE match results on sample node names")
	}

	// 2. Full user-reported YAML (extra Mihomo group fields + PCRE regexes) passes PUT /api/mihomo/custom/groups/yaml and round-trips.
	groupsYAML := `# GateHome 自定义策略组
proxy-groups:
  - name: OpenAI
    type: fallback
    filter: 日本|IEPL
    url: https://www.gstatic.com/generate_204
    interval: 300
  - name: '香港专线'
    type: fallback
    icon: 'https://gitea.nn1nn.top:39919/mosetian/MySelfRules/raw/branch/main/icon/IconSet/Color/Hong_Kong.png'
    include-all: true
    filter: '^(?=.*(?<!深)(港|HK|(?i)Hong))(?!(.*(台|本|坡|美))).*$'
    url: 'https://www.gstatic.com/generate_204'
    interval: 30
    tolerance: 20
    lazy: true
    timeout: 10000
    max-failed-times: 2
  - name: '日本专线'
    type: fallback
    icon: 'https://gitea.nn1nn.top:39919/mosetian/MySelfRules/raw/branch/main/icon/IconSet/Color/JP.png'
    include-all: true
    filter: '^(?=.*(?i:日|🇯🇵|JP|Japan|NRT|HND|KIX|CTS|FUK)).*$'
    url: 'https://detectportal.firefox.com/success.txt'
    interval: 45
    tolerance: 0
    lazy: false
    timeout: 2000
    uselightgbm: false
    collectdata: false
    sample-rate: 1
    prefer-asn: false
`
	if rec := adminRequest(h, "PUT", "/api/mihomo/custom/groups/yaml", map[string]any{"content": groupsYAML, "validate_only": true}, cookie, ""); rec.Code != 200 {
		t.Fatalf("validate extended groups yaml failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminRequest(h, "PUT", "/api/mihomo/custom/groups/yaml", map[string]any{"content": groupsYAML}, cookie, ""); rec.Code != 200 {
		t.Fatalf("save extended groups yaml failed: %d %s", rec.Code, rec.Body.String())
	}

	rtBytes, err := os.ReadFile(a.mihomo.runtimePath())
	if err != nil {
		t.Fatalf("read runtime.yaml failed: %v", err)
	}
	rt := string(rtBytes)
	for _, want := range []string{
		"include-all: true",
		"max-failed-times: 2",
		"timeout: 10000",
		"uselightgbm: false",
		"collectdata: false",
		"sample-rate: 1",
		"prefer-asn: false",
		"tolerance: 20",
		"^(?=.*(?<!深)(港|HK|(?i)Hong))(?!(.*(台|本|坡|美))).*$",
	} {
		if !strings.Contains(rt, want) {
			t.Fatalf("runtime.yaml missing %q:\n%s", want, rt)
		}
	}

	// Editing a group via UI form without sending extra preserves its extended YAML fields.
	rec := adminRequest(h, "GET", "/api/mihomo/custom/groups/yaml", nil, cookie, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "uselightgbm: false") {
		t.Fatalf("groups yaml GET lost extended fields: %d %s", rec.Code, rec.Body.String())
	}
	hk := a.mihomo.cfg.CustomGroups[1]
	hk.Interval = 60
	hk.Extra = nil
	if rec := adminRequest(h, "POST", "/api/mihomo/groups", hk, cookie, ""); rec.Code != 200 {
		t.Fatalf("UI group update failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = adminRequest(h, "GET", "/api/mihomo/custom/groups/yaml", nil, cookie, "")
	if !strings.Contains(rec.Body.String(), "max-failed-times: 2") || !strings.Contains(rec.Body.String(), "interval: 60") {
		t.Fatalf("UI update stripped extra fields: %s", rec.Body.String())
	}
}

func TestMihomoRuleSetStatsAndBatchUpdate(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	// 1. Add manual YAML, remote HTTP, builtin MRS, and disabled HTTP rule-sets.
	for _, body := range []map[string]any{
		{
			"name":     "local-yaml",
			"source":   "manual",
			"behavior": "domain",
			"format":   "yaml",
			"path":     "./ruleset/local-yaml.yaml",
			"content":  "+.corp.local\n+.intranet.local\n",
			"enabled":  true,
		},
		{
			"name":     "remote-ai",
			"source":   "http",
			"behavior": "domain",
			"format":   "yaml",
			"url":      "https://example.com/ai.yaml",
			"path":     "./ruleset/remote-ai.yaml",
			"interval": 3600,
			"enabled":  true,
		},
		{
			"name":     "openai",
			"source":   "builtin",
			"preset":   "openai",
			"behavior": "domain",
			"format":   "mrs",
			"url":      "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/openai.mrs",
			"path":     "./ruleset/openai.mrs",
			"interval": 86400,
			"enabled":  true,
		},
		{
			"name":     "disabled-rs",
			"source":   "http",
			"behavior": "ipcidr",
			"format":   "text",
			"url":      "https://example.com/disabled.txt",
			"path":     "./ruleset/disabled.txt",
			"interval": 3600,
			"enabled":  false,
		},
	} {
		if rec := adminRequest(h, "POST", "/api/mihomo/rule-sets", body, cookie, ""); rec.Code != 200 {
			t.Fatalf("create ruleset %v failed: %d %s", body["name"], rec.Code, rec.Body.String())
		}
	}

	// 2. Offline stats report manual rule count without running controller.
	rec := adminRequest(h, "GET", "/api/mihomo/rule-sets/stats", nil, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("offline stats failed: %d %s", rec.Code, rec.Body.String())
	}
	var offlineResp struct {
		Live      bool                                `json:"live"`
		Providers map[string]MihomoRuleSetRuntimeInfo `json:"providers"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &offlineResp)
	if offlineResp.Live {
		t.Fatalf("expected offline stats live=false")
	}
	if offlineResp.Providers["local-yaml"].RuleCount == nil || *offlineResp.Providers["local-yaml"].RuleCount != 2 {
		t.Fatalf("expected manual rule count 2, got %+v", offlineResp.Providers["local-yaml"])
	}

	// 3. Attach mock Mihomo controller returning live provider stats and handling PUT updates.
	var mu sync.Mutex
	counts := map[string]int{"remote-ai": 42, "openai": 128}
	putCalls := map[string]int{}
	ctrl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/providers/rules":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"providers": map[string]any{
					"remote-ai": map[string]any{
						"name":        "remote-ai",
						"ruleCount":   counts["remote-ai"],
						"updatedAt":   "2026-11-24T12:00:00Z",
						"vehicleType": "HTTP",
					},
					"openai": map[string]any{
						"name":        "openai",
						"ruleCount":   counts["openai"],
						"updatedAt":   "2026-11-24T12:00:00Z",
						"vehicleType": "HTTP",
					},
				},
			})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/providers/rules/"):
			name := strings.TrimPrefix(r.URL.Path, "/providers/rules/")
			putCalls[name]++
			counts[name] += 5
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ctrl.Close()

	a.mihomo.mu.Lock()
	a.mihomo.ctrlBaseURL = ctrl.URL
	a.mihomo.mu.Unlock()

	// 4. Batch update updates enabled remote/builtin providers and skips manual & disabled ones.
	rec = adminRequest(h, "POST", "/api/mihomo/rule-sets/update", map[string]any{}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("batch update failed: %d %s", rec.Code, rec.Body.String())
	}
	var batchResp struct {
		Updated   int                                 `json:"updated"`
		Failed    int                                 `json:"failed"`
		Live      bool                                `json:"live"`
		Providers map[string]MihomoRuleSetRuntimeInfo `json:"providers"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &batchResp)
	if !batchResp.Live || batchResp.Updated != 2 || batchResp.Failed != 0 {
		t.Fatalf("unexpected batch update result: %+v", batchResp)
	}
	if putCalls["remote-ai"] != 1 || putCalls["openai"] != 1 || putCalls["local-yaml"] != 0 || putCalls["disabled-rs"] != 0 {
		t.Fatalf("unexpected PUT calls: %+v", putCalls)
	}
	if batchResp.Providers["remote-ai"].RuleCount == nil || *batchResp.Providers["remote-ai"].RuleCount != 47 ||
		batchResp.Providers["openai"].RuleCount == nil || *batchResp.Providers["openai"].RuleCount != 133 ||
		batchResp.Providers["local-yaml"].RuleCount == nil || *batchResp.Providers["local-yaml"].RuleCount != 2 {
		t.Fatalf("unexpected updated provider counts: %+v", batchResp.Providers)
	}

	// 5. Reject updating manual rule-set explicitly, allow single-target update.
	if rec := adminRequest(h, "POST", "/api/mihomo/rule-sets/update", map[string]any{"names": []string{"local-yaml"}}, cookie, ""); rec.Code != 400 {
		t.Fatalf("expected 400 updating manual rule-set, got %d", rec.Code)
	}
	if rec := adminRequest(h, "POST", "/api/mihomo/rule-sets/update", map[string]any{"names": []string{"remote-ai"}}, cookie, ""); rec.Code != 200 {
		t.Fatalf("single ruleset update failed: %d %s", rec.Code, rec.Body.String())
	}
	if putCalls["remote-ai"] != 2 {
		t.Fatalf("expected single update to call PUT again, got %d", putCalls["remote-ai"])
	}
}

func TestMihomoSubscriptionExcludeGroupsAndRules(t *testing.T) {
	sourceYAML := []byte(`
proxies:
  - { name: "公告: 到期", type: ss, server: 1.1.1.1, port: 443 }
  - { name: "HK-01", type: ss, server: 1.1.1.2, port: 443 }
  - { name: "JP-01", type: ss, server: 1.1.1.3, port: 443 }
proxy-groups:
  - name: "机场总览"
    type: select
    proxies: ["HK-01", "JP-01", "DIRECT"]
  - name: "机场测速"
    type: url-test
    proxies: ["HK-01", "JP-01"]
rule-providers:
  airport-ad:
    type: http
    behavior: domain
    format: yaml
    url: https://example.com/ad.yaml
    path: ./ruleset/airport-ad.yaml
rules:
  - "RULE-SET,airport-ad,REJECT"
  - "DOMAIN-SUFFIX,airport.example,机场总览"
  - "MATCH,机场总览"
`)

	cfg := defaultMihomoConfig()
	cfg.ActiveSubscriptionID = "sub-1"
	cfg.Subscriptions = []MihomoSubscription{{
		ID:                    "sub-1",
		Name:                  "纯节点机场",
		IncludeFilter:         "HK|JP",
		NodeSuffix:            " [自用]",
		ExcludeGroupsAndRules: true,
	}}
	cfg.CustomGroups = []MihomoCustomGroup{{
		ID:      "grp-custom",
		Name:    "漏网之鱼",
		Type:    "select",
		Proxies: []string{"ALL_NODES", "DIRECT"},
	}}
	cfg.CustomRuleSets = []MihomoRuleSet{{
		ID:       "rs-custom",
		Name:     "my-ai",
		Source:   "manual",
		Behavior: "domain",
		Format:   "yaml",
		Path:     "./ruleset/manual/my-ai.yaml",
		Content:  "payload:\n  - '+.openai.com'\n",
		Enabled:  true,
	}}
	cfg.CustomRules = []MihomoCustomRule{
		{ID: "r1", Type: "RULE-SET", Value: "my-ai", Target: "漏网之鱼", Enabled: true},
		{ID: "r2", Type: "MATCH", Target: "漏网之鱼", Enabled: true},
	}

	out, err := synthesizeMihomoYAML(sourceYAML, cfg)
	if err != nil {
		t.Fatalf("synthesize with ExcludeGroupsAndRules failed: %v", err)
	}
	text := string(out)
	groups, nodes := extractYAMLGroupsAndNodes(out, cfg.CustomGroups)
	if len(nodes) != 2 || nodes[0] != "HK-01 [自用]" || nodes[1] != "JP-01 [自用]" {
		t.Fatalf("expected transformed proxies kept, got %v", nodes)
	}
	if len(groups) != 1 || groups[0].Name != "漏网之鱼" {
		t.Fatalf("expected only custom proxy-groups, got %+v", groups)
	}
	for _, unwanted := range []string{"机场总览", "机场测速", "airport-ad", "airport.example"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("expected airport group/provider/rule %q excluded from runtime.yaml:\n%s", unwanted, text)
		}
	}
	for _, wanted := range []string{"my-ai", "RULE-SET,my-ai,漏网之鱼", "MATCH,漏网之鱼"} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("expected custom entry %q in runtime.yaml:\n%s", wanted, text)
		}
	}

	// API persistence & toggle round-trip
	_, h := testAdmin(t)
	cookie := loginForTest(t, h)

	if rec := adminRequest(h, "POST", "/api/mihomo/groups", map[string]any{
		"name":    "漏网之鱼",
		"type":    "select",
		"proxies": []string{"ALL_NODES", "DIRECT"},
	}, cookie, ""); rec.Code != 200 {
		t.Fatalf("add custom group failed: %d %s", rec.Code, rec.Body.String())
	}

	rec := adminRequest(h, "POST", "/api/mihomo/subscriptions", map[string]any{
		"name":                     "机场B",
		"yaml_content":             string(sourceYAML),
		"exclude_groups_and_rules": true,
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("POST subscription with exclude_groups_and_rules failed: %d %s", rec.Code, rec.Body.String())
	}
	var st mihomoStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode status failed: %v", err)
	}
	if len(st.Subscriptions) != 1 || !st.Subscriptions[0].ExcludeGroupsAndRules {
		t.Fatalf("expected exclude_groups_and_rules persisted in view, got %+v", st.Subscriptions)
	}
	subID := st.Subscriptions[0].ID

	rtRes := adminRequest(h, "GET", "/api/mihomo/config/yaml?kind=runtime", nil, cookie, "")
	if rtRes.Code != 200 {
		t.Fatalf("GET runtime yaml failed: %d", rtRes.Code)
	}
	var rtBody struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(rtRes.Body.Bytes(), &rtBody)
	if strings.Contains(rtBody.Content, "机场总览") || strings.Contains(rtBody.Content, "airport-ad") {
		t.Fatalf("expected runtime yaml to exclude airport groups/rules:\n%s", rtBody.Content)
	}

	// Disable exclude_groups_and_rules via PUT and confirm airport groups/rules are restored
	putRes := adminRequest(h, "PUT", "/api/mihomo/subscriptions/"+subID, map[string]any{
		"name":                     "机场B",
		"exclude_groups_and_rules": false,
	}, cookie, "")
	if putRes.Code != 200 {
		t.Fatalf("PUT subscription failed: %d %s", putRes.Code, putRes.Body.String())
	}
	var putSt mihomoStatusResponse
	_ = json.Unmarshal(putRes.Body.Bytes(), &putSt)
	if putSt.Subscriptions[0].ExcludeGroupsAndRules {
		t.Fatalf("expected exclude_groups_and_rules disabled after PUT: %+v", putSt.Subscriptions[0])
	}
	rtRes = adminRequest(h, "GET", "/api/mihomo/config/yaml?kind=runtime", nil, cookie, "")
	_ = json.Unmarshal(rtRes.Body.Bytes(), &rtBody)
	if !strings.Contains(rtBody.Content, "机场总览") || !strings.Contains(rtBody.Content, "airport-ad") {
		t.Fatalf("expected airport groups/rules restored after disabling exclusion:\n%s", rtBody.Content)
	}
}

func TestMihomoMatchCustomRuleYAMLAndAPI(t *testing.T) {
	if _, err := parseCustomRulesYAML([]byte("rules:\n  - MATCH\n"), nil); err == nil {
		t.Fatal("expected single-token MATCH rule to fail parsing")
	}
	if _, err := parseCustomRulesYAML([]byte("rules:\n  - MATCH,extra,漏网之鱼\n"), nil); err == nil {
		t.Fatal("expected MATCH with non-empty middle value to fail parsing")
	}

	_, h := testAdmin(t)
	cookie := loginForTest(t, h)

	// Add a normal rule, then a MATCH rule (must append to bottom), then another normal rule (must prepend to top).
	for _, body := range []map[string]any{
		{"type": "DOMAIN-SUFFIX", "value": "example.com", "target": "DIRECT", "enabled": true},
		{"type": "MATCH", "value": "ignored", "target": "漏网之鱼", "no_resolve": true, "enabled": true},
		{"type": "DOMAIN-SUFFIX", "value": "first.example", "target": "REJECT", "enabled": true},
	} {
		rec := adminRequest(h, "POST", "/api/mihomo/rules", body, cookie, "")
		if rec.Code != 200 {
			t.Fatalf("POST /api/mihomo/rules failed: %d %s", rec.Code, rec.Body.String())
		}
	}

	statusRes := adminRequest(h, "GET", "/api/mihomo/status", nil, cookie, "")
	if statusRes.Code != 200 {
		t.Fatalf("GET status failed: %d", statusRes.Code)
	}
	var st mihomoStatusResponse
	_ = json.Unmarshal(statusRes.Body.Bytes(), &st)
	if len(st.CustomRules) != 3 {
		t.Fatalf("expected 3 custom rules, got %+v", st.CustomRules)
	}
	if st.CustomRules[0].Value != "first.example" || st.CustomRules[1].Value != "example.com" {
		t.Fatalf("unexpected non-MATCH ordering: %+v", st.CustomRules)
	}
	matchRule := st.CustomRules[2]
	if matchRule.Type != "MATCH" || matchRule.Value != "" || matchRule.Target != "漏网之鱼" || matchRule.NoResolve {
		t.Fatalf("unexpected MATCH rule stored: %+v", matchRule)
	}

	// GET custom rules YAML should render "- MATCH,漏网之鱼" cleanly
	rulesYAMLRes := adminRequest(h, "GET", "/api/mihomo/custom/rules/yaml", nil, cookie, "")
	if rulesYAMLRes.Code != 200 {
		t.Fatalf("GET custom rules yaml failed: %d", rulesYAMLRes.Code)
	}
	var yamlResp struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(rulesYAMLRes.Body.Bytes(), &yamlResp)
	if !strings.Contains(yamlResp.Content, "- MATCH,漏网之鱼") {
		t.Fatalf("expected '- MATCH,漏网之鱼' in custom rules YAML:\n%s", yamlResp.Content)
	}

	// PUT custom rules YAML round-trips "- MATCH,漏网之鱼"
	putYAML := "rules:\n  - DOMAIN-SUFFIX,openai.com,DIRECT\n  - MATCH,漏网之鱼\n"
	putRes := adminRequest(h, "PUT", "/api/mihomo/custom/rules/yaml", map[string]any{"content": putYAML}, cookie, "")
	if putRes.Code != 200 {
		t.Fatalf("PUT custom rules yaml with MATCH failed: %d %s", putRes.Code, putRes.Body.String())
	}
	var putBody struct {
		OK      bool                 `json:"ok"`
		Content string               `json:"content"`
		Status  mihomoStatusResponse `json:"status"`
	}
	_ = json.Unmarshal(putRes.Body.Bytes(), &putBody)
	if len(putBody.Status.CustomRules) != 2 || putBody.Status.CustomRules[1].Type != "MATCH" || putBody.Status.CustomRules[1].Target != "漏网之鱼" || putBody.Status.CustomRules[1].Value != "" {
		t.Fatalf("unexpected parsed custom rules after YAML save: %+v", putBody.Status.CustomRules)
	}

	// Runtime YAML should include MATCH,漏网之鱼
	rtRes := adminRequest(h, "GET", "/api/mihomo/config/yaml?kind=runtime", nil, cookie, "")
	_ = json.Unmarshal(rtRes.Body.Bytes(), &yamlResp)
	if !strings.Contains(yamlResp.Content, "MATCH,漏网之鱼") {
		t.Fatalf("expected MATCH,漏网之鱼 in runtime.yaml:\n%s", yamlResp.Content)
	}
}

func TestMihomoTelemetryEndpoint(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	var connCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/connections":
			connCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"downloadTotal": int64(connCalls * 1024 * 100),
				"uploadTotal":   int64(connCalls * 1024 * 20),
				"memory":        int64(42 * 1024 * 1024),
				"connections": []map[string]any{
					{"id": "c1", "upload": 100, "download": 200},
					{"id": "c2", "upload": 300, "download": 400},
				},
			})
		case "/proxies":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					"节点选择":  map[string]any{"name": "节点选择", "type": "Selector", "now": "HK-01", "all": []string{"HK-01"}},
					"HK-01": map[string]any{"name": "HK-01", "type": "Shadowsocks"},
				},
			})
		case "/providers/rules":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"providers": map[string]any{
					"ai": map[string]any{"name": "ai", "ruleCount": 120, "vehicleType": "HTTP"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a.mihomo.mu.Lock()
	a.mihomo.ctrlBaseURL = srv.URL
	a.mihomo.cfg.CustomRules = []MihomoCustomRule{{ID: "r1", Type: "MATCH", Target: "DIRECT", Enabled: true}}
	a.mihomo.cfg.CustomRuleSets = []MihomoRuleSet{{ID: "rs1", Name: "ai", Source: "http", Behavior: "domain", Format: "yaml", URL: "https://example.com/ai.yaml", Path: "./ruleset/ai.yaml", Enabled: true}}
	a.mihomo.mu.Unlock()

	rec1 := adminRequest(h, "GET", "/api/mihomo/telemetry", nil, cookie, "")
	if rec1.Code != 200 {
		t.Fatalf("GET /api/mihomo/telemetry failed: %d %s", rec1.Code, rec1.Body.String())
	}
	var tel1 mihomoTelemetryResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &tel1); err != nil {
		t.Fatalf("decode telemetry failed: %v", err)
	}
	if tel1.ActiveConnections != 2 || tel1.DownloadTotal != 102400 || tel1.UploadTotal != 20480 || tel1.HeapInuseBytes != 42*1024*1024 {
		t.Fatalf("unexpected telemetry payload #1: %+v", tel1)
	}
	if tel1.GroupCount != 1 || tel1.NodeCount != 1 || !tel1.RulesLive || tel1.TotalRulesCount != 121 {
		t.Fatalf("unexpected scale counts: %+v", tel1)
	}

	time.Sleep(250 * time.Millisecond)
	rec2 := adminRequest(h, "GET", "/api/mihomo/telemetry", nil, cookie, "")
	var tel2 mihomoTelemetryResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &tel2)
	if tel2.DownRate <= 0 || tel2.UpRate <= 0 {
		t.Fatalf("expected positive computed rates on second sample, got down=%d up=%d", tel2.DownRate, tel2.UpRate)
	}
}

func TestMihomoCoreCheckUpdateAndDownload(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	assetName := fmt.Sprintf("mihomo-%s-%s-compatible-v1.19.99.gz", runtime.GOOS, runtime.GOARCH)
	payloadBytes, _ := json.Marshal(map[string]any{
		"tag_name":     "v1.19.99",
		"html_url":     "https://github.com/MetaCubeX/mihomo/releases/tag/v1.19.99",
		"published_at": "2026-11-25T00:00:00Z",
		"assets": []map[string]any{
			{"name": assetName, "browser_download_url": "https://github.com/MetaCubeX/mihomo/releases/download/v1.19.99/" + assetName, "size": 12345678},
		},
	})

	a.mihomo.mu.Lock()
	a.mihomo.coreHTTPClient = onlineTestClient(payloadBytes)
	a.mihomo.version = "Mihomo Meta v1.19.16 darwin arm64"
	a.mihomo.mu.Unlock()

	rec := adminRequest(h, "POST", "/api/mihomo/core/check-update", nil, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("POST /api/mihomo/core/check-update failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK         bool                 `json:"ok"`
		CoreUpdate mihomoCoreUpdateInfo `json:"core_update"`
		Status     mihomoStatusResponse `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode check-update failed: %v", err)
	}
	if !resp.OK || !resp.CoreUpdate.UpdateAvailable || resp.CoreUpdate.LatestVersion != "v1.19.99" || resp.CoreUpdate.AssetName != assetName {
		t.Fatalf("unexpected core update response: %+v", resp.CoreUpdate)
	}
	if resp.Status.CoreUpdate == nil || resp.Status.CoreUpdate.LatestVersion != "v1.19.99" {
		t.Fatalf("expected status snapshot to cache core_update: %+v", resp.Status.CoreUpdate)
	}

	if isMihomoUpdateNewer("Mihomo Meta v1.19.99", "v1.19.99") {
		t.Fatal("same version must not report newer update")
	}
}

func TestMihomoStandaloneBackupRestore(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	// Populate subscription, custom group, rule-set, and MATCH rule.
	subYAML := "proxies:\n  - { name: \"HK-01\", type: ss, server: 1.2.3.4, port: 443 }\n"
	if rec := adminRequest(h, "POST", "/api/mihomo/subscriptions", map[string]any{
		"name":                     "主订阅",
		"yaml_content":             subYAML,
		"exclude_groups_and_rules": true,
	}, cookie, ""); rec.Code != 200 {
		t.Fatalf("add subscription failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminRequest(h, "POST", "/api/mihomo/groups", map[string]any{
		"name": "HK组", "type": "select", "proxies": []string{"ALL_NODES", "DIRECT"},
	}, cookie, ""); rec.Code != 200 {
		t.Fatalf("add group failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminRequest(h, "POST", "/api/mihomo/rule-sets", map[string]any{
		"name": "manual-lan", "source": "manual", "behavior": "domain", "format": "text", "content": "+.lan.local",
	}, cookie, ""); rec.Code != 200 {
		t.Fatalf("add ruleset failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminRequest(h, "POST", "/api/mihomo/rules", map[string]any{
		"type": "MATCH", "target": "HK组", "enabled": true,
	}, cookie, ""); rec.Code != 200 {
		t.Fatalf("add match rule failed: %d %s", rec.Code, rec.Body.String())
	}

	exportRec := adminRequest(h, "POST", "/api/mihomo/backup", nil, cookie, "")
	if exportRec.Code != 200 {
		t.Fatalf("POST /api/mihomo/backup failed: %d %s", exportRec.Code, exportRec.Body.String())
	}
	var exported mihomoStandaloneBackup
	if err := json.Unmarshal(exportRec.Body.Bytes(), &exported); err != nil {
		t.Fatalf("decode backup export failed: %v", err)
	}
	if exported.Format != mihomoBackupFormat || len(exported.Subscriptions) != 1 || len(exported.ManualRuleSetFiles) != 1 {
		t.Fatalf("unexpected exported backup: format=%s subs=%d manuals=%d", exported.Format, len(exported.Subscriptions), len(exported.ManualRuleSetFiles))
	}

	// Clear Mihomo config & files to simulate clean host, then restore from backup.
	a.mihomo.mu.Lock()
	a.mihomo.cfg = defaultMihomoConfig()
	_ = saveMihomoConfig(a.mihomo.dir, a.mihomo.cfg)
	a.mihomo.mu.Unlock()
	_ = os.RemoveAll(filepath.Join(a.mihomo.dir, "subscriptions"))
	_ = os.RemoveAll(filepath.Join(a.mihomo.dir, "ruleset"))

	restoreRec := adminRequest(h, "POST", "/api/mihomo/restore", exported, cookie, "")
	if restoreRec.Code != 200 {
		t.Fatalf("POST /api/mihomo/restore failed: %d %s", restoreRec.Code, restoreRec.Body.String())
	}
	var resBody struct {
		OK     bool                 `json:"ok"`
		Status mihomoStatusResponse `json:"status"`
	}
	_ = json.Unmarshal(restoreRec.Body.Bytes(), &resBody)
	if !resBody.OK || len(resBody.Status.Subscriptions) != 1 || !resBody.Status.Subscriptions[0].ExcludeGroupsAndRules || len(resBody.Status.CustomGroups) != 1 || len(resBody.Status.CustomRules) != 1 || resBody.Status.CustomRuleSets[0].Name != "manual-lan" {
		t.Fatalf("unexpected restored mihomo status: %+v", resBody.Status)
	}
	manualPath := filepath.Join(a.mihomo.dir, "ruleset", "manual-lan.txt")
	if b, err := os.ReadFile(manualPath); err != nil || !strings.Contains(string(b), "+.lan.local") {
		t.Fatalf("expected manual ruleset file restored at %s, err=%v", manualPath, err)
	}

	// Invalid backup format must be rejected
	bad := exported
	bad.Format = "invalid-format"
	if rec := adminRequest(h, "POST", "/api/mihomo/restore", bad, cookie, ""); rec.Code != 400 {
		t.Fatalf("expected 400 for invalid backup format, got %d", rec.Code)
	}
}

func TestMihomoGlobalBackupAndRestoreIntegration(t *testing.T) {
	dir, state := fullBackupFixture(t)
	paths := legacyStorage(dir)
	m := newMihomoManager(paths, nil, 9090)

	subID := "sub-abc123"
	m.cfg.ActiveSubscriptionID = subID
	m.cfg.Subscriptions = []MihomoSubscription{{ID: subID, Name: "GlobalSub", ExcludeGroupsAndRules: true}}
	m.cfg.CustomGroups = []MihomoCustomGroup{{ID: "grp-1", Name: "Custom", Type: "select", Proxies: []string{"DIRECT"}}}
	m.cfg.CustomRules = []MihomoCustomRule{{ID: "rule-1", Type: "MATCH", Target: "Custom", Enabled: true}}
	m.cfg.CustomRuleSets = []MihomoRuleSet{{
		ID: "rs-1", Name: "lan-rules", Source: "manual", Behavior: "domain", Format: "text",
		Path: "./ruleset/manual/lan-rules.txt", Content: "+.lan.test", Enabled: true,
	}}
	if err := os.MkdirAll(filepath.Join(m.dir, "subscriptions"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(m.subscriptionFile(subID), []byte("proxies:\n  - { name: \"N1\", type: ss, server: 1.2.3.4, port: 443 }\n")); err != nil {
		t.Fatal(err)
	}
	if err := saveMihomoConfig(m.dir, m.cfg); err != nil {
		t.Fatal(err)
	}
	if err := m.synthesizeAndWriteLocked(); err != nil {
		t.Fatal(err)
	}
	// Create runtime binary & mmdb that must NOT be bundled into global backup and MUST survive restore.
	if err := os.MkdirAll(filepath.Join(m.dir, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	binFile := filepath.Join(m.dir, "bin", "mihomo")
	if err := os.WriteFile(binFile, []byte("keep-local-binary"), 0755); err != nil {
		t.Fatal(err)
	}

	capInfo, err := inspectBackupCapacity(paths, state.State)
	if err != nil || capInfo.MihomoBytes <= 0 {
		t.Fatalf("expected positive mihomo backup capacity, err=%v cap=%+v", err, capInfo)
	}

	payload, err := snapshotBackupPaths(paths, state.State)
	if err != nil {
		t.Fatalf("snapshotBackupPaths failed: %v", err)
	}
	for _, required := range []string{
		"mihomo/config.json",
		"mihomo/runtime.yaml",
		"mihomo/subscriptions/" + subID + ".yaml",
		"mihomo/ruleset/manual/lan-rules.txt",
	} {
		if _, ok := payload.Files[required]; !ok {
			t.Fatalf("missing %q in global backup payload", required)
		}
	}
	for name := range payload.Files {
		if strings.HasPrefix(name, "mihomo/bin/") {
			t.Fatalf("runtime binary %q must not be bundled into backup", name)
		}
	}

	zipBytes, err := encodeBackup(payload, "pw-mihomo-test")
	if err != nil {
		t.Fatalf("encodeBackup failed: %v", err)
	}
	decoded, _, err := decodeBackup(zipBytes, "pw-mihomo-test")
	if err != nil {
		t.Fatalf("decodeBackup failed: %v", err)
	}

	// Wipe mihomo config/subscriptions/manual rulesets before restore, keeping bin/mihomo
	_ = os.Remove(filepath.Join(m.dir, "config.json"))
	_ = os.Remove(filepath.Join(m.dir, "runtime.yaml"))
	_ = os.RemoveAll(filepath.Join(m.dir, "subscriptions"))
	_ = os.RemoveAll(filepath.Join(m.dir, "ruleset"))

	if err := restorePayload(dir, decoded); err != nil {
		t.Fatalf("restorePayload failed: %v", err)
	}
	if b, err := os.ReadFile(binFile); err != nil || string(b) != "keep-local-binary" {
		t.Fatalf("expected local mihomo binary preserved across global restore, err=%v", err)
	}
	restoredCfg, err := loadMihomoConfig(m.dir)
	if err != nil || restoredCfg.ActiveSubscriptionID != subID || len(restoredCfg.CustomRules) != 1 {
		t.Fatalf("expected restored mihomo config, err=%v cfg=%+v", err, restoredCfg)
	}
	if _, err := os.Stat(filepath.Join(m.dir, "ruleset", "manual", "lan-rules.txt")); err != nil {
		t.Fatalf("expected restored manual ruleset file, err=%v", err)
	}
}

func TestMihomoAddRuleSkipReload(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	var reloadCalls int
	testSecret := "skip-reload-secret"
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /configs", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testSecret {
			http.Error(w, "unauthorized", 401)
			return
		}
		reloadCalls++
		w.WriteHeader(204)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a.mihomo.mu.Lock()
	a.mihomo.running = true
	a.mihomo.ctrlBaseURL = srv.URL
	a.mihomo.ctrlSecret = testSecret
	a.mihomo.mu.Unlock()

	rec := adminRequest(h, "POST", "/api/mihomo/rules", map[string]any{
		"type":        "DOMAIN-SUFFIX",
		"value":       "openai.com",
		"target":      "DIRECT",
		"enabled":     true,
		"skip_reload": true,
		"comment":     "from connection",
	}, cookie, "")
	if rec.Code != 200 {
		t.Fatalf("expected 200 adding rule with skip_reload, got %d body=%s", rec.Code, rec.Body.String())
	}
	if reloadCalls != 0 {
		t.Fatalf("expected 0 controller reloads when skip_reload=true, got %d", reloadCalls)
	}

	var status mihomoStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status failed: %v", err)
	}
	if len(status.CustomRules) != 1 || status.CustomRules[0].Value != "openai.com" {
		t.Fatalf("expected rule added to status, got %+v", status.CustomRules)
	}
	runtimeBytes, err := os.ReadFile(filepath.Join(a.mihomo.dir, "runtime.yaml"))
	if err != nil || !strings.Contains(string(runtimeBytes), "DOMAIN-SUFFIX,openai.com,DIRECT") {
		t.Fatalf("expected runtime.yaml updated with new rule, err=%v", err)
	}

	// Default behavior without skip_reload still triggers controller reload when running.
	rec2 := adminRequest(h, "POST", "/api/mihomo/rules", map[string]any{
		"type":    "DOMAIN",
		"value":   "api.example.com",
		"target":  "DIRECT",
		"enabled": true,
	}, cookie, "")
	if rec2.Code != 200 {
		t.Fatalf("expected 200 adding default rule, got %d body=%s", rec2.Code, rec2.Body.String())
	}
	if reloadCalls != 1 {
		t.Fatalf("expected 1 controller reload when skip_reload is omitted, got %d", reloadCalls)
	}
}

func TestMihomoControllerLANExposureAndSecret(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	// 1. Default config binds controller to 127.0.0.1 only and reports controller_secret_configured=true.
	statusRes := adminRequest(h, "GET", "/api/mihomo/status", nil, cookie, "")
	if statusRes.Code != 200 {
		t.Fatalf("GET status failed: %d", statusRes.Code)
	}
	var st mihomoStatusResponse
	if err := json.Unmarshal(statusRes.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.ControllerAllowLAN || !st.ControllerHasSecret {
		t.Fatalf("unexpected default controller status: allow_lan=%v configured=%v", st.ControllerAllowLAN, st.ControllerHasSecret)
	}
	rawDisk, err := os.ReadFile(a.mihomo.runtimePath())
	if err != nil || !strings.Contains(string(rawDisk), "127.0.0.1:19090") {
		t.Fatalf("expected default runtime.yaml external-controller 127.0.0.1:19090, err=%v", err)
	}

	// 2. Reject short secret when LAN exposure is enabled.
	badRes := adminRequest(h, "PUT", "/api/mihomo/settings", map[string]any{
		"enabled":              false,
		"takeover_outbound":    false,
		"controller_port":      19095,
		"controller_allow_lan": true,
		"controller_secret":    "short",
		"settings": map[string]any{
			"mixed_port":        7890,
			"mode":              "rule",
			"log_level":         "info",
			"dns_enhanced_mode": "keep",
		},
	}, cookie, "")
	if badRes.Code != 400 {
		t.Fatalf("expected 400 for short secret on LAN controller, got %d", badRes.Code)
	}

	// 3. Save custom port, LAN exposure, and custom management secret.
	okRes := adminRequest(h, "PUT", "/api/mihomo/settings", map[string]any{
		"enabled":              false,
		"takeover_outbound":    false,
		"controller_port":      19095,
		"controller_allow_lan": true,
		"controller_secret":    "neko-secret-12345",
		"settings": map[string]any{
			"mixed_port":        7890,
			"mode":              "rule",
			"log_level":         "info",
			"dns_enhanced_mode": "keep",
		},
	}, cookie, "")
	if okRes.Code != 200 {
		t.Fatalf("expected 200 saving controller settings, got %d body=%s", okRes.Code, okRes.Body.String())
	}
	if strings.Contains(okRes.Body.String(), "neko-secret-12345") {
		t.Fatal("status response must never leak plaintext controller_secret")
	}

	// runtime.yaml on disk has 0.0.0.0:19095 and secret, while YAML API masks it.
	diskAfter, err := os.ReadFile(a.mihomo.runtimePath())
	if err != nil || !strings.Contains(string(diskAfter), "external-controller: 0.0.0.0:19095") || !strings.Contains(string(diskAfter), "neko-secret-12345") {
		t.Fatalf("unexpected runtime.yaml after LAN exposure update: err=%v body=%s", err, string(diskAfter))
	}
	yamlAPIRes := adminRequest(h, "GET", "/api/mihomo/config/yaml?kind=runtime", nil, cookie, "")
	if yamlAPIRes.Code != 200 || strings.Contains(yamlAPIRes.Body.String(), "neko-secret-12345") {
		t.Fatalf("runtime YAML API must mask controller secret, code=%d", yamlAPIRes.Code)
	}

	// 4. Reveal stored secret on demand via /api/credentials/reveal.
	revealRes := adminRequest(h, "POST", "/api/credentials/reveal", map[string]string{"kind": "mihomo-controller"}, cookie, "")
	if revealRes.Code != 200 {
		t.Fatalf("expected 200 revealing mihomo-controller secret, got %d body=%s", revealRes.Code, revealRes.Body.String())
	}
	var revealed struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(revealRes.Body.Bytes(), &revealed); err != nil || revealed.Value != "neko-secret-12345" {
		t.Fatalf("unexpected revealed secret: %+v err=%v", revealed, err)
	}

	// 5. Leaving controller_secret empty preserves existing secret.
	preserveRes := adminRequest(h, "PUT", "/api/mihomo/settings", map[string]any{
		"enabled":              false,
		"takeover_outbound":    false,
		"controller_port":      19095,
		"controller_allow_lan": true,
		"controller_secret":    "",
		"settings": map[string]any{
			"mixed_port":        7891,
			"mode":              "rule",
			"log_level":         "info",
			"dns_enhanced_mode": "keep",
		},
	}, cookie, "")
	if preserveRes.Code != 200 {
		t.Fatalf("expected 200 preserving controller secret, got %d", preserveRes.Code)
	}
	a.mihomo.mu.Lock()
	preservedSecret := a.mihomo.cfg.ControllerSecret
	a.mihomo.running = true
	a.mihomo.mu.Unlock()
	if preservedSecret != "neko-secret-12345" {
		t.Fatalf("expected controller secret preserved, got %q", preservedSecret)
	}
	foundLANListener := false
	for _, l := range a.mihomo.ActiveListeners() {
		if l.Port == 19095 && l.IP == "0.0.0.0" {
			foundLANListener = true
		}
	}
	if !foundLANListener {
		t.Fatalf("expected 0.0.0.0:19095 in ActiveListeners, got %+v", a.mihomo.ActiveListeners())
	}
}
