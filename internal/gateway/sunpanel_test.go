package gateway

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSunPanelBackupRestoresSiblingData(t *testing.T) {
	root := t.TempDir()
	paths, err := PrepareStorage(filepath.Join(root, "data"), filepath.Join(root, "config"), filepath.Join(root, "log"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStorePaths(paths)
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := HashPassword([]byte("TEST_ONLY_SUNPANEL_PASSWORD"))
	if err := store.SetAdminAccount("admin", hash); err != nil {
		t.Fatal(err)
	}
	original := map[string][]byte{
		"sunpanel/database/database.db":       []byte("TEST_ONLY_SQLITE_SNAPSHOT"),
		"sunpanel/uploads/2026/10/image.svg":  []byte("TEST_ONLY_UPLOAD"),
		"sunpanel/custom/index.css":           []byte("body{color:red}"),
		"sunpanel/runtime/runlog/running.log": []byte("TEST_ONLY_LOG"),
		"sunpanel/conf/conf.ini":              []byte("TEST_ONLY_CONFIGURATION"),
		"sunpanel/lang/zh-cn.ini":             []byte("TEST_ONLY_LANGUAGE"),
	}
	for name, data := range original {
		file := paths.file(name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	payload, err := snapshotBackupPaths(paths, store.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	zip, err := encodeBackup(payload, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := decodeBackup(zip, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.file("sunpanel/uploads/stale"), []byte("STALE"), 0600); err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(decoded.State)
	if err := restoreFilesPaths(paths, state, decoded.Certificates, decoded.Files); err != nil {
		t.Fatal(err)
	}
	for name, want := range original {
		got, err := os.ReadFile(paths.file(name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("Sun-Panel file not restored:", name)
		}
	}
	if _, err := os.Stat(paths.file("sunpanel/uploads/stale")); !os.IsNotExist(err) {
		t.Fatal("stale upload survived")
	}
	if _, err := os.Stat(filepath.Join(paths.Data, "sunpanel")); !os.IsNotExist(err) {
		t.Fatal("data mixed into main directory")
	}
	// Backups from before the integration leave the current Sun-Panel untouched.
	if err := restoreFilesPaths(paths, state, nil, map[string][]byte{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.file("sunpanel/database/database.db")); err != nil {
		t.Fatal("old backup erased Sun-Panel")
	}
}

func TestSunPanelBackupRejectsTraversalAndSymlinks(t *testing.T) {
	for _, name := range []string{"sunpanel/../state.json", "sunpanel/uploads/../../config/state.json", "sunpanel/uploads/a\\b", "sunpanel/source/main.go", "sunpanel//conf/a"} {
		if safeBackupFile(name) {
			t.Fatal("unsafe backup path accepted")
		}
	}
	root := t.TempDir()
	paths := legacyStorage(filepath.Join(root, "data"))
	os.MkdirAll(paths.sunPanelDir(), 0700)
	os.Symlink(t.TempDir(), filepath.Join(paths.sunPanelDir(), "uploads"))
	if _, err := sunPanelBackupNames(paths); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestSunPanelPortAndRemovedConfiguration(t *testing.T) {
	c := DefaultConfig()
	c.SunPanel.Enabled = true
	for _, port := range []int{0, -1, 65536, 18080, 18443} {
		c.SunPanel.Port = port
		if Validate(c) == nil {
			t.Fatal("invalid or conflicting port accepted", port)
		}
	}
	c.SunPanel.Port = 16666
	if validateAdminPort(c, 16666) == nil {
		t.Fatal("admin port collision")
	}
	c.SunPanel.Port = 17777
	if Validate(c) != nil {
		t.Fatal("custom port rejected")
	}
	raw, _ := json.Marshal(DefaultConfig())
	var fields map[string]json.RawMessage
	json.Unmarshal(raw, &fields)
	fields["homepage"] = json.RawMessage(`{"enabled":true,"port":16680,"title":"discard"}`)
	delete(fields, "sunpanel")
	raw, _ = json.Marshal(fields)
	var got Config
	json.Unmarshal(raw, &got)
	migrateConfig(&got)
	raw, _ = json.Marshal(got)
	if bytes.Contains(raw, []byte("homepage")) || got.SunPanel.Enabled || got.SunPanel.Port != 16680 {
		t.Fatal("old homepage was retained")
	}
}

func TestSunPanelLaunchURLValidation(t *testing.T) {
	for _, value := range []string{"", "http://192.168.2.25:16680/", "https://panel.example.test/custom/?view=home#desk", "HTTP://panel.example.test/", "http://[fd00::25]:17777/", "/sunpanel/#/"} {
		c := DefaultConfig()
		c.SunPanel.LaunchURL = value
		if err := Validate(c); err != nil {
			t.Fatalf("valid launch URL rejected: %q: %v", value, err)
		}
	}
	for _, value := range []string{"javascript:alert(1)", "data:text/html,test", "//other.example.test/", "/\\other.example.test/", "panel.example.test", "http:///sunpanel/", "https://user:FAKE_PASSWORD@example.test/", "http://example.test:99999/", "http://example.test/\n"} {
		c := DefaultConfig()
		c.SunPanel.LaunchURL = value
		if Validate(c) == nil {
			t.Fatalf("invalid launch URL accepted: %q", value)
		}
	}
}

func TestSunPanelLaunchURLSavedWithoutRestart(t *testing.T) {
	a, handler := testAdmin(t)
	cookie := loginForTest(t, handler)
	s := a.store.Snapshot()
	s.Config.SunPanel.LaunchURL = "https://panel.example.test/custom/#/"
	w := adminRequest(handler, "PUT", "/api/config", map[string]any{"config": s.Config, "revision": s.Revision}, cookie, "")
	if w.Code != 200 {
		t.Fatal("launch URL save failed")
	}
	var status struct {
		Restart bool `json:"restart_required"`
	}
	w = adminRequest(handler, "GET", "/api/status", nil, cookie, "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &status) != nil || status.Restart {
		t.Fatal("launch URL change required a service restart")
	}
	reopened, err := OpenStorePaths(a.store.paths)
	if err != nil || reopened.Snapshot().Config.SunPanel.LaunchURL != s.Config.SunPanel.LaunchURL {
		t.Fatal("launch URL was not persisted")
	}
	backup, err := snapshotBackupPaths(a.store.paths, a.store.Snapshot())
	if err != nil || backup.State.Config.SunPanel.LaunchURL != s.Config.SunPanel.LaunchURL {
		t.Fatal("launch URL missing from backup")
	}
	s = a.store.Snapshot()
	s.Config.SunPanel.LaunchURL = ""
	w = adminRequest(handler, "PUT", "/api/config", map[string]any{"config": s.Config, "revision": s.Revision}, cookie, "")
	if w.Code != 200 || a.store.Snapshot().Config.SunPanel.LaunchURL != "" {
		t.Fatal("launch URL could not be cleared")
	}
}
