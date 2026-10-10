package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type backupCapacity struct {
	ConfigBytes   int64     `json:"config_bytes"`
	LogBytes      int64     `json:"log_bytes"`
	SunPanelBytes int64     `json:"sunpanel_bytes"`
	ImageBytes    int64     `json:"image_bytes"`
	OtherBytes    int64     `json:"other_bytes"`
	RawBytes      int64     `json:"raw_bytes"`
	SnapshotBytes int64     `json:"snapshot_bytes"`
	LimitBytes    int64     `json:"limit_bytes"`
	CanProceed    bool      `json:"can_proceed"`
	CheckedAt     time.Time `json:"checked_at"`
}

// Stat only: never load image/log contents or stop Sun-Panel to estimate capacity.
func inspectBackupCapacity(paths StoragePaths, state State) (backupCapacity, error) {
	c := backupCapacity{LimitBytes: maxBackupBytes, CheckedAt: time.Now()}
	for _, name := range []string{"certificates", "logs", "subscriptions", "route-images"} {
		info, err := os.Lstat(paths.directory(name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return c, errors.New("容量检查的数据目录须为普通目录")
		}
	}
	config, err := json.Marshal(state)
	if err != nil {
		return c, errors.New("配置容量检查失败")
	}
	c.ConfigBytes = int64(len(config))
	skeleton := backupPayload{State: state, Certificates: map[string][]byte{}, Files: map[string][]byte{}}
	names, err := sunPanelBackupNames(paths)
	if err != nil {
		return c, err
	}
	logs, err := logFileNames(paths.Log)
	if err != nil {
		return c, errors.New("日志容量检查失败")
	}
	for _, name := range logs {
		names = append(names, "logs/"+name)
	}
	for _, subscription := range state.Config.Subscriptions {
		names = append(names, "subscriptions/"+subscription.ID+".json")
	}
	seen := map[string]bool{}
	for _, route := range state.Config.Routes {
		if route.Image != "" && !seen[route.Image] {
			names = append(names, "route-images/"+route.Image+".img")
			seen[route.Image] = true
		}
	}
	certs, err := os.ReadDir(paths.directory("certificates"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return c, errors.New("证书容量检查失败")
	}
	for _, cert := range certs {
		if cert.IsDir() || strings.HasPrefix(cert.Name(), ".pending-") {
			continue
		}
		if !safeCertificateName(cert.Name()) {
			return c, errors.New("证书目录含不支持的文件")
		}
		names = append(names, "certificates/"+cert.Name())
	}
	var encodedBytes int64
	for _, name := range names {
		info, err := os.Lstat(paths.file(name))
		if errors.Is(err, os.ErrNotExist) && !strings.HasPrefix(name, "route-images/") {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return c, errors.New("容量检查发现缺失或非普通数据文件")
		}
		size := info.Size()
		c.RawBytes += size
		encodedBytes += 4 * ((size + 2) / 3)
		switch {
		case strings.HasPrefix(name, "logs/"):
			c.LogBytes += size
		case strings.HasPrefix(name, "sunpanel/"):
			c.SunPanelBytes += size
		case strings.HasPrefix(name, "route-images/"):
			c.ImageBytes += size
		default:
			c.OtherBytes += size
		}
		if strings.HasPrefix(name, "certificates/") {
			skeleton.Certificates[filepath.Base(name)] = []byte{}
		} else {
			skeleton.Files[name] = []byte{}
		}
	}
	// Indented JSON conservatively covers both encrypted backups and rollback files.
	overhead, err := json.MarshalIndent(skeleton, "", "  ")
	if err != nil {
		return c, errors.New("快照容量检查失败")
	}
	c.SnapshotBytes = int64(len(overhead)) + encodedBytes
	c.CanProceed = c.RawBytes <= maxBackupBytes && c.SnapshotBytes <= maxBackupBytes
	return c, nil
}

func checkBackupCapacity(paths StoragePaths, state State) error {
	c, err := inspectBackupCapacity(paths, state)
	if err != nil {
		return err
	}
	if !c.CanProceed {
		return fmt.Errorf("备份与回滚快照预计 %.1f MiB，超过 64 MiB 上限；请先在设置中检查容量并减少保留日志或迁出不需要的文件", float64(c.SnapshotBytes)/(1<<20))
	}
	return nil
}
