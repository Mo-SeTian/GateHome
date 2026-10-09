package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const maxImageUpload = 5 << 20
const maxRouteImage = 128 << 10

var routeImageID = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Raster images are resized and re-encoded, discarding metadata and trailing data.
func normalizeRouteImage(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > maxImageUpload {
		return nil, errors.New("图片须小于等于 5 MiB")
	}
	mime := http.DetectContentType(data)
	if mime == "image/x-icon" || mime == "image/vnd.microsoft.icon" {
		if len(data) > 64<<10 || !validDiscoveryICO(data) {
			return nil, errors.New("ICO 图片无效或超过 64 KiB")
		}
		return data, nil
	}
	if mime != "image/png" && mime != "image/jpeg" && mime != "image/gif" && mime != "image/webp" {
		return nil, errors.New("支持 PNG、JPEG、GIF、WebP 和 ICO 图片")
	}
	c, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || c.Width < 1 || c.Height < 1 || c.Width > 4096 || c.Height > 4096 {
		return nil, errors.New("图片无效或尺寸超过 4096 × 4096")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("图片无法解码")
	}
	w, h := c.Width, c.Height
	if max(w, h) > 128 {
		w = max(1, w*128/max(c.Width, c.Height))
		h = max(1, h*128/max(c.Width, c.Height))
	}
	thumb := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(thumb, thumb.Bounds(), decoded, decoded.Bounds(), draw.Src, nil)
	var out bytes.Buffer
	if png.Encode(&out, thumb) != nil || out.Len() > maxRouteImage {
		return nil, errors.New("图片处理失败，请选择其他图片")
	}
	return out.Bytes(), nil
}

func readRouteImage(dir, id string) ([]byte, error) {
	if !routeImageID.MatchString(id) {
		return nil, errors.New("图片引用无效")
	}
	parent := filepath.Join(dir, "route-images")
	parentInfo, parentErr := os.Lstat(parent)
	if parentErr != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("图片目录无效")
	}
	path := filepath.Join(parent, id+".img")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxRouteImage {
		return nil, errors.New("图片不存在")
	}
	data, err := os.ReadFile(path)
	if err != nil || !validRouteImage(id, data) {
		return nil, errors.New("图片读取失败")
	}
	return data, nil
}

func storeRouteImage(dir string, data []byte) (string, error) {
	data, err := normalizeRouteImage(data)
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "route-images")
	if os.MkdirAll(dir, 0700) != nil {
		return "", errors.New("图片目录无法创建")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("图片目录无效")
	}
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])
	if atomicWrite(filepath.Join(dir, id+".img"), data) != nil {
		return "", errors.New("图片保存失败")
	}
	return id, nil
}

// Only committed rule images enter a backup. Uncommitted uploads expire after a day.
func pruneRouteImages(dir string, c Config) {
	keep := homepageImages(c.Homepage)
	for _, r := range c.Routes {
		keep[r.Image] = true
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "route-images"))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".img") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".img")
		if !routeImageID.MatchString(id) || keep[id] {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(time.Now().Add(-24*time.Hour)) {
			os.Remove(filepath.Join(dir, "route-images", e.Name()))
		}
	}
}

func imageURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || len(raw) > 2048 {
		return nil, errors.New("请输入不含账号密码的 HTTP / HTTPS 图片 URL")
	}
	return u, nil
}

func downloadRouteImage(ctx context.Context, raw string) ([]byte, error) {
	u, err := imageURL(raw)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 32 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, ip := range addresses {
				ip = ip.Unmap()
				if !ip.IsGlobalUnicast() && !ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip == netip.MustParseAddr("100.100.100.200") {
					return nil, errors.New("不允许此图片地址")
				}
			}
			for _, ip := range addresses {
				conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
			}
			return nil, errors.New("图片地址无法连接")
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("重定向过多")
		}
		_, err := imageURL(r.URL.String())
		return err
	}}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, errors.New("图片 URL 无效")
	}
	req.Header.Set("User-Agent", "Gatehouse-ImageImport/1.0")
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("图片下载失败，请检查地址、网络或 HTTPS 证书")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("图片地址未返回成功响应")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxImageUpload+1))
	if err != nil || len(data) > maxImageUpload {
		return nil, errors.New("图片下载失败或超过 5 MiB")
	}
	return data, nil
}

func (a *Admin) routeImageRoutes(mux *http.ServeMux) {
	respond := func(w http.ResponseWriter, data []byte) {
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		if a.maintenance.Busy() {
			apiError(w, 409, "正在执行维护操作")
			return
		}
		dir := a.store.paths.Data
		id, err := storeRouteImage(dir, data)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		pruneRouteImages(dir, a.store.Snapshot().Config)
		jsonResponse(w, 201, map[string]string{"id": id, "url": "/api/route-images/" + id})
	}
	mux.HandleFunc("POST /api/route-images/upload", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxImageUpload+(64<<10))
		reader, err := r.MultipartReader()
		if err != nil {
			apiError(w, 400, "请选择图片文件")
			return
		}
		part, err := reader.NextPart()
		if err != nil || part.FormName() != "file" {
			apiError(w, 400, "请选择图片文件")
			return
		}
		data, err := io.ReadAll(io.LimitReader(part, maxImageUpload+1))
		part.Close()
		if err != nil || len(data) > maxImageUpload {
			apiError(w, 400, "图片超过 5 MiB 或读取失败")
			return
		}
		if _, err = reader.NextPart(); err != io.EOF {
			apiError(w, 400, "一次只能上传一张图片")
			return
		}
		respond(w, data)
	}))
	mux.HandleFunc("POST /api/route-images/import", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL    string `json:"url"`
			ScanID string `json:"scan_id"`
			Port   int    `json:"port"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		var data []byte
		if body.ScanID != "" {
			scan := a.discovery.find(body.ScanID)
			if scan != nil {
				scan.mu.Lock()
				data = scan.icons[body.Port].data
				scan.mu.Unlock()
			}
			if data == nil {
				apiError(w, 404, "扫描图片已失效或此服务没有图标，请重新扫描")
				return
			}
		} else {
			var err error
			data, err = downloadRouteImage(r.Context(), body.URL)
			if err != nil {
				apiError(w, 400, err.Error())
				return
			}
		}
		respond(w, data)
	}))
	mux.HandleFunc("GET /api/route-images/{id}", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		data, err := readRouteImage(a.store.paths.Data, r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", http.DetectContentType(data))
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Write(data)
	}))
}
