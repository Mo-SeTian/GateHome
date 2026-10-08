package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type onlineTestTransport func(*http.Request) (*http.Response, error)

func (f onlineTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func onlineTestMetadata(t *testing.T, data []byte) map[string]any {
	t.Helper()
	sum := sha256.Sum256(data)
	return map[string]any{"tag_name": "v99.0.0", "published_at": "2026-10-08T00:00:00Z", "assets": []map[string]any{{
		"name": "gatehouse-99.0.0-update.zip", "state": "uploaded", "size": len(data), "digest": "sha256:" + hex.EncodeToString(sum[:]),
		"browser_download_url": onlineReleaseBase + "download/v99.0.0/gatehouse-99.0.0-update.zip",
	}}}
}

func onlineTestClient(data []byte) *http.Client {
	return &http.Client{Transport: onlineTestTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header), Request: r}, nil
	})}
}

func TestOnlineUpdateThroughConfiguredProxy(t *testing.T) {
	data := testRelease(t, "99.0.0", releaseArch())
	metadata, _ := json.Marshal(onlineTestMetadata(t, data))
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/Mo-SeTian/GateHome/releases/latest":
			if r.Header.Get("Accept") != "application/vnd.github+json" {
				t.Error("missing GitHub API headers")
			}
			w.Write(metadata)
		case strings.HasSuffix(r.URL.Path, "-update.zip"):
			http.Redirect(w, r, "https://release-assets.githubusercontent.com/test-package", http.StatusFound)
		case r.URL.Path == "/test-package":
			w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer target.Close()
	targetURL, _ := url.Parse(target.URL)
	var mu sync.Mutex
	destinations := map[string]bool{}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("test:TEST_ONLY_PROXY_PASSWORD")) {
			t.Error("update did not use configured proxy credentials")
			http.Error(w, "proxy rejected", 407)
			return
		}
		mu.Lock()
		destinations[r.Host] = true
		mu.Unlock()
		upstream, err := net.Dial("tcp", targetURL.Host)
		if err != nil {
			t.Error("local fixture unavailable")
			return
		}
		defer upstream.Close()
		client, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error("local tunnel failed")
			return
		}
		defer client.Close()
		rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		rw.Flush()
		go func() { io.Copy(upstream, client); upstream.Close() }()
		io.Copy(client, upstream)
	}))
	defer proxy.Close()
	state := State{Config: DefaultConfig(), ProxyPassword: "TEST_ONLY_PROXY_PASSWORD"}
	state.Config.OutboundProxy = OutboundProxyConfig{Enabled: true, URL: proxy.URL, Username: "test"}
	client := onlineUpdateClient(state)
	defer closeClient(client)
	client.Transport.(*http.Transport).TLSClientConfig = target.Client().Transport.(*http.Transport).TLSClientConfig
	client.Transport.(*http.Transport).TLSClientConfig.ServerName = targetURL.Hostname()
	release, err := latestOnlineRelease(context.Background(), client)
	if err != nil || !release.UpdateAvailable || release.Version != "99.0.0" {
		t.Fatal("version check through proxy failed")
	}
	downloaded, err := downloadOnlineRelease(context.Background(), client, release)
	if err != nil || !bytes.Equal(data, downloaded) {
		t.Fatal("verified download through proxy and CDN redirect failed")
	}
	m, err := NewMaintenance(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.inspectUpdateVersion(downloaded, release.Version)
	if err != nil || result["version"] != "99.0.0" || m.stage == nil || m.Busy() {
		t.Fatal("download was not safely staged for the existing update protocol")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, host := range []string{"api.github.com:443", "github.com:443", "release-assets.githubusercontent.com:443"} {
		if !destinations[host] {
			t.Fatal("a version, download or redirect request bypassed the configured proxy")
		}
	}
	state.Config.OutboundProxy.Enabled = false
	direct := onlineUpdateClient(state)
	defer closeClient(direct)
	if direct.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("disabled proxy still used for updates")
	}
}

func TestOnlineUpdateRejectsUntrustedMetadataAndDownloads(t *testing.T) {
	data := testRelease(t, "99.0.0", releaseArch())
	for _, name := range []string{"draft", "prerelease", "tag", "url", "digest", "size", "missing", "duplicate"} {
		t.Run(name, func(t *testing.T) {
			meta := onlineTestMetadata(t, data)
			assets := meta["assets"].([]map[string]any)
			switch name {
			case "draft", "prerelease":
				meta[name] = true
			case "tag":
				meta["tag_name"] = "v../../unsafe"
			case "url":
				assets[0]["browser_download_url"] = "https://example.com/untrusted.zip"
			case "digest":
				assets[0]["digest"] = ""
			case "size":
				assets[0]["size"] = maxUpdateBytes + 1
			case "missing":
				meta["assets"] = nil
			case "duplicate":
				meta["assets"] = append(assets, assets[0])
			}
			body, _ := json.Marshal(meta)
			if _, err := latestOnlineRelease(context.Background(), onlineTestClient(body)); err == nil {
				t.Fatal("unsafe release metadata accepted")
			}
		})
	}
	meta, _ := json.Marshal(onlineTestMetadata(t, data))
	release, err := latestOnlineRelease(context.Background(), onlineTestClient(meta))
	if err != nil {
		t.Fatal("valid fixture rejected")
	}
	for _, corrupt := range [][]byte{append(bytes.Clone(data), 0), bytes.Repeat([]byte{'x'}, len(data)), data[:len(data)-1]} {
		if _, err := downloadOnlineRelease(context.Background(), onlineTestClient(corrupt), release); err == nil {
			t.Fatal("corrupt or truncated package accepted")
		}
	}
	for _, address := range []string{"http://github.com/package", "https://github.com.evil.example/package", "https://127.0.0.1/package", "https://github.com:444/package", "https://test:TEST_ONLY_PASSWORD@github.com/package"} {
		client := onlineUpdateClient(State{})
		client.Transport = onlineTestTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{address}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		})
		if _, err := fetchOnlineUpdate(context.Background(), client, onlineReleaseAPI, 1024); err == nil {
			t.Fatal("untrusted redirect accepted")
		}
	}
}

func TestOnlineUpdateFailureDoesNotReplaceStageOrExposeSecrets(t *testing.T) {
	m, err := NewMaintenance(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.inspectUpdate(testRelease(t, "98.0.0", releaseArch())); err != nil {
		t.Fatal(err)
	}
	originalID := m.stage.ID
	original, _ := os.ReadFile(filepath.Join(m.dir, "update-staged"))
	for _, data := range [][]byte{testRelease(t, "97.0.0", releaseArch()), []byte("corrupt zip")} {
		if _, err := m.inspectUpdateVersion(data, "99.0.0"); err == nil {
			t.Fatal("untrusted staged package accepted")
		}
	}
	otherArch := "amd64"
	if releaseArch() == "amd64" {
		otherArch = "arm64"
	}
	if _, err := m.inspectUpdateVersion(testRelease(t, "99.0.0", otherArch), "99.0.0"); err == nil {
		t.Fatal("wrong architecture staged")
	}
	after, _ := os.ReadFile(filepath.Join(m.dir, "update-staged"))
	if m.stage.ID != originalID || !bytes.Equal(after, original) || m.Busy() {
		t.Fatal("failed validation replaced current stage or scheduled restart")
	}
	if _, err := os.Stat(filepath.Join(m.dir, "operation.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed validation wrote an update operation")
	}
	client := &http.Client{Transport: onlineTestTransport(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("TEST_ONLY_PROXY_PASSWORD TEST_ONLY_SIGNED_DOWNLOAD_TOKEN")
	})}
	_, err = fetchOnlineUpdate(context.Background(), client, onlineReleaseAPI, 1024)
	if err == nil || strings.Contains(err.Error(), "TEST_ONLY_") {
		t.Fatal("network failure exposed credentials or download tokens")
	}
}

func TestOnlineUpdateAPIAuthorizationAndProxyFailure(t *testing.T) {
	a, _ := testAdmin(t)
	var proxyCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		http.Error(w, "TEST_ONLY_PROXY_PASSWORD", 407)
	}))
	defer proxy.Close()
	state := a.store.Snapshot()
	state.Config.OutboundProxy = OutboundProxyConfig{Enabled: true, URL: proxy.URL, Username: "test"}
	password := "TEST_ONLY_PROXY_PASSWORD"
	if err := a.store.UpdateWithProxy(state.Config, nil, &password, state.Revision); err != nil {
		t.Fatal(err)
	}
	h := a.Handler()
	cookie := loginForTest(t, h)
	if w := adminRequest(h, "GET", "/api/maintenance/online-update-status", nil, nil, ""); w.Code != 401 {
		t.Fatal("update progress accessible without login")
	}
	a.onlineUpdate.status = onlineUpdateStatus{Phase: "downloading", Version: "99.0.0", Downloaded: 25, Total: 100}
	if w := adminRequest(h, "GET", "/api/maintenance/online-update-status", nil, cookie, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"downloaded":25`) || strings.Contains(w.Body.String(), password) {
		t.Fatal("safe progress snapshot unavailable")
	}
	a.updateMu.Lock()
	blocked := adminRequest(h, "POST", "/api/maintenance/download-online-update", map[string]string{"version": "99.0.0"}, cookie, "")
	a.updateMu.Unlock()
	if blocked.Code != 409 || proxyCalls.Load() != 0 {
		t.Fatal("concurrent update was not rejected before any network activity")
	}
	for _, path := range []string{"check-online-update", "download-online-update"} {
		path = "/api/maintenance/" + path
		body := map[string]string{}
		if strings.Contains(path, "download") {
			body["version"] = "99.0.0"
		}
		if w := adminRequest(h, "POST", path, body, nil, ""); w.Code != 401 {
			t.Fatal("online update accessible without login")
		}
		if w := adminRequest(h, "POST", path, body, cookie, "https://evil.example"); w.Code != 403 {
			t.Fatal("cross-site update accepted")
		}
		r := httptest.NewRequest("POST", "http://localhost:16666"+path, strings.NewReader(`{}`))
		r.AddCookie(cookie)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("update without CSRF header accepted")
		}
	}
	w := adminRequest(h, "POST", "/api/maintenance/check-online-update", map[string]string{}, cookie, "")
	if w.Code != 502 || proxyCalls.Load() != 1 || strings.Contains(w.Body.String(), password) {
		t.Fatal("proxy failure was not handled safely or bypassed configured proxy")
	}
	if w := adminRequest(h, "POST", "/api/maintenance/download-online-update", map[string]string{"version": "99.0.0"}, cookie, ""); w.Code != 409 || proxyCalls.Load() != 1 {
		t.Fatal("unsupported deployment downloaded an update")
	}
	for _, input := range []map[string]string{{"version": "0.0.0"}, {"version": "../unsafe"}, {"version": "99.0.0", "url": "http://127.0.0.1"}} {
		if w := adminRequest(h, "POST", "/api/maintenance/download-online-update", input, cookie, ""); w.Code != 400 {
			t.Fatal("invalid update request accepted")
		}
	}
}

func TestSupervisorPreparationFailureKeepsOriginalService(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("Unix process test")
	}
	dir, app := t.TempDir(), t.TempDir()
	m, err := NewMaintenance(dir, app)
	if err != nil {
		t.Fatal(err)
	}
	// Missing state.json makes the rollback snapshot fail before the program changes.
	script := []byte("#!/bin/sh\nprintf '{\"pid\":%s,\"version\":\"" + Version + "\"}' \"$$\" > \"$2/maintenance/ready.json\"\ntrap 'exit 0' TERM INT\nwhile :; do sleep 0.1; done\n")
	if os.WriteFile(filepath.Join(app, "gatehouse"), script, 0750) != nil || writeJSON(filepath.Join(m.dir, "operation.json"), maintenanceOperation{Kind: "update", Version: "99.0.0"}) != nil {
		t.Fatal("fixture setup failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Supervise(ctx, dir, app, "127.0.0.1:16666") }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error("original service was not preserved")
			}
		case <-time.After(5 * time.Second):
			t.Error("test supervisor did not stop")
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ready, _ := os.ReadFile(filepath.Join(m.dir, "ready.json"))
		if len(ready) > 0 {
			if _, err := os.Stat(filepath.Join(m.dir, "operation.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed update would retry indefinitely")
			}
			program, _ := os.ReadFile(filepath.Join(app, "gatehouse"))
			if !bytes.Equal(program, script) {
				t.Fatal("failed preparation changed the original program")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("failed update stopped the original service")
}
