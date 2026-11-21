package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"sun-panel/lib/imageasset"
)

func TestAdminTrustedProxyClientIPAndRateLimitIsolation(t *testing.T) {
	a, h := testAdmin(t)
	c := a.store.Snapshot().Config
	c.TrustedProxies = []string{"127.0.0.1"}
	c.AdminAccess = AdminAccessConfig{Enabled: true, Origins: []string{"https://console.example.test"}}
	if err := a.store.Update(c, nil, a.store.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	if err := a.proxy.Configure(c); err != nil {
		t.Fatal(err)
	}
	doLogin := func(clientIP, password string) int {
		body, _ := json.Marshal(map[string]string{"username": "admin", "password": password})
		r := httptest.NewRequest("POST", "http://console.example.test/api/login", bytes.NewReader(body))
		r.RemoteAddr = "127.0.0.1:50000"
		r.Header.Set("X-Forwarded-For", clientIP)
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("Origin", "https://console.example.test")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Gatehouse-Request", "1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	for i := 0; i < 8; i++ {
		if code := doLogin("203.0.113.10", "WRONG_PASSWORD_123"); code != 401 {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, code)
		}
	}
	if code := doLogin("203.0.113.10", "TEST_ONLY_ADMIN_PASSWORD"); code != 429 {
		t.Fatalf("expected attacker IP to be rate limited (429), got %d", code)
	}
	if code := doLogin("203.0.113.20", "TEST_ONLY_ADMIN_PASSWORD"); code != 200 {
		t.Fatalf("legitimate admin from different IP behind trusted proxy was locked out: got %d", code)
	}
}

func TestAdminOriginRejectsPlainHTTPReverseProxyWhenDisabled(t *testing.T) {
	_, h := testAdmin(t)
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "TEST_ONLY_ADMIN_PASSWORD"})
	r := httptest.NewRequest("POST", "http://console.example.test:18080/api/login", bytes.NewReader(body))
	r.Header.Set("Origin", "http://console.example.test:18080")
	r.Header.Set("X-Forwarded-For", "198.51.100.8")
	r.Header.Set("X-Forwarded-Host", "console.example.test:18080")
	r.Header.Set("X-Forwarded-Proto", "http")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Gatehouse-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("plain-HTTP reverse proxy bypassed disabled AdminAccess: got %d", w.Code)
	}
}

func TestRouteAuthAllowedBehindTrustedTLSProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("OK"))
	}))
	defer upstream.Close()
	a, _ := testAdmin(t)
	c := a.store.Snapshot().Config
	c.TrustedProxies = []string{"127.0.0.1"}
	c.Routes = []Route{{
		ID:       "r-auth",
		GroupID:  "default",
		Name:     "Protected",
		Host:     "app.example.test",
		Upstream: upstream.URL,
		Enabled:  true,
		TLS:      false,
		Auth:     RouteAuthConfig{Enabled: true, Username: "alice"},
	}}
	hash, err := bcrypt.GenerateFromPassword([]byte("TEST_ONLY_ROUTE_PASS"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.proxy.configure(c, map[string]string{"default/app.example.test": string(hash)}); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"username": {"alice"}, "password": {"TEST_ONLY_ROUTE_PASS"}, "return": {"/"}}.Encode()

	// Trusted proxy with X-Forwarded-Proto: https succeeds and issues Secure cookie
	req := httptest.NewRequest("POST", "http://app.example.test/.gatehouse-auth", strings.NewReader(form))
	req.RemoteAddr = "127.0.0.1:41234"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://app.example.test")
	req.Header.Set("X-Forwarded-For", "198.51.100.5")
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	a.proxy.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 behind trusted TLS proxy, got %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("expected Secure auth cookie behind trusted TLS proxy, got %+v", cookies)
	}

	// Untrusted peer spoofing X-Forwarded-Proto: https is still rejected
	spoof := httptest.NewRequest("POST", "http://app.example.test/.gatehouse-auth", strings.NewReader(form))
	spoof.RemoteAddr = "198.51.100.9:41234"
	spoof.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	spoof.Header.Set("Origin", "https://app.example.test")
	spoof.Header.Set("X-Forwarded-Proto", "https")
	w2 := httptest.NewRecorder()
	a.proxy.ServeHTTP(w2, spoof)
	if w2.Code != 403 {
		t.Fatalf("untrusted X-Forwarded-Proto was accepted: got %d", w2.Code)
	}
}

func TestSVGSecurityHardeningInIconLibraryAndImageAsset(t *testing.T) {
	malicious := []string{
		`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><rect width="10" height="10"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><body xmlns="http://www.w3.org/1999/xhtml"/></foreignObject></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><rect filter="url(#ok) url(https://evil.test/track.svg#f)" width="10" height="10"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><rect fill="\75rl(https://evil.test/x)" width="10" height="10"/></svg>`,
		`<!DOCTYPE svg [<!ENTITY xxe "bad">]><svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`,
	}
	for _, sample := range malicious {
		if _, _, err := iconDimensions(strings.NewReader(sample), "svg"); err == nil {
			t.Fatalf("malicious SVG accepted by iconDimensions: %s", sample)
		}
		if imageasset.Validate("icon.svg", []byte(sample)) {
			t.Fatalf("malicious SVG accepted by imageasset.Validate: %s", sample)
		}
	}
	clean := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><defs><linearGradient id="g"><stop offset="0%"/></linearGradient></defs><rect width="32" height="32" fill="url(#g)"/></svg>`
	w, h, err := iconDimensions(strings.NewReader(clean), "svg")
	if err != nil || w != 32 || h != 32 {
		t.Fatalf("valid SVG rejected: w=%d h=%d err=%v", w, h, err)
	}
}

func TestSSRFBlocksAWSIPv6IMDSAndOutboundProxyMetadata(t *testing.T) {
	if _, err := discoveryIP("fd00:ec2::254"); err == nil {
		t.Fatal("discoveryIP accepted AWS IPv6 IMDS fd00:ec2::254")
	}
	if allowedServiceIP(netip.MustParseAddr("fd00:ec2::254")) {
		t.Fatal("allowedServiceIP accepted AWS IPv6 IMDS")
	}
	for _, raw := range []string{"http://169.254.169.254:80", "http://100.100.100.200:80", "http://[fd00:ec2::254]:80"} {
		if err := validateOutboundProxy(OutboundProxyConfig{Enabled: true, URL: raw}); err == nil {
			t.Fatalf("validateOutboundProxy accepted metadata proxy %s", raw)
		}
	}
}

func TestIconPackReimportOverwritesIconsJSON(t *testing.T) {
	a, _ := testAdmin(t)
	stage1 := filepath.Join(a.store.paths.Data, "icons", ".stage-1")
	if err := os.MkdirAll(filepath.Join(stage1, "svg"), 0700); err != nil {
		t.Fatal(err)
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><rect width="24" height="24"/></svg>`
	if err := os.WriteFile(filepath.Join(stage1, "svg", "demo.svg"), []byte(svg), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage1, "icons.json"), []byte(`[{"name":"demo","cnName":"旧名称"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.publishIconPack(stage1, "TestPack"); err != nil {
		t.Fatal(err)
	}

	stage2 := filepath.Join(a.store.paths.Data, "icons", ".stage-2")
	if err := os.MkdirAll(filepath.Join(stage2, "svg"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage2, "svg", "demo.svg"), []byte(svg), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage2, "icons.json"), []byte(`[{"name":"demo","cnName":"新名称"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.publishIconPack(stage2, "TestPack"); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(a.store.paths.Data, "icons", "imported", "TestPack", "icons.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "新名称") {
		t.Fatalf("icons.json was not updated on re-import: %s", string(raw))
	}
}

func TestDeleteIPBlockNormalizesIP(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	c := a.store.Snapshot().Config
	c.Routes = []Route{{ID: "r1", GroupID: "default", Name: "R1", Host: "r1.example.test", Upstream: "http://127.0.0.1:8080", Enabled: true}}
	if err := a.store.Update(c, nil, a.store.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	if err := a.proxy.Configure(c); err != nil {
		t.Fatal(err)
	}
	if err := a.proxy.defense.add(c.Routes[0], "192.0.2.5", 10*time.Minute, "test"); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"rule": "default/r1.example.test", "ip": " ::ffff:192.0.2.5 "})
	r := httptest.NewRequest("DELETE", "http://localhost:16666/api/ip-blocks", bytes.NewReader(body))
	r.AddCookie(cookie)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Gatehouse-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("expected 200 when unblocking non-canonical IPv4-mapped IPv6, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestBetaCheckFallsBackToHigherStableOnBetaError(t *testing.T) {
	oldVersion := Version
	Version = "1.0.1-beta.12"
	defer func() { Version = oldVersion }()

	a, _ := testAdmin(t)
	state := a.store.Snapshot()
	key := onlineReleaseCacheKey(state)
	expires := time.Now().Add(time.Minute)
	a.releaseCache.beta = onlineReleaseCacheEntry{
		key:     key,
		err:     errors.New("该版本尚未提供完整的更新 ZIP，请稍后重试"),
		expires: expires,
	}
	a.releaseCache.stable = onlineReleaseCacheEntry{
		key: key,
		release: onlineRelease{
			Channel:         stableChannel,
			Version:         "1.0.2",
			PublishedAt:     time.Now().UTC(),
			UpdateAvailable: true,
		},
		expires: expires,
	}
	got, err := a.checkOnlineRelease(context.Background(), http.DefaultClient, state, betaChannel)
	if err != nil {
		t.Fatalf("expected fallback promotion to stable, got err: %v", err)
	}
	if got.Channel != stableChannel || got.Version != "1.0.2" {
		t.Fatalf("unexpected promoted release: %+v", got)
	}
}

func TestFailedUpdateRollbackRemovesPhantomBackup(t *testing.T) {
	m, _, _ := updateHistoryFixture(t)
	snapshot, err := snapshotDiskPaths(m.paths)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(snapshot)
	if err := copyProgram(filepath.Join(m.appDir, "gatehouse"), filepath.Join(m.appDir, "gatehouse.previous")); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= updateBackupRetention; i++ {
		if err := m.saveUpdateBackup(Version, "98.0."+string(rune('0'+i)), data); err != nil {
			t.Fatal(err)
		}
	}
	beforeList, err := m.listUpdateBackups()
	if err != nil || len(beforeList) != updateBackupRetention {
		t.Fatalf("expected %d backups before update, got %d", updateBackupRetention, len(beforeList))
	}
	beforeIDs := map[string]bool{}
	for _, b := range beforeList {
		beforeIDs[b.ID] = true
	}

	if _, err := m.beginTransition(); err != nil {
		t.Fatal(err)
	}
	if err := m.rollbackTransition(); err != nil {
		t.Fatal(err)
	}
	afterList, err := m.listUpdateBackups()
	if err != nil || len(afterList) != updateBackupRetention {
		t.Fatalf("expected %d genuine backups preserved after rollback, got %d", updateBackupRetention, len(afterList))
	}
	for _, b := range afterList {
		if !beforeIDs[b.ID] {
			t.Fatalf("phantom backup %s was not removed on rollback", b.ID)
		}
	}
}

func TestWAFHighBodyLimitAllowsSmallPostWithoutExhaustingPool(t *testing.T) {
	p, _, _ := protectionProxy(t, Protection{WAF: WAFPolicy{Mode: "detect", BodyLimit: 32 << 20}})
	// Reserve 120 MiB out of 128 MiB global pool; a small POST on a 32 MiB BodyLimit route should still fit.
	if !p.wafMemory.TryAcquire(120 << 20) {
		t.Fatal("failed to pre-acquire memory")
	}
	defer p.wafMemory.Release(120 << 20)
	if w := protectionRequest(p, "POST", "/", "name=test"); w.Code != 200 {
		t.Fatalf("small POST on 32 MiB BodyLimit route exhausted global WAF pool: got %d", w.Code)
	}
}
