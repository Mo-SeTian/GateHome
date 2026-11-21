package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type webDAVFile struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}
type webDAVDirectory struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}
type webDAVClient struct {
	base                   *url.URL
	client                 *http.Client
	username, password, id string
}

func webDAVBase(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || len(raw) > 2048 {
		return nil, errors.New("请填写不含账号密码、参数或片段的 HTTP(S) WebDAV 目录地址")
	}
	if strings.ContainsAny(u.Path, "\x00\\") || path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") && u.Path != "" && u.Path != "/" {
		return nil, errors.New("WebDAV 目录路径无效")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return nil, errors.New("WebDAV 端口无效")
		}
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/"
	u.RawPath = ""
	return u, nil
}

func newWebDAVClient(state State) (*webDAVClient, error) {
	settings := state.WebDAVBackup
	base, err := webDAVBase(settings.Config.URL)
	if err != nil {
		return nil, err
	}
	client := serviceEndpointClient(state, 90*time.Second)
	return &webDAVClient{base: base, client: client, username: settings.Config.Username, password: settings.Password, id: settings.InstanceID}, nil
}
func (d *webDAVClient) close() { closeClient(d.client) }
func (d *webDAVClient) target(name string) *url.URL {
	if name == "" {
		copy := *d.base
		return &copy
	}
	return d.base.ResolveReference(&url.URL{Path: name})
}
func (d *webDAVClient) request(ctx context.Context, method string, target *url.URL, data []byte, headers map[string]string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("WebDAV 请求无效")
	}
	if d.username != "" || d.password != "" {
		request.SetBasicAuth(d.username, d.password)
	}
	request.Header.Set("User-Agent", "GateHome-Backup/1")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := d.client.Do(request)
	if err != nil {
		return nil, errors.New("WebDAV 请求失败，请检查地址、网络、证书和出站代理")
	}
	return response, nil
}
func webDAVStatus(response *http.Response, allowed ...int) error {
	for _, code := range allowed {
		if response.StatusCode == code {
			return nil
		}
	}
	return webDAVStatusError(response.StatusCode)
}
func webDAVStatusError(status int) error {
	switch status {
	case http.StatusUnauthorized:
		return errors.New("WebDAV 认证失败（HTTP 401），请核对账号和存储密码。坚果云需要第三方应用密码")
	case http.StatusForbidden:
		return errors.New("WebDAV 拒绝访问（HTTP 403），请检查目录权限")
	case http.StatusNotFound:
		return errors.New("WebDAV 路径不存在或该目录不支持此操作（HTTP 404），请检查所选目录")
	default:
		return fmt.Errorf("WebDAV 返回 HTTP %d", status)
	}
}

const webDAVProps = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getcontentlength/></d:prop></d:propfind>`

type davResponse struct {
	Href      string `xml:"href"`
	PropStats []struct {
		Status string `xml:"status"`
		Prop   struct {
			Type *struct {
				Collection *struct{} `xml:"collection"`
			} `xml:"resourcetype"`
			Length string `xml:"getcontentlength"`
		} `xml:"prop"`
	} `xml:"propstat"`
}

func (d *webDAVClient) propfind(ctx context.Context, target *url.URL, depth string) ([]davResponse, int, error) {
	response, err := d.request(ctx, "PROPFIND", target, []byte(webDAVProps), map[string]string{"Depth": depth, "Content-Type": "application/xml; charset=utf-8"})
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode == 404 {
		return nil, 404, nil
	}
	if err := webDAVStatus(response, 207); err != nil {
		return nil, response.StatusCode, err
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return nil, 207, errors.New("WebDAV 目录响应过大或读取失败")
	}
	var result struct {
		Responses []davResponse `xml:"response"`
	}
	if xml.Unmarshal(data, &result) != nil || len(result.Responses) > 5000 {
		return nil, 207, errors.New("WebDAV 目录响应无效")
	}
	return result.Responses, 207, nil
}
func davProperty(row davResponse) (bool, int64, bool) {
	var collection, valid bool
	var size int64
	for _, stat := range row.PropStats {
		parts := strings.Fields(stat.Status)
		if len(parts) < 2 || parts[1] != "200" {
			continue
		}
		valid = true
		if stat.Prop.Type != nil {
			collection = stat.Prop.Type.Collection != nil
		}
		if stat.Prop.Length != "" {
			size, _ = strconv.ParseInt(strings.TrimSpace(stat.Prop.Length), 10, 64)
		}
	}
	return collection, size, valid
}

func webDAVSameOrigin(a, b *url.URL) bool {
	return a.Scheme == b.Scheme && strings.EqualFold(a.Host, b.Host)
}

func (d *webDAVClient) directories(ctx context.Context) ([]webDAVDirectory, error) {
	rows, status, err := d.propfind(ctx, d.base, "1")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, webDAVStatusError(status)
	}
	folders := []webDAVDirectory{}
	seen := make(map[string]bool)
	for _, row := range rows {
		isDir, _, valid := davProperty(row)
		if !valid {
			continue
		}
		href, err := url.Parse(strings.TrimSpace(row.Href))
		if err != nil || href.User != nil || href.RawQuery != "" || href.ForceQuery || href.Fragment != "" {
			continue
		}
		target, err := webDAVBase(d.base.ResolveReference(href).String())
		if err != nil || !webDAVSameOrigin(target, d.base) {
			continue
		}
		if target.Path == d.base.Path {
			if !isDir {
				return nil, errors.New("WebDAV 地址必须指向目录")
			}
			continue
		}
		if !isDir || path.Dir(path.Clean(target.Path)) != path.Clean(d.base.Path) || seen[target.Path] {
			continue
		}
		seen[target.Path] = true
		folders = append(folders, webDAVDirectory{Name: path.Base(target.Path), URL: target.String()})
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].Name < folders[j].Name })
	return folders, nil
}
func (d *webDAVClient) ensureCollection(ctx context.Context, target *url.URL, remaining int) error {
	rows, status, err := d.propfind(ctx, target, "0")
	if err != nil {
		return err
	}
	if status == 207 {
		for _, row := range rows {
			if collection, _, valid := davProperty(row); valid && collection {
				return nil
			}
		}
		return errors.New("WebDAV 地址必须指向目录")
	}
	if remaining == 0 || target.Path == "/" {
		return errors.New("WebDAV 父目录不存在，请检查目录地址")
	}
	parent := *target
	parent.Path = path.Dir(strings.TrimSuffix(target.Path, "/")) + "/"
	parent.RawPath = ""
	if parent.Path == "//" {
		parent.Path = "/"
	}
	if err := d.ensureCollection(ctx, &parent, remaining-1); err != nil {
		return err
	}
	response, err := d.request(ctx, "MKCOL", target, nil, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return webDAVStatus(response, 201, 405)
}

var webDAVBackupName = regexp.MustCompile(`^gatehome-backup-[a-f0-9]{24}-\d{8}T\d{6}Z-[a-f0-9]{12}\.zip$`)

func (d *webDAVClient) owns(name string) bool {
	return len(d.id) == 24 && webDAVBackupName.MatchString(name) && strings.HasPrefix(name, "gatehome-backup-"+d.id+"-")
}
func (d *webDAVClient) list(ctx context.Context) ([]webDAVFile, error) {
	rows, status, err := d.propfind(ctx, d.base, "1")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, webDAVStatusError(status)
	}
	files := []webDAVFile{}
	seen := map[string]bool{}
	for _, row := range rows {
		relative, err := url.Parse(strings.TrimSpace(row.Href))
		if err != nil || relative.User != nil || relative.RawQuery != "" || relative.Fragment != "" {
			continue
		}
		target := d.base.ResolveReference(relative)
		if !webDAVSameOrigin(target, d.base) || path.Dir(target.Path) != path.Clean(d.base.Path) {
			continue
		}
		name := path.Base(target.Path)
		if !webDAVBackupName.MatchString(name) || seen[name] {
			continue
		}
		collection, size, valid := davProperty(row)
		if !valid || collection || size < 0 {
			continue
		}
		created, err := time.Parse("20060102T150405Z", strings.Split(name, "-")[3])
		if err != nil {
			continue
		}
		seen[name] = true
		files = append(files, webDAVFile{Name: name, Size: size, CreatedAt: created})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].CreatedAt.Equal(files[j].CreatedAt) {
			return files[i].Name > files[j].Name
		}
		return files[i].CreatedAt.After(files[j].CreatedAt)
	})
	return files, nil
}
func (d *webDAVClient) get(ctx context.Context, name string) ([]byte, error) {
	if !webDAVBackupName.MatchString(name) {
		return nil, errors.New("请选择 GateHome 备份文件")
	}
	response, err := d.request(ctx, "GET", d.target(name), nil, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if err := webDAVStatus(response, 200); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBackupBytes+(1<<20)+1))
	if err != nil || len(data) > maxBackupBytes+(1<<20) {
		return nil, errors.New("WebDAV 备份读取失败或超过容量上限")
	}
	return data, nil
}
func (d *webDAVClient) remove(ctx context.Context, name string) error {
	if !d.owns(name) {
		return errors.New("不能删除其他文件")
	}
	response, err := d.request(ctx, "DELETE", d.target(name), nil, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return webDAVStatus(response, 200, 204, 404)
}
func (d *webDAVClient) upload(ctx context.Context, name string, data []byte, keep int) (string, error) {
	if !d.owns(name) {
		return "", errors.New("备份名称无效")
	}
	if err := d.ensureCollection(ctx, d.base, 12); err != nil {
		return "", fmt.Errorf("检查备份目录失败：%w", err)
	}
	staged := name + ".part"
	response, err := d.request(ctx, "PUT", d.target(staged), data, map[string]string{"If-None-Match": "*", "Content-Type": "application/octet-stream"})
	if err != nil {
		return "", fmt.Errorf("上传临时备份失败：%w", err)
	}
	err = webDAVStatus(response, 200, 201, 204)
	response.Body.Close()
	if err != nil {
		return "", fmt.Errorf("上传临时备份失败：%w", err)
	}
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		response, err := d.request(ctx, "DELETE", d.target(staged), nil, nil)
		if err == nil {
			response.Body.Close()
		}
	}
	response, err = d.request(ctx, "MOVE", d.target(staged), nil, map[string]string{"Destination": d.target(name).String(), "Overwrite": "F"})
	if err != nil {
		cleanup()
		return "", fmt.Errorf("提交备份文件失败（MOVE）：%w", err)
	}
	err = webDAVStatus(response, 201, 204)
	response.Body.Close()
	if err != nil {
		cleanup()
		return "", fmt.Errorf("提交备份文件失败（MOVE）：%w", err)
	}
	verified, err := d.get(ctx, name)
	if err != nil {
		return name, fmt.Errorf("读取备份进行校验失败，旧备份保留：%w", err)
	}
	if sha256.Sum256(verified) != sha256.Sum256(data) {
		return name, errors.New("远程备份校验失败，旧备份保留，请检查后重新备份")
	}
	files, err := d.list(ctx)
	if err != nil {
		return name, fmt.Errorf("备份已上传并校验，但无法读取目录进行保留清理：%w", err)
	}
	kept := 1
	for _, file := range files {
		if file.Name == name || !d.owns(file.Name) {
			continue
		}
		if kept >= keep {
			if err := d.remove(ctx, file.Name); err != nil {
				return name, fmt.Errorf("备份已上传并校验，但旧备份清理失败：%w", err)
			}
		} else {
			kept++
		}
	}
	return name, nil
}
