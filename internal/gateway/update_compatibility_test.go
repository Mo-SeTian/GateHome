package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdatesRemainAvailableWithoutHistory(t *testing.T) {
	for _, history := range []bool{false, true} {
		for _, online := range []bool{false, true} {
			name := "legacy"
			if history {
				name = "history"
			}
			if online {
				name += "/online"
			} else {
				name += "/upload"
			}
			t.Run(name, func(t *testing.T) {
				for _, key := range []string{"GATEHOUSE_SUPERVISED", "GATEHOUSE_BACKUP_FILES", "GATEHOUSE_SUNPANEL_STORAGE"} {
					t.Setenv(key, "1")
				}
				t.Setenv("GATEHOUSE_UPDATE_HISTORY", "0")
				if history {
					t.Setenv("GATEHOUSE_UPDATE_HISTORY", "1")
				}
				a, _ := testAdmin(t)
				m, err := NewMaintenancePaths(a.store.paths, t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				if !m.Available() {
					t.Skip("managed updates require Linux")
				}
				a.SetMaintenance(m)
				archive := testRelease(t, "99.0.0-beta.1", releaseArch())
				preview, err := m.inspectUpdate(archive)
				if err != nil || preview["can_apply"] != true {
					t.Fatal("a valid update was blocked by the history capability")
				}
				if !history && !strings.Contains(preview["message"].(string), "不支持") {
					t.Fatal("legacy preview omitted its history limitation")
				}
				h := a.Handler()
				cookie := loginForTest(t, h)
				path := "/api/maintenance/apply-update"
				input := map[string]string{"id": preview["id"].(string)}
				if online {
					// Reuse a verified download through the real API without external traffic.
					release := onlineRelease{Channel: betaChannel, Version: "99.0.0-beta.1", UpdateAvailable: true, digest: digestBytes(archive)}
					a.releaseCache.beta = onlineReleaseCacheEntry{key: onlineReleaseCacheKey(a.store.Snapshot()), release: release, expires: time.Now().Add(time.Minute)}
					a.onlineUpdate.preparedID, a.onlineUpdate.preparedDigest = preview["id"].(string), release.digest
					path = "/api/maintenance/download-online-update"
					input = map[string]string{"channel": "beta", "version": release.Version}
				}
				response := adminRequest(h, "POST", path, input, cookie, "")
				if response.Code != 202 {
					t.Fatalf("update was rejected: HTTP %d", response.Code)
				}
				if !history && strings.Contains(response.Body.String(), "先备份") {
					t.Fatal("legacy update promised an unavailable history backup")
				}
				select {
				case <-m.RestartSignal():
				case <-time.After(3 * time.Second):
					t.Fatal("update did not reach the launcher")
				}
				a.updateMu.Lock()
				defer a.updateMu.Unlock()
				data, err := os.ReadFile(filepath.Join(m.dir, "operation.json"))
				var op maintenanceOperation
				if err != nil || json.Unmarshal(data, &op) != nil || op.Kind != "update" || op.Version != "99.0.0-beta.1" || op.Digest == "" {
					t.Fatal("verified update was not staged correctly")
				}
				if (history && op.FromVersion != Version) || (!history && op.FromVersion != "") {
					t.Fatal("update did not match the launcher's backup capability")
				}
			})
		}
	}
}

func TestLegacyUpdateRetainsSunPanelGuard(t *testing.T) {
	t.Setenv("GATEHOUSE_SUPERVISED", "1")
	t.Setenv("GATEHOUSE_SUNPANEL_STORAGE", "0")
	t.Setenv("GATEHOUSE_UPDATE_HISTORY", "0")
	a, _ := testAdmin(t)
	m, err := NewMaintenancePaths(a.store.paths, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !m.Available() {
		t.Skip("managed updates require Linux")
	}
	a.SetMaintenance(m)
	preview, err := m.inspectUpdate(testRelease(t, "99.0.0-beta.1", releaseArch()))
	if err != nil || preview["can_apply"] != false {
		t.Fatal("an update without Sun-Panel data protection was allowed")
	}
	h := a.Handler()
	cookie := loginForTest(t, h)
	w := adminRequest(h, "POST", "/api/maintenance/apply-update", map[string]string{"id": preview["id"].(string)}, cookie, "")
	if w.Code != 400 || !strings.Contains(w.Body.String(), "Sun-Panel") {
		t.Fatal("offline update bypassed the Sun-Panel guard")
	}
	w = adminRequest(h, "POST", "/api/maintenance/download-online-update", map[string]string{"channel": "beta", "version": "99.0.0-beta.1"}, cookie, "")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "Sun-Panel") || m.Busy() {
		t.Fatal("online update bypassed the Sun-Panel guard")
	}
	if _, err := os.Stat(filepath.Join(m.dir, "operation.json")); !os.IsNotExist(err) {
		t.Fatal("an incompatible launcher received an update operation")
	}
}
