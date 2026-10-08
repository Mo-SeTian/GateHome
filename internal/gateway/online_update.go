package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const onlineReleaseAPI = "https://api.github.com/repos/Mo-SeTian/GateHome/releases/latest"
const onlineReleaseBase = "https://github.com/Mo-SeTian/GateHome/releases/"

type onlineRelease struct {
	Version          string    `json:"version"`
	PublishedAt      time.Time `json:"published_at"`
	Size             int64     `json:"size"`
	UpdateAvailable  bool      `json:"update_available"`
	ReleaseURL       string    `json:"release_url"`
	assetURL, digest string
}

type onlineUpdateStatus struct {
	Phase      string `json:"phase"`
	Version    string `json:"version"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	Error      string `json:"error"`
}

type onlineUpdateJob struct {
	mu     sync.Mutex
	status onlineUpdateStatus
}

func (j *onlineUpdateJob) snapshot() onlineUpdateStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status
}

func (j *onlineUpdateJob) setPhase(phase, message string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status.Phase, j.status.Error = phase, message
}

type updateProgressWriter struct {
	bytes  int64
	notify func(int64)
}

func (w *updateProgressWriter) Write(data []byte) (int, error) {
	w.bytes += int64(len(data))
	w.notify(w.bytes)
	return len(data), nil
}

func onlineUpdateClient(state State) *http.Client {
	c := outboundClient(state, 4*time.Minute)
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !allowedUpdateURL(r.URL) {
			return errors.New("更新下载重定向地址不受信任")
		}
		return nil
	}
	return c
}

func allowedUpdateURL(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch u.Hostname() {
	case "github.com", "api.github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
		return true
	}
	return false
}

func fetchOnlineUpdate(ctx context.Context, c *http.Client, address string, limit int64) ([]byte, error) {
	return fetchOnlineUpdateProgress(ctx, c, address, limit, nil)
}

func fetchOnlineUpdateProgress(ctx context.Context, c *http.Client, address string, limit int64, progress func(int64)) ([]byte, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, errors.New("更新请求无效")
	}
	r.Header.Set("User-Agent", "GateHome/"+Version)
	if r.URL.Hostname() == "api.github.com" {
		r.Header.Set("Accept", "application/vnd.github+json")
		r.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	response, err := c.Do(r)
	if err != nil {
		return nil, errors.New("连接 GitHub 失败，请检查网络和已保存的出站代理")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == 403 && response.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, errors.New("GitHub 版本检查额度暂时用尽，请稍后重试")
		}
		return nil, fmt.Errorf("GitHub 请求失败（HTTP %d），请稍后重试或检查出站代理", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, errors.New("更新下载超过大小限制")
	}
	reader := io.LimitReader(response.Body, limit+1)
	if progress != nil {
		reader = io.TeeReader(reader, &updateProgressWriter{notify: progress})
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, errors.New("更新下载中断，请检查网络或出站代理后重试")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("更新下载超过大小限制")
	}
	return data, nil
}

func latestOnlineRelease(ctx context.Context, c *http.Client) (onlineRelease, error) {
	data, err := fetchOnlineUpdate(ctx, c, onlineReleaseAPI, 1<<20)
	if err != nil {
		return onlineRelease{}, err
	}
	var release struct {
		Tag         string    `json:"tag_name"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"published_at"`
		Assets      []struct {
			Name   string `json:"name"`
			State  string `json:"state"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"assets"`
	}
	if json.Unmarshal(data, &release) != nil || release.Draft || release.Prerelease || !strings.HasPrefix(release.Tag, "v") {
		return onlineRelease{}, errors.New("GitHub 未返回有效的正式版本")
	}
	version := strings.TrimPrefix(release.Tag, "v")
	comparison, err := compareVersions(version, Version)
	if err != nil {
		return onlineRelease{}, errors.New("GitHub 正式版本号无效")
	}
	name := "gatehouse-" + version + "-update.zip"
	wantURL := onlineReleaseBase + "download/" + release.Tag + "/" + name
	result := onlineRelease{Version: version, PublishedAt: release.PublishedAt, UpdateAvailable: comparison > 0, ReleaseURL: onlineReleaseBase + "tag/" + release.Tag}
	for _, asset := range release.Assets {
		if asset.Name != name {
			continue
		}
		digest := strings.TrimPrefix(asset.Digest, "sha256:")
		decoded, err := hex.DecodeString(digest)
		if result.assetURL != "" || asset.State != "uploaded" || asset.URL != wantURL || asset.Size < 1 || asset.Size > maxUpdateBytes || !strings.HasPrefix(asset.Digest, "sha256:") || err != nil || len(decoded) != sha256.Size {
			return onlineRelease{}, errors.New("正式版本更新包的地址、大小或 SHA-256 校验信息无效")
		}
		result.assetURL, result.digest, result.Size = asset.URL, strings.ToLower(digest), asset.Size
	}
	if result.assetURL == "" {
		return onlineRelease{}, errors.New("正式版本尚未提供完整的更新 ZIP，请稍后重试")
	}
	return result, nil
}

func downloadOnlineRelease(ctx context.Context, c *http.Client, release onlineRelease) ([]byte, error) {
	return downloadOnlineReleaseProgress(ctx, c, release, nil)
}

func downloadOnlineReleaseProgress(ctx context.Context, c *http.Client, release onlineRelease, progress func(int64)) ([]byte, error) {
	data, err := fetchOnlineUpdateProgress(ctx, c, release.assetURL, release.Size, progress)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != release.Size || hex.EncodeToString(sum[:]) != release.digest {
		return nil, errors.New("更新 ZIP 的大小或 SHA-256 校验失败，当前版本保持运行，请重新检查版本后重试")
	}
	return data, nil
}

func (a *Admin) onlineUpdateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/maintenance/online-update-status", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, a.onlineUpdate.snapshot())
	}))
	mux.HandleFunc("POST /api/maintenance/check-online-update", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if !decodeBody(w, r, &struct{}{}) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		client := onlineUpdateClient(a.store.Snapshot())
		defer closeClient(client)
		release, err := latestOnlineRelease(ctx, client)
		if err != nil {
			apiError(w, 502, err.Error())
			return
		}
		jsonResponse(w, 200, release)
	}))
	mux.HandleFunc("POST /api/maintenance/download-online-update", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Version string `json:"version"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		comparison, err := compareVersions(input.Version, Version)
		if err != nil || comparison <= 0 {
			apiError(w, 400, "请选择检查到的更高正式版本")
			return
		}
		// Return promptly even when another download or config write owns the lock.
		if !a.updateMu.TryLock() {
			apiError(w, 409, "正在保存配置或执行维护操作，请稍后重试")
			return
		}
		if a.maintenance == nil || !a.maintenance.Available() || a.maintenance.Busy() {
			a.updateMu.Unlock()
			apiError(w, 409, "当前无法更新，请使用 Linux 安装脚本或新版 Docker 启动方式，并等待维护结束")
			return
		}
		state := a.store.Snapshot()
		a.onlineUpdate.mu.Lock()
		a.onlineUpdate.status = onlineUpdateStatus{Phase: "checking", Version: input.Version}
		a.onlineUpdate.mu.Unlock()
		jsonResponse(w, 202, a.onlineUpdate.snapshot())
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		go func() {
			defer a.updateMu.Unlock()
			if err := a.runOnlineUpdate(state, input.Version); err != nil {
				a.onlineUpdate.setPhase("error", err.Error())
				if a.logs != nil {
					a.logs.Add(LogEntry{Category: "admin", Action: "在线更新", Target: input.Version, OK: false, Message: err.Error()})
				}
			}
		}()
	}))
}

func (a *Admin) runOnlineUpdate(state State, version string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	client := onlineUpdateClient(state)
	defer closeClient(client)
	release, err := latestOnlineRelease(ctx, client)
	if err != nil {
		return err
	}
	if release.Version != version || !release.UpdateAvailable {
		return errors.New("最新正式版本已变化，请重新检查版本后更新")
	}
	a.onlineUpdate.mu.Lock()
	a.onlineUpdate.status.Phase, a.onlineUpdate.status.Total = "downloading", release.Size
	a.onlineUpdate.mu.Unlock()
	data, err := downloadOnlineReleaseProgress(ctx, client, release, func(downloaded int64) {
		a.onlineUpdate.mu.Lock()
		a.onlineUpdate.status.Downloaded = downloaded
		a.onlineUpdate.mu.Unlock()
	})
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return errors.New("更新下载超时，请检查网络或出站代理后重试")
	}
	a.onlineUpdate.setPhase("verifying", "")
	result, err := a.maintenance.inspectUpdateVersion(data, release.Version)
	if err != nil {
		return err
	}
	if err := checkRestartPorts(a.ports, state.Config); err != nil {
		return err
	}
	if err := a.maintenance.schedule(result["id"].(string), "update"); err != nil {
		return err
	}
	a.onlineUpdate.setPhase("restarting", "")
	if a.logs != nil {
		a.logs.Add(LogEntry{Category: "admin", Action: "在线更新", Target: release.Version, OK: true, Message: "更新包校验通过，已安排更新并重启"})
	}
	a.maintenance.requestRestart()
	return nil
}
