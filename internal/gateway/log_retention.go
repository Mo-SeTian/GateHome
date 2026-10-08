package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type LogRetentionConfig struct {
	MaxSizeMB int `json:"max_size_mb"`
	KeepDays  int `json:"keep_days"`
}

var defaultLogRetention = LogRetentionConfig{MaxSizeMB: 16, KeepDays: 30}

func effectiveLogRetention(c LogRetentionConfig) LogRetentionConfig {
	if c == (LogRetentionConfig{}) {
		return defaultLogRetention
	}
	return c
}

func validateLogRetention(c LogRetentionConfig) error {
	c = effectiveLogRetention(c)
	if c.MaxSizeMB < 1 || c.MaxSizeMB > 1024 || c.KeepDays < 1 || c.KeepDays > 3650 {
		return errors.New("日志空间须为 1–1024 MiB，保留时间须为 1–3650 天")
	}
	return nil
}

func logFileNumber(name string) (int64, bool) {
	if name == "calls.jsonl" {
		return 0, true
	}
	if !strings.HasPrefix(name, "calls.jsonl.") {
		return 0, false
	}
	suffix := strings.TrimPrefix(name, "calls.jsonl.")
	n, err := strconv.ParseInt(suffix, 10, 64)
	return n, err == nil && n > 0 && strconv.FormatInt(n, 10) == suffix
}

// Legacy .1 is read first; timestamped archives follow, then the active file.
func logFileNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, entry := range entries {
		if _, ok := logFileNumber(entry.Name()); ok {
			names = append(names, entry.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool {
		a, _ := logFileNumber(names[i])
		b, _ := logFileNumber(names[j])
		return a != 0 && (b == 0 || a < b)
	})
	return names, nil
}

func (l *Logs) retention() LogRetentionConfig {
	if l.store != nil {
		l.store.mu.RLock()
		defer l.store.mu.RUnlock()
		return effectiveLogRetention(l.store.state.Config.LogRetention)
	}
	return defaultLogRetention
}

func (l *Logs) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				_ = l.Cleanup(now)
			}
		}
	}()
}

func (l *Logs) Cleanup(now time.Time) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.cleanupLocked(l.retention(), now)
	l.cleanupError = err != nil
	return err
}

type LogStorageStatus struct {
	UsedBytes   int64     `json:"used_bytes"`
	LastCleanup time.Time `json:"last_cleanup"`
	WriteError  bool      `json:"write_error"`
}

func (l *Logs) StorageStatus() LogStorageStatus {
	if l == nil {
		return LogStorageStatus{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return LogStorageStatus{l.totalSize, l.lastCleanup, l.writeError || l.cleanupError}
}

func (l *Logs) rotateLocked(now time.Time) error {
	names, err := logFileNames(filepath.Dir(l.path))
	if err != nil {
		return err
	}
	n := now.UnixNano()
	for _, name := range names {
		value, _ := logFileNumber(name)
		if value >= n {
			n = value + 1
		}
	}
	if err := os.Rename(l.path, l.path+"."+strconv.FormatInt(n, 10)); err != nil {
		return err
	}
	l.size = 0
	return nil
}

// Rewrite only when records expire or the final file itself exceeds the budget.
func pruneLogFile(path string, cutoff time.Time, limit int64) (int64, int64, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxLogBytes {
		return 0, 0, errors.New("日志文件读取失败或类型无效")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	kept := make([]byte, 0, len(data))
	firstID := int64(0)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		var entry LogEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			entry = LogEntry{}
		}
		if entry.Time.IsZero() || !entry.Time.Before(cutoff) {
			kept = append(kept, scanner.Bytes()...)
			kept = append(kept, '\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	for int64(len(kept)) > limit {
		kept = kept[bytes.IndexByte(kept, '\n')+1:]
	}
	if len(kept) > 0 {
		scanner := bufio.NewScanner(bytes.NewReader(kept))
		scanner.Buffer(make([]byte, 4096), 64<<10)
		for scanner.Scan() {
			var first LogEntry
			if json.Unmarshal(scanner.Bytes(), &first) == nil && first.ID > 0 {
				firstID = first.ID
				break
			}
		}
	}
	if !bytes.Equal(data, kept) {
		if err := atomicWrite(path, kept); err != nil {
			return 0, 0, err
		}
	}
	return int64(len(kept)), firstID, nil
}

func (l *Logs) cleanupLocked(c LogRetentionConfig, now time.Time) error {
	names, err := logFileNames(filepath.Dir(l.path))
	if err != nil {
		return err
	}
	cutoff := now.Add(-time.Duration(c.KeepDays) * 24 * time.Hour)
	limit := int64(c.MaxSizeMB) << 20
	sizes := make([]int64, len(names))
	ids := make([]int64, len(names))
	var total int64
	for i, name := range names {
		sizes[i], ids[i], err = pruneLogFile(filepath.Join(filepath.Dir(l.path), name), cutoff, maxLogBytes)
		if err != nil {
			return err
		}
		total += sizes[i]
	}
	var oldestID int64
	for i, name := range names {
		path := filepath.Join(filepath.Dir(l.path), name)
		if (sizes[i] == 0 || total > limit) && i < len(names)-1 {
			if err := os.Remove(path); err != nil {
				return err
			}
			total -= sizes[i]
			continue
		}
		if total > limit {
			oldSize := sizes[i]
			sizes[i], ids[i], err = pruneLogFile(path, cutoff, limit)
			if err != nil {
				return err
			}
			total -= oldSize - sizes[i]
		}
		if oldestID == 0 && sizes[i] > 0 {
			oldestID = ids[i]
		}
		if path == l.path {
			l.size = sizes[i]
		}
	}
	kept := l.entries[:0]
	for _, entry := range l.entries {
		if oldestID != 0 && entry.ID >= oldestID && (entry.Time.IsZero() || !entry.Time.Before(cutoff)) {
			kept = append(kept, entry)
		}
	}
	clear(l.entries[len(kept):])
	l.entries = kept
	l.totalSize, l.lastCleanup = total, now
	return nil
}
