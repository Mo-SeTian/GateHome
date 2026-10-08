package gateway

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeRetentionEntries(t *testing.T, path string, first, count int, when time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	for i := first; i < first+count; i++ {
		if err := encoder.Encode(LogEntry{ID: int64(i), Time: when, Category: "access", Status: 200, Message: strings.Repeat("x", 1900)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := atomicWrite(path, data.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func TestLogRetentionExpiresRecordsAtStartupAndWithoutTraffic(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	path := filepath.Join(dir, "logs", "calls.jsonl")
	writeRetentionEntries(t, path+".1", 1, 1, now.Add(-31*24*time.Hour))
	writeRetentionEntries(t, path+".100", 2, 1, now.Add(-24*time.Hour))
	writeRetentionEntries(t, path, 3, 1, now.Add(-time.Hour))
	l, err := NewLogs(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	page := l.Page(LogFilter{}, 1, 20, 0)
	if page.Total != 2 || page.Entries[0].ID != 3 || page.Entries[1].ID != 2 {
		t.Fatal("startup did not expire only old records")
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatal("expired archive was not removed")
	}
	if err := l.Cleanup(now.Add(31 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if l.Page(LogFilter{}, 1, 20, 0).Total != 0 || l.StorageStatus().UsedBytes != 0 || l.Statistics(24, "", now).Total != 0 {
		t.Fatal("idle cleanup left expired records in disk, queries or statistics")
	}
	l.Add(LogEntry{Category: "access"})
	if l.lastID != 4 {
		t.Fatal("cleanup reused a record ID")
	}
}

func TestLogRetentionExpiresOnlyOldRecordsInSameFile(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	path := filepath.Join(dir, "logs", "calls.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	_ = encoder.Encode(LogEntry{ID: 1, Time: now.Add(-31 * 24 * time.Hour), Category: "access", Message: "TEST_ONLY_EXPIRED"})
	_ = encoder.Encode(LogEntry{ID: 2, Time: now.Add(-24 * time.Hour), Category: "access", Message: "TEST_ONLY_RECENT"})
	if err := atomicWrite(path, data.Bytes()); err != nil {
		t.Fatal(err)
	}
	l, err := NewLogs(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	page := l.Page(LogFilter{}, 1, 20, 0)
	retained, err := os.ReadFile(path)
	if err != nil || page.Total != 1 || page.Entries[0].ID != 2 || bytes.Contains(retained, []byte("TEST_ONLY_EXPIRED")) || !bytes.Contains(retained, []byte("TEST_ONLY_RECENT")) {
		t.Fatal("partial expiry removed recent records or left expired records")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("cleanup changed log file permissions")
	}
}

func TestLogRetentionCleanupFailureIsVisibleAndLeavesFilesIntact(t *testing.T) {
	l, err := NewLogs(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	l.Add(LogEntry{Category: "access"})
	before, _ := os.ReadFile(l.path)
	if err := os.Symlink(l.path, l.path+".100"); err != nil {
		t.Fatal(err)
	}
	if err := l.Cleanup(time.Now()); err == nil || !l.StorageStatus().WriteError {
		t.Fatal("cleanup failure was not reported")
	}
	if page := l.Page(LogFilter{}, 1, 20, 0); !page.WriteError || page.Total != 1 {
		t.Fatal("cleanup failure hid existing records or its error")
	}
	after, _ := os.ReadFile(l.path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed cleanup changed the log file")
	}
	if err := os.Remove(l.path + ".100"); err != nil {
		t.Fatal(err)
	}
	if err := l.Cleanup(time.Now()); err != nil || l.StorageStatus().WriteError {
		t.Fatal("successful cleanup did not clear its error")
	}
}

func TestLogRetentionSpaceKeepsNewestAcrossAllArchives(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	c.LogRetention = LogRetentionConfig{MaxSizeMB: 1, KeepDays: 30}
	if err := store.Update(c, nil, 0); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "logs", "calls.jsonl")
	now := time.Now()
	writeRetentionEntries(t, path+".1", 1, 300, now)
	writeRetentionEntries(t, path+".100", 301, 300, now)
	writeRetentionEntries(t, path, 601, 100, now)
	if err := os.WriteFile(filepath.Join(dir, "logs", "unrelated.log"), []byte("TEST_ONLY_UNRELATED"), 0600); err != nil {
		t.Fatal(err)
	}
	l, err := NewLogs(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	page := l.Page(LogFilter{}, 1, 20, 0)
	if l.StorageStatus().UsedBytes > 1<<20 || page.Total != 400 || page.Entries[0].ID != 700 {
		t.Fatal("space cleanup did not keep the newest archives")
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatal("the oldest archive was not deleted first")
	}
	if _, err := os.Stat(filepath.Join(dir, "logs", "unrelated.log")); err != nil {
		t.Fatal("cleanup touched an unrelated file")
	}
	// A restored active file can exceed a newly configured smaller budget.
	writeRetentionEntries(t, path, 701, 1000, now)
	if err := l.Cleanup(now); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Size() > 1<<20 {
		t.Fatal("the active file was not trimmed to the budget")
	}
	reopened, err := NewLogs(dir, store)
	if err != nil || reopened.Page(LogFilter{}, 1, 20, 0).Entries[0].ID != 1700 {
		t.Fatal("size trimming lost the newest record or broke restart")
	}
}

func TestLogRetentionRotatesAndEnforcesBudgetDuringWrites(t *testing.T) {
	dir := t.TempDir()
	store, _ := OpenStore(dir)
	c := DefaultConfig()
	c.LogRetention = LogRetentionConfig{MaxSizeMB: 1, KeepDays: 30}
	if err := store.Update(c, nil, 0); err != nil {
		t.Fatal(err)
	}
	l, err := NewLogs(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 600; i++ {
		l.Add(LogEntry{Category: "access", Message: strings.Repeat("x", 1900)})
		if s := l.StorageStatus(); s.UsedBytes > 1<<20 || s.WriteError {
			t.Fatal("a log write exceeded its budget or failed")
		}
	}
	reopened, err := NewLogs(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	page := reopened.Page(LogFilter{}, 1, 20, 0)
	if page.Total >= 600 || page.Entries[0].ID != 600 {
		t.Fatal("rotation did not remove the oldest records first")
	}
}

func TestLogRetentionConfigAPIAndLegacyDefaults(t *testing.T) {
	a, h := testAdmin(t)
	l, err := NewLogs(a.store.paths.Data, a.store)
	if err != nil {
		t.Fatal(err)
	}
	a.SetLogs(l)
	writeRetentionEntries(t, l.path+".100", 1, 1, time.Now().Add(-2*24*time.Hour))
	l.lastID = 1
	l.Add(LogEntry{Category: "access"})
	cookie := loginForTest(t, h)
	c := a.store.Snapshot().Config
	c.LogRetention = LogRetentionConfig{MaxSizeMB: 2, KeepDays: 1}
	w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": a.store.Snapshot().Revision}, cookie, "")
	if w.Code != 200 || a.store.Snapshot().Config.LogRetention != c.LogRetention {
		t.Fatal("log retention settings did not save")
	}
	if _, err := os.Stat(l.path + ".100"); !os.IsNotExist(err) {
		t.Fatal("saving did not immediately apply the new retention period")
	}
	for _, invalid := range []LogRetentionConfig{{0, 1}, {-1, 30}, {1025, 30}, {1, 0}, {1, 3651}} {
		c.LogRetention = invalid
		if w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": a.store.Snapshot().Revision}, cookie, ""); w.Code != 400 {
			t.Fatal("invalid retention settings were accepted")
		}
	}
	s := a.store.Snapshot()
	s.Config.LogRetention = LogRetentionConfig{}
	if err := writeJSON(a.store.path, s); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(a.store.paths.Data)
	if err != nil || reopened.Snapshot().Config.LogRetention != defaultLogRetention {
		t.Fatal("legacy configuration did not receive retention defaults")
	}
}

func TestBackupRecognizesOnlySafeLogArchives(t *testing.T) {
	for _, name := range []string{"logs/calls.jsonl", "logs/calls.jsonl.1", "logs/calls.jsonl.1700000000000000000"} {
		if !safeBackupFile(name) {
			t.Fatal("valid log archive rejected")
		}
	}
	for _, name := range []string{"logs/calls.jsonl.0", "logs/calls.jsonl.-1", "logs/calls.jsonl.01", "logs/calls.jsonl.1/../state.json", "logs/calls.jsonl.1.restore-old", "logs/calls.jsonl.99999999999999999999"} {
		if safeBackupFile(name) {
			t.Fatal("unsafe log archive path accepted")
		}
	}
}

func TestBackupIncludesAllLogArchivesAndRetentionSettings(t *testing.T) {
	dir, original := fullBackupFixture(t)
	original.State.Config.LogRetention = LogRetentionConfig{MaxSizeMB: 32, KeepDays: 90}
	writeRetentionEntries(t, filepath.Join(dir, "logs", "calls.jsonl.100"), 2, 1, time.Now())
	writeRetentionEntries(t, filepath.Join(dir, "logs", "calls.jsonl"), 3, 1, time.Now())
	payload, err := snapshotBackup(dir, original.State)
	if err != nil {
		t.Fatal(err)
	}
	if _, logs, _ := backupFileCounts(payload); logs != 3 || payload.Files["logs/calls.jsonl.100"] == nil {
		t.Fatal("backup omitted a rotated archive")
	}
	dest := t.TempDir()
	if err := restorePayload(dest, payload); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dest)
	if err != nil || store.Snapshot().Config.LogRetention != original.State.Config.LogRetention {
		t.Fatal("restoration omitted retention settings")
	}
	l, err := NewLogs(dest, store)
	if err != nil || l.Page(LogFilter{}, 1, 20, 0).Total != 3 {
		t.Fatal("restored archives did not reopen")
	}
}
