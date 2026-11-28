package gateway

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	mihomoReleaseLatestAPI    = "https://api.github.com/repos/MetaCubeX/mihomo/releases/latest"
	zashboardReleaseLatestAPI = "https://api.github.com/repos/Zephyruso/zashboard/releases/latest"
	maxMihomoBinaryBytes      = 128 << 20
	maxMihomoUnzipBytes       = 200 << 20
	maxZashboardZipBytes      = 32 << 20
	maxZashboardExtractBytes  = 96 << 20
	maxZashboardExtractFiles  = 2000
	maxMihomoMMDBBytes        = 32 << 20
	maxMihomoGeoDatBytes      = 64 << 20
)

var mmdbMetadataMarker = []byte{0xab, 0xcd, 0xef, 'M', 'a', 'x', 'M', 'i', 'n', 'd', '.', 'c', 'o', 'm'}

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
	return strings.TrimSpace(strings.Split(string(out), "\n")[0])
}

func isSupportedExecutableBinary(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	if bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'}) {
		return true
	}
	if bytes.HasPrefix(data, []byte{0xfe, 0xed, 0xfa, 0xce}) ||
		bytes.HasPrefix(data, []byte{0xce, 0xfa, 0xed, 0xfe}) ||
		bytes.HasPrefix(data, []byte{0xfe, 0xed, 0xfa, 0xcf}) ||
		bytes.HasPrefix(data, []byte{0xcf, 0xfa, 0xed, 0xfe}) ||
		bytes.HasPrefix(data, []byte{0xbe, 0xba, 0xfe, 0xca}) {
		return true
	}
	if bytes.HasPrefix(data, []byte{0xca, 0xfe, 0xba, 0xbe}) && len(data) >= 8 {
		nfat := uint32(data[4])<<24 | uint32(data[5])<<16 | uint32(data[6])<<8 | uint32(data[7])
		if nfat > 0 && nfat <= 20 {
			return true
		}
	}
	return len(data) >= 64 && data[0] == 'M' && data[1] == 'Z'
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
	if !isSupportedExecutableBinary(payload) {
		return "", errors.New("内核文件必须为有效的可执行二进制文件（ELF / Mach-O / PE），禁止上传脚本")
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

	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	dest := m.managedBinaryPath()
	m.mu.Lock()
	wasRunning := m.running
	m.mu.Unlock()
	if wasRunning {
		_ = m.stopWithLifecycleHeld()
	}

	if err := os.Rename(tmpPath, dest); err != nil {
		return "", err
	}
	_ = os.Chmod(dest, 0755)

	m.mu.Lock()
	m.version = verLine
	m.mu.Unlock()

	if wasRunning {
		_ = m.startWithLifecycleHeld()
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
		if regexp.MustCompile(`^` + regexp.QuoteMeta(prefix) + `-v[0-9]`).MatchString(name) {
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

type mihomoCoreUpdateInfo struct {
	CurrentVersion  string    `json:"current_version,omitempty"`
	LatestVersion   string    `json:"latest_version,omitempty"`
	UpdateAvailable bool      `json:"update_available"`
	AssetName       string    `json:"asset_name,omitempty"`
	AssetSize       int64     `json:"asset_size,omitempty"`
	ReleaseURL      string    `json:"release_url,omitempty"`
	PublishedAt     string    `json:"published_at,omitempty"`
	CheckedAt       time.Time `json:"checked_at,omitempty"`
}

var mihomoSemverPattern = regexp.MustCompile(`v?((?:0|[1-9][0-9]{0,5})\.(?:0|[1-9][0-9]{0,5})\.(?:0|[1-9][0-9]{0,5})(?:-beta\.[1-9][0-9]{0,5})?)`)

func extractMihomoSemver(raw string) string {
	m := mihomoSemverPattern.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	return m[1]
}

func isMihomoUpdateNewer(currentRaw, latestRaw string) bool {
	latestVer := extractMihomoSemver(latestRaw)
	if latestVer == "" {
		return false
	}
	currentVer := extractMihomoSemver(currentRaw)
	if currentVer == "" {
		// If current binary is missing or its version string is unparseable, allow installing when not empty/missing.
		return strings.TrimSpace(currentRaw) == ""
	}
	cmp, err := compareVersions(latestVer, currentVer)
	return err == nil && cmp > 0
}

func (m *mihomoManager) coreReleaseAPIURL() string {
	m.mu.Lock()
	override := m.coreReleaseAPI
	m.mu.Unlock()
	if override != "" {
		return override
	}
	return mihomoReleaseLatestAPI
}

func (m *mihomoManager) CheckCoreUpdate(ctx context.Context, state State) (mihomoCoreUpdateInfo, error) {
	m.mu.Lock()
	customClient := m.coreHTTPClient
	m.mu.Unlock()
	client := customClient
	if client == nil {
		client = onlineUpdateClient(state)
		defer closeClient(client)
	}

	metaBytes, err := fetchOnlineUpdate(ctx, client, m.coreReleaseAPIURL(), 2<<20, state.GitHubToken)
	if err != nil {
		return mihomoCoreUpdateInfo{}, fmt.Errorf("检查 mihomo 内核更新失败: %v", err)
	}
	var release struct {
		TagName     string               `json:"tag_name"`
		HTMLURL     string               `json:"html_url"`
		PublishedAt string               `json:"published_at"`
		Assets      []githubReleaseAsset `json:"assets"`
	}
	if err := json.Unmarshal(metaBytes, &release); err != nil {
		return mihomoCoreUpdateInfo{}, errors.New("解析 mihomo 版本信息失败")
	}

	m.mu.Lock()
	bin := m.resolveBinaryLocked()
	curVer := m.version
	m.mu.Unlock()
	if curVer == "" && bin != "" {
		if probed := m.probeBinaryVersion(bin); probed != "" {
			curVer = probed
			m.mu.Lock()
			if m.version == "" {
				m.version = probed
			}
			m.mu.Unlock()
		}
	}

	asset, _ := selectMihomoAsset(release.Assets, runtime.GOOS, runtime.GOARCH)
	info := mihomoCoreUpdateInfo{
		CurrentVersion:  curVer,
		LatestVersion:   strings.TrimSpace(release.TagName),
		UpdateAvailable: bin == "" || isMihomoUpdateNewer(curVer, release.TagName),
		AssetName:       asset.Name,
		AssetSize:       asset.Size,
		ReleaseURL:      release.HTMLURL,
		PublishedAt:     release.PublishedAt,
		CheckedAt:       time.Now(),
	}

	m.mu.Lock()
	copied := info
	m.coreUpdateInfo = &copied
	m.mu.Unlock()
	return info, nil
}

func (m *mihomoManager) DownloadCore(ctx context.Context, state State) (string, error) {
	m.mu.Lock()
	customClient := m.coreHTTPClient
	m.mu.Unlock()
	client := customClient
	if client == nil {
		client = onlineUpdateClient(state)
		defer closeClient(client)
	}

	metaBytes, err := fetchOnlineUpdate(ctx, client, m.coreReleaseAPIURL(), 2<<20, state.GitHubToken)
	if err != nil {
		return "", fmt.Errorf("获取 mihomo 发布信息失败: %v", err)
	}
	var release struct {
		TagName     string               `json:"tag_name"`
		HTMLURL     string               `json:"html_url"`
		PublishedAt string               `json:"published_at"`
		Assets      []githubReleaseAsset `json:"assets"`
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
	m.mu.Lock()
	m.coreUpdateInfo = &mihomoCoreUpdateInfo{
		CurrentVersion:  ver,
		LatestVersion:   strings.TrimSpace(release.TagName),
		UpdateAvailable: false,
		AssetName:       asset.Name,
		AssetSize:       asset.Size,
		ReleaseURL:      release.HTMLURL,
		PublishedAt:     release.PublishedAt,
		CheckedAt:       time.Now(),
	}
	m.mu.Unlock()
	return ver, nil
}

func (m *mihomoManager) zashboardDir() string {
	return filepath.Join(m.dir, "ui", "zashboard")
}

func (m *mihomoManager) zashboardStatus() (bool, string) {
	dir := m.zashboardDir()
	info, err := os.Stat(filepath.Join(dir, "index.html"))
	if err != nil || info.IsDir() {
		return false, ""
	}
	verBytes, err := os.ReadFile(filepath.Join(dir, ".version"))
	if err != nil {
		return true, ""
	}
	return true, strings.TrimSpace(string(verBytes))
}

func selectZashboardAsset(assets []githubReleaseAsset) (githubReleaseAsset, bool) {
	var cdnFallback *githubReleaseAsset
	for i := range assets {
		name := strings.ToLower(strings.TrimSpace(assets[i].Name))
		if name == "dist.zip" {
			return assets[i], true
		}
		if name == "dist-cdn-fonts.zip" && cdnFallback == nil {
			cdnFallback = &assets[i]
		}
	}
	if cdnFallback != nil {
		return *cdnFallback, true
	}
	return githubReleaseAsset{}, false
}

func detectZashboardStripPrefix(files []*zip.File) string {
	hasRootIndex := false
	topDirs := map[string]bool{}
	for _, f := range files {
		clean := strings.TrimPrefix(filepath.ToSlash(f.Name), "./")
		clean = strings.TrimLeft(clean, "/")
		if clean == "" || strings.HasSuffix(clean, "/") {
			continue
		}
		if clean == "index.html" {
			hasRootIndex = true
			break
		}
		if idx := strings.IndexByte(clean, '/'); idx > 0 {
			topDirs[clean[:idx]] = true
		} else {
			topDirs[""] = true
		}
	}
	if hasRootIndex || len(topDirs) != 1 {
		return ""
	}
	for dir := range topDirs {
		if dir != "" && dir != "." && dir != ".." {
			return dir + "/"
		}
	}
	return ""
}

func (m *mihomoManager) installZashboardZip(raw []byte, version string) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("Zashboard 压缩包为空")
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", fmt.Errorf("解析 Zashboard 压缩包失败: %v", err)
	}
	if len(zr.File) > maxZashboardExtractFiles {
		return "", fmt.Errorf("压缩包文件数量过多 (%d)", len(zr.File))
	}

	uiParent := filepath.Join(m.dir, "ui")
	if err := os.MkdirAll(uiParent, 0700); err != nil {
		return "", err
	}
	tmpDir, err := os.MkdirTemp(uiParent, ".zashboard-tmp-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	stripPrefix := detectZashboardStripPrefix(zr.File)
	var totalWritten int64

	for _, f := range zr.File {
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return "", errors.New("压缩包包含不允许的符号链接")
		}
		slashName := strings.TrimPrefix(filepath.ToSlash(f.Name), "./")
		if stripPrefix != "" {
			slashName = strings.TrimPrefix(slashName, stripPrefix)
		}
		slashName = strings.TrimPrefix(slashName, "/")
		if slashName == "" {
			continue
		}
		if strings.Contains(slashName, "\\") || filepath.IsAbs(slashName) {
			return "", fmt.Errorf("压缩包包含非法路径: %s", f.Name)
		}
		parts := strings.Split(slashName, "/")
		for _, p := range parts {
			if p == ".." {
				return "", fmt.Errorf("压缩包路径越界: %s", f.Name)
			}
		}

		destPath := filepath.Join(tmpDir, filepath.FromSlash(slashName))
		rel, err := filepath.Rel(tmpDir, destPath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("压缩包路径越界: %s", f.Name)
		}

		if f.FileInfo().IsDir() || strings.HasSuffix(slashName, "/") {
			if err := os.MkdirAll(destPath, 0700); err != nil {
				return "", err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0700); err != nil {
			return "", err
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			rc.Close()
			return "", err
		}
		remaining := maxZashboardExtractBytes - totalWritten + 1
		n, copyErr := io.Copy(out, io.LimitReader(rc, remaining))
		closeErr := out.Close()
		rc.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		totalWritten += n
		if totalWritten > maxZashboardExtractBytes {
			return "", errors.New("Zashboard 解压内容超过大小限制")
		}
	}

	idxInfo, err := os.Stat(filepath.Join(tmpDir, "index.html"))
	if err != nil || idxInfo.IsDir() || idxInfo.Size() == 0 {
		return "", errors.New("压缩包缺少有效的 index.html")
	}
	cleanVer := strings.TrimSpace(version)
	if cleanVer != "" {
		if err := os.WriteFile(filepath.Join(tmpDir, ".version"), []byte(cleanVer+"\n"), 0600); err != nil {
			return "", err
		}
	}

	target := m.zashboardDir()
	backup := target + ".old"
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = os.RemoveAll(backup)
	hasExisting := false
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return "", err
		}
		hasExisting = true
	}
	if err := os.Rename(tmpDir, target); err != nil {
		if hasExisting {
			_ = os.Rename(backup, target)
		}
		return "", err
	}
	if hasExisting {
		_ = os.RemoveAll(backup)
	}
	return cleanVer, nil
}

func (m *mihomoManager) DownloadZashboard(ctx context.Context, state State) (string, error) {
	client := onlineUpdateClient(state)
	defer closeClient(client)

	metaBytes, err := fetchOnlineUpdate(ctx, client, zashboardReleaseLatestAPI, 2<<20, state.GitHubToken)
	if err != nil {
		return "", fmt.Errorf("获取 Zashboard 发布信息失败: %v", err)
	}
	var release struct {
		TagName string               `json:"tag_name"`
		Assets  []githubReleaseAsset `json:"assets"`
	}
	if err := json.Unmarshal(metaBytes, &release); err != nil {
		return "", errors.New("解析 Zashboard 版本信息失败")
	}
	asset, ok := selectZashboardAsset(release.Assets)
	if !ok || asset.URL == "" {
		return "", fmt.Errorf("未找到 Zashboard 发行包 (%s)", release.TagName)
	}
	raw, err := fetchOnlineUpdate(ctx, client, asset.URL, maxZashboardZipBytes, "")
	if err != nil {
		return "", fmt.Errorf("下载 Zashboard 压缩包失败: %v", err)
	}
	return m.installZashboardZip(raw, release.TagName)
}

func (m *mihomoManager) mmdbPath() string {
	return filepath.Join(m.dir, "country.mmdb")
}

func (m *mihomoManager) geoipPath() string {
	return filepath.Join(m.dir, "geoip.dat")
}

func (m *mihomoManager) geositePath() string {
	return filepath.Join(m.dir, "geosite.dat")
}

func (m *mihomoManager) geoMetaPath() string {
	return filepath.Join(m.dir, "geodata-meta.json")
}

type mihomoGeoMeta struct {
	Version   string    `json:"version,omitempty"`
	Source    string    `json:"source,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
	LastError string    `json:"last_error,omitempty"`
}

type mihomoGeoFileInfo struct {
	Installed bool      `json:"installed"`
	Size      int64     `json:"size,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
	Version   string    `json:"version,omitempty"`
}

type mihomoGeoDataStatus struct {
	CountryMMDB   mihomoGeoFileInfo `json:"country_mmdb"`
	GeoIP         mihomoGeoFileInfo `json:"geoip"`
	GeoSite       mihomoGeoFileInfo `json:"geosite"`
	AutoUpdate    bool              `json:"auto_update"`
	IntervalHours int               `json:"interval_hours"`
	NextUpdateAt  time.Time         `json:"next_update_at,omitzero"`
	Source        string            `json:"source,omitempty"`
	LastError     string            `json:"last_error,omitempty"`
}

func (m *mihomoManager) readGeoMeta() mihomoGeoMeta {
	var meta mihomoGeoMeta
	if raw, err := os.ReadFile(m.geoMetaPath()); err == nil {
		_ = json.Unmarshal(raw, &meta)
	}
	return meta
}

func (m *mihomoManager) saveGeoMeta(meta mihomoGeoMeta) {
	_ = os.MkdirAll(m.dir, 0700)
	if data, err := json.MarshalIndent(meta, "", "  "); err == nil {
		_ = atomicWrite(m.geoMetaPath(), append(data, '\n'))
	}
}

func isValidGeoDatFile(path string) (bool, int64, time.Time) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false, 0, time.Time{}
	}
	size := info.Size()
	if size < 1024 || size > maxMihomoGeoDatBytes {
		return false, 0, time.Time{}
	}
	return true, size, info.ModTime()
}

func (m *mihomoManager) geoDataStatus(cfg MihomoConfig) mihomoGeoDataStatus {
	meta := m.readGeoMeta()
	mmdbOK, mmdbSize, mmdbTime := m.mmdbStatus()
	geoipOK, geoipSize, geoipTime := isValidGeoDatFile(m.geoipPath())
	geositeOK, geositeSize, geositeTime := isValidGeoDatFile(m.geositePath())
	verFor := func(installed bool, updatedAt time.Time) string {
		if !installed {
			return ""
		}
		if meta.Version != "" {
			return meta.Version
		}
		if !updatedAt.IsZero() {
			return updatedAt.Local().Format("20060102")
		}
		return ""
	}
	intervalHours := effectiveGeoUpdateIntervalHours(cfg.Settings)
	status := mihomoGeoDataStatus{
		CountryMMDB:   mihomoGeoFileInfo{Installed: mmdbOK, Size: mmdbSize, UpdatedAt: mmdbTime, Version: verFor(mmdbOK, mmdbTime)},
		GeoIP:         mihomoGeoFileInfo{Installed: geoipOK, Size: geoipSize, UpdatedAt: geoipTime, Version: verFor(geoipOK, geoipTime)},
		GeoSite:       mihomoGeoFileInfo{Installed: geositeOK, Size: geositeSize, UpdatedAt: geositeTime, Version: verFor(geositeOK, geositeTime)},
		AutoUpdate:    cfg.Settings.GeoAutoUpdateEnabled,
		IntervalHours: intervalHours,
		Source:        meta.Source,
		LastError:     meta.LastError,
	}
	if status.AutoUpdate && mmdbOK && geoipOK && geositeOK {
		oldest := mmdbTime
		for _, t := range []time.Time{geoipTime, geositeTime} {
			if !t.IsZero() && (oldest.IsZero() || t.Before(oldest)) {
				oldest = t
			}
		}
		if !oldest.IsZero() {
			status.NextUpdateAt = oldest.Add(time.Duration(intervalHours) * time.Hour)
		}
	}
	return status
}

func isValidMMDBData(raw []byte) bool {
	if len(raw) < 1024 || len(raw) > maxMihomoMMDBBytes {
		return false
	}
	tailStart := 0
	if len(raw) > 128<<10 {
		tailStart = len(raw) - (128 << 10)
	}
	return bytes.Contains(raw[tailStart:], mmdbMetadataMarker)
}

func isValidMMDBFile(path string) (bool, int64, time.Time) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false, 0, time.Time{}
	}
	size := info.Size()
	if size < 1024 || size > maxMihomoMMDBBytes {
		return false, 0, time.Time{}
	}
	f, err := os.Open(path)
	if err != nil {
		return false, 0, time.Time{}
	}
	defer f.Close()
	readSize := size
	var offset int64
	if readSize > 128<<10 {
		offset = size - (128 << 10)
		readSize = 128 << 10
	}
	buf := make([]byte, readSize)
	if _, err := f.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
		return false, 0, time.Time{}
	}
	if !bytes.Contains(buf, mmdbMetadataMarker) {
		return false, 0, time.Time{}
	}
	return true, size, info.ModTime()
}

func (m *mihomoManager) mmdbStatus() (bool, int64, time.Time) {
	return isValidMMDBFile(m.mmdbPath())
}

func (m *mihomoManager) installMMDBData(raw []byte) (int64, error) {
	if !isValidMMDBData(raw) {
		return 0, errors.New("无效的 country.mmdb 文件（缺少 MaxMind 数据库标识或大小异常）")
	}
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return 0, err
	}
	_ = os.Chmod(m.dir, 0700)
	if err := atomicWrite(m.mmdbPath(), raw); err != nil {
		return 0, err
	}
	meta := m.readGeoMeta()
	meta.UpdatedAt = time.Now().UTC()
	if meta.Version == "" {
		meta.Version = time.Now().Format("20060102")
	}
	m.saveGeoMeta(meta)
	return int64(len(raw)), nil
}

func allowedMMDBHost(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "fastly.jsdelivr.net", "testingcf.jsdelivr.net", "cdn.jsdelivr.net",
		"github.com", "api.github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
		return true
	}
	return false
}

func (m *mihomoManager) downloadGeoAsset(ctx context.Context, state State, filename, targetPath string, maxBytes int64, validator func([]byte) bool) (int64, string, error) {
	client := outboundClient(state, 30*time.Second)
	defer closeClient(client)
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !allowedMMDBHost(r.URL) {
			return errors.New("Geo 规则库下载重定向地址不受信任")
		}
		r.Header.Del("Authorization")
		r.Header.Del("Cookie")
		return nil
	}

	mirrors := []string{
		"https://fastly.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/" + filename,
		"https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/" + filename,
		"https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/" + filename,
		"https://github.com/MetaCubeX/meta-rules-dat/releases/latest/download/" + filename,
	}

	var lastErr error
	for _, mirrorURL := range mirrors {
		u, err := url.Parse(mirrorURL)
		if err != nil || !allowedMMDBHost(u) {
			continue
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, mirrorURL, nil)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "GateHome/"+Version)
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			cancel()
			lastErr = fmt.Errorf("%s 返回 HTTP %d", u.Hostname(), resp.StatusCode)
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		resp.Body.Close()
		cancel()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if int64(len(raw)) > maxBytes {
			lastErr = fmt.Errorf("%s 文件超过 %d MiB 限制", filename, maxBytes>>20)
			continue
		}
		if len(raw) < 1024 || (validator != nil && !validator(raw)) {
			lastErr = fmt.Errorf("无效的 %s 文件（格式校验未通过）", filename)
			continue
		}
		if err := os.MkdirAll(m.dir, 0700); err != nil {
			return 0, "", err
		}
		_ = os.Chmod(m.dir, 0700)
		if err := atomicWrite(targetPath, raw); err != nil {
			lastErr = err
			continue
		}
		return int64(len(raw)), u.Hostname(), nil
	}
	if lastErr == nil {
		lastErr = errors.New("无可用下载镜像")
	}
	return 0, "", fmt.Errorf("%s 下载失败 (%v)", filename, lastErr)
}

func (m *mihomoManager) fetchLatestGeoVersion(ctx context.Context, state State) string {
	client := outboundClient(state, 5*time.Second)
	defer closeClient(client)
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "https://api.github.com/repos/MetaCubeX/meta-rules-dat/releases/latest", nil)
	if err != nil {
		return time.Now().Format("20060102")
	}
	req.Header.Set("User-Agent", "GateHome/"+Version)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return time.Now().Format("20060102")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return time.Now().Format("20060102")
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 128<<10)).Decode(&release) == nil && strings.TrimSpace(release.TagName) != "" {
		return strings.TrimSpace(release.TagName)
	}
	return time.Now().Format("20060102")
}

func (m *mihomoManager) DownloadMMDB(ctx context.Context, state State) (int64, string, error) {
	size, host, err := m.downloadGeoAsset(ctx, state, "country.mmdb", m.mmdbPath(), maxMihomoMMDBBytes, isValidMMDBData)
	if err != nil {
		return 0, "", fmt.Errorf("%v，请在「端口与核心」手动上传 country.mmdb", err)
	}
	meta := m.readGeoMeta()
	meta.Source = host
	meta.UpdatedAt = time.Now().UTC()
	if meta.Version == "" {
		meta.Version = time.Now().Format("20060102")
	}
	m.saveGeoMeta(meta)
	return size, host, nil
}

func (m *mihomoManager) UpdateAllGeoData(ctx context.Context, state State) (mihomoGeoDataStatus, error) {
	assets := []struct {
		name      string
		path      string
		maxBytes  int64
		validator func([]byte) bool
	}{
		{"country.mmdb", m.mmdbPath(), maxMihomoMMDBBytes, isValidMMDBData},
		{"geoip.dat", m.geoipPath(), maxMihomoGeoDatBytes, nil},
		{"geosite.dat", m.geositePath(), maxMihomoGeoDatBytes, nil},
	}
	var sources []string
	var errs []string
	for _, asset := range assets {
		_, host, err := m.downloadGeoAsset(ctx, state, asset.name, asset.path, asset.maxBytes, asset.validator)
		if err != nil {
			errs = append(errs, err.Error())
		} else if host != "" && !strings.Contains(strings.Join(sources, ","), host) {
			sources = append(sources, host)
		}
	}
	meta := m.readGeoMeta()
	if len(sources) > 0 {
		meta.Source = strings.Join(sources, ", ")
		meta.UpdatedAt = time.Now().UTC()
		meta.Version = m.fetchLatestGeoVersion(ctx, state)
	}
	if len(errs) > 0 {
		meta.LastError = strings.Join(errs, "；")
	} else {
		meta.LastError = ""
	}
	m.saveGeoMeta(meta)

	m.mu.Lock()
	cfgCopy := m.cfg
	m.mu.Unlock()
	status := m.geoDataStatus(cfgCopy)
	if len(errs) > 0 {
		return status, errors.New(meta.LastError)
	}
	return status, nil
}
