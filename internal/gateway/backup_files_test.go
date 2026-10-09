package gateway

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func fullBackupFixture(t *testing.T) (string, backupPayload) {
	t.Helper()
	a, _ := testAdmin(t)
	dir := filepath.Dir(a.store.path)
	state := a.store.Snapshot()
	id, err := storeRouteImage(dir, testRoutePNG(t, 32, 16))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := HashPassword([]byte("TEST_ONLY_ROUTE_PASSWORD"))
	if err != nil {
		t.Fatal(err)
	}
	state.Config.Routes = []Route{{GroupID: "default", Name: "NAS", Host: "nas.example.test", Upstream: "http://127.0.0.1:5000", Enabled: true, Image: id, Auth: RouteAuthConfig{Enabled: true, Username: "visitor", FailureLimit: 5, FreezeSeconds: 3600}}}
	state.RoutePasswordHashes = map[string]string{"default/nas.example.test": hash}
	now := time.Now().UTC()
	rule := "default/nas.example.test"
	state.IPBlocks = map[string]IPBlock{blockKey(rule, "192.0.2.20"): {IP: "192.0.2.20", Rule: rule, RuleName: "NAS", Reason: "账号验证失败", Source: "automatic", Failures: 5, StartedAt: now, Until: now.Add(time.Hour)}}
	state.ProxyPassword = "TEST_ONLY_PROXY_SECRET"
	state.Config.Subscriptions = []Subscription{{ID: "local", Name: "测试订阅", URL: "https://example.test/cidrs.txt", Enabled: true, Interval: 86400}}
	state.Config.OutboundProxy = OutboundProxyConfig{Enabled: true, URL: "http://127.0.0.1:7890", Username: "test"}
	if writeJSON(a.store.path, state) != nil {
		t.Fatal("fixture state save failed")
	}
	for _, name := range []string{"certificates", "logs", "subscriptions", "maintenance"} {
		os.MkdirAll(filepath.Join(dir, name), 0700)
	}
	cert := []byte(`{"test":"TEST_ONLY_PRIVATE_KEY"}`)
	atomicWrite(filepath.Join(dir, "certificates", "account-staging.json"), cert)
	atomicWrite(filepath.Join(dir, "certificates", "production-nas.example.test.json"), cert)
	for i, name := range []string{"calls.jsonl.1", "calls.jsonl"} {
		e := LogEntry{ID: int64(i + 1), Time: now, Category: "access", Action: "request", Remote: "192.0.2.20:50000", Rule: rule, Status: 403, Message: "测试安全事件", Outcome: "auth_failure", FreezeCreated: i == 1}
		data, _ := json.Marshal(e)
		atomicWrite(filepath.Join(dir, "logs", name), append(data, '\n'))
	}
	writeJSON(filepath.Join(dir, "subscriptions", "local.json"), subscriptionCache{URL: state.Config.Subscriptions[0].URL, FetchedAt: now, CIDRs: []string{"192.0.2.0/24", "2001:db8::/32"}})
	atomicWrite(filepath.Join(dir, "maintenance", "restore-staged.json"), []byte("TEST_ONLY_TEMPORARY"))
	// A canceled upload must not be bundled into a business-data backup.
	storeRouteImage(dir, testRoutePNG(t, 16, 32))
	payload, err := snapshotBackup(dir, state)
	if err != nil {
		t.Fatal("full snapshot failed:", err)
	}
	return dir, payload
}
func TestBackupAllPersistentDataRoundTrip(t *testing.T) {
	_, payload := fullBackupFixture(t)
	if images, logs, caches := backupFileCounts(payload); images != 1 || logs != 2 || caches != 1 || len(payload.Files) != 4 || len(payload.Certificates) != 2 {
		t.Fatal("snapshot omitted persistent data or included temporary data")
	}
	encrypted, err := encodeBackup(payload, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"TEST_ONLY_PRIVATE_KEY", "TEST_ONLY_PROXY_SECRET", payload.State.PasswordHash, payload.State.RoutePasswordHashes["default/nas.example.test"]} {
		if bytes.Contains(encrypted, []byte(value)) {
			t.Fatal("backup exposed protected data")
		}
	}
	decoded, manifest, err := decodeBackup(encrypted, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal("complete backup rejected:", err)
	}
	if manifest.Format != "gatehouse-backup-v2" {
		t.Fatal("new backup could be mistaken for an old partial format")
	}
	dest := t.TempDir()
	for _, name := range []string{"certificates", "logs", "subscriptions", "route-images"} {
		os.MkdirAll(filepath.Join(dest, name), 0700)
		atomicWrite(filepath.Join(dest, name, "stale-file"), []byte("stale"))
	}
	if restorePayload(dest, decoded) != nil {
		t.Fatal("complete restoration failed")
	}
	store, err := OpenStore(dest)
	if err != nil || !reflect.DeepEqual(store.Snapshot(), decoded.State) {
		t.Fatal("restored configuration, credentials or freeze records changed")
	}
	for name, want := range decoded.Files {
		got, err := os.ReadFile(legacyStorage(dest).file(name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("persistent file was not restored")
		}
		info, _ := os.Stat(legacyStorage(dest).file(name))
		if info.Mode().Perm() != 0600 {
			t.Fatal("restored file is not private")
		}
	}
	for name, want := range decoded.Certificates {
		got, err := os.ReadFile(filepath.Join(dest, "certificates", name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("certificate or account private key lost or changed")
		}
	}
	for _, name := range []string{"certificates", "logs", "subscriptions", "route-images"} {
		if _, err := os.Stat(filepath.Join(dest, name, "stale-file")); !os.IsNotExist(err) {
			t.Fatal("stale runtime file was retained in a complete restore")
		}
	}
	logs, err := NewLogs(dest, store)
	if err != nil || len(logs.entries) != 2 || !logs.entries[1].FreezeCreated {
		t.Fatal("security logs did not reopen")
	}
	subs, err := NewSubscriptions(dest, store)
	if err != nil || len(subs.Status()) != 1 || !subs.Status()[0].Ready || subs.Status()[0].Count != 2 {
		t.Fatal("restored cache not usable offline")
	}
	if _, err := readRouteImage(dest, store.Snapshot().Config.Routes[0].Image); err != nil {
		t.Fatal("restored image cannot be displayed")
	}
}
func TestBackupUnsafeFilesAndLegacyCompatibility(t *testing.T) {
	_, p := fullBackupFixture(t)
	for _, name := range []string{"../state.json", "/tmp/file", "logs/../state.json", "logs/calls.jsonl/other", "subscriptions/../local.json", "route-images/bad.img", "maintenance/operation.json"} {
		p.Files[name] = []byte("invalid")
		if validateBackupFiles(p) == nil {
			t.Fatal("unsafe backup path accepted")
		}
		delete(p.Files, name)
	}
	for _, name := range []string{"logs/calls.jsonl", "subscriptions/local.json", "route-images/" + p.State.Config.Routes[0].Image + ".img"} {
		old := p.Files[name]
		p.Files[name] = []byte("invalid")
		if validateBackupFiles(p) == nil {
			t.Fatal("invalid persistent data accepted")
		}
		p.Files[name] = old
	}
	imageName := "route-images/" + p.State.Config.Routes[0].Image + ".img"
	old := p.Files[imageName]
	delete(p.Files, imageName)
	if validateBackupFiles(p) == nil {
		t.Fatal("missing referenced image accepted")
	}
	p.Files[imageName] = old
	external := t.TempDir()
	symlinkDir := t.TempDir()
	os.Symlink(external, filepath.Join(symlinkDir, "logs"))
	if _, err := snapshotBackup(symlinkDir, p.State); err == nil {
		t.Fatal("symlink backup directory followed")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "logs"), 0700)
	atomicWrite(filepath.Join(dir, "logs", "calls.jsonl"), []byte("TEST_ONLY_EXISTING_LOG"))
	p.Files = nil
	p.State.Config.Routes[0].Image = ""
	zip, err := encodeBackup(p, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	legacy, legacyManifest, err := decodeBackup(zip, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil || legacy.Files != nil || legacyManifest.Format != "gatehouse-backup-v1" {
		t.Fatal("legacy backup incompatible")
	}
	if restorePayload(dir, legacy) != nil {
		t.Fatal("legacy restore failed")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "logs", "calls.jsonl"))
	if string(data) != "TEST_ONLY_EXISTING_LOG" {
		t.Fatal("legacy backup erased data it never included")
	}
}
func TestBackupRestoreAtomicFailureAndSupervisorRollback(t *testing.T) {
	source, p := fullBackupFixture(t)
	dest := t.TempDir()
	for _, name := range []string{"certificates", "logs", "subscriptions", "route-images"} {
		os.MkdirAll(filepath.Join(dest, name), 0700)
		atomicWrite(filepath.Join(dest, name, "keep"), []byte("TEST_ONLY_EXISTING"))
	}
	os.Mkdir(filepath.Join(dest, "state.json"), 0700)
	if restorePayload(dest, p) == nil {
		t.Fatal("restore unexpectedly succeeded")
	}
	for _, name := range []string{"certificates", "logs", "subscriptions", "route-images"} {
		data, _ := os.ReadFile(filepath.Join(dest, name, "keep"))
		if string(data) != "TEST_ONLY_EXISTING" {
			t.Fatal("failed state write did not roll back directories")
		}
	}
	app := t.TempDir()
	atomicWrite(filepath.Join(app, "gatehouse"), []byte("TEST_ONLY_PROGRAM"))
	m, err := NewMaintenance(source, app)
	if err != nil {
		t.Fatal(err)
	}
	before, err := snapshotDisk(source)
	if err != nil {
		t.Fatal(err)
	}
	p.State.Config.Routes[0].Name = "changed"
	p.Files = map[string][]byte{"route-images/" + p.State.Config.Routes[0].Image + ".img": p.Files["route-images/"+p.State.Config.Routes[0].Image+".img"]}
	p.Certificates = map[string][]byte{}
	writeJSON(filepath.Join(m.dir, "restore-staged.json"), p)
	writeJSON(filepath.Join(m.dir, "operation.json"), maintenanceOperation{Kind: "restore", Version: Version})
	if active, err := m.beginTransition(); !active || err != nil {
		t.Fatal("full maintenance restore failed")
	}
	if err := m.rollbackTransition(); err != nil {
		t.Fatal("supervisor full data rollback failed:", err)
	}
	after, err := snapshotDisk(source)
	var beforeState, afterState any
	json.Unmarshal(before.State, &beforeState)
	json.Unmarshal(after.State, &afterState)
	if err != nil || !reflect.DeepEqual(beforeState, afterState) || !reflect.DeepEqual(before.Files, after.Files) || !reflect.DeepEqual(before.Certificates, after.Certificates) {
		t.Fatal("rollback did not recover every persistent file and original state")
	}
	// Old launchers must not silently perform a partial restore of a new backup.
	m.backupFiles = false
	writeJSON(filepath.Join(m.dir, "restore-staged.json"), p)
	m.supervised = true
	m.sunPanelFiles = true
	m.stage = &maintenanceStage{ID: "test-stage", Kind: "restore", Version: Version, Created: time.Now()}
	if runtime.GOOS == "linux" {
		if err := m.schedule("test-stage", "restore"); err == nil {
			t.Fatal("old launcher accepted incomplete restore")
		}
	}
}
