package gateway

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// StoragePaths separates writable configuration, request logs and runtime data.
type StoragePaths struct {
	Config, Log, Data string
}

func legacyStorage(data string) StoragePaths {
	return StoragePaths{Config: data, Log: filepath.Join(data, "logs"), Data: data}
}

func (p StoragePaths) file(name string) string {
	if name == "state.json" {
		return filepath.Join(p.Config, name)
	}
	if strings.HasPrefix(name, "logs/") {
		return filepath.Join(p.Log, strings.TrimPrefix(name, "logs/"))
	}
	return filepath.Join(p.Data, filepath.FromSlash(name))
}

func (p StoragePaths) directory(name string) string {
	if name == "logs" {
		return p.Log
	}
	return filepath.Join(p.Data, name)
}

func (p StoragePaths) splitLogs() bool {
	return filepath.Clean(p.Log) != filepath.Join(filepath.Clean(p.Data), "logs")
}

// Empty flags preserve the legacy -data layout for existing launchers.
func PrepareStorage(data, config, logs string) (StoragePaths, error) {
	p := legacyStorage(data)
	if config != "" {
		p.Config = config
	}
	if logs != "" {
		p.Log = logs
	}
	for _, dir := range []string{p.Config, p.Log, p.Data} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return p, err
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return p, errors.New("持久化路径须为普通目录")
		}
	}
	if filepath.Clean(p.Config) != filepath.Clean(p.Data) {
		if err := migrateStorageFile(filepath.Join(p.Data, "state.json"), p.file("state.json")); err != nil {
			return p, err
		}
	}
	if p.splitLogs() {
		old := filepath.Join(p.Data, "logs")
		info, err := os.Lstat(old)
		if errors.Is(err, os.ErrNotExist) {
			return p, nil
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return p, errors.New("旧日志目录无效")
		}
		entries, err := os.ReadDir(old)
		if err != nil {
			return p, err
		}
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				return p, errors.New("旧日志目录含非普通文件")
			}
			if err := migrateStorageFile(filepath.Join(old, entry.Name()), filepath.Join(p.Log, entry.Name())); err != nil {
				return p, err
			}
		}
		if err := os.Remove(old); err != nil {
			return p, err
		}
	}
	return p, nil
}

// Copy then remove also works when the directories are different Docker mounts.
// Identical destination files allow an interrupted migration to resume safely.
func migrateStorageFile(source, target string) error {
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("旧数据文件不是普通文件")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	targetInfo, err := os.Lstat(target)
	if err == nil {
		if os.SameFile(info, targetInfo) {
			return errors.New("迁移源与目标指向同一文件，请使用独立持久化目录")
		}
		if !targetInfo.Mode().IsRegular() {
			return errors.New("迁移目标不是普通文件")
		}
		existing, err := os.ReadFile(target)
		if err != nil || !bytes.Equal(existing, data) {
			return errors.New("新旧数据冲突，请保留两份数据并检查后重试")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if err := atomicWrite(target, data); err != nil {
		return err
	}
	return os.Remove(source)
}
