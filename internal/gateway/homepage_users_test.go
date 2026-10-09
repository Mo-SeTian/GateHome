package gateway

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHomepageImportAndUploadOwnership(t *testing.T) {
	a, admin := testAdmin(t)
	setupHomepage(t, a, homepageFixture(t, a))
	adminCookie := loginForTest(t, admin)
	id := addHomepageUserForTest(t, a, admin, adminCookie, "TEST_ONLY_IMPORT_USER")
	page := a.HomepageHandler()
	cookie := homepageLoginForTest(t, page, "TEST_ONLY_IMPORT_USER", "TEST_ONLY_PAGE_PASSWORD")
	document, _ := a.store.pages.snapshot(id)
	document.Homepage.Groups = []HomepageGroup{{ID: "own", Name: "Own", Pages: []HomepagePage{{ID: "target", Name: "Target", Rows: 2, Columns: 3, MobileColumns: 2, Links: []HomepageLink{}}}}}
	if _, err := a.store.pages.update(id, document.Homepage, document.Revision); err != nil {
		t.Fatal("own page setup failed")
	}
	image, err := storeRouteImage(a.store.paths.Data, testRoutePNG(t, 40, 32))
	if err != nil {
		t.Fatal("source image setup failed")
	}
	c := a.store.Snapshot().Config
	c.Routes = []Route{{GroupID: "default", Name: "Test app", Host: "test.example.test", Upstream: "http://127.0.0.1:4321/", Image: image, Enabled: true}}
	if a.store.Update(c, nil, a.store.Snapshot().Revision) != nil {
		t.Fatal("source route setup failed")
	}
	w := adminRequest(page, "GET", "/api/editor/importable", nil, cookie, "")
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("password_hash")) || bytes.Contains(w.Body.Bytes(), []byte("TEST_ONLY_FAKE_TOKEN")) || bytes.Contains(w.Body.Bytes(), []byte("firewall")) {
		t.Fatal("import projection leaked configuration")
	}
	document, _ = a.store.pages.snapshot(id)
	input := map[string]any{"page_id": "target", "services": []string{"default/test.example.test"}, "revision": document.Revision}
	if adminRequest(page, "POST", "/api/editor/import", input, cookie, "").Code != 200 {
		t.Fatal("import failed")
	}
	document, _ = a.store.pages.snapshot(id)
	link := document.Homepage.Groups[0].Pages[0].Links[0]
	if link.LAN != c.Routes[0].Upstream || link.WAN != "http://test.example.test:18080/" || link.Image != image {
		t.Fatal("import lost addresses or icon")
	}
	dir, _ := homepageSpaceDirectory(a.store.paths.Data, id)
	if _, err := readRouteImage(dir, image); err != nil {
		t.Fatal("imported icon was not copied to owner directory")
	}
	input["revision"] = document.Revision
	if adminRequest(page, "POST", "/api/editor/import", input, cookie, "").Code != 200 {
		t.Fatal("repeat import failed")
	}
	document, _ = a.store.pages.snapshot(id)
	if len(document.Homepage.Groups[0].Pages[0].Links) != 1 {
		t.Fatal("repeat import duplicated app")
	}
	os.Remove(filepath.Join(a.store.paths.Data, "route-images", image+".img"))
	if adminRequest(page, "GET", "/api/editor/images/"+image, nil, cookie, "").Code != 200 {
		t.Fatal("source icon deletion affected imported icon")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "test.png")
	part.Write(testRoutePNG(t, 24, 32))
	writer.Close()
	r := httptest.NewRequest("POST", "http://localhost:16666/api/editor/images/upload", bytes.NewReader(body.Bytes()))
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("X-Gatehouse-Request", "1")
	r.AddCookie(cookie)
	response := httptest.NewRecorder()
	page.ServeHTTP(response, r)
	if response.Code != 201 {
		t.Fatal("authenticated multipart upload failed")
	}
	var uploaded struct {
		ID string `json:"id"`
	}
	json.Unmarshal(response.Body.Bytes(), &uploaded)
	if _, err := readRouteImage(dir, uploaded.ID); err != nil {
		t.Fatal("uploaded icon missing from user directory")
	}
	if _, err := readRouteImage(a.store.paths.Data, uploaded.ID); err == nil {
		t.Fatal("Page upload stored in GateHome image directory")
	}
	r = httptest.NewRequest("POST", "http://localhost:16666/api/editor/images/upload", bytes.NewReader(body.Bytes()))
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("X-Gatehouse-Request", "1")
	r.Header.Set("Origin", "https://attacker.example.test")
	r.AddCookie(cookie)
	response = httptest.NewRecorder()
	page.ServeHTTP(response, r)
	if response.Code != 403 {
		t.Fatal("multipart upload CSRF accepted")
	}
}

func addHomepageUserForTest(t *testing.T, a *Admin, admin http.Handler, cookie *http.Cookie, name string) string {
	t.Helper()
	w := adminRequest(admin, "POST", "/api/homepage/users", map[string]any{"username": name, "password": "TEST_ONLY_PAGE_PASSWORD", "enabled": true, "revision": a.store.Snapshot().Revision}, cookie, "")
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("password_hash")) || bytes.Contains(w.Body.Bytes(), []byte("TEST_ONLY_PAGE_PASSWORD")) {
		t.Fatal("homepage account creation failed or leaked credentials")
	}
	for id, user := range a.store.Snapshot().HomepageUsers {
		if user.Username == name {
			return id
		}
	}
	t.Fatal("created homepage account missing")
	return ""
}

func TestHomepageUsersOwnDesktopIsolation(t *testing.T) {
	a, admin := testAdmin(t)
	setupHomepage(t, a, homepageFixture(t, a))
	adminCookie := loginForTest(t, admin)
	alice := addHomepageUserForTest(t, a, admin, adminCookie, "TEST_ONLY_ALICE")
	bob := addHomepageUserForTest(t, a, admin, adminCookie, "TEST_ONLY_BOB")
	page := a.HomepageHandler()
	aliceCookie := homepageLoginForTest(t, page, "TEST_ONLY_ALICE", "TEST_ONLY_PAGE_PASSWORD")
	bobCookie := homepageLoginForTest(t, page, "TEST_ONLY_BOB", "TEST_ONLY_PAGE_PASSWORD")
	for _, path := range []string{"/api/editor/config", "/api/editor/importable"} {
		if adminRequest(page, "GET", path, nil, nil, "").Code != 401 || adminRequest(page, "GET", path, nil, adminCookie, "").Code != 401 {
			t.Fatal("editor accepted anonymous or GateHome cookie")
		}
		if adminRequest(page, "GET", path, nil, aliceCookie, "https://attacker.example.test").Code != 403 {
			t.Fatal("editor allowed cross origin access")
		}
	}
	document, _ := a.store.pages.snapshot(bob)
	home := cloneHomepage(document.Homepage)
	home.Title = "TEST_ONLY_BOB_PRIVATE_DESKTOP"
	dir, _ := homepageSpaceDirectory(a.store.paths.Data, bob)
	image, _ := storeRouteImage(dir, testRoutePNG(t, 32, 20))
	home.SearchEngines[0].Image = image
	if _, err := a.store.pages.update(bob, home, document.Revision); err != nil {
		t.Fatal("private desktop fixture failed")
	}
	for _, path := range []string{"/api/homepage?space=" + bob, "/custom.css?space=" + bob, "/images/" + image + "?space=" + bob} {
		if adminRequest(page, "GET", path, nil, aliceCookie, "").Code != 401 {
			t.Fatal("user could read another private desktop")
		}
	}
	if adminRequest(page, "GET", "/api/editor/images/"+image+"?space="+bob, nil, aliceCookie, "").Code != 404 {
		t.Fatal("editor asset path leaked another user's cached image")
	}
	w := adminRequest(page, "GET", "/api/editor/config?space="+bob, nil, aliceCookie, "")
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(home.Title)) {
		t.Fatal("editor query changed ownership")
	}
	aliceDocument, _ := a.store.pages.snapshot(alice)
	changed := cloneHomepage(aliceDocument.Homepage)
	changed.Title = "TEST_ONLY_ALICE_DESKTOP"
	beforeBob, _ := a.store.pages.snapshot(bob)
	input := homepageDocument{Homepage: changed, Revision: aliceDocument.Revision}
	if adminRequest(page, "PUT", "/api/editor/config?space="+bob, input, aliceCookie, "").Code != 200 {
		t.Fatal("own desktop save failed")
	}
	afterBob, _ := a.store.pages.snapshot(bob)
	if !reflect.DeepEqual(beforeBob, afterBob) {
		t.Fatal("user changed another user's desktop")
	}
	if adminRequest(page, "PUT", "/api/editor/config", input, aliceCookie, "").Code == 200 {
		t.Fatal("stale desktop revision accepted")
	}
	current, _ := a.store.pages.snapshot(alice)
	current.Homepage.Port = 18000
	if adminRequest(page, "PUT", "/api/editor/config", current, aliceCookie, "").Code == 200 {
		t.Fatal("Page user changed listener settings")
	}
	forged := *aliceCookie
	forged.Name = "gatehouse_session"
	if adminRequest(admin, "GET", "/api/config", nil, &forged, "").Code != 401 || adminRequest(admin, "POST", "/api/homepage/users", nil, &forged, "").Code != 401 {
		t.Fatal("Page credentials granted GateHome administration")
	}
	if adminRequest(admin, "POST", "/api/login", map[string]string{"username": "TEST_ONLY_ALICE", "password": "TEST_ONLY_PAGE_PASSWORD"}, nil, "").Code == 200 {
		t.Fatal("Page account logged into GateHome")
	}
	if adminRequest(page, "GET", "/api/homepage?space="+bob, nil, bobCookie, "").Code != 200 {
		t.Fatal("owner cannot view private desktop")
	}
	user := a.store.Snapshot().HomepageUsers[bob]
	if adminRequest(admin, "PUT", "/api/homepage/users/"+bob, map[string]any{"username": user.Username, "enabled": false, "revision": a.store.Snapshot().Revision}, adminCookie, "").Code != 200 {
		t.Fatal("account disable failed")
	}
	if adminRequest(page, "GET", "/api/editor/config", nil, bobCookie, "").Code != 401 {
		t.Fatal("disabled account session remained valid")
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatal("disabling an account discarded its desktop")
	}
	w = adminRequest(admin, "GET", "/api/config", nil, adminCookie, "")
	var response struct {
		Config struct {
			Homepage map[string]any `json:"homepage"`
		} `json:"config"`
	}
	if json.Unmarshal(w.Body.Bytes(), &response) != nil || len(response.Config.Homepage) != 2 || response.Config.Homepage["port"] == nil || response.Config.Homepage["enabled"] == nil || bytes.Contains(w.Body.Bytes(), []byte(changed.Title)) {
		t.Fatal("GateHome config exposed desktop content")
	}
}

func TestHomepageUsersBackupAndLegacyMigration(t *testing.T) {
	a, admin := testAdmin(t)
	setupHomepage(t, a, homepageFixture(t, a))
	id := addHomepageUserForTest(t, a, admin, loginForTest(t, admin), "TEST_ONLY_RESTORE_USER")
	document, _ := a.store.pages.snapshot(id)
	document.Homepage.Title = "TEST_ONLY_INDEPENDENT_SPACE"
	if _, err := a.store.pages.update(id, document.Homepage, document.Revision); err != nil {
		t.Fatal("user desktop fixture failed")
	}
	payload, err := snapshotBackupPaths(a.store.paths, a.store.Snapshot())
	if err != nil {
		t.Fatal("multi-user snapshot failed")
	}
	data, err := encodeBackup(payload, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal("multi-user encryption failed")
	}
	decoded, _, err := decodeBackup(data, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal("multi-user backup decoding failed")
	}
	destination := t.TempDir()
	if restorePayload(destination, decoded) != nil {
		t.Fatal("multi-user restoration failed")
	}
	restored, err := OpenStore(destination)
	if err != nil || !reflect.DeepEqual(restored.Snapshot().HomepageUsers, a.store.Snapshot().HomepageUsers) {
		t.Fatal("restoration lost account credentials")
	}
	wanted, _ := a.store.pages.snapshot(id)
	actual, ok := restored.pages.snapshot(id)
	if !ok || !reflect.DeepEqual(wanted, actual) {
		t.Fatal("restoration lost independent desktop")
	}
	for _, path := range []string{"page/admin/config.json", "page/" + id + "/config.json"} {
		content := payload.Files[path]
		delete(payload.Files, path)
		if validateBackupFiles(payload) == nil {
			t.Fatal("backup with missing desktop accepted")
		}
		payload.Files[path] = content
	}
	for _, path := range []string{"page/../../state.json", "page/admin/../state.json", "page/admin/config.json/extra", "page/unknown/config.json"} {
		if safeBackupFile(path) {
			t.Fatal("unsafe Page backup path accepted")
		}
	}
	legacy := a.store.Snapshot()
	legacy.HomepageUsers, legacy.HomepageData = nil, false
	legacy.Config.Homepage = homepageFixture(t, a)
	legacy.Config.Homepage.Port = 16680
	legacy.Config.Homepage.Enabled = true
	oldFiles := map[string][]byte{}
	adminDir, _ := homepageSpaceDirectory(a.store.paths.Data, homepageAdminSpace)
	for image := range homepageImages(legacy.Config.Homepage) {
		content, _ := readRouteImage(adminDir, image)
		oldFiles["route-images/"+image+".img"] = content
	}
	background, _ := readHomepageBackground(adminDir, legacy.Config.Homepage.Background)
	oldFiles["homepage-backgrounds/"+legacy.Config.Homepage.Background+".jpg"] = background
	legacyBackup := backupPayload{State: legacy, Certificates: map[string][]byte{}, Files: oldFiles}
	oldData, _ := encodeBackup(legacyBackup, "TEST_ONLY_OLD_BACKUP_PASSWORD")
	migrated, _, err := decodeBackup(oldData, "TEST_ONLY_OLD_BACKUP_PASSWORD")
	if err != nil || migrated.Files["page/admin/config.json"] == nil {
		t.Fatal("legacy backup did not migrate to Page storage")
	}
	var own homepageDocument
	if json.Unmarshal(migrated.Files["page/admin/config.json"], &own) != nil || own.Homepage.CustomCSS != legacy.Config.Homepage.CustomCSS || own.Homepage.Groups[0].Pages[0].Links[0].Image != legacy.Config.Homepage.Groups[0].Pages[0].Links[0].Image {
		t.Fatal("legacy migration changed content or image references")
	}
	root := t.TempDir()
	for name, content := range oldFiles {
		os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0700)
		atomicWrite(filepath.Join(root, name), content)
	}
	writeJSON(filepath.Join(root, "state.json"), legacy)
	opened, err := OpenStore(root)
	if err != nil {
		t.Fatal("legacy on-disk migration failed")
	}
	if _, ok := opened.pages.snapshot(homepageAdminSpace); !ok || !opened.Snapshot().HomepageData {
		t.Fatal("migrated desktop missing")
	}
	rootState, _ := os.ReadFile(filepath.Join(root, "state.json"))
	if strings.Contains(string(rootState), legacy.Config.Homepage.CustomCSS) {
		t.Fatal("desktop content stayed in GateHome state")
	}
}
