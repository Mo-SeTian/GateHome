package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testRoutePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.NRGBA{60, 110, 250, 255})
	var b bytes.Buffer
	if png.Encode(&b, img) != nil {
		t.Fatal("fixture encoding failed")
	}
	return b.Bytes()
}
func TestRouteImagesNormalizationAndStorage(t *testing.T) {
	raw := testRoutePNG(t, 512, 256)
	raw = append(raw, []byte("TEST_ONLY_METADATA")...)
	normalized, err := normalizeRouteImage(raw)
	if err != nil || bytes.Contains(normalized, []byte("TEST_ONLY_METADATA")) {
		t.Fatal("image metadata was not removed")
	}
	dimensions, format, err := image.DecodeConfig(bytes.NewReader(normalized))
	if err != nil || format != "png" || dimensions.Width != 128 || dimensions.Height != 64 {
		t.Fatal("thumbnail size or format incorrect")
	}
	var jpegData, gifData bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	jpeg.Encode(&jpegData, img, nil)
	gif.Encode(&gifData, img, nil)
	for _, data := range [][]byte{jpegData.Bytes(), gifData.Bytes()} {
		if _, err := normalizeRouteImage(data); err != nil {
			t.Fatal("supported raster format rejected")
		}
	}
	for _, data := range [][]byte{nil, []byte(`<svg onload="alert(1)"></svg>`), []byte("<html>not an image</html>"), make([]byte, maxImageUpload+1), testRoutePNG(t, 4097, 1), raw[:25], {0, 0, 1, 0, 1, 0}} {
		if _, err := normalizeRouteImage(data); err == nil {
			t.Fatal("invalid or oversized image accepted")
		}
	}
	webp, _ := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if _, err := normalizeRouteImage(webp); err != nil {
		t.Fatal("WebP rejected")
	}
	icoPNG := testRoutePNG(t, 32, 32)
	ico := make([]byte, 22)
	ico[2] = 1
	ico[4] = 1
	ico[6] = 32
	ico[7] = 32
	ico[10] = 1
	ico[12] = 32
	binary.LittleEndian.PutUint32(ico[14:], uint32(len(icoPNG)))
	binary.LittleEndian.PutUint32(ico[18:], 22)
	ico = append(ico, icoPNG...)
	if _, err := normalizeRouteImage(ico); err != nil {
		t.Fatal("ICO rejected")
	}
	dir := t.TempDir()
	id, err := storeRouteImage(dir, raw)
	if err != nil || !routeImageID.MatchString(id) {
		t.Fatal("image storage failed")
	}
	id2, err := storeRouteImage(dir, raw)
	if err != nil || id2 != id {
		t.Fatal("content deduplication failed")
	}
	data, err := readRouteImage(dir, id)
	if err != nil || !bytes.Equal(data, normalized) {
		t.Fatal("stored image changed")
	}
	path := filepath.Join(dir, "route-images", id+".img")
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("image permissions incorrect")
	}
	os.Chtimes(path, time.Now().Add(-48*time.Hour), time.Now().Add(-48*time.Hour))
	pruneRouteImages(dir, Config{Routes: []Route{{Image: id}}})
	if _, err := os.Stat(path); err != nil {
		t.Fatal("committed image pruned")
	}
	pruneRouteImages(dir, Config{})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("abandoned image retained indefinitely")
	}
	id, _ = storeRouteImage(dir, raw)
	path = filepath.Join(dir, "route-images", id+".img")
	os.WriteFile(path, []byte("TEST_ONLY_CORRUPTED_IMAGE"), 0600)
	if _, err := readRouteImage(dir, id); err == nil {
		t.Fatal("corrupted image served")
	}
	os.Remove(path)
	os.Symlink(filepath.Join(dir, "state.json"), path)
	if _, err := readRouteImage(dir, id); err == nil {
		t.Fatal("symlink image served")
	}
	if _, err := readRouteImage(dir, "../state"); err == nil {
		t.Fatal("unsafe image ID accepted")
	}
}

func TestRouteImagesURLImport(t *testing.T) {
	data := testRoutePNG(t, 16, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("image import forwarded credentials")
		}
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/icon", 302)
		case "/unsafe":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", 302)
		case "/large":
			w.Write(make([]byte, maxImageUpload+1))
		default:
			w.Write(data)
		}
	}))
	defer server.Close()
	for _, path := range []string{"/icon", "/redirect"} {
		got, err := downloadRouteImage(context.Background(), server.URL+path)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal("private service image import failed")
		}
	}
	for _, raw := range []string{"file:///etc/passwd", "data:image/png;base64,AA", server.URL + "/large", server.URL + "/unsafe", "http://TEST_ONLY_USER:TEST_ONLY_PASSWORD@localhost/icon", "http://127.0.0.1:bad/icon", "http://169.254.169.254/icon", "http://100.100.100.200/icon"} {
		if _, err := downloadRouteImage(context.Background(), raw); err == nil {
			t.Fatal("unsafe URL accepted")
		}
	}
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	defer tlsServer.Close()
	if _, err := downloadRouteImage(context.Background(), tlsServer.URL+"?token=TEST_ONLY_URL_SECRET"); err == nil || strings.Contains(err.Error(), "TEST_ONLY_URL_SECRET") {
		t.Fatal("TLS validation or error redaction failed")
	}
}

func uploadRouteImage(h http.Handler, data []byte, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)
	part, _ := writer.CreateFormFile("file", "fixture.png")
	part.Write(data)
	writer.Close()
	r := httptest.NewRequest("POST", "http://localhost:16666/api/route-images/upload", &b)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("X-Gatehouse-Request", "1")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestRouteImageAPIAuthUploadScanAndConfig(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	raw := testRoutePNG(t, 16, 16)
	for _, path := range []string{"/api/route-images/upload", "/api/route-images/import"} {
		if w := adminRequest(h, "POST", path, map[string]string{}, nil, ""); w.Code != 401 {
			t.Fatal("anonymous image write accepted")
		}
	}
	if adminRequest(h, "GET", "/api/route-images/"+strings.Repeat("a", 64), nil, nil, "").Code != 401 {
		t.Fatal("anonymous image read accepted")
	}
	if uploadRouteImage(h, raw, cookie, "https://evil.example").Code != 403 {
		t.Fatal("cross site image upload accepted")
	}
	if uploadRouteImage(h, []byte("<svg></svg>"), cookie, "").Code != 400 {
		t.Fatal("active markup upload accepted")
	}
	if uploadRouteImage(h, make([]byte, maxImageUpload+1), cookie, "").Code != 400 {
		t.Fatal("oversized upload accepted")
	}
	w := uploadRouteImage(h, raw, cookie, "")
	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	id := result["id"]
	if w.Code != 201 || !routeImageID.MatchString(id) {
		t.Fatal("authenticated image upload failed")
	}
	w = adminRequest(h, "GET", "/api/route-images/"+id, nil, cookie, "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("image response headers incorrect")
	}
	s := a.store.Snapshot()
	s.Config.Routes = append(s.Config.Routes, Route{GroupID: s.Config.Groups[0].ID, Name: "NAS", Host: "nas.example.test", Upstream: "http://127.0.0.1:5000", Enabled: true, Image: id})
	if a.store.Update(s.Config, nil, s.Revision) != nil {
		t.Fatal("rule could not save uploaded image")
	}
	s = a.store.Snapshot()
	s.Config.Routes = append([]Route(nil), s.Config.Routes...)
	s.Config.Routes[0].Image = strings.Repeat("b", 64)
	if a.store.Update(s.Config, nil, s.Revision) == nil {
		t.Fatal("missing image reference accepted")
	}
	if a.store.Snapshot().Config.Routes[0].Image != id {
		t.Fatal("failed save changed rule image")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" {
			w.Write(raw)
		} else {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<title>NAS</title><link rel="icon" href="/favicon.ico">`))
		}
	}))
	defer server.Close()
	scan, err := a.discovery.start(netip.MustParseAddr("127.0.0.1"), []int{discoveryTestPort(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	view := waitDiscovery(t, scan)
	if len(view.Services) != 1 || view.Services[0].Icon == "" {
		t.Fatal("scan did not find image")
	}
	w = adminRequest(h, "POST", "/api/route-images/import", map[string]any{"scan_id": view.ID, "port": view.Services[0].Port}, cookie, "")
	if w.Code != 201 {
		t.Fatal("scan image adoption failed")
	}
	a.discovery.mu.Lock()
	a.discovery.current = nil
	a.discovery.mu.Unlock()
	if adminRequest(h, "GET", "/api/route-images/"+id, nil, cookie, "").Code != 200 {
		t.Fatal("committed scan image depended on temporary scan")
	}
	if adminRequest(h, "POST", "/api/route-images/import", map[string]any{"scan_id": "missing", "port": 80}, cookie, "").Code != 404 {
		t.Fatal("expired scan adopted")
	}
}
