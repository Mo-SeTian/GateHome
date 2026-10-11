package siteFavicon

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoundedDownloadAndRelativeDiscovery(t *testing.T) {
	var icon bytes.Buffer
	png.Encode(&icon, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/nested/page", 302)
		case "/nested/page":
			w.Write([]byte(`<link rel="shortcut icon" href="icons/logo?test=1">`))
		case "/nested/icons/logo":
			if r.URL.Query().Get("test") != "1" {
				w.WriteHeader(404)
				return
			}
			w.Write(icon.Bytes())
		case "/oversize":
			if r.Method == "HEAD" {
				w.Header().Set("Content-Length", "1")
				return
			}
			w.(http.Flusher).Flush()
			w.Write(bytes.Repeat([]byte("x"), 4097))
		case "/bad":
			w.Write([]byte(`<svg><script>alert(1)</script></svg>`))
		case "/html-too-big":
			w.(http.Flusher).Flush()
			w.Write(bytes.Repeat([]byte("x"), (2<<20)+1))
		case "/metadata":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", 302)
		}
	}))
	defer server.Close()
	raw, err := GetOneFaviconURL(server.URL + "/start")
	if err != nil || raw != server.URL+"/nested/icons/logo?test=1" {
		t.Fatal("relative discovery lost final URL or query")
	}
	dir := t.TempDir()
	file, err := DownloadImage(raw, dir, 4096)
	if err != nil || filepath.Ext(file.Name()) != ".png" {
		t.Fatal("valid extensionless icon was not stored")
	}
	stored, _ := os.ReadFile(file.Name())
	if !bytes.Equal(stored, icon.Bytes()) {
		t.Fatal("image bytes changed")
	}
	os.Remove(file.Name())
	for _, path := range []string{"/oversize", "/bad", "/metadata"} {
		if _, err := DownloadImage(server.URL+path, dir, 4096); err == nil {
			t.Fatal("oversized/active/metadata image accepted")
		}
	}
	if _, err := GetOneFaviconURL(server.URL + "/html-too-big"); err == nil {
		t.Fatal("oversized discovery page accepted")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatal("failed download left partial files")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DownloadImageContext(ctx, raw, dir, 4096); err == nil {
		t.Fatal("canceled download succeeded")
	}
	client, transport := faviconClient()
	defer transport.CloseIdleConnections()
	if client.Timeout == 0 || transport.ResponseHeaderTimeout == 0 {
		t.Fatal("network timeout missing")
	}
}
func TestURLAndMetadataGuards(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "http://TEST_ONLY_USER:TEST_ONLY_PASSWORD@example.test/", "http://169.254.169.254/", "http://[::ffff:169.254.169.254]/", "http://100.100.100.200/", strings.Repeat("x", 4097)} {
		if _, err := safeURL(raw); err == nil {
			t.Fatal("unsafe URL accepted")
		}
	}
	for _, ip := range []string{"127.0.0.1", "192.168.2.2", "2001:db8::1"} {
		if !allowedAddress(netip.MustParseAddr(ip)) {
			t.Fatal("LAN/test destination rejected")
		}
	}
}
