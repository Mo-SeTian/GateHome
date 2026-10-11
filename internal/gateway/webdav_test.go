package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

func davFixture(t *testing.T, wrap func(http.Handler) http.Handler) (State, string) {
	t.Helper()
	root := t.TempDir()
	handler := &webdav.Handler{Prefix: "/dav", FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()}
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "TEST_ONLY_DAV_USER" || password != "TEST_ONLY_STORAGE_PASSWORD" {
			w.WriteHeader(401)
			return
		}
		handler.ServeHTTP(w, r)
	})
	if wrap != nil {
		h = wrap(h)
	}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	state := State{WebDAVBackup: WebDAVBackupState{InstanceID: strings.Repeat("a", 24), Password: "TEST_ONLY_STORAGE_PASSWORD", BackupPassword: "TEST_ONLY_BACKUP_PASSWORD", Config: WebDAVBackupConfig{URL: server.URL + "/dav/nested/备份/", Username: "TEST_ONLY_DAV_USER", Keep: 2}}}
	return state, root
}
func davName(id, date, nonce string) string {
	return "gatehome-backup-" + id + "-" + date + "-" + nonce + ".zip"
}
func TestWebDAVAtomicUploadVerificationAndOwnedRetention(t *testing.T) {
	state, root := davFixture(t, nil)
	client, err := newWebDAVClient(state)
	if err != nil {
		t.Fatal("client initialization failed")
	}
	defer client.close()
	ctx := context.Background()
	if client.ensureCollection(ctx, client.base, 12) != nil {
		t.Fatal("nested collection creation failed")
	}
	folder := filepath.Join(root, "nested", "备份")
	own := state.WebDAVBackup.InstanceID
	foreign := davName(strings.Repeat("b", 24), "20261011T030000Z", "bbbbbbbbbbbb")
	old := davName(own, "20260101T030000Z", "aaaaaaaaaaaa")
	os.WriteFile(filepath.Join(folder, old), []byte("TEST_ONLY_OLD_BACKUP"), 0600)
	os.WriteFile(filepath.Join(folder, foreign), []byte("TEST_ONLY_FOREIGN_BACKUP"), 0600)
	os.WriteFile(filepath.Join(folder, "family-photo.png"), []byte("TEST_ONLY_UNRELATED"), 0600)
	current := davName(own, "20261011T030000Z", "111111111111")
	if _, err := client.upload(ctx, current, []byte("TEST_ONLY_ARCHIVE"), 2); err != nil {
		t.Fatal("upload/verify failed")
	}
	// Same-second uploads must retain the newly verified file, irrespective of nonce ordering.
	newer := davName(own, "20261011T030000Z", "000000000000")
	if _, err := client.upload(ctx, newer, []byte("TEST_ONLY_NEW_ARCHIVE"), 1); err != nil {
		t.Fatal("retention upload failed")
	}
	rows, err := client.list(ctx)
	if err != nil || len(rows) != 2 {
		t.Fatal("remote backup list omitted another instance's backup")
	}
	owned := 0
	for _, row := range rows {
		if client.owns(row.Name) {
			owned++
			if row.Name != newer {
				t.Fatal("owned retention did not keep current backup")
			}
		}
	}
	if owned != 1 {
		t.Fatal("owned retention did not keep current backup")
	}
	for _, name := range []string{foreign, "family-photo.png"} {
		if _, err := os.Stat(filepath.Join(folder, name)); err != nil {
			t.Fatal("unrelated or another instance's file deleted")
		}
	}
	if client.remove(ctx, foreign) == nil {
		t.Fatal("another instance's backup deletion accepted")
	}
	if _, err := client.get(ctx, "../"+foreign); err == nil || client.remove(ctx, "../family-photo.png") == nil {
		t.Fatal("path traversal accepted")
	}
	parts, _ := filepath.Glob(filepath.Join(folder, "*.part"))
	if len(parts) != 0 {
		t.Fatal("successful upload left staging files")
	}
}

func TestWebDAVCorruptionPreservesPriorBackup(t *testing.T) {
	state, root := davFixture(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				w.Write([]byte("TEST_ONLY_CORRUPTED"))
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	client, _ := newWebDAVClient(state)
	defer client.close()
	ctx := context.Background()
	client.ensureCollection(ctx, client.base, 12)
	prior := davName(client.id, "20260101T030000Z", "111111111111")
	os.WriteFile(filepath.Join(root, "nested", "备份", prior), []byte("TEST_ONLY_OLD"), 0600)
	current := davName(client.id, "20261011T030000Z", "222222222222")
	if _, err := client.upload(ctx, current, []byte("TEST_ONLY_CORRECT"), 1); err == nil {
		t.Fatal("corruption not detected")
	}
	if _, err := os.Stat(filepath.Join(root, "nested", "备份", prior)); err != nil {
		t.Fatal("old backup removed before verification")
	}
}

func TestWebDAVDoesNotForwardCredentialsOnRedirect(t *testing.T) {
	var calls atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer other.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 302) }))
	defer redirect.Close()
	state := State{WebDAVBackup: WebDAVBackupState{Password: "TEST_ONLY_STORAGE_PASSWORD", Config: WebDAVBackupConfig{URL: redirect.URL + "/dav/", Username: "TEST_ONLY_DAV_USER"}}}
	client, _ := newWebDAVClient(state)
	defer client.close()
	if _, err := client.list(context.Background()); err == nil || calls.Load() != 0 {
		t.Fatal("redirect followed or credentials forwarded")
	}
}

func TestWebDAVScheduleTimezoneWeeklyAndRestartSlots(t *testing.T) {
	config := normalizeWebDAVConfig(WebDAVBackupConfig{ScheduleEnabled: true})
	before := time.Date(2026, 10, 11, 18, 59, 0, 0, time.UTC) // Shanghai: 02:59 on October 12.
	key, slot := webDAVScheduleSlot(config, before)
	if slot.In(time.FixedZone("Shanghai", 8*3600)).Format("2006-01-02 15:04") != "2026-10-11 03:00" {
		t.Fatal("host timezone affected schedule")
	}
	after := before.Add(time.Minute)
	nextKey, _ := webDAVScheduleSlot(config, after)
	if nextKey == key {
		t.Fatal("daily due slot did not advance")
	}
	saved := WebDAVBackupState{Config: config, LastSlot: nextKey}
	next := webDAVNextRun(saved, after)
	if next == nil || next.Sub(after) != 24*time.Hour {
		t.Fatal("restart would duplicate completed schedule")
	}
	config.ScheduleMode = "weekly"
	config.Weekday = 1
	_, weekly := webDAVScheduleSlot(config, after)
	if weekly.Weekday() != time.Monday || weekly.After(after) {
		t.Fatal("weekly slot incorrect")
	}
}

func TestWebDAVSecretsAndCompleteBackupRestore(t *testing.T) {
	a, _ := testAdmin(t)
	state, root := davFixture(t, nil)
	config := normalizeWebDAVConfig(state.WebDAVBackup.Config)
	password, backupPassword := state.WebDAVBackup.Password, state.WebDAVBackup.BackupPassword
	if a.store.saveWebDAVBackup(config, &password, &backupPassword, a.store.Snapshot().Revision) != nil {
		t.Fatal("settings save failed")
	}
	cfg := a.store.Snapshot()
	cfg.WebDAVBackup.Config.ScheduleEnabled = true
	archiveState := cfg
	paths := a.store.paths
	for _, entry := range []struct{ name, data string }{{"sunpanel/conf/conf.ini", "TEST_ONLY_PANEL_CONF"}, {"sunpanel/database/database.db", "TEST_ONLY_PANEL_DB"}, {"sunpanel/uploads/icons/test.png", "TEST_ONLY_PANEL_IMAGE"}, {"sunpanel/custom/index.css", "TEST_ONLY_CUSTOM"}} {
		target := filepath.Join(paths.sunPanelDir(), strings.TrimPrefix(entry.name, "sunpanel/"))
		os.MkdirAll(filepath.Dir(target), 0700)
		os.WriteFile(target, []byte(entry.data), 0600)
	}
	payload, err := snapshotBackupPaths(paths, archiveState)
	if err != nil {
		t.Fatal("snapshot failed")
	}
	encoded, err := encodeBackup(payload, backupPassword)
	if err != nil {
		t.Fatal("backup encryption failed")
	}
	client, _ := newWebDAVClient(a.store.Snapshot())
	defer client.close()
	name := davName(client.id, "20261011T030000Z", "123456abcdef")
	if _, err := client.upload(context.Background(), name, encoded, 2); err != nil {
		t.Fatal("complete backup upload failed")
	}
	// A replacement installation can discover and restore the prior installation's backups.
	replacement := a.store.Snapshot()
	replacement.WebDAVBackup.InstanceID = strings.Repeat("b", 24)
	recoveryClient, _ := newWebDAVClient(replacement)
	defer recoveryClient.close()
	files, err := recoveryClient.list(context.Background())
	if err != nil || len(files) != 1 || files[0].Name != name {
		t.Fatal("replacement installation cannot discover prior backup")
	}
	downloaded, err := recoveryClient.get(context.Background(), name)
	if err != nil || !bytes.Equal(downloaded, encoded) {
		t.Fatal("remote download mismatch")
	}
	restored, _, err := decodeBackup(downloaded, backupPassword)
	if err != nil {
		t.Fatal("remote backup decode failed")
	}
	for _, name := range []string{"sunpanel/conf/conf.ini", "sunpanel/database/database.db", "sunpanel/uploads/icons/test.png", "sunpanel/custom/index.css"} {
		if len(restored.Files[name]) == 0 {
			t.Fatal("Sun-Panel persistent data omitted")
		}
	}
	if restored.State.WebDAVBackup.Password != password || restored.State.WebDAVBackup.BackupPassword != backupPassword || restored.State.PasswordHash != cfg.PasswordHash {
		t.Fatal("saved settings or account omitted")
	}
	public := httptest.NewRecorder()
	a.webDAVResponse(public)
	for _, secret := range []string{password, backupPassword} {
		if strings.Contains(public.Body.String(), secret) {
			t.Fatal("credential returned by settings API")
		}
	}
	// Restore into an isolated empty runtime, using the same filesystem restoration path as maintenance.
	destination := t.TempDir()
	newPaths, err := PrepareStorage(filepath.Join(destination, "data"), filepath.Join(destination, "config"), filepath.Join(destination, "log"))
	if err != nil {
		t.Fatal("restore paths failed")
	}
	stateData, _ := json.Marshal(restored.State)
	if restoreFilesPaths(newPaths, stateData, restored.Certificates, restored.Files) != nil {
		t.Fatal("complete file restore failed")
	}
	recovered, err := os.ReadFile(filepath.Join(newPaths.sunPanelDir(), "uploads/icons/test.png"))
	if err != nil || string(recovered) != "TEST_ONLY_PANEL_IMAGE" {
		t.Fatal("Sun-Panel picture was not restored")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("fixture lost")
	}
}

func TestWebDAVBackgroundJobAndPersistence(t *testing.T) {
	a, _ := testAdmin(t)
	state, _ := davFixture(t, nil)
	settings := normalizeWebDAVConfig(state.WebDAVBackup.Config)
	password, backupPassword := state.WebDAVBackup.Password, state.WebDAVBackup.BackupPassword
	if a.store.saveWebDAVBackup(settings, &password, &backupPassword, a.store.Snapshot().Revision) != nil {
		t.Fatal("configuration failed")
	}
	maintenance, err := NewMaintenancePaths(a.store.paths, "")
	if err != nil {
		t.Fatal("maintenance initialization failed")
	}
	a.SetMaintenance(maintenance)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.webDAV.ctx = ctx
	if err := a.startWebDAVBackup("TEST_ONLY_SLOT"); err != nil {
		t.Fatal("job did not start")
	}
	if err := a.startWebDAVBackup(""); err == nil {
		t.Fatal("concurrent job accepted")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		a.webDAV.mu.Lock()
		busy := a.webDAV.busy
		a.webDAV.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.webDAV.mu.Lock()
	job := a.webDAV.job
	busy := a.webDAV.busy
	a.webDAV.mu.Unlock()
	if busy || job.Phase != "done" || job.Name == "" {
		t.Fatal("background job did not complete")
	}
	saved := a.store.Snapshot()
	if saved.WebDAVBackup.LastSlot != "TEST_ONLY_SLOT" || saved.WebDAVBackup.LastSuccess.IsZero() {
		t.Fatal("schedule state not persisted")
	}
	data, err := os.ReadFile(a.store.path)
	if err != nil {
		t.Fatal("state not persisted")
	}
	var restored State
	if json.Unmarshal(data, &restored) != nil || restored.WebDAVBackup.LastFile != job.Name {
		t.Fatal("restart lost job state")
	}
	// Blank password fields preserve stored credentials; changing the storage account clears the old password.
	if a.store.saveWebDAVBackup(settings, nil, nil, saved.Revision) != nil || a.store.Snapshot().WebDAVBackup.Password != password {
		t.Fatal("blank password did not preserve credential")
	}
	settings.Username = "TEST_ONLY_ANOTHER_ACCOUNT"
	if a.store.saveWebDAVBackup(settings, nil, nil, a.store.Snapshot().Revision) != nil || a.store.Snapshot().WebDAVBackup.Password != "" {
		t.Fatal("old credential carried to another account")
	}
}

func TestWebDAVSchedulerCatchesUpOnce(t *testing.T) {
	a, _ := testAdmin(t)
	state, _ := davFixture(t, nil)
	password, backupPassword := state.WebDAVBackup.Password, state.WebDAVBackup.BackupPassword
	settings := normalizeWebDAVConfig(state.WebDAVBackup.Config)
	settings.ScheduleEnabled = true
	if a.store.saveWebDAVBackup(settings, &password, &backupPassword, a.store.Snapshot().Revision) != nil {
		t.Fatal("scheduler configuration failed")
	}
	maintenance, err := NewMaintenancePaths(a.store.paths, "")
	if err != nil {
		t.Fatal("maintenance initialization failed")
	}
	a.SetMaintenance(maintenance)
	a.store.mu.Lock()
	next := a.store.state
	next.WebDAVBackup.LastSlot = "TEST_ONLY_MISSED_SLOT"
	a.store.state = next
	a.store.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.StartWebDAVBackups(ctx)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !a.store.Snapshot().WebDAVBackup.LastSuccess.IsZero() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	slot, _ := webDAVScheduleSlot(settings, time.Now())
	if a.store.Snapshot().WebDAVBackup.LastSlot != slot || a.store.Snapshot().WebDAVBackup.LastSuccess.IsZero() {
		t.Fatal("missed slot did not execute")
	}
	client, _ := newWebDAVClient(a.store.Snapshot())
	defer client.close()
	first, _ := client.list(context.Background())
	if len(first) != 1 {
		t.Fatal("catch-up backup missing")
	}
	a.StartWebDAVBackups(ctx)
	time.Sleep(50 * time.Millisecond)
	second, _ := client.list(context.Background())
	if len(second) != 1 {
		t.Fatal("restart duplicated completed slot")
	}
}

func TestWebDAVMoveFailureCleansStagingAndPreservesOldFiles(t *testing.T) {
	state, root := davFixture(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "MOVE" {
				w.WriteHeader(503)
				w.Write([]byte("TEST_ONLY_STORAGE_PASSWORD"))
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	client, _ := newWebDAVClient(state)
	defer client.close()
	ctx := context.Background()
	if client.ensureCollection(ctx, client.base, 12) != nil {
		t.Fatal("collection failed")
	}
	name := davName(client.id, "20261011T030000Z", "112233aabbcc")
	_, err := client.upload(ctx, name, []byte("TEST_ONLY_PAYLOAD"), 1)
	if err == nil || strings.Contains(err.Error(), "TEST_ONLY_STORAGE_PASSWORD") {
		t.Fatal("MOVE failure ignored or response credential exposed")
	}
	parts, _ := filepath.Glob(filepath.Join(root, "nested", "备份", "*.part"))
	if len(parts) != 0 {
		t.Fatal("failed MOVE left temporary file")
	}
	files, _ := client.list(ctx)
	if len(files) != 0 {
		t.Fatal("failed MOVE created complete backup")
	}
}

func TestWebDAVBackupSettingsValidation(t *testing.T) {
	valid := WebDAVBackupState{InstanceID: strings.Repeat("a", 24), Config: normalizeWebDAVConfig(WebDAVBackupConfig{URL: "https://dav.example.test/backups/"}), BackupPassword: "TEST_ONLY_BACKUP_PASSWORD"}
	if validateWebDAVBackupState(valid) != nil {
		t.Fatal("valid settings rejected")
	}
	for _, kind := range []string{"identifier", "password", "timezone", "keep", "credentials-in-url"} {
		state := valid
		switch kind {
		case "identifier":
			state.InstanceID = "../outside"
		case "password":
			state.BackupPassword = ""
		case "timezone":
			state.Config.Timezone = "TEST_ONLY_INVALID_ZONE"
		case "keep":
			state.Config.Keep = 101
		case "credentials-in-url":
			state.Config.URL = "https://user:TEST_ONLY_PASSWORD@dav.example.test/"
		}
		if validateWebDAVBackupState(state) == nil {
			t.Fatal("invalid backup configuration accepted")
		}
	}
}
