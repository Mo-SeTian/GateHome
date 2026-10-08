package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

const commonWebPorts = "80,443,3000,3001,4000,4200,5000,5001,5055,5173,5244,5601,5666,5700,5800,6006,7000,7001,7080,7443,7575,7681,7777,7800,7878,8000,8001,8008,8010,8080,8081,8082,8083,8088,8090,8096,8100,8123,8181,8200,8443,8686,8765,8888,8920,8989,9000,9001,9090,9091,9117,9200,9443,9999,10000,18080,18443"
const maxDiscoveredServices = 100

type discoveredService struct {
	Port          int    `json:"port"`
	Scheme        string `json:"scheme"`
	Upstream      string `json:"upstream"`
	Name          string `json:"name"`
	Status        int    `json:"status"`
	Icon          string `json:"icon,omitempty"`
	TLSUntrusted  bool   `json:"tls_untrusted,omitempty"`
	RedirectOther bool   `json:"redirect_other,omitempty"`
}

type discoveryStatus struct {
	ID        string              `json:"id"`
	IP        string              `json:"ip"`
	State     string              `json:"state"`
	Total     int                 `json:"total"`
	Completed int                 `json:"completed"`
	OpenTCP   int                 `json:"open_tcp"`
	Truncated bool                `json:"truncated"`
	Services  []discoveredService `json:"services"`
}
type discoveryIcon struct {
	data []byte
	mime string
}
type discoveryScan struct {
	mu     sync.Mutex
	status discoveryStatus
	icons  map[int]discoveryIcon
	cancel context.CancelFunc
}
type serviceDiscovery struct {
	mu      sync.Mutex
	current *discoveryScan
}

func discoveryIP(raw string) (netip.Addr, error) {
	ip, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil || ip.Zone() != "" || !(ip.Unmap().IsPrivate() || ip.Unmap().IsLoopback()) {
		return netip.Addr{}, errors.New("请输入单个内网或回环 IPv4 / IPv6 地址")
	}
	return ip.Unmap(), nil
}

func discoveryPorts(raw string) ([]int, error) {
	if strings.TrimSpace(raw) == "" {
		raw = commonWebPorts
	}
	if len(raw) > 2048 {
		return nil, errors.New("端口范围过长")
	}
	seen := make([]bool, 65536)
	for _, part := range strings.Split(raw, ",") {
		bounds := strings.Split(strings.TrimSpace(part), "-")
		if len(bounds) > 2 {
			return nil, errors.New("端口格式应为 80,443,8000-8100")
		}
		start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
		end := start
		if len(bounds) == 2 {
			var endErr error
			end, endErr = strconv.Atoi(strings.TrimSpace(bounds[1]))
			if endErr != nil {
				return nil, errors.New("端口格式应为 80,443,8000-8100")
			}
		}
		if err != nil || start < 1 || end > 65535 || end < start {
			return nil, errors.New("端口必须在 1–65535 之间，范围起点不能大于终点")
		}
		for port := start; port <= end; port++ {
			seen[port] = true
		}
	}
	ports := []int{}
	for port := 1; port <= 65535; port++ {
		if seen[port] {
			ports = append(ports, port)
		}
	}
	return ports, nil
}

func (d *serviceDiscovery) start(ip netip.Addr, ports []int) (*discoveryScan, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current != nil {
		state := d.current.snapshot().State
		if state == "running" || state == "stopping" {
			return nil, errors.New("已有扫描正在进行，请完成或停止后再试")
		}
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, errors.New("暂时无法创建扫描任务")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	scan := &discoveryScan{status: discoveryStatus{ID: hex.EncodeToString(id[:]), IP: ip.String(), State: "running", Total: len(ports), Services: []discoveredService{}}, icons: map[int]discoveryIcon{}, cancel: cancel}
	d.current = scan
	go scan.run(ctx, ip, ports)
	return scan, nil
}
func (d *serviceDiscovery) find(id string) *discoveryScan {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current != nil && d.current.status.ID == id {
		return d.current
	}
	return nil
}
func (s *discoveryScan) snapshot() discoveryStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := s.status
	result.Services = append([]discoveredService{}, s.status.Services...)
	sort.Slice(result.Services, func(i, j int) bool { return result.Services[i].Port < result.Services[j].Port })
	return result
}
func (s *discoveryScan) stop() {
	s.mu.Lock()
	if s.status.State == "running" {
		s.status.State = "stopping"
		s.cancel()
	}
	s.mu.Unlock()
}

func (s *discoveryScan) run(ctx context.Context, ip netip.Addr, ports []int) {
	defer s.cancel()
	// A discovery client has no proxy, cookies, credentials or DNS target.
	// TLS validation is reported separately so self-signed NAS pages can be identified.
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 500 * time.Millisecond}).DialContext,
		DisableKeepAlives: true, TLSHandshakeTimeout: time.Second, ResponseHeaderTimeout: 2 * time.Second,
		MaxResponseHeaderBytes: 32 << 10, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 4 || discoveryURL(via[0].URL, r.URL.String()) == nil {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	queue := make(chan int)
	var workers sync.WaitGroup
	for i := 0; i < min(64, len(ports)); i++ {
		workers.Go(func() {
			for port := range queue {
				if ctx.Err() != nil {
					return
				}
				conn, err := (&net.Dialer{Timeout: 500 * time.Millisecond}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(port)))
				if err == nil {
					conn.Close()
					s.mu.Lock()
					s.status.OpenTCP++
					full := len(s.status.Services) >= maxDiscoveredServices
					s.mu.Unlock()
					if !full {
						service, icon := probeService(ctx, client, ip, port)
						if service != nil {
							s.mu.Lock()
							if len(s.status.Services) < maxDiscoveredServices {
								if icon != nil {
									s.icons[port] = *icon
									service.Icon = "/api/service-discovery/" + s.status.ID + "/icon/" + strconv.Itoa(port)
								}
								s.status.Services = append(s.status.Services, *service)
							} else {
								s.status.Truncated = true
							}
							s.mu.Unlock()
						}
					} else {
						s.mu.Lock()
						s.status.Truncated = true
						s.mu.Unlock()
					}
				}
				s.mu.Lock()
				s.status.Completed++
				s.mu.Unlock()
			}
		})
	}
send:
	for _, port := range ports {
		select {
		case queue <- port:
		case <-ctx.Done():
			break send
		}
	}
	close(queue)
	workers.Wait()
	s.mu.Lock()
	s.status.State = "completed"
	if ctx.Err() == context.Canceled {
		s.status.State = "cancelled"
	} else if ctx.Err() != nil {
		s.status.State = "timed_out"
	}
	s.mu.Unlock()
}

// Only the selected IP and port can be fetched, including redirects and favicons.
func discoveryURL(base *url.URL, raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return nil
	}
	u = base.ResolveReference(u)
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil || ip.Unmap().String() != base.Hostname() || u.Port() != base.Port() || (u.Scheme != "http" && u.Scheme != "https") {
		return nil
	}
	u.Fragment = ""
	return u
}

func discoveryName(raw string) string {
	raw = strings.Join(strings.FieldsFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	var name strings.Builder
	for _, r := range raw {
		if name.Len()+len(string(r)) > 96 {
			break
		}
		name.WriteRune(r)
	}
	return name.String()
}

func discoveryHTML(reader io.Reader) (string, string) {
	z := html.NewTokenizer(reader)
	z.SetMaxBuf(32 << 10)
	var title, appName, favicon string
	inTitle := false
	for {
		t := z.Next()
		if t == html.ErrorToken {
			break
		}
		token := z.Token()
		if t == html.TextToken && inTitle {
			title += token.Data
		}
		if t == html.EndTagToken && token.Data == "title" {
			inTitle = false
		}
		if t != html.StartTagToken && t != html.SelfClosingTagToken {
			continue
		}
		if token.Data == "title" {
			inTitle = true
		}
		attrs := map[string]string{}
		for _, attr := range token.Attr {
			attrs[attr.Key] = attr.Val
		}
		if token.Data == "meta" && (attrs["name"] == "application-name" || attrs["property"] == "og:site_name") {
			appName = attrs["content"]
		}
		if token.Data == "link" && favicon == "" && (strings.Contains(" "+strings.ToLower(attrs["rel"])+" ", " icon ") || attrs["rel"] == "apple-touch-icon") {
			favicon = attrs["href"]
		}
	}
	if title = discoveryName(title); title == "" {
		title = discoveryName(appName)
	}
	return title, favicon
}

func probeService(ctx context.Context, client *http.Client, ip netip.Addr, port int) (*discoveredService, *discoveryIcon) {
	for _, scheme := range []string{"https", "http"} {
		origin := &url.URL{Scheme: scheme, Host: net.JoinHostPort(ip.String(), strconv.Itoa(port)), Path: "/"}
		req, _ := http.NewRequestWithContext(ctx, "GET", origin.String(), nil)
		req.Header.Set("User-Agent", "Gatehouse-ServiceDiscovery/1.0")
		req.Header.Set("Accept", "text/html,*/*;q=0.5")
		response, err := client.Do(req)
		if err != nil {
			continue
		}
		limited := io.LimitReader(response.Body, 128<<10)
		reader, err := charset.NewReader(limited, response.Header.Get("Content-Type"))
		var name, iconPath string
		if err == nil {
			name, iconPath = discoveryHTML(io.LimitReader(reader, 128<<10))
		}
		response.Body.Close()
		final := response.Request.URL
		result := &discoveredService{Port: port, Scheme: final.Scheme, Upstream: final.Scheme + "://" + final.Host, Name: name, Status: response.StatusCode}
		if name == "" {
			result.Name = "Web 服务 · " + strconv.Itoa(port)
		}
		if final.Scheme == "https" && response.TLS != nil {
			intermediates := x509.NewCertPool()
			for _, cert := range response.TLS.PeerCertificates[1:] {
				intermediates.AddCert(cert)
			}
			_, err := response.TLS.PeerCertificates[0].Verify(x509.VerifyOptions{DNSName: ip.String(), Intermediates: intermediates})
			result.TLSUntrusted = err != nil
		}
		result.RedirectOther = response.StatusCode >= 300 && response.StatusCode < 400 && response.Header.Get("Location") != "" && discoveryURL(final, response.Header.Get("Location")) == nil
		for _, path := range []string{iconPath, "/favicon.ico"} {
			if path == "" {
				continue
			}
			if icon := fetchDiscoveryIcon(ctx, client, final, path); icon != nil {
				return result, icon
			}
		}
		return result, nil
	}
	return nil, nil
}

func fetchDiscoveryIcon(ctx context.Context, client *http.Client, base *url.URL, path string) *discoveryIcon {
	u := discoveryURL(base, path)
	if u == nil {
		return nil
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	req.Header.Set("User-Agent", "Gatehouse-ServiceDiscovery/1.0")
	response, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return nil
	}
	mime := http.DetectContentType(data)
	if mime == "image/vnd.microsoft.icon" || mime == "image/x-icon" {
		if !validDiscoveryICO(data) {
			return nil
		}
	} else {
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 512 || config.Height > 512 || (mime != "image/png" && mime != "image/jpeg" && mime != "image/gif" && mime != "image/webp") {
			return nil
		}
	}
	return &discoveryIcon{data: data, mime: mime}
}

func validDiscoveryICO(data []byte) bool {
	if len(data) < 22 || !bytes.Equal(data[:4], []byte{0, 0, 1, 0}) {
		return false
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count == 0 || 6+16*count > len(data) {
		return false
	}
	for i := 0; i < count; i++ {
		entry := data[6+16*i : 22+16*i]
		size, offset := uint64(binary.LittleEndian.Uint32(entry[8:12])), uint64(binary.LittleEndian.Uint32(entry[12:16]))
		if size == 0 || offset < uint64(6+16*count) || offset+size > uint64(len(data)) {
			return false
		}
	}
	return true
}

func (a *Admin) serviceDiscoveryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/service-discovery", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IP    string `json:"ip"`
			Ports string `json:"ports"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		ip, err := discoveryIP(body.IP)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		ports, err := discoveryPorts(body.Ports)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		scan, err := a.discovery.start(ip, ports)
		if err != nil {
			apiError(w, 409, err.Error())
			return
		}
		jsonResponse(w, 202, scan.snapshot())
	}))
	mux.HandleFunc("GET /api/service-discovery/{id}", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		scan := a.discovery.find(r.PathValue("id"))
		if scan == nil {
			apiError(w, 404, "扫描结果已失效，请重新扫描")
			return
		}
		jsonResponse(w, 200, scan.snapshot())
	}))
	mux.HandleFunc("POST /api/service-discovery/{id}/cancel", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		scan := a.discovery.find(r.PathValue("id"))
		if scan == nil {
			apiError(w, 404, "扫描结果已失效")
			return
		}
		scan.stop()
		jsonResponse(w, 200, scan.snapshot())
	}))
	mux.HandleFunc("GET /api/service-discovery/{id}/icon/{port}", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		scan := a.discovery.find(r.PathValue("id"))
		port, _ := strconv.Atoi(r.PathValue("port"))
		var icon discoveryIcon
		if scan != nil {
			scan.mu.Lock()
			icon = scan.icons[port]
			scan.mu.Unlock()
		}
		if icon.data == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", icon.mime)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Write(icon.data)
	}))
}
