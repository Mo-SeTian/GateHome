package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func updateHistoryFixture(t *testing.T) (*Maintenance, []byte, []byte) {
	t.Helper()
	dir, _ := fullBackupFixture(t)
	app := t.TempDir()
	m, err := NewMaintenance(dir, app)
	if err != nil {
		t.Fatal(err)
	}
	sun := filepath.Join(m.paths.sunPanelDir(), "custom", "TEST_ONLY_DATA")
	if err := os.MkdirAll(filepath.Dir(sun), 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(sun, []byte("TEST_ONLY_SUNPANEL")); err != nil {
		t.Fatal(err)
	}
	old, next := testELF(releaseArch()), testELF(releaseArch())
	old[63], next[63] = 1, 2
	if err := writeProgram(filepath.Join(app, "gatehouse"), old); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(m.dir, "update-staged"), next); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(m.dir, "operation.json"), maintenanceOperation{Kind: "update", FromVersion: Version, Version: "99.0.0-beta.1", Digest: digestBytes(next)}); err != nil {
		t.Fatal(err)
	}
	return m, old, next
}

func TestUpdateBackupAndHistoryRollbackRestoreProgramAndAllData(t *testing.T) {
	m, old, next := updateHistoryFixture(t)
	before, err := snapshotDiskPaths(m.paths)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := m.beginTransition(); err != nil || !changed {
		t.Fatalf("update did not begin: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(m.appDir, "gatehouse"))
	if !bytes.Equal(got, next) {
		t.Fatal("new program was not installed")
	}
	list, err := m.listUpdateBackups()
	if err != nil || len(list) != 1 || list[0].Version != Version || list[0].TargetVersion != "99.0.0-beta.1" {
		t.Fatal("update point missing")
	}
	_, stored, program, err := m.loadUpdateBackup(list[0].ID)
	var beforeState, storedState any
	json.Unmarshal(before.State, &beforeState)
	json.Unmarshal(stored.State, &storedState)
	if err != nil || !reflect.DeepEqual(storedState, beforeState) || !reflect.DeepEqual(stored.Files, before.Files) ||
		!reflect.DeepEqual(stored.Certificates, before.Certificates) || !bytes.Equal(program, old) {
		t.Fatal("pre-update program or data omitted")
	}
	m.recordUpdateResult(true, "TEST_ONLY_SUCCESS")
	if result := m.lastUpdateResult(); result == nil || !result.OK || result.Version != "99.0.0-beta.1" {
		t.Fatal("maintenance outcome not retained")
	}
	if err := m.finishTransition(); err != nil {
		t.Fatal(err)
	}
	current := State{}
	if json.Unmarshal(before.State, &current) != nil {
		t.Fatal("state fixture invalid")
	}
	current.Revision += 10
	current.Config.Zone = "changed.example.test"
	if err := writeJSON(m.paths.file("state.json"), current); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(m.paths.sunPanelDir(), "custom", "TEST_ONLY_DATA"), []byte("TEST_ONLY_CHANGED")); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(m.dir, "operation.json"), maintenanceOperation{Kind: "rollback", Version: list[0].Version, BackupID: list[0].ID}); err != nil {
		t.Fatal(err)
	}
	if changed, err := m.beginTransition(); err != nil || !changed {
		t.Fatalf("history restore failed: %v", err)
	}
	after, err := snapshotDiskPaths(m.paths)
	if err != nil {
		t.Fatal(err)
	}
	var restored State
	if json.Unmarshal(after.State, &restored) != nil {
		t.Fatal("restored state invalid")
	}
	var original State
	json.Unmarshal(before.State, &original)
	if restored.Revision != current.Revision+1 || restored.Config.Zone != original.Config.Zone || !reflect.DeepEqual(after.Files, before.Files) || !reflect.DeepEqual(after.Certificates, before.Certificates) {
		t.Fatal("historical program/data not restored consistently")
	}
	got, _ = os.ReadFile(filepath.Join(m.appDir, "gatehouse"))
	if !bytes.Equal(got, old) {
		t.Fatal("historical program was not restored")
	}
	// The same failed-start recovery must also undo a requested historical restore.
	if err := m.rollbackTransition(); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(filepath.Join(m.appDir, "gatehouse"))
	data, _ := os.ReadFile(m.paths.file("state.json"))
	if !bytes.Equal(got, next) || !bytes.Contains(data, []byte("changed.example.test")) {
		t.Fatal("failed history restore lost the working version")
	}
}

func TestBackupFailureAndTamperPreventProgramReplacement(t *testing.T) {
	for _, failure := range []string{"backup-directory", "staged-digest"} {
		t.Run(failure, func(t *testing.T) {
			m, old, _ := updateHistoryFixture(t)
			before, _ := os.ReadFile(m.paths.file("state.json"))
			if failure == "backup-directory" {
				atomicWrite(m.updateBackupDir(), []byte("TEST_ONLY_BLOCKER"))
			} else {
				atomicWrite(filepath.Join(m.dir, "update-staged"), []byte("TEST_ONLY_CORRUPT"))
			}
			changed, err := m.beginTransition()
			got, _ := os.ReadFile(filepath.Join(m.appDir, "gatehouse"))
			after, _ := os.ReadFile(m.paths.file("state.json"))
			if err == nil || changed || !bytes.Equal(got, old) || !bytes.Equal(before, after) {
				t.Fatal("failed backup/validation mutated the running version")
			}
			if _, err := os.Stat(filepath.Join(m.dir, "transition.json")); !os.IsNotExist(err) {
				t.Fatal("failed preparation left active transition")
			}
		})
	}
}

func TestUpdateBackupRetentionIntegrityAndPrivateFiles(t *testing.T) {
	m, _, _ := updateHistoryFixture(t)
	if _, err := m.beginTransition(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshotDiskPaths(m.paths)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(snapshot)
	for i := 1; i <= 4; i++ {
		if err := m.saveUpdateBackup(Version, fmt.Sprintf("99.0.%d", i), data); err != nil {
			t.Fatal(err)
		}
	}
	list, err := m.listUpdateBackups()
	if err != nil || len(list) != updateBackupRetention {
		t.Fatal("history retention not enforced")
	}
	id := list[0].ID
	for _, name := range []string{"gatehouse", "snapshot.json", "metadata.json"} {
		path := filepath.Join(m.updateBackupDir(), id, name)
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("backup file is not private")
		}
		original, _ := os.ReadFile(path)
		if err := atomicWrite(path, []byte("TEST_ONLY_CORRUPT")); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := m.loadUpdateBackup(id); err == nil {
			t.Fatal("corrupt backup accepted")
		}
		if err := atomicWrite(path, original); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"../escape", strings.Repeat("a", 31), strings.Repeat("A", 32)} {
		if _, _, _, err := m.loadUpdateBackup(id); err == nil {
			t.Fatal("invalid backup ID accepted")
		}
	}
	dir := filepath.Join(m.updateBackupDir(), id)
	if err := os.Rename(dir, dir+"-real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir+"-real", dir); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := m.loadUpdateBackup(id); err == nil {
		t.Fatal("symlink backup accepted")
	}
}

func TestUpdateHistoryAPIAuthAndLegacyLauncherGuard(t *testing.T) {
	a, _ := testAdmin(t)
	m, err := NewMaintenance(filepath.Dir(a.store.path), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.SetMaintenance(m)
	h := a.Handler()
	cookie := loginForTest(t, h)
	for _, endpoint := range []string{"update-backups", "rollback-update"} {
		method := "GET"
		var body any
		if endpoint == "rollback-update" {
			method = "POST"
			body = map[string]string{"id": strings.Repeat("a", 32)}
		}
		if w := adminRequest(h, method, "/api/maintenance/"+endpoint, body, nil, ""); w.Code != 401 {
			t.Fatal("history accessible without authentication")
		}
		if method == "POST" {
			if w := adminRequest(h, method, "/api/maintenance/"+endpoint, body, cookie, "https://evil.example"); w.Code != 403 {
				t.Fatal("cross-origin rollback accepted")
			}
		}
	}
	w := adminRequest(h, "GET", "/api/maintenance/update-backups", nil, cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"backups":[]`) {
		t.Fatal("empty history unavailable")
	}
	for _, endpoint := range []string{"rollback-update", "download-online-update"} {
		w := adminRequest(h, "POST", "/api/maintenance/"+endpoint, map[string]string{"id": strings.Repeat("a", 32), "version": "99.0.0"}, cookie, "")
		if w.Code != 400 { // Unknown JSON fields must be rejected before scheduling.
			t.Fatal("invalid maintenance body accepted")
		}
	}
	if w := adminRequest(h, "POST", "/api/maintenance/rollback-update", map[string]string{"id": strings.Repeat("a", 32)}, cookie, ""); w.Code != 409 || m.Busy() {
		t.Fatal("legacy launcher scheduled history restore")
	}
}

func TestContainerRestartPreservesExplicitRollback(t *testing.T) {
	m, _, _ := updateHistoryFixture(t)
	if err := atomicWrite(filepath.Join(m.dir, "container-image-version"), []byte(Version)); err != nil {
		t.Fatal(err)
	}
	if err := m.syncContainerImage(context.Background(), "TEST_ONLY_NONEXISTENT_BINARY"); err != nil {
		t.Fatal("same image reconciled a deliberately restored worker")
	}
	if err := os.Remove(filepath.Join(m.dir, "container-image-version")); err != nil {
		t.Fatal(err)
	}
	// A pending user operation has priority over image reconciliation.
	if err := m.syncContainerImage(context.Background(), "TEST_ONLY_NONEXISTENT_BINARY"); err != nil {
		t.Fatal("pending operation triggered image reconciliation")
	}
	data, err := os.ReadFile(filepath.Join(m.dir, "container-image-version"))
	if err != nil || string(data) != Version {
		t.Fatal("pending operation did not retain the current image marker")
	}
}

func TestInterruptedUpdateCleanupRetainsFailureResult(t *testing.T) {
	m, _, _ := updateHistoryFixture(t)
	if _, err := m.beginTransition(); err != nil {
		t.Fatal(err)
	}
	m.recordUpdateResult(true, "TEST_ONLY_SUCCESS")
	// Simulate interruption after operation removal but before transaction cleanup.
	if err := os.Remove(filepath.Join(m.dir, "operation.json")); err != nil {
		t.Fatal(err)
	}
	m.recordUpdateResult(false, "TEST_ONLY_INTERRUPTED")
	if err := m.rollbackTransition(); err != nil {
		t.Fatal(err)
	}
	if result := m.lastUpdateResult(); result == nil || result.OK {
		t.Fatal("interrupted cleanup left a misleading success result")
	}
}

func TestLegacyWorkerUpdateStillCreatesHistoryBackup(t *testing.T) {
	m, _, next := updateHistoryFixture(t)
	probe := filepath.Join(t.TempDir(), "version-probe")
	if err := os.WriteFile(probe, []byte("#!/bin/sh\nprintf '%s\\n' '0.0.1'\n"), 0750); err != nil {
		t.Fatal(err)
	}
	// Old workers use this operation format after being restored from history.
	op := maintenanceOperation{Kind: "update", Version: "99.0.0", Digest: digestBytes(next)}
	if err := writeJSON(filepath.Join(m.dir, "operation.json"), op); err != nil {
		t.Fatal(err)
	}
	if changed, err := m.beginSupervisedTransition(context.Background(), probe); err != nil || !changed {
		t.Fatalf("legacy worker update failed: %v", err)
	}
	backups, err := m.listUpdateBackups()
	if err != nil || len(backups) != 1 || backups[0].Version != "0.0.1" {
		t.Fatal("old worker update bypassed the automatic backup")
	}
}

func TestManagedUpdateAndRollbackAPI(t *testing.T) {
	for _, name := range []string{"GATEHOUSE_SUPERVISED", "GATEHOUSE_BACKUP_FILES", "GATEHOUSE_SUNPANEL_STORAGE", "GATEHOUSE_UPDATE_HISTORY"} {
		t.Setenv(name, "1")
	}
	a, _ := testAdmin(t)
	app := t.TempDir()
	m, err := NewMaintenancePaths(a.store.paths, app)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Available() {
		t.Skip("managed updates require Linux")
	}
	if err := writeProgram(filepath.Join(app, "gatehouse"), testELF(releaseArch())); err != nil {
		t.Fatal(err)
	}
	preview, err := m.inspectUpdate(testRelease(t, "99.0.0-beta.1", releaseArch()))
	if err != nil || preview["can_apply"] != true {
		t.Fatal("new launcher rejected a valid beta package")
	}
	a.SetMaintenance(m)
	h := a.Handler()
	cookie := loginForTest(t, h)
	w := adminRequest(h, "POST", "/api/maintenance/apply-update", map[string]string{"id": preview["id"].(string)}, cookie, "")
	if w.Code != 202 || !m.Busy() {
		t.Fatal("valid update was not scheduled through the maintenance API")
	}
	select {
	case <-m.RestartSignal():
	default:
		t.Fatal("update did not notify the launcher")
	}
	if changed, err := m.beginTransition(); err != nil || !changed {
		t.Fatalf("scheduled update failed: %v", err)
	}
	if err := m.finishTransition(); err != nil {
		t.Fatal(err)
	}
	// A restarted worker has fresh maintenance state and reads the persisted history.
	m, err = NewMaintenancePaths(a.store.paths, app)
	if err != nil {
		t.Fatal(err)
	}
	a.SetMaintenance(m)
	backups, err := m.listUpdateBackups()
	if err != nil || len(backups) != 1 {
		t.Fatal("scheduled update omitted its backup")
	}
	w = adminRequest(h, "POST", "/api/maintenance/rollback-update", map[string]string{"id": backups[0].ID}, cookie, "")
	if w.Code != 202 || !m.Busy() {
		t.Fatal("historical restore was not scheduled through the API")
	}
	select {
	case <-m.RestartSignal():
	default:
		t.Fatal("restore did not notify the launcher")
	}
	w = adminRequest(h, "POST", "/api/maintenance/rollback-update", map[string]string{"id": backups[0].ID}, cookie, "")
	if w.Code != 400 {
		t.Fatal("duplicate rollback request was accepted")
	}
	if changed, err := m.beginTransition(); err != nil || !changed {
		t.Fatalf("scheduled history restore failed: %v", err)
	}
}
