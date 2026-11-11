package gateway

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const defaultIconRepo = "https://github.com/xushier/HD-Icons"
const iconGitHubMirror = "https://gh-proxy.com"
const maxIconArchive = 512 << 20
const maxIconMetadata = 2 << 20

var importIconStyles = []string{"border-radius", "circle", "svg"}
var githubIconSlug = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,99}$`)

type iconImportStatus struct {
	State      string `json:"state"`
	Repository string `json:"repository,omitempty"`
	Progress   int    `json:"progress"`
	Message    string `json:"message,omitempty"`
	Count      int    `json:"count"`
}

type iconImportRequest struct {
	Repository string `json:"repository"`
	Mirror     string `json:"mirror"`
}

func iconArchiveURL(input iconImportRequest, customMirror string) (archive, pack string, err error) {
	raw := strings.TrimSpace(input.Repository)
	if raw == "" {
		raw = defaultIconRepo
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", errors.New("请输入公开 GitHub 仓库地址，例如 " + defaultIconRepo)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || !githubIconSlug.MatchString(parts[0]) || !githubIconSlug.MatchString(parts[1]) || u.RawPath != "" {
		return "", "", errors.New("GitHub 仓库地址格式无效")
	}
	repo := strings.TrimSuffix(parts[1], ".git")
	archive = "https://codeload.github.com/" + parts[0] + "/" + repo + "/zip/HEAD"
	pack = parts[0] + "--" + repo
	if strings.EqualFold(parts[0]+"/"+repo, "xushier/HD-Icons") {
		pack = "HD-Icons"
	}
	mirror := ""
	switch input.Mirror {
	case "", "direct":
	case "ghproxy":
		mirror = iconGitHubMirror
	case "custom":
		mirror = strings.TrimRight(strings.TrimSpace(customMirror), "/")
		m, err := url.Parse(mirror)
		if err != nil || m.Scheme != "https" || m.Hostname() == "" || m.User != nil || m.RawQuery != "" || m.Fragment != "" {
			return "", "", errors.New("请在服务器设置有效的 GATEHOME_ICON_IMPORT_MIRROR HTTPS 地址")
		}
	default:
		return "", "", errors.New("无效的 GitHub 镜像选项")
	}
	if mirror != "" {
		archive = mirror + "/https://github.com/" + parts[0] + "/" + repo + "/archive/HEAD.zip"
	}
	return archive, pack, nil
}

func (l *iconLibrary) importProgress(progress int, message string) {
	l.jobMu.Lock()
	defer l.jobMu.Unlock()
	l.job.Progress = progress
	l.job.Message = message
}

func (a *Admin) startIconImport(w http.ResponseWriter, r *http.Request) {
	var input iconImportRequest
	if !decodeBody(w, r, &input) {
		return
	}
	archive, pack, err := iconArchiveURL(input, os.Getenv("GATEHOME_ICON_IMPORT_MIRROR"))
	if err != nil {
		apiError(w, 400, err.Error())
		return
	}
	if a.maintenance.Busy() {
		apiError(w, 409, "正在执行维护操作")
		return
	}
	library := &a.icons
	library.jobMu.Lock()
	if library.job.State == "running" {
		library.jobMu.Unlock()
		apiError(w, 409, "已有图标库导入任务运行中")
		return
	}
	status := iconImportStatus{State: "running", Repository: strings.TrimSpace(input.Repository), Progress: 5, Message: "正在下载 GitHub 图标包"}
	if status.Repository == "" {
		status.Repository = defaultIconRepo
	}
	library.job = status
	library.jobMu.Unlock()

	client := outboundClient(a.store.Snapshot(), 8*time.Minute)
	initial, _ := url.Parse(archive)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 4 || req.URL.Scheme != "https" || req.URL.User != nil ||
			(req.URL.Host != initial.Host && req.URL.Host != "github.com" && req.URL.Host != "codeload.github.com") {
			return errors.New("GitHub 图标包重定向地址无效")
		}
		return nil
	}
	go func() {
		defer closeClient(client)
		count, err := a.importIcons(client, archive, pack)
		library.jobMu.Lock()
		defer library.jobMu.Unlock()
		if err != nil {
			library.job.State = "failed"
			library.job.Message = err.Error()
			return
		}
		library.job.State = "completed"
		library.job.Progress = 100
		library.job.Count = count
		library.job.Message = "图标导入完成"
	}()
	jsonResponse(w, 202, status)
}

func (a *Admin) importIcons(client *http.Client, archive, pack string) (int, error) {
	data := a.store.paths.Data
	stage, err := stageIcons(data, ".import-")
	if err != nil {
		return 0, errors.New("无法暂存图标包，请检查数据目录")
	}
	defer os.RemoveAll(stage)
	zipPath := filepath.Join(stage, "archive.zip")
	if err := downloadIconArchive(client, archive, zipPath); err != nil {
		return 0, err
	}
	a.icons.importProgress(55, "正在验证并解压图标")
	packDir := filepath.Join(stage, "pack")
	count, err := extractIconArchive(zipPath, packDir, pack)
	if err != nil {
		return 0, err
	}
	a.icons.importProgress(85, "正在更新图标索引")
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	if a.maintenance.Busy() {
		return 0, errors.New("正在执行维护操作，请在维护结束后重新导入")
	}
	a.icons.mu.Lock()
	defer a.icons.mu.Unlock()
	if err := a.publishIconPack(packDir, pack); err != nil {
		return 0, errors.New("保存图标库或索引失败，请检查磁盘空间与数据目录")
	}
	return count, nil
}

func downloadIconArchive(client *http.Client, archive, filename string) error {
	resp, err := client.Get(archive)
	if err != nil {
		return errors.New("图标包下载失败，请检查网络或切换加速选项")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub 图标包下载失败（HTTP %d）", resp.StatusCode)
	}
	if resp.ContentLength > maxIconArchive {
		return errors.New("图标压缩包超过 512 MiB")
	}
	file, err := os.Create(filename)
	if err != nil {
		return errors.New("无法暂存图标包")
	}
	size, err := io.Copy(file, io.LimitReader(resp.Body, maxIconArchive+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return errors.New("图标包下载或写入中断")
	}
	if size > maxIconArchive {
		return errors.New("图标压缩包超过 512 MiB")
	}
	return nil
}

func extractIconArchive(filename, dest, pack string) (int, error) {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return 0, errors.New("下载内容不是有效的 ZIP 图标包")
	}
	defer archive.Close()
	if len(archive.File) > 20_000 {
		return 0, errors.New("图标包文件过多")
	}
	var count int
	var size uint64
	for _, entry := range archive.File {
		if !entry.Mode().IsRegular() || !fs.ValidPath(entry.Name) || strings.Contains(entry.Name, "\\") {
			continue
		}
		_, rel, ok := strings.Cut(entry.Name, "/")
		if !ok {
			continue
		}
		limit := uint64(maxImageUpload)
		if rel == "icons.json" {
			limit = maxIconMetadata
		} else {
			style, _, ok := strings.Cut(rel, "/")
			if !ok || !slices.Contains(importIconStyles, style) || !safeIconPath("imported/"+pack+"/"+rel) {
				continue
			}
		}
		if entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > limit {
			continue
		}
		size += entry.UncompressedSize64
		if size > 768<<20 {
			return 0, errors.New("解压后的图标包超过 768 MiB")
		}
		stream, err := entry.Open()
		if err != nil {
			return 0, errors.New("图标压缩包损坏")
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			stream.Close()
			return 0, errors.New("无法解压图标")
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			stream.Close()
			return 0, errors.New("图标包包含重复路径或磁盘写入失败")
		}
		written, copyErr := io.Copy(out, io.LimitReader(stream, int64(limit)+1))
		closeErr := out.Close()
		stream.Close()
		if copyErr != nil || closeErr != nil || uint64(written) != entry.UncompressedSize64 {
			return 0, errors.New("图标压缩包损坏或写入失败")
		}
		if rel == "icons.json" {
			continue
		}
		file, err := os.Open(target)
		if err != nil {
			return 0, errors.New("无法读取已解压的图标")
		}
		_, _, err = iconDimensions(file, iconFormat(rel))
		file.Close()
		if err != nil {
			if err := os.Remove(target); err != nil {
				return 0, errors.New("无法清理无效的图标")
			}
			continue
		}
		count++
	}
	if count == 0 {
		return 0, errors.New("图标包中没有可导入的圆角、圆形或 SVG 图标")
	}
	return count, nil
}

// Existing paths remain valid after re-import, including previously selected route icons.
func (a *Admin) publishIconPack(stage, pack string) error {
	data := a.store.paths.Data
	root, err := os.OpenRoot(data)
	if err != nil {
		return err
	}
	defer root.Close()
	dest := "icons/imported/" + pack
	if _, err := plainIconPath(root, dest, true, true); err != nil {
		return err
	}
	stageRel, err := filepath.Rel(data, stage)
	if err != nil {
		return err
	}
	var installed []string
	err = fs.WalkDir(os.DirFS(stage), ".", func(rel string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		target := dest + "/" + rel
		if _, err := plainIconPath(root, path.Dir(target), true, true); err != nil {
			return err
		}
		if _, err := plainIconPath(root, target, false, false); !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := root.Rename(filepath.ToSlash(stageRel)+"/"+rel, target); err != nil {
			return err
		}
		installed = append(installed, target)
		return nil
	})
	if err == nil {
		err = a.icons.reindex(data)
	}
	if err != nil {
		for _, name := range installed {
			_ = root.Remove(name)
		}
	}
	return err
}

func readPackMetadata(root *os.Root, pack string) map[string]iconMetadata {
	items := map[string]iconMetadata{}
	prefix := "imported/" + pack + "/"
	rel := prefix + "icons.json"
	info, err := plainIconPath(root, rel, false, false)
	if err != nil || info.Size() > maxIconMetadata {
		return items
	}
	file, err := root.Open(rel)
	if err != nil {
		return items
	}
	defer file.Close()
	var meta struct {
		Icons []iconMetadata `json:"icons"`
	}
	if json.NewDecoder(io.LimitReader(file, maxIconMetadata)).Decode(&meta) != nil {
		return items
	}
	for _, item := range meta.Icons {
		rel := strings.TrimPrefix(item.RelPath, prefix)
		if rel == "" {
			u, err := url.Parse(item.URL)
			if err != nil {
				continue
			}
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if i := slices.IndexFunc(parts, func(s string) bool { return slices.Contains(importIconStyles, s) }); i >= 0 {
				rel = strings.Join(parts[i:], "/")
			}
		}
		if safeIconPath(prefix + rel) {
			items[prefix+rel] = item
		}
	}
	return items
}
