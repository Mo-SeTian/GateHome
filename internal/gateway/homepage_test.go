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
	h.Enabled = true
	h.CustomCSS = ".gh-main { max-width: 1200px; }"
	h.SearchEngines = append(h.SearchEngines, HomepageSearchEngine{ID: "docs", Name: "文档", URL: "https://search.example.test/?q={query}&lang=zh"})
	icon, err := storeRouteImage(a.store.paths.Data, testRoutePNG(t, 64, 64))
	if err != nil {
		t.Fatal("icon fixture failed")
	}
	background, err := storeHomepageBackground(a.store.paths.Data, testRoutePNG(t, 800, 400))
	if err != nil {
		t.Fatal("background fixture failed")
	}
	h.Background = background
	h.Groups = []HomepageGroup{{ID: "home", Name: "家庭", Pages: []HomepagePage{{ID: "daily", Name: "日常", Rows: 2, Columns: 3, MobileColumns: 2, Links: []HomepageLink{{ID: "nas", Name: "NAS", LAN: "http://192.168.2.10:5000/", WAN: "https://nas.example.test/", Image: icon, Favorite: true}}}}}}
	return h
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
	c := DefaultConfig()
	h.SearchEngines = nil
	h.Title = "保留的空间"
	h.CustomCSS = ".gh-main { max-width: 1200px; }"
	c.Homepage = h
	wanted := h
	wanted.SearchEngines = defaultHomepage().SearchEngines
	if !migrateConfig(&c) || !reflect.DeepEqual(c.Homepage, wanted) {
		t.Fatal("legacy search migration changed existing homepage settings")
	}
}

func TestHomepageValidationMigrationAndPortConflicts(t *testing.T) {
	a, _ := testAdmin(t)
	c := a.store.Snapshot().Config
	c.Homepage = homepageFixture(t, a)
	if Validate(c) != nil {
		t.Fatal("valid homepage rejected")
	}
	invalid := []func(*Config){
		func(c *Config) { c.Homepage.Port = 1023 }, func(c *Config) { c.Homepage.Port = c.Groups[0].HTTPPort },
		func(c *Config) { c.Homepage.Groups[0].Pages[0].Rows = 0 }, func(c *Config) { c.Homepage.Groups[0].Pages[0].Columns = 9 },
		func(c *Config) { c.Homepage.Groups[0].Pages[0].MobileColumns = 4 }, func(c *Config) { c.Homepage.Groups[0].Pages[0].Links[0].LAN = "javascript:alert(1)" },
		func(c *Config) { c.Homepage.Groups[0].Pages[0].Links[0].WAN = "https://user:password@example.test/" },
		func(c *Config) { c.Homepage.Groups[0].Pages[0].Links[0].ID = "daily" }, func(c *Config) { c.Homepage.Background = "../../state.json" },
		func(c *Config) { c.Homepage.CustomCSS = strings.Repeat("a", 32769) }, func(c *Config) { c.Homepage.Groups[0].Pages = nil },
	}
	for i, change := range invalid {
		data, _ := json.Marshal(c)
		var next Config
		json.Unmarshal(data, &next)
		change(&next)
		if Validate(next) == nil {
			t.Fatalf("invalid homepage case %d accepted", i)
		}
	}
	c.Homepage.Port = a.adminPort
	if validateAdminPort(c, a.adminPort) == nil {
		t.Fatal("admin port conflict accepted")
	}
	c.Homepage.Enabled = false
	if validateAdminPort(c, a.adminPort) != nil {
		t.Fatal("disabled homepage reserved admin port")
	}
	c.Homepage = HomepageConfig{}
	c.Groups[0].HTTPPort = 16680
	if !migrateConfig(&c) || c.Homepage.Enabled || c.Homepage.Port != 16680 || Validate(c) != nil {
		t.Fatal("migration enabled homepage or broke an existing listener")
	}
	c.Homepage.Enabled = true
	c.Homepage.Port = 16681
	if listenerSignature(c) == listenerSignature(a.ports) {
		t.Fatal("homepage listener change omitted from restart signal")
	}
}

func TestHomepageIsolationPrivateLoginAndCSS(t *testing.T) {
	a, admin := testAdmin(t)
	h := homepageFixture(t, a)
	cookie := loginForTest(t, admin)
	update := map[string]any{"homepage": h, "revision": a.store.Snapshot().Revision}
	if adminRequest(admin, "PUT", "/api/homepage", update, nil, "").Code != 401 {
		t.Fatal("anonymous homepage mutation")
	}
	if adminRequest(admin, "PUT", "/api/homepage", update, cookie, "https://attacker.example.test").Code != 403 {
		t.Fatal("cross origin homepage mutation")
	}
	if adminRequest(admin, "PUT", "/api/homepage", update, cookie, "").Code != 200 {
		t.Fatal("homepage save failed")
	}
	if adminRequest(admin, "PUT", "/api/homepage", update, cookie, "").Code == 200 {
		t.Fatal("stale revision overwrote homepage")
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
	unused, _ := storeRouteImage(a.store.paths.Data, testRoutePNG(t, 20, 10))
	if adminRequest(viewer, "GET", "/images/"+unused, nil, nil, "").Code != 404 {
		t.Fatal("unselected route icon exposed")
	}
	if adminRequest(viewer, "GET", "/images/"+h.Groups[0].Pages[0].Links[0].Image, nil, nil, "").Code != 200 {
		t.Fatal("selected icon missing")
	}
	c := a.store.Snapshot().Config
	c.Homepage.Public = false
	if a.store.Update(c, nil, a.store.Snapshot().Revision) != nil {
		t.Fatal("private setup failed")
	}
	for _, path := range []string{"/api/homepage", "/custom.css", "/images/" + h.Groups[0].Pages[0].Links[0].Image, "/background/" + h.Background} {
		if adminRequest(viewer, "GET", path, nil, cookie, "").Code != 401 {
			t.Fatal("private homepage accepted administrator cookie or anonymous access")
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
	c = a.store.Snapshot().Config
	c.Homepage.Enabled = false
	a.store.Update(c, nil, a.store.Snapshot().Revision)
	if adminRequest(viewer, "GET", "/", nil, nil, "").Code != 503 {
		t.Fatal("disabled homepage remains available")
	}
}

func TestHomepageAssetsBackupRestoreAndPruning(t *testing.T) {
	a, _ := testAdmin(t)
	c := a.store.Snapshot().Config
	c.Homepage = homepageFixture(t, a)
	if a.store.Update(c, nil, a.store.Snapshot().Revision) != nil {
		t.Fatal("homepage setup failed")
	}
	payload, err := snapshotBackupPaths(a.store.paths, a.store.Snapshot())
	if err != nil {
		t.Fatal("homepage snapshot failed")
	}
	if images, _, _ := backupFileCounts(payload); images != 2 {
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
	if err != nil || !reflect.DeepEqual(restored.Snapshot().Config.Homepage, c.Homepage) {
		t.Fatal("homepage settings CSS links or pages lost during restoration")
	}
	for id := range homepageImages(c.Homepage) {
		if _, err := readRouteImage(dest, id); err != nil {
			t.Fatal("homepage icon not restored")
		}
	}
	if _, err := readHomepageBackground(dest, c.Homepage.Background); err != nil {
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
	if err != nil || !reflect.DeepEqual(splitStore.Snapshot().Config.Homepage, c.Homepage) {
		t.Fatal("split storage lost homepage configuration")
	}
	newConfigRoot, _ := os.Stat(paths.Config)
	newLogRoot, _ := os.Stat(paths.Log)
	if !os.SameFile(configRoot, newConfigRoot) || !os.SameFile(logRoot, newLogRoot) {
		t.Fatal("restore replaced Docker mount roots")
	}
	if _, err := readHomepageBackground(paths.Data, c.Homepage.Background); err != nil {
		t.Fatal("split storage lost background")
	}

	id := c.Homepage.Groups[0].Pages[0].Links[0].Image
	path := filepath.Join(a.store.paths.Data, "route-images", id+".img")
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(path, old, old)
	pruneRouteImages(a.store.paths.Data, c)
	if _, err := os.Stat(path); err != nil {
		t.Fatal("homepage-only icon pruned")
	}
	for _, name := range []string{"route-images/" + id + ".img", "homepage-backgrounds/" + c.Homepage.Background + ".jpg"} {
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
	backgroundPath := filepath.Join(dest, "homepage-backgrounds", c.Homepage.Background+".jpg")
	info, _ := os.Stat(backgroundPath)
	if info.Mode().Perm() != 0600 {
		t.Fatal("background permissions incorrect")
	}
	os.Remove(backgroundPath)
	os.Symlink(filepath.Join(dest, "state.json"), backgroundPath)
	if _, err := readHomepageBackground(dest, c.Homepage.Background); err == nil {
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
	c.Homepage = homepageFixture(t, a)
	c.Homepage.Port = port
	c.Homepage.Public = false
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
	m.homepageFiles = false
	if m.checkHomepageFiles(nil) != nil {
		t.Fatal("legacy launcher rejected configuration without homepage assets")
	}
	h := homepageFixture(t, a)
	if m.checkHomepageFiles(&h) == nil {
		t.Fatal("legacy launcher accepted incoming homepage assets")
	}
	c := a.store.Snapshot().Config
	c.Homepage = h
	if a.store.Update(c, nil, a.store.Snapshot().Revision) != nil {
		t.Fatal("asset setup failed")
	}
	if m.checkHomepageFiles(nil) == nil {
		t.Fatal("legacy launcher could discard current homepage icons during rollback")
	}
	t.Setenv("GATEHOUSE_HOMEPAGE_FILES", "1")
	supported, err := NewMaintenancePaths(a.store.paths, m.appDir)
	if err != nil || !supported.homepageFiles || supported.checkHomepageFiles(&h) != nil {
		t.Fatal("new launcher capability rejected")
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
