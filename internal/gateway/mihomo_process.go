package gateway

import (
	"bufio"
	"bytes"
	"compress/gzip"
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
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	mihomoReleaseLatestAPI = "https://api.github.com/repos/MetaCubeX/mihomo/releases/latest"
	maxMihomoBinaryBytes   = 128 << 20
	maxMihomoUnzipBytes    = 200 << 20
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
	if strings.Contains(lower, "secret=") || strings.Contains(lower, "token=") {
		line = "[redacted sensitive log line]"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	if len(r.lines) > 50 {
		r.lines = r.lines[len(r.lines)-50:]
	}
}

func (r *mihomoLogRing) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

type mihomoManager struct {
	mu        sync.Mutex
	dir       string
	store     *Store
	adminPort int

	cfg       MihomoConfig
	cmd       *exec.Cmd
	waitDone  chan error
	running   bool
	pid       int
	version   string
	lastError string
	logs      mihomoLogRing

	ctrlBaseURL string // optional override for tests
	ctrlSecret  string // optional override for tests
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

func mihomoLocalClient(timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	return &http.Client{Transport: tr, Timeout: timeout}
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
	m.mu.Lock()
	if m.running && m.cmd != nil {
		m.mu.Unlock()
		return nil
	}
	bin := m.resolveBinaryLocked()
	if bin == "" {
		m.mu.Unlock()
		return errors.New("未检测到 mihomo 内核程序，请先在「订阅与配置」下载或上传内核")
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
	m.mu.Unlock()

	pipeReader := func(r io.ReadCloser) {
		if r == nil {
			return
		}
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			m.logs.add(scanner.Text())
		}
	}
	go pipeReader(stdout)
	go pipeReader(stderr)

	go func() {
		err := cmd.Wait()
		waitDone <- err
		close(waitDone)
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.cmd == cmd {
			m.cmd = nil
			m.running = false
			m.pid = 0
			if err != nil && m.lastError == "" {
				m.lastError = "mihomo 进程退出: " + err.Error()
			}
		}
	}()

	// Poll controller /version up to 5s until ready or process exits early.
	deadline := time.Now().Add(5 * time.Second)
	client := mihomoLocalClient(500 * time.Millisecond)
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
			m.mu.Unlock()
			return errors.New(msg)
		default:
		}
		ver, err := fetchMihomoControllerVersion(client, ctrlURL, secret)
		if err == nil {
			m.mu.Lock()
			if m.cmd == cmd {
				m.running = true
				m.version = ver
				m.lastError = ""
			}
			m.mu.Unlock()
			return nil
		}
		readyErr = err
		time.Sleep(100 * time.Millisecond)
	}

	_ = m.Stop()
	msg := fmt.Sprintf("mihomo 控制接口未就绪: %v", readyErr)
	m.mu.Lock()
	m.lastError = msg
	m.mu.Unlock()
	return errors.New(msg)
}

func (m *mihomoManager) Stop() error {
	m.mu.Lock()
	cmd := m.cmd
	waitDone := m.waitDone
	if cmd == nil || cmd.Process == nil {
		m.running = false
		m.pid = 0
		m.cmd = nil
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
	m.mu.Unlock()
	return nil
}

func (m *mihomoManager) Restart() error {
	if err := m.Stop(); err != nil {
		return err
	}
	return m.Start()
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
	client := mihomoLocalClient(10 * time.Second)
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

func (m *mihomoManager) TestGroupDelay(group, testURL string, timeoutMS int) (map[string]int, error) {
	m.mu.Lock()
	running := m.running || m.ctrlBaseURL != ""
	ctrlURL, secret := m.controllerURLAndSecretLocked()
	m.mu.Unlock()
	if !running {
		return nil, errors.New("mihomo 内核未运行")
	}
	if strings.TrimSpace(testURL) == "" {
		testURL = "https://www.gstatic.com/generate_204"
	}
	if timeoutMS <= 0 || timeoutMS > 15000 {
		timeoutMS = 5000
	}
	q := url.Values{}
	q.Set("url", testURL)
	q.Set("timeout", strconv.Itoa(timeoutMS))
	endpoint := ctrlURL + "/group/" + url.PathEscape(group) + "/delay?" + q.Encode()
	delays := map[string]int{}
	if err := m.doControllerJSON(http.MethodGet, endpoint, secret, nil, &delays); err != nil {
		return nil, err
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
		out = append(out, runtimeListener{
			Port:     m.cfg.ControllerPort,
			IP:       "127.0.0.1",
			Protocol: "TCP",
			Purpose:  "内部控制 · HTTP",
			Services: []string{"Mihomo Controller"},
		})
	}
	return out
}

func (m *mihomoManager) probeBinaryVersion(path string) string {
	if path == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-v").CombinedOutput()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	return line
}

func (m *mihomoManager) installBinaryData(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("上传的文件为空")
	}
	// Transparently gunzip if gzip header 0x1f 0x8b is present.
	payload := raw
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		gz, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return "", fmt.Errorf("gzip 解压失败: %v", err)
		}
		defer gz.Close()
		decompressed, err := io.ReadAll(io.LimitReader(gz, maxMihomoUnzipBytes+1))
		if err != nil {
			return "", fmt.Errorf("gzip 解压读取失败: %v", err)
		}
		if int64(len(decompressed)) > maxMihomoUnzipBytes {
			return "", errors.New("解压后二进制文件超过大小限制")
		}
		payload = decompressed
	}
	if len(payload) < 1024 {
		return "", errors.New("内核文件大小异常")
	}

	binDir := filepath.Join(m.dir, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(binDir, ".mihomo-upload-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0755); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, tmpPath, "-v").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("校验内核失败 (-v 返回错误): %v", err)
	}
	verLine := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	if !strings.Contains(strings.ToLower(verLine), "mihomo") && !strings.Contains(strings.ToLower(verLine), "meta") {
		return "", fmt.Errorf("无效的 mihomo 内核输出: %s", verLine)
	}

	dest := m.managedBinaryPath()
	wasRunning := false
	m.mu.Lock()
	wasRunning = m.running
	m.mu.Unlock()
	if wasRunning {
		_ = m.Stop()
	}

	if err := os.Rename(tmpPath, dest); err != nil {
		return "", err
	}
	_ = os.Chmod(dest, 0755)

	m.mu.Lock()
	m.version = verLine
	m.mu.Unlock()

	if wasRunning {
		_ = m.Start()
	}
	return verLine, nil
}

func selectMihomoAsset(assets []githubReleaseAsset, goos, goarch string) (githubReleaseAsset, bool) {
	archToken := goarch
	prefix := fmt.Sprintf("mihomo-%s-%s", goos, archToken)
	var fallback *githubReleaseAsset
	for i := range assets {
		name := strings.ToLower(assets[i].Name)
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".gz") {
			continue
		}
		if strings.Contains(name, "alpha") || strings.Contains(name, "go120") || strings.Contains(name, "go122") || strings.Contains(name, "cgo") {
			continue
		}
		if goarch == "amd64" && goos == "linux" && strings.Contains(name, "-compatible-") {
			return assets[i], true
		}
		if regexp.MustCompile(`^`+regexp.QuoteMeta(prefix)+`-v[0-9]`).MatchString(name) {
			fallback = &assets[i]
		} else if fallback == nil {
			fallback = &assets[i]
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return githubReleaseAsset{}, false
}

type githubReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

func (m *mihomoManager) DownloadCore(ctx context.Context, state State) (string, error) {
	client := onlineUpdateClient(state)
	defer closeClient(client)

	metaBytes, err := fetchOnlineUpdate(ctx, client, mihomoReleaseLatestAPI, 2<<20, state.GitHubToken)
	if err != nil {
		return "", fmt.Errorf("获取 mihomo 发布信息失败: %v", err)
	}
	var release struct {
		TagName string               `json:"tag_name"`
		Assets  []githubReleaseAsset `json:"assets"`
	}
	if err := json.Unmarshal(metaBytes, &release); err != nil {
		return "", errors.New("解析 mihomo 版本信息失败")
	}
	asset, ok := selectMihomoAsset(release.Assets, runtime.GOOS, runtime.GOARCH)
	if !ok || asset.URL == "" {
		return "", fmt.Errorf("未找到适用于 %s/%s 的 mihomo 发行包 (%s)", runtime.GOOS, runtime.GOARCH, release.TagName)
	}
	raw, err := fetchOnlineUpdate(ctx, client, asset.URL, maxMihomoBinaryBytes, "")
	if err != nil {
		return "", fmt.Errorf("下载内核包失败: %v", err)
	}
	ver, err := m.installBinaryData(raw)
	if err != nil {
		return "", err
	}
	return ver, nil
}

func (m *mihomoManager) fetchSubscriptionYAML(ctx context.Context, subURL string) ([]byte, int64, int64, int64, int64, error) {
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
		return nil, 0, 0, 0, 0, fmt.Errorf("订阅下载失败: %v", err)
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
