package gateway

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
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
		var gzBuf bytes.Buffer
		zw := gzip.NewWriter(&gzBuf)
		_, _ = zw.Write([]byte(script))
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
