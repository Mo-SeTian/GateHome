package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func homepageFixture(t *testing.T, a *Admin) HomepageConfig {
	t.Helper()
	h := defaultHomepage()
	normalizeHomepage(&h)
	h.ClockColor = "#8547d1"
	h.CustomCSS = ".gh-main { max-width: 1200px; }"
	engineIcon, err := storeRouteImage(filepath.Join(a.store.paths.Data, "page", homepageAdminSpace), testRoutePNG(t, 48, 32))
	if err != nil {
		t.Fatal("search icon fixture failed")
	}
	h.SearchEngines = append(h.SearchEngines, HomepageSearchEngine{ID: "docs", Name: "文档", URL: "https://search.example.test/?q={query}&lang=zh", Image: engineIcon})
	icon, err := storeRouteImage(filepath.Join(a.store.paths.Data, "page", homepageAdminSpace), testRoutePNG(t, 64, 64))
	if err != nil {
		t.Fatal("icon fixture failed")
	}
	background, err := storeHomepageBackground(filepath.Join(a.store.paths.Data, "page", homepageAdminSpace), testRoutePNG(t, 800, 400))
	if err != nil {
		t.Fatal("background fixture failed")
	}
	h.Background = background
	h.Groups = []HomepageGroup{{ID: "home", Name: "家庭", Pages: []HomepagePage{{ID: "daily", Name: "日常", Rows: 2, Columns: 3, MobileColumns: 2, Links: []HomepageLink{{ID: "nas", Name: "NAS", LAN: "http://192.168.2.10:5000/", WAN: "https://nas.example.test/", Image: icon, Favorite: true}}}}}}
	return h
}

func TestHomepageClockColorValidationAndPersistence(t *testing.T) {
	h := defaultHomepage()
	if h.ClockColor != "#000000" {
		t.Fatal("default clock color changed")
	}
	for _, color := range []string{"", "#000000", "#FFFFFF", "#85a7D1"} {
		h.ClockColor = color
		if validateHomepage(h, nil) != nil {
			t.Fatal("valid clock color rejected")
		}
	}
	for _, color := range []string{"red", "transparent", "#fff", "#12345678", "#gggggg", " #123456", "#123456\n", "#123456;display:none"} {
		h.ClockColor = color
		if validateHomepage(h, nil) == nil {
			t.Fatal("invalid clock color accepted")
		}
	}
	h.ClockColor = ""
	wanted := cloneHomepage(h)
	wanted.ClockColor = "#000000"
	normalizeHomepage(&wanted)
	normalizeHomepage(&h)
	if !reflect.DeepEqual(h, wanted) {
		t.Fatal("legacy clock default changed other content")
	}
	a, _ := testAdmin(t)
	setupHomepage(t, a, h)
	page := a.HomepageHandler()
	cookie := homepageLoginForTest(t, page, "admin", "TEST_ONLY_ADMIN_PASSWORD")
	document, _ := a.store.pages.snapshot(homepageAdminSpace)
	document.Homepage.ClockColor = "#8547d1"
	if adminRequest(page, "PUT", "/api/editor/config", document, cookie, "").Code != 200 {
		t.Fatal("clock color save failed")
	}
	saved, _ := a.store.pages.snapshot(homepageAdminSpace)
	if saved.Homepage.ClockColor != "#8547d1" {
		t.Fatal("saved clock color lost")
	}
	loaded, err := readHomepageDocument(a.store.paths.Data, homepageAdminSpace)
	if err != nil || !reflect.DeepEqual(saved, loaded) {
		t.Fatal("clock color did not survive reading from disk")
	}
	invalid := loaded
	invalid.Homepage.ClockColor = "#123456;display:none"
	if adminRequest(page, "PUT", "/api/editor/config", invalid, cookie, "").Code != http.StatusBadRequest {
		t.Fatal("API accepted invalid clock color")
	}
	after, _ := a.store.pages.snapshot(homepageAdminSpace)
	if !reflect.DeepEqual(saved, after) {
		t.Fatal("invalid color changed desktop or revision")
	}
}

func TestHomepageSearchEnginesValidationAndMigration(t *testing.T) {
	h := defaultHomepage()
	h.SearchEngines = append(h.SearchEngines, HomepageSearchEngine{ID: "custom", Name: "自定义", URL: "https://search.example.test/find/{query}?language=zh"})
	if validateHomepage(h, nil) != nil {
		t.Fatal("valid custom search rejected")
	}
	for _, address := range []string{"javascript:alert(1)", "ftp://example.test/?q={query}", "https://user:password@example.test/?q={query}", "https://example.test/search", "https://example.test/?a={query}&b={query}", "https://{query}.example.test/", "https://example.test/#{query}", "https://example.test/?q={query}\n"} {
		bad := h
		bad.SearchEngines = []HomepageSearchEngine{{ID: "test", Name: "测试", URL: address}}
		if validateHomepage(bad, nil) == nil {
			t.Fatal("unsafe or incomplete search template accepted")
		}
	}
	bad := h
	bad.SearchEngines = []HomepageSearchEngine{}
	if validateHomepage(bad, nil) == nil {
		t.Fatal("empty search engine list accepted")
	}
	bad.SearchEngines = append([]HomepageSearchEngine{}, h.SearchEngines...)
	bad.SearchEngines[1].ID = bad.SearchEngines[0].ID
	if validateHomepage(bad, nil) == nil {
		t.Fatal("duplicate search engine ID accepted")
	}
	bad.SearchEngines = []HomepageSearchEngine{{ID: "test", URL: "https://example.test/?q={query}"}}
	if validateHomepage(bad, nil) == nil {
		t.Fatal("unnamed search engine accepted")
	}
	bad.SearchEngines = []HomepageSearchEngine{{ID: "test", Name: "测试", URL: "https://example.test/?q={query}", Image: "../../state.json"}}
	if validateHomepage(bad, nil) == nil {
		t.Fatal("invalid search icon reference accepted")
	}
	h.SearchEngines = nil
	h.Title = "TEST_ONLY_PRESERVED_SPACE"
	h.CustomCSS = ".gh-main { max-width: 1200px; }"
	wanted := h
	wanted.SearchEngines = defaultHomepage().SearchEngines
	normalizeHomepage(&wanted)
	normalizeHomepage(&h)
	if !reflect.DeepEqual(h, wanted) {
		t.Fatal("legacy search migration changed desktop content")
	}

}

func TestHomepageValidationMigrationAndPortConflicts(t *testing.T) {
	a, _ := testAdmin(t)
	h := homepageFixture(t, a)
	if validateHomepageContent(h) != nil {
		t.Fatal("valid desktop rejected")
	}
	invalid := []func(*HomepageConfig){
		func(h *HomepageConfig) { h.Port = 16681 }, func(h *HomepageConfig) { h.Enabled = true },
		func(h *HomepageConfig) { h.Groups[0].Pages[0].Rows = 0 }, func(h *HomepageConfig) { h.Groups[0].Pages[0].Columns = 9 },
		func(h *HomepageConfig) { h.Groups[0].Pages[0].MobileColumns = 4 }, func(h *HomepageConfig) { h.Groups[0].Pages[0].Links[0].LAN = "javascript:alert(1)" },
		func(h *HomepageConfig) { h.Groups[0].Pages[0].Links[0].WAN = "https://user:password@example.test/" },
		func(h *HomepageConfig) { h.Groups[0].Pages[0].Links[0].ID = "daily" }, func(h *HomepageConfig) { h.Background = "../../state.json" },
		func(h *HomepageConfig) { h.CustomCSS = strings.Repeat("a", 32769) }, func(h *HomepageConfig) { h.Groups[0].Pages = nil },
	}
	for i, change := range invalid {
		next := cloneHomepage(h)
		change(&next)
		if validateHomepageContent(next) == nil {
			t.Fatalf("invalid desktop case %d accepted", i)
		}
	}
	missing := cloneHomepage(h)
	missing.SearchEngines[2].Image = strings.Repeat("f", 64)
	if _, err := a.store.pages.update(homepageAdminSpace, missing, 1); err == nil {
		t.Fatal("missing search icon accepted")
	}
	c := a.store.Snapshot().Config
	c.Homepage.Enabled = true
	c.Homepage.Port = c.Groups[0].HTTPPort
	if Validate(c) == nil {
		t.Fatal("business listener port collision accepted")
	}
	c.Homepage.Port = a.adminPort
	if validateAdminPort(c, a.adminPort) == nil {
		t.Fatal("admin port collision accepted")
	}
	c.Homepage.Enabled = false
	if validateAdminPort(c, a.adminPort) != nil {
		t.Fatal("disabled homepage reserved admin port")
	}
	c.Homepage = HomepageConfig{}
	if !migrateConfig(&c) || c.Homepage.Port != 16680 || Validate(c) != nil {
		t.Fatal("legacy listener migration failed")
	}
	c.Homepage.Enabled = true
	c.Homepage.Port = 16681
	if listenerSignature(c) == listenerSignature(a.ports) {
		t.Fatal("listener change omitted")
	}
}

func setupHomepage(t *testing.T, a *Admin, h HomepageConfig) {
	t.Helper()
	normalizeHomepage(&h)
	doc, _ := a.store.pages.snapshot(homepageAdminSpace)
	if _, err := a.store.pages.update(homepageAdminSpace, h, doc.Revision); err != nil {
		t.Fatal("desktop setup failed")
	}
	c := a.store.Snapshot().Config
	c.Homepage.Enabled = true
	if a.store.Update(c, nil, a.store.Snapshot().Revision) != nil {
		t.Fatal("listener setup failed")
	}
}

func homepageLoginForTest(t *testing.T, h http.Handler, username, password string) *http.Cookie {
	t.Helper()
	w := adminRequest(h, "POST", "/login", map[string]string{"username": username, "password": password}, nil, "")
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatal("homepage login failed")
	}
	return w.Result().Cookies()[0]
}

func TestHomepageThemeAssetAndProbePolicy(t *testing.T) {
	a, admin := testAdmin(t)
	setupHomepage(t, a, defaultHomepage())
	viewer := a.HomepageHandler()
	w := adminRequest(viewer, "GET", "/homepage-sunset.png", nil, nil, "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || !bytes.HasPrefix(w.Body.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("bundled homepage background missing or incorrectly served")
	}
	w = adminRequest(viewer, "GET", "/", nil, nil, "")
	policy := w.Header().Get("Content-Security-Policy")
	for _, directive := range []string{"script-src 'self'", "img-src 'self' data:", "connect-src 'self' http: https:", "frame-ancestors 'none'", "base-uri 'none'", "form-action 'self'"} {
		if !strings.Contains(policy, directive) {
			t.Fatal("homepage browser probe changed unrelated document protections")
		}
	}
	if strings.Contains(adminRequest(admin, "GET", "/", nil, nil, "").Header().Get("Content-Security-Policy"), "connect-src 'self' http: https:") {
		t.Fatal("homepage probe permissions widened the administrator document")
	}
}

func TestHomepageIsolationPrivateLoginAndCSS(t *testing.T) {
	a, admin := testAdmin(t)
	h := homepageFixture(t, a)
	cookie := loginForTest(t, admin)
	setupHomepage(t, a, h)
	update := map[string]any{"enabled": true, "port": 16680, "revision": a.store.Snapshot().Revision}
	if adminRequest(admin, "PUT", "/api/homepage", update, nil, "").Code != 401 {
		t.Fatal("anonymous listener mutation")
	}
	if adminRequest(admin, "PUT", "/api/homepage", update, cookie, "https://attacker.example.test").Code != 403 {
		t.Fatal("listener CSRF")
	}
	if adminRequest(admin, "PUT", "/api/homepage", update, cookie, "").Code != 200 {
		t.Fatal("listener save failed")
	}
	if adminRequest(admin, "PUT", "/api/homepage", update, cookie, "").Code == 200 {
		t.Fatal("stale listener revision accepted")
	}
	viewer := a.HomepageHandler()
	for _, path := range []string{"/api/config", "/api/account", "/api/status", "/api/maintenance/backup", "/state.json", "/homepage-admin.js"} {
		if w := adminRequest(viewer, "GET", path, nil, cookie, ""); w.Code != 404 {
			t.Fatalf("homepage exposed administrative path %s", path)
		}
	}
	if adminRequest(viewer, "PUT", "/api/homepage", update, cookie, "").Code == 200 {
		t.Fatal("homepage listener accepted configuration mutation")
	}
	w := adminRequest(viewer, "GET", "/api/homepage", nil, nil, "")
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("TEST_ONLY_FAKE_TOKEN")) || bytes.Contains(w.Body.Bytes(), []byte("max-width")) {
		t.Fatal("public projection exposed secrets or embedded CSS")
	}
	if w := adminRequest(viewer, "GET", "/custom.css", nil, nil, ""); w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/css") || w.Body.String() != h.CustomCSS {
		t.Fatal("custom CSS response incorrect")
	}
	if w := adminRequest(admin, "GET", "/custom.css", nil, cookie, ""); w.Code != 404 {
		t.Fatal("homepage CSS leaked onto administrative document")
	}
	unused, _ := storeRouteImage(filepath.Join(a.store.paths.Data, "page", homepageAdminSpace), testRoutePNG(t, 20, 10))
	if adminRequest(viewer, "GET", "/images/"+unused, nil, nil, "").Code != 404 {
		t.Fatal("unselected route icon exposed")
	}
	if adminRequest(viewer, "GET", "/images/"+h.Groups[0].Pages[0].Links[0].Image, nil, nil, "").Code != 200 {
		t.Fatal("selected icon missing")
	}
	if adminRequest(viewer, "GET", "/images/"+h.SearchEngines[2].Image, nil, nil, "").Code != 200 {
		t.Fatal("selected search icon missing")
	}
	for _, path := range []string{"/search-baidu.svg", "/search-google.svg", "/search-generic.svg"} {
		for _, handler := range []http.Handler{viewer, admin} {
			if w := adminRequest(handler, "GET", path, nil, nil, ""); w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/svg+xml") {
				t.Fatal("bundled search icon unavailable")
			}
		}
	}
	h.Public = false
	document, _ := a.store.pages.snapshot(homepageAdminSpace)
	if _, err := a.store.pages.update(homepageAdminSpace, h, document.Revision); err != nil {
		t.Fatal("private setup failed")
	}
	for _, path := range []string{"/api/homepage", "/custom.css", "/images/" + h.Groups[0].Pages[0].Links[0].Image, "/images/" + h.SearchEngines[2].Image, "/background/" + h.Background, "/api/editor/config"} {
		if adminRequest(viewer, "GET", path, nil, cookie, "").Code != 401 {
			t.Fatal("private desktop accepted administrator cookie")
		}
	}
	request := httptest.NewRequest("POST", "http://home.example.test:16680/login", strings.NewReader(`{"username":"admin","password":"TEST_ONLY_ADMIN_PASSWORD"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Gatehouse-Request", "1")
	request.Header.Set("Origin", "https://home.example.test:16680")
	login := httptest.NewRecorder()
	viewer.ServeHTTP(login, request)
	cookies := login.Result().Cookies()
	if login.Code != 200 || len(cookies) != 1 || cookies[0].Name != "gatehomepage_session" || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatal("private homepage authentication or cookie isolation failed")
	}
	if adminRequest(viewer, "GET", "/api/homepage", nil, cookies[0], "").Code != 200 {
		t.Fatal("private homepage session rejected")
	}
	forged := *cookies[0]
	forged.Name = "gatehouse_session"
	if adminRequest(admin, "GET", "/api/config", nil, &forged, "").Code != 401 {
		t.Fatal("homepage cookie granted administrative access")
	}
	if adminRequest(viewer, "POST", "/login", map[string]string{"username": "admin", "password": "TEST_ONLY_ADMIN_PASSWORD"}, nil, "https://attacker.example.test").Code != 403 {
		t.Fatal("homepage login CSRF accepted")
	}
	if adminRequest(viewer, "POST", "/logout", map[string]string{}, cookies[0], "").Code != 200 || adminRequest(viewer, "GET", "/api/homepage", nil, cookies[0], "").Code != 401 {
		t.Fatal("private logout failed to invalidate session")
	}
	c := a.store.Snapshot().Config
	c.Homepage.Enabled = false
	a.store.Update(c, nil, a.store.Snapshot().Revision)
	if adminRequest(viewer, "GET", "/", nil, nil, "").Code != 503 {
		t.Fatal("disabled homepage remains available")
	}
}

func TestHomepageAssetsBackupRestoreAndPruning(t *testing.T) {
	a, _ := testAdmin(t)
	h := homepageFixture(t, a)
	setupHomepage(t, a, h)
	payload, err := snapshotBackupPaths(a.store.paths, a.store.Snapshot())
	if err != nil {
		t.Fatal("homepage snapshot failed")
	}
	if images, _, _ := backupFileCounts(payload); images != 3 {
		t.Fatal("homepage assets omitted from snapshot")
	}
	encrypted, err := encodeBackup(payload, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal("backup encryption failed")
	}
	decoded, _, err := decodeBackup(encrypted, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal("backup decoding failed")
	}
	dest := t.TempDir()
	if restorePayload(dest, decoded) != nil {
		t.Fatal("homepage restore failed")
	}
	restored, err := OpenStore(dest)
	if err != nil || !reflect.DeepEqual(restored.pages.documents[homepageAdminSpace].Homepage, h) {
		t.Fatal("homepage settings CSS links or pages lost during restoration")
	}
	for id := range homepageImages(h) {
		if _, err := readRouteImage(filepath.Join(dest, "page", homepageAdminSpace), id); err != nil {
			t.Fatal("homepage icon not restored")
		}
	}
	if _, err := readHomepageBackground(filepath.Join(dest, "page", homepageAdminSpace), h.Background); err != nil {
		t.Fatal("background not restored")
	}
	paths := StoragePaths{Config: filepath.Join(t.TempDir(), "config"), Log: filepath.Join(t.TempDir(), "log"), Data: filepath.Join(t.TempDir(), "data")}
	os.MkdirAll(paths.Config, 0700)
	os.MkdirAll(paths.Log, 0700)
	os.MkdirAll(paths.Data, 0700)
	configRoot, _ := os.Stat(paths.Config)
	logRoot, _ := os.Stat(paths.Log)
	stateJSON, _ := json.Marshal(decoded.State)
	if restoreFilesPaths(paths, stateJSON, decoded.Certificates, decoded.Files) != nil {
		t.Fatal("split storage restore failed")
	}
	splitStore, err := OpenStorePaths(paths)
	if err != nil || !reflect.DeepEqual(splitStore.pages.documents[homepageAdminSpace].Homepage, h) {
		t.Fatal("split storage lost homepage configuration")
	}
	newConfigRoot, _ := os.Stat(paths.Config)
	newLogRoot, _ := os.Stat(paths.Log)
	if !os.SameFile(configRoot, newConfigRoot) || !os.SameFile(logRoot, newLogRoot) {
		t.Fatal("restore replaced Docker mount roots")
	}
	if _, err := readHomepageBackground(filepath.Join(paths.Data, "page", homepageAdminSpace), h.Background); err != nil {
		t.Fatal("split storage lost background")
	}

	id := h.Groups[0].Pages[0].Links[0].Image
	engineIcon := h.SearchEngines[2].Image
	old := time.Now().Add(-48 * time.Hour)
	for _, image := range []string{id, engineIcon} {
		os.Chtimes(filepath.Join(a.store.paths.Data, "page", homepageAdminSpace, "route-images", image+".img"), old, old)
	}
	pruneRouteImages(filepath.Join(a.store.paths.Data, "page", homepageAdminSpace), Config{Homepage: h})
	for _, image := range []string{id, engineIcon} {
		if _, err := readRouteImage(filepath.Join(a.store.paths.Data, "page", homepageAdminSpace), image); err != nil {
			t.Fatal("homepage-only icon pruned")
		}
	}
	for _, name := range []string{"page/admin/route-images/" + id + ".img", "page/admin/route-images/" + engineIcon + ".img", "page/admin/homepage-backgrounds/" + h.Background + ".jpg"} {
		data := payload.Files[name]
		delete(payload.Files, name)
		if validateBackupFiles(payload) == nil {
			t.Fatal("backup missing homepage asset accepted")
		}
		payload.Files[name] = data
	}
	for _, raw := range [][]byte{nil, []byte(`<svg onload="alert(1)"></svg>`), testRoutePNG(t, 4097, 1), make([]byte, maxImageUpload+1)} {
		if _, err := normalizeHomepageBackground(raw); err == nil {
			t.Fatal("invalid background accepted")
		}
	}
	backgroundPath := filepath.Join(dest, "page", homepageAdminSpace, "homepage-backgrounds", h.Background+".jpg")
	info, _ := os.Stat(backgroundPath)
	if info.Mode().Perm() != 0600 {
		t.Fatal("background permissions incorrect")
	}
	os.Remove(backgroundPath)
	os.Symlink(filepath.Join(dest, "state.json"), backgroundPath)
	if _, err := readHomepageBackground(filepath.Join(dest, "page", homepageAdminSpace), h.Background); err == nil {
		t.Fatal("background symlink followed")
	}
	if safeBackupFile("homepage-backgrounds/../../state.json") {
		t.Fatal("background traversal permitted")
	}
}

func TestPrivateHomepageThroughSelfProxy(t *testing.T) {
	a, admin := testAdmin(t)
	viewer := httptest.NewServer(a.HomepageHandler())
	defer viewer.Close()
	u, _ := url.Parse(viewer.URL)
	port, _ := strconv.Atoi(u.Port())
	c := a.store.Snapshot().Config
	h := homepageFixture(t, a)
	h.Public = false
	setupHomepage(t, a, h)
	c = a.store.Snapshot().Config
	c.Homepage.Port = port
	c.Routes = []Route{{GroupID: "default", Name: "首页", Host: "home.example.test", Upstream: viewer.URL, Enabled: true}}
	if a.store.Update(c, nil, a.store.Snapshot().Revision) != nil {
		t.Fatal("self proxy setup failed")
	}
	a.proxy.ConfigureState(a.store.Snapshot())
	request := httptest.NewRequest("POST", "http://home.example.test/login", strings.NewReader(`{"username":"admin","password":"TEST_ONLY_ADMIN_PASSWORD"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Gatehouse-Request", "1")
	request.Header.Set("Origin", "https://home.example.test")
	response := httptest.NewRecorder()
	a.proxy.ServeHTTP(response, request)
	cookies := response.Result().Cookies()
	if response.Code != 200 || len(cookies) != 1 || cookies[0].Name != "gatehomepage_session" || !cookies[0].Secure {
		t.Fatal("self proxy did not preserve homepage session")
	}
	request = httptest.NewRequest("GET", "http://home.example.test/api/homepage", nil)
	request.AddCookie(cookies[0])
	response = httptest.NewRecorder()
	a.proxy.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal("authenticated homepage unavailable through self proxy")
	}
	if adminRequest(viewer.Config.Handler, "GET", "/api/homepage", nil, cookies[0], "https://attacker.example.test").Code != 403 {
		t.Fatal("private homepage accepted cross origin read")
	}
	forged := *cookies[0]
	forged.Name = "gatehouse_session"
	if adminRequest(admin, "GET", "/api/config", nil, &forged, "").Code != http.StatusUnauthorized {
		t.Fatal("homepage token could be renamed to an admin token")
	}
	adminCookie := loginForTest(t, admin)
	if adminRequest(admin, "PUT", "/api/account", map[string]string{"username": "TEST_ONLY_RENAMED", "current_password": "TEST_ONLY_ADMIN_PASSWORD"}, adminCookie, "").Code != 200 {
		t.Fatal("account update fixture failed")
	}
	if adminRequest(viewer.Config.Handler, "GET", "/api/homepage", nil, cookies[0], "").Code != 401 {
		t.Fatal("administrator account change did not revoke homepage login")
	}
}

func TestHomepageLauncherCompatibilityAndRollback(t *testing.T) {
	a, _ := testAdmin(t)
	m, err := NewMaintenancePaths(a.store.paths, t.TempDir())
	if err != nil {
		t.Fatal("maintenance fixture failed")
	}
	m.homepageFiles = true
	m.pageFiles = false
	if m.checkHomepageFiles(nil) == nil {
		t.Fatal("legacy launcher accepted page directory without rollback support")
	}
	h := homepageFixture(t, a)
	setupHomepage(t, a, h)
	t.Setenv("GATEHOUSE_HOMEPAGE_FILES", "1")
	t.Setenv("GATEHOUSE_PAGE_STORAGE", "1")
	supported, err := NewMaintenancePaths(a.store.paths, m.appDir)
	if err != nil || !supported.pageFiles || supported.checkHomepageFiles(&h) != nil {
		t.Fatal("new page storage capability rejected")
	}
	before, err := snapshotDiskPaths(a.store.paths)
	if err != nil {
		t.Fatal("snapshot failed")
	}
	atomicWrite(filepath.Join(supported.appDir, "gatehouse"), []byte("TEST_ONLY_OLD_PROGRAM"))
	atomicWrite(filepath.Join(supported.dir, "update-staged"), []byte("TEST_ONLY_CORRUPT_UPDATE"))
	writeJSON(filepath.Join(supported.dir, "operation.json"), maintenanceOperation{Kind: "update", Version: "0.0.99", Digest: "invalid"})
	transitioned, err := supported.beginTransition()
	if !transitioned || err == nil {
		t.Fatal("corrupt update did not fail inside transition")
	}
	for name := range before.Files {
		os.Remove(a.store.paths.file(name))
	}
	if supported.rollbackTransition() != nil {
		t.Fatal("failed update rollback failed")
	}
	after, err := snapshotDiskPaths(a.store.paths)
	var beforeState, afterState State
	if json.Unmarshal(before.State, &beforeState) != nil || json.Unmarshal(after.State, &afterState) != nil {
		t.Fatal("rollback configuration could not be decoded")
	}
	if err != nil || !reflect.DeepEqual(beforeState, afterState) || !reflect.DeepEqual(before.Files, after.Files) || !reflect.DeepEqual(before.Certificates, after.Certificates) {
		t.Fatal("rollback omitted homepage configuration or assets")
	}
}
