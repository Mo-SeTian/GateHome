package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCapacityIncludesEncodingAndBlocksBeforeBackupAndDownload(t *testing.T) {
	a, _ := testAdmin(t)
	root := filepath.Join(a.store.paths.sunPanelDir(), "custom")
	if os.MkdirAll(root, 0700) != nil {
		t.Fatal("capacity fixture failed")
	}
	file, err := os.Create(filepath.Join(root, "TEST_ONLY_CAPACITY"))
	if err != nil {
		t.Fatal("capacity fixture failed")
	}
	file.Truncate(50 << 20)
	file.Close()
	state := a.store.Snapshot()
	capacity, err := inspectBackupCapacity(a.store.paths, state)
	if err != nil || capacity.CanProceed || capacity.SunPanelBytes != 50<<20 || capacity.RawBytes > maxBackupBytes || capacity.SnapshotBytes <= maxBackupBytes {
		t.Fatal("encoding overhead was not counted")
	}
	a.SetMaintenance(&Maintenance{paths: a.store.paths})
	cookie := loginForTest(t, a.Handler())
	w := adminRequest(a.Handler(), "POST", "/api/maintenance/backup", map[string]string{"password": "TEST_ONLY_BACKUP_PASSWORD"}, cookie, "")
	if w.Code != 400 {
		t.Fatal("over-capacity backup was accepted")
	}
	if err := a.runOnlineUpdate(state, "99.0.0"); err == nil {
		t.Fatal("over-capacity download was not rejected before network access")
	}
	w = adminRequest(a.Handler(), "GET", "/api/maintenance/capacity", nil, cookie, "")
	var output backupCapacity
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &output) != nil || output.CanProceed {
		t.Fatal("capacity API did not expose the blocker")
	}
}

func TestCapacityEstimateCoversActualEncodedSnapshot(t *testing.T) {
	a, _ := testAdmin(t)
	path := filepath.Join(a.store.paths.sunPanelDir(), "custom", "TEST_ONLY_FILE")
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("TEST_ONLY_PAYLOAD"), 0600)
	state := a.store.Snapshot()
	c, err := inspectBackupCapacity(a.store.paths, state)
	if err != nil || !c.CanProceed {
		t.Fatal("small valid snapshot was rejected")
	}
	actual, err := snapshotBackupPaths(a.store.paths, state)
	if err != nil {
		t.Fatal("snapshot fixture failed")
	}
	encoded, _ := json.MarshalIndent(actual, "", "  ")
	if c.SnapshotBytes != int64(len(encoded)) {
		t.Fatal("stat-based estimate did not match the encoded snapshot")
	}
}

func TestRouteAssociationIDSurvivesEditsAndReload(t *testing.T) {
	a, _ := testAdmin(t)
	s := a.store.Snapshot()
	s.Config.Routes = []Route{{GroupID: s.Config.Groups[0].ID, Name: "NAS", Host: "nas.example.test", Upstream: "http://192.168.1.2:5000", Enabled: true}}
	if a.store.Update(s.Config, nil, s.Revision) != nil {
		t.Fatal("initial route failed")
	}
	s = a.store.Snapshot()
	id := s.Config.Routes[0].ID
	s.Config.Routes[0].Host = "renamed.example.test"
	s.Config.Routes[0].Upstream = "http://192.168.1.3:5000"
	if a.store.Update(s.Config, nil, s.Revision) != nil {
		t.Fatal("route edit failed")
	}
	reopened, err := OpenStorePaths(a.store.paths)
	if err != nil || id == "" || reopened.Snapshot().Config.Routes[0].ID != id {
		t.Fatal("source identity changed after editing or reopening")
	}
}
