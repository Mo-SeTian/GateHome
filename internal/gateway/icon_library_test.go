package gateway

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testLibrarySVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 36"><rect width="48" height="36"/></svg>`

func uploadLibraryIcons(t *testing.T, h http.Handler, cookie *http.Cookie, files map[string][]byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, data := range files {
		part, err := form.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "http://localhost:16666/api/icons/upload", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("X-Gatehouse-Request", "1")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	return response
}

func TestIconLibraryAPI(t *testing.T) {
	a, h := testAdmin(t)
	dir := a.store.paths.Data
	cookie := loginForTest(t, h)
	get := func(path string) *httptest.ResponseRecorder { return adminRequest(h, "GET", path, nil, cookie, "") }
	del := func(path string) int { return adminRequest(h, "DELETE", "/api/icons/"+path, nil, cookie, "").Code }

	for _, path := range []string{"/api/icons", "/api/icons/stats", "/api/icons/import-status", "/api/icons/file/builtin/nas.svg"} {
		if w := adminRequest(h, "GET", path, nil, nil, ""); w.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s returned %d", path, w.Code)
		}
	}
	if get("/api/icons/import-status").Code != 200 || adminRequest(h, "POST", "/api/icons/import-github", iconImportRequest{Repository: "https://example.test/icon-pack"}, cookie, "").Code != 400 {
		t.Fatal("import endpoint validation failed")
	}
	var stats iconStats
	response := get("/api/icons/stats")
	if json.Unmarshal(response.Body.Bytes(), &stats) != nil || stats.Path != "/icons/" ||
		stats.Total < 5 || stats.Total != stats.Sources["builtin"] || stats.Bytes < 1 || stats.Categories["nas"] < 1 {
		t.Fatal("built-in icon counts are incorrect")
	}
	for _, term := range []string{"gate", "网关", "wangguan", "wg", "lyq"} {
		response := get("/api/icons?limit=1&category=tool&source=builtin&style=svg&keyword=" + url.QueryEscape(term))
		var found iconIndex
		if json.Unmarshal(response.Body.Bytes(), &found) != nil || len(found.Items) != 1 || found.Items[0].Name != "gateway" {
			t.Fatalf("failed to find gateway for %s: %s", term, response.Body.String())
		}
	}
	for _, rel := range []string{"builtin", "imported", "uploads", "index.json"} {
		if _, err := os.Stat(filepath.Join(dir, "icons", rel)); err != nil {
			t.Fatal(err)
		}
	}
	var diskIndex iconIndex
	raw, err := os.ReadFile(filepath.Join(dir, "icons", "index.json"))
	if err != nil || json.Unmarshal(raw, &diskIndex) != nil || len(diskIndex.Postings["网关"]) == 0 {
		t.Fatal("on-disk inverted index missing")
	}

	response = uploadLibraryIcons(t, h, cookie, map[string][]byte{
		"测试 应用.svg": []byte(testLibrarySVG), "custom.png": testRoutePNG(t, 512, 256),
	})
	var uploaded iconIndex
	if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &uploaded) != nil || len(uploaded.Items) != 2 {
		t.Fatalf("batch upload failed: %d %s", response.Code, response.Body.String())
	}
	for _, icon := range uploaded.Items {
		response = get(icon.URL)
		if response.Code != 200 || response.Header().Get("Cache-Control") != "public, max-age=604800" ||
			response.Header().Get("X-Content-Type-Options") != "nosniff" ||
			!strings.Contains(response.Header().Get("Content-Security-Policy"), "sandbox") ||
			response.Header().Get("Content-Type") != iconMIMEs[icon.Format] {
			t.Fatalf("bad static icon response: %d %v", response.Code, response.Header())
		}
		if icon.Format == "svg" && (icon.Width != 48 || icon.Height != 36) ||
			icon.Format == "png" && (icon.Width != 512 || icon.Height != 256) {
			t.Fatal("image resolution not preserved")
		}
	}
	response = get("/api/icons?source=uploads&keyword=csyy")
	var chinese iconIndex
	if json.Unmarshal(response.Body.Bytes(), &chinese) != nil || len(chinese.Items) != 1 {
		t.Fatal("uploaded Chinese filename was not indexed")
	}
	icon := chinese.Items[0]
	if uploadLibraryIcons(t, h, cookie, map[string][]byte{"invalid.svg": []byte("<html/>"), "valid.svg": []byte(testLibrarySVG)}).Code != 400 {
		t.Fatal("invalid upload batch accepted")
	}
	response = get("/api/icons?source=uploads")
	if json.Unmarshal(response.Body.Bytes(), &uploaded) != nil || len(uploaded.Items) != 2 {
		t.Fatal("failed upload changed the library")
	}
	if del("builtin/nas.svg") != 403 {
		t.Fatal("built-in icon deletion accepted")
	}

	s := a.store.Snapshot()
	legacy, err := storeRouteImage(dir, testRoutePNG(t, 10, 10))
	if err != nil {
		t.Fatal(err)
	}
	s.Config.Routes = []Route{
		{GroupID: s.Config.Groups[0].ID, Name: "Library", Host: "library.example.test", Upstream: "http://localhost:5000", Image: iconRefPrefix + icon.RelPath},
		{GroupID: s.Config.Groups[0].ID, Name: "Legacy", Host: "legacy.example.test", Upstream: "http://localhost:5000", Image: legacy},
	}
	if err := a.store.Update(s.Config, nil, s.Revision); err != nil {
		t.Fatal(err)
	}
	if del(strings.TrimPrefix(icon.URL, iconFilePrefix)) != 409 {
		t.Fatal("deleting an in-use icon should be blocked")
	}
	payload, err := snapshotBackupPaths(a.store.paths, a.store.Snapshot())
	if err != nil || len(payload.Files[routeImageBackupPath(iconRefPrefix+icon.RelPath)]) == 0 || len(payload.Files[routeImageBackupPath(legacy)]) == 0 {
		t.Fatalf("backup lost a route image: %v", err)
	}
	restoreDir := t.TempDir()
	if err := restorePayload(restoreDir, payload); err != nil || !routeImageExists(restoreDir, iconRefPrefix+icon.RelPath) || !routeImageExists(restoreDir, legacy) {
		t.Fatalf("restore lost a route image: %v", err)
	}
	s = a.store.Snapshot()
	s.Config.Routes = s.Config.Routes[1:]
	if err := a.store.Update(s.Config, nil, s.Revision); err != nil {
		t.Fatal(err)
	}
	if del(strings.TrimPrefix(icon.URL, iconFilePrefix)) != 204 || get(icon.URL).Code != 404 {
		t.Fatal("deletion did not remove uploaded file")
	}
	if err := os.WriteFile(filepath.Join(dir, "icons", "uploads", "manual.svg"), []byte(testLibrarySVG), 0600); err != nil {
		t.Fatal(err)
	}
	if adminRequest(h, "POST", "/api/icons/reindex", nil, cookie, "").Code != 200 {
		t.Fatal("manual reindex failed")
	}
	response = get("/api/icons?source=uploads&keyword=manual")
	var manual iconIndex
	if json.Unmarshal(response.Body.Bytes(), &manual) != nil || len(manual.Items) != 1 {
		t.Fatal("manual reindex did not refresh search")
	}
}

func TestIconLibraryPathSafety(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	dir := a.store.paths.Data
	outside := t.TempDir()
	secretFile := filepath.Join(outside, "outside.svg")
	if err := os.WriteFile(secretFile, []byte(testLibrarySVG), 0600); err != nil {
		t.Fatal(err)
	}
	uploads := filepath.Join(dir, "icons", "uploads")
	for path, target := range map[string]string{"linked.svg": secretFile, "linkdir": outside, "builtin": "../builtin"} {
		if err := os.Symlink(target, filepath.Join(uploads, path)); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"index.json", "uploads/../builtin/nas.svg", "uploads/%2e%2e/builtin/nas.svg",
		"uploads/%252e%252e/outside.svg", "uploads/%5c..%5coutside.svg", "uploads//linked.svg",
		"uploads/linked.svg", "uploads/linkdir/outside.svg", "uploads/builtin/nas.svg", "uploads/../../state.json"} {
		response := adminRequest(h, "GET", iconFilePrefix+rel, nil, cookie, "")
		if response.Code != 404 {
			t.Errorf("unsafe icon read %q returned %d", rel, response.Code)
		}
	}
	for _, rel := range []string{"uploads/linked.svg", "uploads/linkdir/outside.svg", "uploads/%2e%2e/builtin/nas.svg"} {
		response := adminRequest(h, "DELETE", "/api/icons/"+rel, nil, cookie, "")
		if response.Code != 400 {
			t.Errorf("unsafe icon deletion %q returned %d", rel, response.Code)
		}
	}
	if _, err := os.Stat(secretFile); err != nil {
		t.Fatal("external file changed")
	}
}

func TestIconLibraryImport(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	var archive bytes.Buffer
	zipper := zip.NewWriter(&archive)
	for name, data := range map[string][]byte{
		"pack-HEAD/svg/Jellyfin.svg":       []byte(testLibrarySVG),
		"pack-HEAD/circle/Jellyfin.png":    testRoutePNG(t, 32, 32),
		"pack-HEAD/border-radius/Plex.png": testRoutePNG(t, 64, 64),
		"pack-HEAD/icons.json":             []byte(`{"icons":[{"relPath":"svg/Jellyfin.svg","cnName":"自建影院","tags":["video-server"]}]}`),
		"pack-HEAD/svg/../../outside.svg":  []byte(testLibrarySVG),
		"pack-HEAD/svg/fake.svg":           []byte("<html/>"),
		"pack-HEAD/docs/unrelated.svg":     []byte(testLibrarySVG),
	} {
		entry, err := zipper.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("import forwarded credentials")
		}
		w.Write(archive.Bytes())
	}))
	defer server.Close()
	for i := 0; i < 2; i++ {
		if count, err := a.importIcons(server.Client(), server.URL, "HD-Icons"); err != nil || count != 3 {
			t.Fatalf("import %d failed: count=%d err=%v", i, count, err)
		}
	}
	response := adminRequest(h, "GET", "/api/icons?source=hd-icons&style=svg&keyword=zjyy", nil, cookie, "")
	var found iconIndex
	if json.Unmarshal(response.Body.Bytes(), &found) != nil || len(found.Items) != 1 || found.Items[0].RelPath != "imported/HD-Icons/svg/Jellyfin.svg" ||
		found.Items[0].Category != "media" {
		t.Fatal("import metadata and filters were not indexed")
	}
	if _, err := os.Stat(filepath.Join(a.store.paths.Data, "icons", "imported", "HD-Icons", "outside.svg")); !os.IsNotExist(err) {
		t.Fatal("archive traversal wrote a file")
	}
	if adminRequest(h, "DELETE", "/api/icons/imported/HD-Icons/svg/Jellyfin.svg", nil, cookie, "").Code != 403 {
		t.Fatal("imported icon deletion accepted")
	}
	for mirror, prefix := range map[string]string{"": "https://codeload.github.com/", "ghproxy": iconGitHubMirror + "/", "custom": "https://mirror.example/"} {
		archive, pack, err := iconArchiveURL(iconImportRequest{Mirror: mirror}, "https://mirror.example")
		if err != nil || pack != "HD-Icons" || !strings.HasPrefix(archive, prefix) {
			t.Fatalf("mirror option %q was not honored", mirror)
		}
	}
}
