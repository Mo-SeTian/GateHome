package gateway

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (p StoragePaths) sunPanelDir() string {
	return filepath.Join(filepath.Dir(filepath.Clean(p.Data)), "sunpanel")
}

var sunPanelDirectories = []string{"conf", "database", "uploads", "runtime", "lang", "custom"}

func safeSunPanelBackupFile(name string) bool {
	if !strings.HasPrefix(name, "sunpanel/") || path.Clean(name) != name || strings.ContainsAny(name, "\\\x00:") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) < 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	for _, dir := range sunPanelDirectories {
		if parts[1] == dir {
			return true
		}
	}
	return false
}

func sunPanelFileCount(files map[string][]byte) int {
	n := 0
	for name := range files {
		if strings.HasPrefix(name, "sunpanel/") {
			n++
		}
	}
	return n
}

func sunPanelBackupNames(paths StoragePaths) ([]string, error) {
	root := paths.sunPanelDir()
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("Sun-Panel 数据目录无效")
	}
	names := []string{}
	err = filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if file == root {
			return nil
		}
		relative, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		name := "sunpanel/" + filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("Sun-Panel 数据目录不允许符号链接")
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() || !safeSunPanelBackupFile(name) {
			return errors.New("Sun-Panel 目录包含非数据文件；请分离源码与运行目录")
		}
		names = append(names, name)
		return nil
	})
	return names, err
}
