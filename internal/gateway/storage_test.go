package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func splitStorageFixture(t *testing.T) (StoragePaths, backupPayload) {
	t.Helper()
	dir, payload := fullBackupFixture(t)
	paths, err := PrepareStorage(dir, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return paths, payload
}

func TestSplitStorageMigrationAndReopen(t *testing.T) {
	paths, want := splitStorageFixture(t)
	store, err := OpenStorePaths(paths)
	if err != nil || !reflect.DeepEqual(store.Snapshot(), want.State) {
		t.Fatal("configuration or credentials changed during migration")
	}
	if _, err := os.Stat(filepath.Join(paths.Data, "state.json")); !os.IsNotExist(err) {
		t.Fatal("configuration was left in the runtime data directory")
	}
	if _, err := os.Stat(filepath.Join(paths.Data, "logs")); !os.IsNotExist(err) {
		t.Fatal("logs were left in the runtime data directory")
	}
	got, err := snapshotBackupPaths(paths, store.Snapshot())
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("persistent files changed or were omitted after migration")
	}
	logs, err := NewLogsAt(paths.Log, store)
	if err != nil || len(logs.entries) != 2 || !logs.entries[1].FreezeCreated {
		t.Fatal("security logs did not survive reopening")
	}
	if err := store.Update(store.Snapshot().Config, nil, store.Snapshot().Revision); err != nil {
		t.Fatal("saved image reference used the configuration directory:", err)
	}
	if _, err := PrepareStorage(paths.Data, paths.Config, paths.Log); err != nil {
		t.Fatal("reinstall migration was not idempotent")
	}
}

func TestSplitStorageBackupRestoreAndRollback(t *testing.T) {
	paths, original := splitStorageFixture(t)
	before, err := snapshotDiskPaths(paths)
	if err != nil {
		t.Fatal(err)
	}
	logRoot, _ := os.Stat(paths.Log)
	configRoot, _ := os.Stat(paths.Config)
	app := t.TempDir()
	atomicWrite(filepath.Join(app, "gatehouse"), []byte("TEST_ONLY_PROGRAM"))
	m, err := NewMaintenancePaths(paths, app)
	if err != nil {
		t.Fatal(err)
	}
	original.State.Config.Groups[0].Name = "恢复后的组"
	state, _ := json.Marshal(original.State)
	original.Files["logs/calls.jsonl"] = []byte(`{"id":9,"message":"TEST_ONLY_RESTORED"}` + "\n")
	writeJSON(filepath.Join(m.dir, "restore-staged.json"), diskBackup{State: state, Certificates: original.Certificates, Files: original.Files})
	writeJSON(filepath.Join(m.dir, "operation.json"), maintenanceOperation{Kind: "restore", Version: Version})
	if transition, err := m.beginTransition(); err != nil || !transition {
		t.Fatal("split-directory restoration failed:", err)
	}
	store, err := OpenStorePaths(paths)
	if err != nil || store.Snapshot().Config.Groups[0].Name != "恢复后的组" {
		t.Fatal("restored configuration was not applied")
	}
	data, _ := os.ReadFile(paths.file("logs/calls.jsonl"))
	if !bytes.Equal(data, original.Files["logs/calls.jsonl"]) {
		t.Fatal("restored log remained in a different directory")
	}
	if err := m.rollbackTransition(); err != nil {
		t.Fatal("split-directory rollback failed:", err)
	}
	after, err := snapshotDiskPaths(paths)
	var beforeState, afterState State
	json.Unmarshal(before.State, &beforeState)
	json.Unmarshal(after.State, &afterState)
	if err != nil || !reflect.DeepEqual(beforeState, afterState) || !reflect.DeepEqual(before.Files, after.Files) || !reflect.DeepEqual(before.Certificates, after.Certificates) {
		t.Fatal("rollback did not restore all persistent data")
	}
	newLogRoot, _ := os.Stat(paths.Log)
	newConfigRoot, _ := os.Stat(paths.Config)
	if !os.SameFile(logRoot, newLogRoot) || !os.SameFile(configRoot, newConfigRoot) {
		t.Fatal("restore replaced a Docker mount root")
	}
}

func TestSplitRestoreFailureRetainsLogsAndData(t *testing.T) {
	paths, payload := splitStorageFixture(t)
	before, _ := snapshotBackupPaths(paths, payload.State)
	os.Remove(paths.file("state.json"))
	os.Mkdir(paths.file("state.json"), 0700)
	state, _ := json.Marshal(payload.State)
	payload.Files["logs/calls.jsonl"] = []byte(`{"id":99}` + "\n")
	payload.Certificates = map[string][]byte{}
	if err := restoreFilesPaths(paths, state, payload.Certificates, payload.Files); err == nil {
		t.Fatal("failed configuration write was ignored")
	}
	after, err := snapshotBackupPaths(paths, before.State)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("failed restore left partial logs or data")
	}
}

func TestStorageMigrationRefusesConflictsAndSymlinks(t *testing.T) {
	for _, kind := range []string{"conflict", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			data, config, logs := t.TempDir(), t.TempDir(), t.TempDir()
			source, target := filepath.Join(data, "state.json"), filepath.Join(config, "state.json")
			atomicWrite(source, []byte("TEST_ONLY_OLD_STATE"))
			if kind == "conflict" {
				atomicWrite(target, []byte("TEST_ONLY_NEW_STATE"))
			} else if kind == "hardlink" {
				os.Link(source, target)
			} else {
				external := filepath.Join(t.TempDir(), "state.json")
				atomicWrite(external, []byte("TEST_ONLY_EXTERNAL_STATE"))
				os.Symlink(external, target)
			}
			if _, err := PrepareStorage(data, config, logs); err == nil {
				t.Fatal("unsafe migration was accepted")
			}
			original, _ := os.ReadFile(source)
			if string(original) != "TEST_ONLY_OLD_STATE" {
				t.Fatal("failed migration destroyed the original configuration")
			}
		})
	}
}

func TestSupervisorPassesSplitDirectoriesToChild(t *testing.T) {
	paths, err := PrepareStorage(t.TempDir(), t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := t.TempDir()
	script := `#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in -data) data="$2";; -config) config="$2";; -log) logs="$2";; esac
  shift 2
done
printf TEST_ONLY_CHILD > "$config/child.flag"
printf TEST_ONLY_CHILD > "$logs/child.flag"
printf '{"pid":%s,"version":"` + Version + `"}' "$$" > "$data/maintenance/ready.json"
trap 'exit 0' TERM INT
while :; do sleep 0.1; done
`
	os.WriteFile(filepath.Join(app, "gatehouse"), []byte(script), 0750)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- SupervisePaths(ctx, paths, app, "127.0.0.1:16666") }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		config, _ := os.ReadFile(filepath.Join(paths.Config, "child.flag"))
		logs, _ := os.ReadFile(filepath.Join(paths.Log, "child.flag"))
		if string(config) == "TEST_ONLY_CHILD" && string(logs) == "TEST_ONLY_CHILD" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not receive split directories")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestBackupPasswordHasNoLengthRestriction(t *testing.T) {
	_, payload := fullBackupFixture(t)
	for _, test := range []struct{ name, password string }{{"empty", ""}, {"one", "x"}, {"unicode", "密"}, {"long", strings.Repeat("TEST_ONLY_", 1024)}} {
		t.Run(test.name, func(t *testing.T) {
			data, err := encodeBackup(payload, test.password)
			if err != nil {
				t.Fatal("password length was restricted")
			}
			got, _, err := decodeBackup(data, test.password)
			if err != nil {
				t.Fatal("password did not round-trip:", err)
			}
			if !reflect.DeepEqual(got, payload) {
				t.Fatal("backup content did not survive a password round-trip")
			}
			if _, _, err := decodeBackup(data, test.password+"TEST_ONLY_WRONG"); err == nil {
				t.Fatal("wrong password was accepted")
			}
		})
	}
}

func TestContainerWorkerUpgradeRetainsNewerWebUpdates(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(executable)
	for _, version := range []string{"0.0.0", "99.0.0"} {
		t.Run(version, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "gatehouse")
			script := []byte("#!/bin/sh\nprintf '%s\\n' '" + version + "'\n")
			os.WriteFile(binary, script, 0750)
			if err := syncContainerProgram(context.Background(), binary); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(binary)
			want := script
			if version == "0.0.0" {
				want = current
			}
			if !bytes.Equal(got, want) {
				t.Fatal("image startup chose the wrong persisted worker")
			}
		})
	}
}
