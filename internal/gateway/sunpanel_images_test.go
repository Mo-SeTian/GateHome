package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestSunPanelImageImport(t *testing.T) {
	a, _ := testAdmin(t)
	var downloads, uploads atomic.Int32
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		if r.Header.Get("token") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials crossed the image download boundary")
		}
		if r.URL.Path == "/html" {
			w.Write([]byte("<html>not an image</html>"))
			return
		}
		w.Write(testRoutePNG(t, 256, 128))
	}))
	defer imageServer.Close()
	failUpload := false
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("token") != "TEST_ONLY_PANEL_TOKEN" {
			jsonResponse(w, 200, map[string]int{"code": 1001})
			return
		}
		if r.URL.Path == "/sunpanel/api/user/getInfo" {
			jsonResponse(w, 200, map[string]int{"code": 0})
			return
		}
		if r.URL.Path != "/sunpanel/api/file/uploadImg" {
			t.Error("unexpected worker request")
			return
		}
		file, _, err := r.FormFile("imgfile")
		if err != nil {
			t.Error("upload did not use the existing image API")
			return
		}
		defer file.Close()
		config, format, err := image.DecodeConfig(file)
		if err != nil || format != "png" || config.Width != 128 || config.Height != 64 {
			t.Error("remote image was not normalized before uploading")
			return
		}
		uploads.Add(1)
		if failUpload {
			jsonResponse(w, 200, map[string]any{"code": 1300, "msg": "TEST_ONLY_WRITE_FAILURE"})
			return
		}
		jsonResponse(w, 200, map[string]any{"code": 0, "data": map[string]string{"imageUrl": "/sunpanel/uploads/test.png"}})
	}))
	defer worker.Close()
	a.SetSunPanel(&SunPanel{bridgeURL: worker.URL})
	request := func(handler http.Handler, raw, token string, cookie *http.Cookie, origin string) int {
		body, _ := json.Marshal(map[string]string{"url": raw})
		r := httptest.NewRequest("POST", "http://localhost/sunpanel/api/file/importImg", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Gatehouse-Request", "1")
		r.Header.Set("token", token)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			return w.Code
		}
		var result struct {
			Code int `json:"code"`
			Data struct {
				ImageURL string `json:"imageUrl"`
			} `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatal("invalid import response")
		}
		if result.Code == 0 && result.Data.ImageURL != "/sunpanel/uploads/test.png" {
			t.Fatal("import did not return the local Sun-Panel URL")
		}
		return result.Code
	}
	for _, token := range []string{"", "TEST_ONLY_EXPIRED_TOKEN"} {
		if request(a.SunPanelHandler(), imageServer.URL, token, nil, "") == 0 {
			t.Fatal("anonymous import accepted")
		}
	}
	if downloads.Load() != 0 {
		t.Fatal("image fetched before authentication")
	}
	cookie := loginForTest(t, a.Handler())
	if request(a.Handler(), imageServer.URL, "TEST_ONLY_PANEL_TOKEN", nil, "") != 401 {
		t.Fatal("embedded import bypassed GateHome login")
	}
	if request(a.Handler(), imageServer.URL, "TEST_ONLY_PANEL_TOKEN", cookie, "https://evil.example") != 403 {
		t.Fatal("embedded import accepted an unauthorized origin")
	}
	for _, handler := range []http.Handler{a.Handler(), a.SunPanelHandler()} {
		if request(handler, imageServer.URL, "TEST_ONLY_PANEL_TOKEN", cookie, "") != 0 {
			t.Fatal("image URL could not be imported through both entrances")
		}
	}
	if request(a.SunPanelHandler(), imageServer.URL+"/html", "TEST_ONLY_PANEL_TOKEN", nil, "") == 0 || uploads.Load() != 2 {
		t.Fatal("non-image data reached the upload API")
	}
	failUpload = true
	if request(a.SunPanelHandler(), imageServer.URL, "TEST_ONLY_PANEL_TOKEN", nil, "") == 0 {
		t.Fatal("failed local persistence reported success")
	}
}

func TestSunPanelImageDownloadProxy(t *testing.T) {
	raw := testRoutePNG(t, 1, 1)
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1)
		if !r.URL.IsAbs() || r.URL.Hostname() != "203.0.113.8" {
			t.Error("image proxy target incorrect")
		}
		w.Write(raw)
	}))
	defer proxy.Close()
	state := State{Config: Config{OutboundProxy: OutboundProxyConfig{Enabled: true, URL: proxy.URL}}}
	data, err := downloadRouteImageWithProxy(context.Background(), "http://203.0.113.8/icon.png", state)
	if err != nil || !bytes.Equal(data, raw) || proxied.Load() != 1 {
		t.Fatal("public image did not use configured proxy")
	}
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(w, bytes.NewReader(raw)) }))
	defer local.Close()
	data, err = downloadRouteImageWithProxy(context.Background(), local.URL, state)
	if err != nil || !bytes.Equal(data, raw) || proxied.Load() != 1 {
		t.Fatal("local image did not remain direct")
	}
	for _, url := range []string{"http://169.254.169.254/icon", "http://100.100.100.200/icon", "file:///etc/passwd"} {
		if _, err = downloadRouteImageWithProxy(context.Background(), url, state); err == nil {
			t.Fatal("blocked image target accepted through proxy")
		}
	}
}
