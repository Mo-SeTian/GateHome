package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type mihomoLogRing struct {
	mu    sync.Mutex
	lines []string
}

func (r *mihomoLogRing) add(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	lower := strings.ToLower(line)
	if strings.Contains(lower, "secret=") ||
		strings.Contains(lower, "token=") ||
		strings.Contains(lower, "authorization:") ||
		strings.Contains(lower, "bearer ") ||
		strings.Contains(lower, "password=") ||
		strings.Contains(lower, "pass=") {
		line = "[redacted sensitive log line]"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	if len(r.lines) > 500 {
		r.lines = r.lines[len(r.lines)-500:]
	}
}

func (r *mihomoLogRing) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = nil
}

func (r *mihomoLogRing) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

type mihomoManager struct {
	lifecycleMu sync.Mutex
	mu          sync.Mutex
	dir         string
	store       *Store
	adminPort   int

	cfg       MihomoConfig
	cmd       *exec.Cmd
	waitDone  chan error
	running   bool
	pid       int
	startedAt time.Time
	version   string
	lastError string
	logs      mihomoLogRing

	coreReleaseAPI string       // optional override for tests
	coreHTTPClient *http.Client // optional override for tests
	coreUpdateInfo *mihomoCoreUpdateInfo

	telemetryMu       sync.Mutex
	lastTrafficSample mihomoTrafficSample
	lastCPUSample     mihomoCPUSample

	ctrlBaseURL        string        // optional override for tests
	ctrlSecret         string        // optional override for tests
	startTimeout       time.Duration // optional override for tests
	ctrlOnce           sync.Once
	ctrlTransport      *http.Transport
	onAlert            func(key, kind, resource string, failing bool, failure, recovery string, now time.Time)
	onClearAlerts      func(keys ...string)
	autoUpdateAttempts map[string]time.Time
	adminLogs          *Logs
	fetchSubFn         func(context.Context, string) ([]byte, int64, int64, int64, int64, error)
}

func newMihomoManager(paths StoragePaths, store *Store, adminPort int) *mihomoManager {
	dir := paths.directory("mihomo")
	cfg, err := loadMihomoConfig(dir)
	lastErr := ""
	if err != nil {
		lastErr = err.Error()
		cfg = defaultMihomoConfig()
	}
	return &mihomoManager{
		dir:       dir,
		store:     store,
		adminPort: adminPort,
		cfg:       cfg,
		lastError: lastErr,
	}
}

func (m *mihomoManager) runtimePath() string {
	return filepath.Join(m.dir, "runtime.yaml")
}

func (m *mihomoManager) subscriptionFile(id string) string {
	return filepath.Join(m.dir, "subscriptions", id+".yaml")
}

func (m *mihomoManager) managedBinaryPath() string {
	name := "mihomo"
	if runtime.GOOS == "windows" {
		name = "mihomo.exe"
	}
	return filepath.Join(m.dir, "bin", name)
}

func (m *mihomoManager) resolveBinaryLocked() string {
	if m.cfg.BinaryPath != "" {
		if info, err := os.Stat(m.cfg.BinaryPath); err == nil && !info.IsDir() {
			return m.cfg.BinaryPath
		}
	}
	managed := m.managedBinaryPath()
	if info, err := os.Stat(managed); err == nil && !info.IsDir() {
		return managed
	}
	if p, err := exec.LookPath("mihomo"); err == nil {
		return p
	}
	return ""
}

func (m *mihomoManager) controllerURLAndSecretLocked() (string, string) {
	if m.ctrlBaseURL != "" {
		return m.ctrlBaseURL, m.ctrlSecret
	}
	return fmt.Sprintf("http://127.0.0.1:%d", m.cfg.ControllerPort), m.cfg.ControllerSecret
}

var (
	mihomoSharedTransportOnce sync.Once
	mihomoSharedTransport     *http.Transport
)

func mihomoLocalClient(timeout time.Duration) *http.Client {
	mihomoSharedTransportOnce.Do(func() {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.Proxy = nil
		tr.MaxIdleConns = 32
		tr.MaxIdleConnsPerHost = 16
		mihomoSharedTransport = tr
	})
	return &http.Client{Transport: mihomoSharedTransport, Timeout: timeout}
}

func (m *mihomoManager) localClient(timeout time.Duration) *http.Client {
	m.ctrlOnce.Do(func() {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.Proxy = nil
		tr.MaxIdleConns = 32
		tr.MaxIdleConnsPerHost = 16
		m.ctrlTransport = tr
	})
	return &http.Client{Transport: m.ctrlTransport, Timeout: timeout}
}

func (m *mihomoManager) readSourceYAML(cfg MihomoConfig) ([]byte, error) {
	if cfg.ActiveSubscriptionID == "" {
		return nil, nil
	}
	p := m.subscriptionFile(cfg.ActiveSubscriptionID)
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

func (m *mihomoManager) synthesizeAndWriteLocked() error {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return err
	}
	if err := m.writeManualRuleSetFilesLocked(m.cfg); err != nil {
		return err
	}
	source, err := m.readSourceYAML(m.cfg)
	if err != nil {
		return err
	}
	runtimeBytes, err := synthesizeMihomoYAML(source, m.cfg)
	if err != nil {
		return err
	}
	return atomicWrite(m.runtimePath(), runtimeBytes)
}

func (m *mihomoManager) ensureRuntimeYAMLLocked() error {
	if m.cfg.ManualOverride {
		if b, err := os.ReadFile(m.runtimePath()); err == nil && len(bytes.TrimSpace(b)) > 0 {
			return nil
		}
		m.cfg.ManualOverride = false
		_ = saveMihomoConfig(m.dir, m.cfg)
	}
	return m.synthesizeAndWriteLocked()
}

func (m *mihomoManager) StartIfEnabled() {
	m.mu.Lock()
	enabled := m.cfg.Enabled
	bin := m.resolveBinaryLocked()
	m.mu.Unlock()
	if !enabled || bin == "" {
		return
	}
	if err := m.Start(); err != nil {
		m.mu.Lock()
		m.lastError = err.Error()
		m.mu.Unlock()
	}
}

func (m *mihomoManager) Start() error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	return m.startWithLifecycleHeld()
}

func (m *mihomoManager) startWithLifecycleHeld() error {
	m.mu.Lock()
	if m.running && m.cmd != nil {
		m.mu.Unlock()
		return nil
	}
	bin := m.resolveBinaryLocked()
	if bin == "" {
		m.mu.Unlock()
		return errors.New("未检测到 mihomo 内核程序，请先在「端口与核心」下载或上传内核")
	}
	gwCfg := Config{}
	if m.store != nil {
		gwCfg = m.store.Snapshot().Config
	}
	if err := validateMihomoSettings(m.cfg, gwCfg, m.adminPort); err != nil {
		m.mu.Unlock()
		return err
	}
	if err := m.ensureRuntimeYAMLLocked(); err != nil {
		m.mu.Unlock()
		return err
	}
	if m.store != nil {
		if ok, _, _ := m.mmdbStatus(); !ok {
			_ = os.Remove(m.mmdbPath())
			stateSnap := m.store.Snapshot()
			m.mu.Unlock()
			m.logs.add("country.mmdb 缺失或已损坏，正在尝试从镜像源自动下载…")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			size, host, dlErr := m.DownloadMMDB(ctx, stateSnap)
			cancel()
			if dlErr != nil {
				m.logs.add("country.mmdb 镜像预下载失败: " + dlErr.Error() + "（将继续尝试启动，若长时间卡住请在「端口与核心」手动上传 country.mmdb）")
			} else {
				m.logs.add(fmt.Sprintf("country.mmdb 已从 %s 下载就绪 (%d bytes)", host, size))
			}
			m.mu.Lock()
			if m.running && m.cmd != nil {
				m.mu.Unlock()
				return nil
			}
			bin = m.resolveBinaryLocked()
			if bin == "" {
				m.mu.Unlock()
				return errors.New("未检测到 mihomo 内核程序，请先在「端口与核心」下载或上传内核")
			}
		}
	}
	absDir, err := filepath.Abs(m.dir)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	absRuntime, err := filepath.Abs(m.runtimePath())
	if err != nil {
		m.mu.Unlock()
		return err
	}

	cmd := exec.Command(bin, "-d", absDir, "-f", absRuntime)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		m.lastError = "mihomo 启动失败: " + err.Error()
		m.mu.Unlock()
		return errors.New(m.lastError)
	}

	m.cmd = cmd
	m.pid = cmd.Process.Pid
	m.lastError = ""
	waitDone := make(chan error, 1)
	m.waitDone = waitDone
	ctrlURL, secret := m.controllerURLAndSecretLocked()
	startWait := m.startTimeout
	if startWait <= 0 {
		startWait = 60 * time.Second
	}
	m.mu.Unlock()

	pipeReader := func(r io.ReadCloser) {
		if r == nil {
			return
		}
		reader := bufio.NewReaderSize(r, 8<<10)
		var buf strings.Builder
		for {
			part, isPrefix, err := reader.ReadLine()
			if len(part) > 0 && buf.Len() < 8192 {
				remain := 8192 - buf.Len()
				if len(part) > remain {
					buf.Write(part[:remain])
					buf.WriteString("...[truncated]")
				} else {
					buf.Write(part)
				}
			}
			if !isPrefix && buf.Len() > 0 {
				m.logs.add(buf.String())
				buf.Reset()
			}
			if err != nil {
				if buf.Len() > 0 {
					m.logs.add(buf.String())
				}
				break
			}
		}
	}
	go pipeReader(stdout)
	go pipeReader(stderr)

	go func() {
		err := cmd.Wait()
		waitDone <- err
		close(waitDone)
		unexpectedExit := false
		m.mu.Lock()
		if m.cmd == cmd {
			m.cmd = nil
			m.running = false
			m.pid = 0
			m.startedAt = time.Time{}
			m.resetTelemetrySamples()
			unexpectedExit = m.cfg.Enabled
			if err != nil && m.lastError == "" {
				m.lastError = "mihomo 进程退出: " + err.Error()
			}
		}
		m.mu.Unlock()
		if unexpectedExit {
			m.notifyAlert("mihomo-core", "mihomo", "Mihomo 核心", true, "Mihomo 核心进程异常退出", "Mihomo 核心已恢复运行", time.Now())
		}
	}()

	// Poll controller /version until ready or process exits early (allows initial MMDB/Geodata download).
	deadline := time.Now().Add(startWait)
	client := m.localClient(500 * time.Millisecond)
	var readyErr error
	for time.Now().Before(deadline) {
		select {
		case err := <-waitDone:
			lines := m.logs.snapshot()
			tail := ""
			if len(lines) > 0 {
				tail = " (" + lines[len(lines)-1] + ")"
			}
			msg := "mihomo 启动后立即退出" + tail
			if err != nil {
				msg = fmt.Sprintf("mihomo 启动失败 (%v)%s", err, tail)
			}
			m.mu.Lock()
			m.lastError = msg
			m.running = false
			m.startedAt = time.Time{}
			m.resetTelemetrySamples()
			m.mu.Unlock()
			return errors.New(msg)
		default:
		}
		ver, err := fetchMihomoControllerVersion(client, ctrlURL, secret)
		if err == nil {
			m.mu.Lock()
			if m.cmd == cmd {
				m.running = true
				m.startedAt = time.Now()
				m.version = ver
				m.lastError = ""
			}
			m.mu.Unlock()
			m.notifyAlert("mihomo-core", "mihomo", "Mihomo 核心", false, "", "Mihomo 核心已恢复运行", time.Now())
			return nil
		}
		readyErr = err
		time.Sleep(150 * time.Millisecond)
	}

	_ = m.stopWithLifecycleHeld()
	tail := ""
	if lines := m.logs.snapshot(); len(lines) > 0 {
		tail = "（最近日志: " + lines[len(lines)-1] + "）"
	}
	msg := fmt.Sprintf("mihomo 控制接口未就绪: %v%s", readyErr, tail)
	m.mu.Lock()
	m.lastError = msg
	m.mu.Unlock()
	return errors.New(msg)
}

func (m *mihomoManager) Stop() error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	return m.stopWithLifecycleHeld()
}

func (m *mihomoManager) stopWithLifecycleHeld() error {
	m.mu.Lock()
	cmd := m.cmd
	waitDone := m.waitDone
	if cmd == nil || cmd.Process == nil {
		m.running = false
		m.pid = 0
		m.startedAt = time.Time{}
		m.cmd = nil
		m.resetTelemetrySamples()
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	_ = cmd.Process.Signal(syscall.SIGTERM)
	if waitDone != nil {
		select {
		case <-waitDone:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-waitDone
		}
	} else {
		_ = cmd.Process.Kill()
	}

	m.mu.Lock()
	if m.cmd == cmd {
		m.cmd = nil
	}
	m.running = false
	m.pid = 0
	m.startedAt = time.Time{}
	m.resetTelemetrySamples()
	m.mu.Unlock()
	return nil
}

func (m *mihomoManager) Restart() error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if err := m.stopWithLifecycleHeld(); err != nil {
		return err
	}
	return m.startWithLifecycleHeld()
}

func (m *mihomoManager) Reload() error {
	m.mu.Lock()
	running := m.running
	ctrlURL, secret := m.controllerURLAndSecretLocked()
	absRuntime, err := filepath.Abs(m.runtimePath())
	m.mu.Unlock()
	if !running {
		return errors.New("mihomo 内核未运行")
	}
	if err != nil {
		return err
	}
	payload := map[string]string{"path": absRuntime}
	return m.doControllerJSON(http.MethodPut, ctrlURL+"/configs?force=true", secret, payload, nil)
}

func (m *mihomoManager) PatchMode(mode string) error {
	if !validMihomoModes[mode] {
		return errors.New("代理模式须为 rule、global 或 direct")
	}
	m.mu.Lock()
	m.cfg.Settings.Mode = mode
	if err := saveMihomoConfig(m.dir, m.cfg); err != nil {
		m.mu.Unlock()
		return err
	}
	if !m.cfg.ManualOverride {
		_ = m.synthesizeAndWriteLocked()
	}
	running := m.running
	ctrlURL, secret := m.controllerURLAndSecretLocked()
	m.mu.Unlock()

	if running {
		return m.doControllerJSON(http.MethodPatch, ctrlURL+"/configs", secret, map[string]string{"mode": mode}, nil)
	}
	return nil
}

func (m *mihomoManager) doControllerJSON(method, targetURL, secret string, body any, out any) error {
	return m.doControllerJSONWithTimeout(method, targetURL, secret, body, out, 10*time.Second)
}

func (m *mihomoManager) doControllerJSONWithTimeout(method, targetURL, secret string, body any, out any, timeout time.Duration) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, targetURL, reader)
	if err != nil {
		return err
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := m.localClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBytes))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("内核接口返回 %d: %s", resp.StatusCode, msg)
	}
	if out != nil && len(bytes.TrimSpace(respBytes)) > 0 {
		if err := json.Unmarshal(respBytes, out); err != nil {
			return err
		}
	}
	return nil
}

func fetchMihomoControllerVersion(client *http.Client, ctrlURL, secret string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, ctrlURL+"/version", nil)
	if err != nil {
		return "", err
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	var res struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if res.Version == "" {
		res.Version = "unknown"
	}
	return res.Version, nil
}

type mihomoProxyRecord struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Now     string   `json:"now"`
	All     []string `json:"all"`
	History []struct {
		Delay int `json:"delay"`
	} `json:"history"`
}

// GetGroupsAndDelays returns live groups from external controller or falls back to runtime.yaml.
func (m *mihomoManager) GetGroupsAndDelays() (groups []MihomoGroupInfo, delays map[string]int, allNodes []string, live bool, err error) {
	m.mu.Lock()
	running := m.running
	ctrlURL, secret := m.controllerURLAndSecretLocked()
	customGroups := append([]MihomoCustomGroup(nil), m.cfg.CustomGroups...)
	rtPath := m.runtimePath()
	hasOverride := m.ctrlBaseURL != ""
	m.mu.Unlock()

	delays = map[string]int{}
	runtimeBytes, _ := os.ReadFile(rtPath)
	yamlGroups, yamlNodes := extractYAMLGroupsAndNodes(runtimeBytes, customGroups)

	if !running && !hasOverride {
		return yamlGroups, delays, yamlNodes, false, nil
	}

	var payload struct {
		Proxies map[string]mihomoProxyRecord `json:"proxies"`
	}
	if err := m.doControllerJSON(http.MethodGet, ctrlURL+"/proxies", secret, nil, &payload); err != nil {
		return yamlGroups, delays, yamlNodes, false, err
	}

	customByName := map[string]string{}
	for _, cg := range customGroups {
		customByName[cg.Name] = cg.ID
	}

	seenGroup := map[string]bool{}
	for name, rec := range payload.Proxies {
		if len(rec.History) > 0 {
			delays[name] = rec.History[len(rec.History)-1].Delay
		}
		if len(rec.All) == 0 && rec.Type != "Selector" && rec.Type != "URLTest" && rec.Type != "Fallback" && rec.Type != "LoadBalance" {
			if name != "" && name != "DIRECT" && name != "REJECT" && name != "GLOBAL" {
				allNodes = append(allNodes, name)
			}
		}
	}

	// Preserve group ordering from runtime.yaml when available, then append any controller-only groups.
	for _, yg := range yamlGroups {
		if rec, ok := payload.Proxies[yg.Name]; ok && len(rec.All) > 0 {
			seenGroup[yg.Name] = true
			id, isCustom := customByName[yg.Name]
			groups = append(groups, MihomoGroupInfo{
				Name:    yg.Name,
				Type:    rec.Type,
				Now:     rec.Now,
				All:     rec.All,
				Custom:  isCustom,
				GroupID: id,
			})
		}
	}
	for name, rec := range payload.Proxies {
		if seenGroup[name] || name == "GLOBAL" || len(rec.All) == 0 {
			continue
		}
		id, isCustom := customByName[name]
		groups = append(groups, MihomoGroupInfo{
			Name:    name,
			Type:    rec.Type,
			Now:     rec.Now,
			All:     rec.All,
			Custom:  isCustom,
			GroupID: id,
		})
	}
	if len(allNodes) == 0 {
		allNodes = yamlNodes
	}
	return groups, delays, allNodes, true, nil
}

func (m *mihomoManager) SelectProxy(group, node string) error {
	m.mu.Lock()
	running := m.running || m.ctrlBaseURL != ""
	ctrlURL, secret := m.controllerURLAndSecretLocked()
	m.mu.Unlock()
	if !running {
		return errors.New("mihomo 内核未运行")
	}
	endpoint := ctrlURL + "/proxies/" + url.PathEscape(group)
	return m.doControllerJSON(http.MethodPut, endpoint, secret, map[string]string{"name": node}, nil)
}

func normalizeDelayQuery(testURL string, timeoutMS int) string {
	if strings.TrimSpace(testURL) == "" {
		testURL = "https://www.gstatic.com/generate_204"
	}
	if timeoutMS <= 0 || timeoutMS > 15000 {
		timeoutMS = 5000
	}
	q := url.Values{}
	q.Set("url", testURL)
	q.Set("timeout", strconv.Itoa(timeoutMS))
	return q.Encode()
}

func (m *mihomoManager) TestNodeDelay(node, testURL string, timeoutMS int) (int, error) {
	m.mu.Lock()
	running := m.running || m.ctrlBaseURL != ""
	ctrlURL, secret := m.controllerURLAndSecretLocked()
	m.mu.Unlock()
	if !running {
		return 0, errors.New("mihomo 内核未运行")
	}
	encodedQ := normalizeDelayQuery(testURL, timeoutMS)
	var res struct {
		Delay int `json:"delay"`
	}
	endpoint := ctrlURL + "/proxies/" + url.PathEscape(node) + "/delay?" + encodedQ
	if err := m.doControllerJSONWithTimeout(http.MethodGet, endpoint, secret, nil, &res, 12*time.Second); err != nil || res.Delay <= 0 {
		return 0, nil
	}
	return res.Delay, nil
}

func (m *mihomoManager) TestGroupDelay(group, testURL string, timeoutMS int) (map[string]int, error) {
	m.mu.Lock()
	running := m.running || m.ctrlBaseURL != ""
	ctrlURL, secret := m.controllerURLAndSecretLocked()
	m.mu.Unlock()
	if !running {
		return nil, errors.New("mihomo 内核未运行")
	}
	encodedQ := normalizeDelayQuery(testURL, timeoutMS)

	// Query group membership once so we can mark timed-out nodes as 0 and fall back per-node if needed.
	var proxiesPayload struct {
		Proxies map[string]mihomoProxyRecord `json:"proxies"`
	}
	_ = m.doControllerJSONWithTimeout(http.MethodGet, ctrlURL+"/proxies", secret, nil, &proxiesPayload, 8*time.Second)

	endpoint := ctrlURL + "/group/" + url.PathEscape(group) + "/delay?" + encodedQ
	delays := map[string]int{}
	groupErr := m.doControllerJSONWithTimeout(http.MethodGet, endpoint, secret, nil, &delays, 20*time.Second)

	var testableMembers []string
	var fallbackTargets []string
	needFullFallback := groupErr != nil || len(delays) == 0

	if rec, ok := proxiesPayload.Proxies[group]; ok {
		for _, member := range rec.All {
			switch member {
			case "", "DIRECT", "REJECT", "PASS", "COMPATIBLE":
				continue
			}
			testableMembers = append(testableMembers, member)
			_, alreadyTested := delays[member]
			if needFullFallback || (!alreadyTested && len(proxiesPayload.Proxies[member].All) > 0) {
				fallbackTargets = append(fallbackTargets, member)
			}
		}
	}

	if len(fallbackTargets) > 0 {
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 8)
		for _, member := range fallbackTargets {
			wg.Add(1)
			go func(nodeName string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				var singleRes struct {
					Delay int `json:"delay"`
				}
				nodeURL := ctrlURL + "/proxies/" + url.PathEscape(nodeName) + "/delay?" + encodedQ
				err := m.doControllerJSONWithTimeout(http.MethodGet, nodeURL, secret, nil, &singleRes, 12*time.Second)
				mu.Lock()
				if err == nil && singleRes.Delay > 0 {
					delays[nodeName] = singleRes.Delay
				} else if _, exists := delays[nodeName]; !exists {
					delays[nodeName] = 0
				}
				mu.Unlock()
			}(member)
		}
		wg.Wait()
	} else if groupErr != nil && len(delays) == 0 {
		return nil, groupErr
	}

	for _, member := range testableMembers {
		if _, exists := delays[member]; !exists {
			delays[member] = 0
		}
	}
	return delays, nil
}

func (m *mihomoManager) OutboundProxyURL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running || !m.cfg.TakeoverOutbound {
		return ""
	}
	return activeOutboundURL(m.cfg.Settings)
}

func (m *mihomoManager) ActiveListeners() []runtimeListener {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return nil
	}
	listenIP := "127.0.0.1"
	if m.cfg.Settings.AllowLAN {
		listenIP = "*"
	}
	var out []runtimeListener
	if m.cfg.Settings.MixedPort > 0 {
		out = append(out, runtimeListener{
			Port:     m.cfg.Settings.MixedPort,
			IP:       listenIP,
			Protocol: "TCP",
			Purpose:  "代理分流 · Mixed",
			Services: []string{"Mihomo HTTP+SOCKS5"},
		})
	}
	if m.cfg.Settings.HTTPPort > 0 {
		out = append(out, runtimeListener{
			Port:     m.cfg.Settings.HTTPPort,
			IP:       listenIP,
			Protocol: "TCP",
			Purpose:  "代理分流 · HTTP",
			Services: []string{"Mihomo HTTP"},
		})
	}
	if m.cfg.Settings.SocksPort > 0 {
		out = append(out, runtimeListener{
			Port:     m.cfg.Settings.SocksPort,
			IP:       listenIP,
			Protocol: "TCP",
			Purpose:  "代理分流 · SOCKS5",
			Services: []string{"Mihomo SOCKS5"},
		})
	}
	if m.cfg.Settings.DNSListenPort > 0 {
		for _, proto := range []string{"UDP", "TCP"} {
			out = append(out, runtimeListener{
				Port:     m.cfg.Settings.DNSListenPort,
				IP:       listenIP,
				Protocol: proto,
				Purpose:  "代理分流 · DNS",
				Services: []string{"Mihomo DNS"},
			})
		}
	}
	if m.cfg.ControllerPort > 0 {
		ctrlIP := "127.0.0.1"
		ctrlPurpose := "内部控制 · HTTP"
		if m.cfg.ControllerAllowLAN {
			ctrlIP = "0.0.0.0"
			ctrlPurpose = "管理 API · HTTP (局域网)"
		}
		out = append(out, runtimeListener{
			Port:     m.cfg.ControllerPort,
			IP:       ctrlIP,
			Protocol: "TCP",
			Purpose:  ctrlPurpose,
			Services: []string{"Mihomo Controller"},
		})
	}
	return out
}

func (m *mihomoManager) fetchSubscriptionYAML(ctx context.Context, subURL string) ([]byte, int64, int64, int64, int64, error) {
	if m.fetchSubFn != nil {
		return m.fetchSubFn(ctx, subURL)
	}
	if err := validateMihomoSubscriptionURL(subURL); err != nil {
		return nil, 0, 0, 0, 0, err
	}
	var state State
	if m.store != nil {
		state = m.store.Snapshot()
	}
	client := outboundClient(state, 30*time.Second)
	defer closeClient(client)
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("订阅重定向次数过多")
		}
		if err := validateMihomoSubscriptionURL(r.URL.String()); err != nil {
			return err
		}
		r.Header.Del("Authorization")
		r.Header.Del("Cookie")
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subURL, nil)
	if err != nil {
		return nil, 0, 0, 0, 0, err
	}
	req.Header.Set("User-Agent", "clash-verge/v2.0.3 mihomo/1.19")
	req.Header.Set("Accept", "application/yaml, text/yaml, text/plain, */*")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, 0, 0, 0, fmt.Errorf("订阅下载失败: %s", sanitizeFetchError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, 0, 0, 0, fmt.Errorf("订阅服务器返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20+1))
	if err != nil {
		return nil, 0, 0, 0, 0, err
	}
	if len(body) > 10<<20 {
		return nil, 0, 0, 0, 0, errors.New("订阅文件超过 10 MiB 上限")
	}
	if err := validateMihomoYAML(body); err != nil {
		return nil, 0, 0, 0, 0, errors.New("订阅返回内容不是有效的 Clash/Mihomo YAML 配置")
	}
	up, down, total, exp := parseSubscriptionUserInfo(resp.Header.Get("subscription-userinfo"))
	return body, up, down, total, exp, nil
}

func sanitizeFetchError(err error) string {
	if err == nil {
		return ""
	}
	cur := err
	var rawURLs []string
	for {
		var ue *url.Error
		if !errors.As(cur, &ue) || ue.Err == nil {
			break
		}
		if ue.URL != "" {
			rawURLs = append(rawURLs, ue.URL)
		}
		cur = ue.Err
	}
	msg := cur.Error()
	for _, raw := range rawURLs {
		if raw != "" && strings.Contains(msg, raw) {
			_, masked, ok := maskSubscriptionURL(raw)
			if !ok || masked == "" {
				masked = "[redacted-url]"
			}
			msg = strings.ReplaceAll(msg, raw, masked)
		}
	}
	return msg
}

// isPortListening returns whether TCP 127.0.0.1:port accepts connections right now (diagnostic helper).
func isLoopbackTCPPortOpen(port int) bool {
	if port < 1 || port > 65535 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 150*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (m *mihomoManager) notifyAlert(key, kind, resource string, failing bool, failure, recovery string, now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	cb := m.onAlert
	m.mu.Unlock()
	if cb != nil {
		cb(key, kind, resource, failing, failure, recovery, now)
	}
}

func (m *mihomoManager) clearSubAlerts(id string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	cb := m.onClearAlerts
	m.mu.Unlock()
	if cb != nil {
		cb("mihomo-sub/"+id, "mihomo-expire/"+id, "mihomo-quota/"+id)
	}
}

type subRefreshError struct {
	code int
	err  error
}

func (e subRefreshError) Error() string { return e.err.Error() }
func (e subRefreshError) Unwrap() error { return e.err }

func (m *mihomoManager) RefreshSubscription(ctx context.Context, id string) error {
	m.mu.Lock()
	idx := indexMihomoSub(m.cfg.Subscriptions, id)
	if idx < 0 {
		m.mu.Unlock()
		return subRefreshError{code: http.StatusNotFound, err: errors.New("未找到该订阅")}
	}
	sub := m.cfg.Subscriptions[idx]
	m.mu.Unlock()

	if sub.URL == "" {
		return subRefreshError{code: http.StatusBadRequest, err: errors.New("本地导入配置无远程链接，无法刷新")}
	}

	data, up, down, total, exp, err := m.fetchSubscriptionYAML(ctx, sub.URL)
	if err != nil {
		m.mu.Lock()
		subName := sub.Name
		if curIdx := indexMihomoSub(m.cfg.Subscriptions, id); curIdx >= 0 {
			subName = m.cfg.Subscriptions[curIdx].Name
			m.cfg.Subscriptions[curIdx].LastError = err.Error()
			_ = saveMihomoConfig(m.dir, m.cfg)
		}
		m.mu.Unlock()
		m.notifyAlert("mihomo-sub/"+id, "mihomo", subName, true, "Mihomo 订阅更新失败 · "+err.Error(), "Mihomo 订阅更新已恢复", time.Now())
		return subRefreshError{code: http.StatusBadRequest, err: err}
	}

	m.mu.Lock()
	curIdx := indexMihomoSub(m.cfg.Subscriptions, id)
	if curIdx < 0 {
		m.mu.Unlock()
		return subRefreshError{code: http.StatusNotFound, err: errors.New("订阅已被删除")}
	}
	subName := m.cfg.Subscriptions[curIdx].Name
	subDir := filepath.Join(m.dir, "subscriptions")
	_ = os.MkdirAll(subDir, 0700)
	subPath := m.subscriptionFile(id)
	prevData, prevReadErr := os.ReadFile(subPath)
	if err := atomicWrite(subPath, data); err != nil {
		m.mu.Unlock()
		return subRefreshError{code: http.StatusInternalServerError, err: err}
	}
	isActive := m.cfg.ActiveSubscriptionID == id
	if isActive {
		prevManual := m.cfg.ManualOverride
		m.cfg.ManualOverride = false
		if err := m.synthesizeAndWriteLocked(); err != nil {
			if prevReadErr == nil {
				_ = atomicWrite(subPath, prevData)
			} else {
				_ = os.Remove(subPath)
			}
			m.cfg.ManualOverride = prevManual
			m.cfg.Subscriptions[curIdx].LastError = "配置合成失败，已保留原订阅: " + err.Error()
			_ = saveMihomoConfig(m.dir, m.cfg)
			m.mu.Unlock()
			m.notifyAlert("mihomo-sub/"+id, "mihomo", subName, true, "Mihomo 订阅合成失败，已保留原配置", "Mihomo 订阅更新已恢复", time.Now())
			return subRefreshError{code: http.StatusBadRequest, err: errors.New("订阅内容与当前配置合成失败，已保留原配置: " + err.Error())}
		}
	}
	m.cfg.Subscriptions[curIdx].UpdatedAt = time.Now()
	m.cfg.Subscriptions[curIdx].NodeCount = countYAMLProxies(data)
	m.cfg.Subscriptions[curIdx].LastError = ""
	m.cfg.Subscriptions[curIdx].Upload = up
	m.cfg.Subscriptions[curIdx].Download = down
	m.cfg.Subscriptions[curIdx].Total = total
	m.cfg.Subscriptions[curIdx].Expire = exp
	if err := saveMihomoConfig(m.dir, m.cfg); err != nil {
		m.mu.Unlock()
		return subRefreshError{code: http.StatusInternalServerError, err: err}
	}
	running := m.running && isActive
	m.mu.Unlock()

	if running {
		_ = m.Reload()
	}
	m.notifyAlert("mihomo-sub/"+id, "mihomo", subName, false, "", "Mihomo 订阅更新已恢复", time.Now())
	m.EvaluateSubscriptionAlerts(time.Now())
	return nil
}

func indexMihomoSub(subs []MihomoSubscription, id string) int {
	for i := range subs {
		if subs[i].ID == id {
			return i
		}
	}
	return -1
}

func (m *mihomoManager) RunAutoUpdateTick(ctx context.Context, now time.Time) {
	if m == nil {
		return
	}
	const failureCooldown = 15 * time.Minute
	var dueIDs []string
	m.mu.Lock()
	if m.autoUpdateAttempts == nil {
		m.autoUpdateAttempts = map[string]time.Time{}
	}
	for _, sub := range m.cfg.Subscriptions {
		if sub.URL == "" || sub.AutoUpdateInterval < 60 {
			continue
		}
		if lastAttempt, ok := m.autoUpdateAttempts[sub.ID]; ok && now.Sub(lastAttempt) < failureCooldown {
			continue
		}
		interval := time.Duration(sub.AutoUpdateInterval) * time.Minute
		if sub.UpdatedAt.IsZero() || !now.Before(sub.UpdatedAt.Add(interval)) {
			dueIDs = append(dueIDs, sub.ID)
			m.autoUpdateAttempts[sub.ID] = now
		}
	}
	adminLogs := m.adminLogs
	m.mu.Unlock()

	for _, id := range dueIDs {
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		reqCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
		err := m.RefreshSubscription(reqCtx, id)
		cancel()
		msg := "Mihomo 订阅定时自动更新成功"
		if err != nil {
			msg = "Mihomo 订阅定时自动更新失败: " + err.Error()
		}
		m.logs.add(msg)
		if adminLogs != nil {
			adminLogs.Add(LogEntry{
				Category:   "subscriptions",
				Action:     "Mihomo 订阅自动更新",
				Target:     id,
				OK:         err == nil,
				DurationMS: time.Since(started).Milliseconds(),
				Message:    msg,
			})
		}
	}
}

func (m *mihomoManager) EvaluateSubscriptionAlerts(now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	subs := append([]MihomoSubscription(nil), m.cfg.Subscriptions...)
	m.mu.Unlock()

	for _, sub := range subs {
		if sub.Expire > 0 {
			exp := time.Unix(sub.Expire, 0)
			failing := !exp.After(now.Add(3 * 24 * time.Hour))
			msg := fmt.Sprintf("订阅将于 %s 到期", exp.Local().Format("2006-01-02 15:04"))
			if !exp.After(now) {
				msg = "订阅已过期"
			}
			m.notifyAlert("mihomo-expire/"+sub.ID, "mihomo", sub.Name, failing, msg, "订阅有效期已恢复", now)
		}
		if sub.Total > 0 {
			remaining := sub.Total - (sub.Upload + sub.Download)
			if remaining < 0 {
				remaining = 0
			}
			ratio := float64(remaining) / float64(sub.Total)
			failing := remaining == 0 || ratio <= 0.10
			pct := int(ratio * 100)
			msg := fmt.Sprintf("订阅剩余流量不足 10%%（剩余 %d%%）", pct)
			if remaining == 0 {
				msg = "订阅流量已耗尽"
			}
			m.notifyAlert("mihomo-quota/"+sub.ID, "mihomo", sub.Name, failing, msg, "订阅流量余量已恢复", now)
		}
	}
}
