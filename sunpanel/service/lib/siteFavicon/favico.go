package siteFavicon

import (
	"bytes"
	"context"
	"errors"
	"github.com/PuerkitoBio/goquery"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sun-panel/lib/imageasset"
	"time"
)

func safeURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || len(raw) > 4096 || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("需要不含账号密码的 HTTP/HTTPS 地址")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !allowedAddress(ip) {
		return nil, errors.New("不允许此图标地址")
	}
	return u, nil
}

// LAN and loopback services are supported, but metadata/link-local destinations are never fetched.
func allowedAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	return (ip.IsGlobalUnicast() || ip.IsLoopback()) && !ip.IsLinkLocalUnicast() && ip != netip.MustParseAddr("100.100.100.200")
}
func faviconClient() (*http.Client, *http.Transport) {
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 32 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return nil, errors.New("地址解析失败")
			}
			for _, ip := range ips {
				if !allowedAddress(ip) {
					return nil, errors.New("不允许此图标地址")
				}
			}
			for _, ip := range ips {
				conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
			}
			return nil, errors.New("图标地址无法连接")
		}}
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("重定向过多")
		}
		_, err := safeURL(r.URL.String())
		return err
	}}
	return client, transport
}
func fetch(ctx context.Context, raw, method string) (*http.Response, func(), error) {
	u, err := safeURL(raw)
	if err != nil {
		return nil, nil, err
	}
	client, transport := faviconClient()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, nil, errors.New("图标地址无效")
	}
	req.Header.Set("User-Agent", "Sun-Panel-ImageImport/1.0")
	response, err := client.Do(req)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, nil, errors.New("图标访问失败或超时")
	}
	close := func() { response.Body.Close(); transport.CloseIdleConnections() }
	if response.StatusCode != http.StatusOK {
		close()
		return nil, nil, errors.New("图标服务器未返回成功响应")
	}
	return response, close, nil
}
func boundedRead(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, errors.New("图标读取失败")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("内容超过大小限制")
	}
	return data, nil
}
func IsHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https" || strings.HasPrefix(raw, "//"))
}
func GetOneFaviconURL(raw string) (string, error) {
	return GetOneFaviconURLContext(context.Background(), raw)
}
func GetOneFaviconURLContext(ctx context.Context, raw string) (string, error) {
	response, close, err := fetch(ctx, raw, "GET")
	if err != nil {
		return "", err
	}
	defer close()
	data, err := boundedRead(response.Body, 2<<20)
	if err != nil {
		return "", err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("页面解析失败")
	}
	icon := ""
	doc.Find("link").Each(func(_ int, s *goquery.Selection) {
		if icon != "" {
			return
		}
		rel, _ := s.Attr("rel")
		href, _ := s.Attr("href")
		if !strings.Contains(strings.ToLower(rel), "icon") || href == "" {
			return
		}
		reference, err := url.Parse(href)
		if err != nil {
			return
		}
		resolved := response.Request.URL.ResolveReference(reference).String()
		if _, err := safeURL(resolved); err == nil {
			icon = resolved
		}
	})
	if icon == "" {
		return "", errors.New("页面未提供图标")
	}
	return icon, nil
}
func GetRemoteFileSize(raw string) (int64, error) {
	response, close, err := fetch(context.Background(), raw, "HEAD")
	if err != nil {
		return 0, err
	}
	defer close()
	return response.ContentLength, nil
}
func DownloadImage(raw, savePath string, maxSize int64) (*os.File, error) {
	return DownloadImageContext(context.Background(), raw, savePath, maxSize)
}
func DownloadImageContext(ctx context.Context, raw, savePath string, maxSize int64) (*os.File, error) {
	if maxSize <= 0 || maxSize > imageasset.Limit {
		return nil, errors.New("图片大小限制无效")
	}
	response, close, err := fetch(ctx, raw, "GET")
	if err != nil {
		return nil, err
	}
	defer close()
	data, err := boundedRead(response.Body, maxSize)
	if err != nil {
		return nil, err
	}
	ext, err := imageasset.Extension(data)
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(savePath, "favicon-*"+ext)
	if err != nil {
		return nil, errors.New("图标目录不可写")
	}
	_, err = file.Write(data)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		os.Remove(file.Name())
		return nil, errors.New("图标保存失败")
	}
	return file, nil
}
func GetOneFaviconURLAndUpload(raw string) (string, bool) {
	icon, err := GetOneFaviconURL(raw)
	return icon, err == nil
}
