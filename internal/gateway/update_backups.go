package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

const updateBackupRetention = 3

type updateBackup struct {
	ID            string    `json:"id"`
	Version       string    `json:"version"`
	TargetVersion string    `json:"target_version"`
	CreatedAt     time.Time `json:"created_at"`
	Bytes         int64     `json:"bytes"`
	ProgramSHA    string    `json:"program_sha256"`
	SnapshotSHA   string    `json:"snapshot_sha256"`
}

type updateResult struct {
	Kind    string    `json:"kind"`
	Version string    `json:"version"`
	At      time.Time `json:"at"`
	OK      bool      `json:"ok"`
	Message string    `json:"message"`
}

func digestBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func validUpdateBackupID(id string) bool {
	decoded, err := hex.DecodeString(id)
	return err == nil && len(decoded) == 16 && hex.EncodeToString(decoded) == id
}

func readUpdateFile(path string, max int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > max {
		return nil, errors.New("更新备份文件无效或超过大小限制")
	}
	return os.ReadFile(path)
}

func (m *Maintenance) updateBackupDir() string { return filepath.Join(m.dir, "update-backups") }

func (m *Maintenance) readUpdateBackupMetadata(id string) (updateBackup, error) {
	var b updateBackup
	if !validUpdateBackupID(id) {
		return b, errors.New("更新备份编号无效")
	}
	root := m.updateBackupDir()
	dir := filepath.Join(root, id)
	for _, path := range []string{root, dir} {
		if info, err := os.Lstat(path); err != nil || !info.IsDir() {
			return b, errors.New("找不到该更新备份")
		}
	}
	data, err := readUpdateFile(filepath.Join(dir, "metadata.json"), 4096)
	if err != nil || json.Unmarshal(data, &b) != nil || b.ID != id || !versionPattern.MatchString(b.Version) ||
		!versionPattern.MatchString(b.TargetVersion) || b.CreatedAt.IsZero() || b.Bytes < 1 ||
		len(b.ProgramSHA) != 64 || len(b.SnapshotSHA) != 64 {
		return updateBackup{}, errors.New("更新备份清单无效")
	}
	return b, nil
}

func (m *Maintenance) listUpdateBackups() ([]updateBackup, error) {
	list := []updateBackup{}
	root := m.updateBackupDir()
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return list, nil
	}
	if err != nil || !info.IsDir() {
		return nil, errors.New("更新备份目录不可读")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, errors.New("更新备份目录不可读")
	}
	for _, entry := range entries {
		if b, err := m.readUpdateBackupMetadata(entry.Name()); err == nil {
			list = append(list, b)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	return list, nil
}

func (m *Maintenance) saveUpdateBackup(from, to string, snapshot []byte) error {
	root := m.updateBackupDir()
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(root); err != nil || !info.IsDir() {
		return errors.New("更新备份目录无效")
	}
	program, err := readUpdateFile(filepath.Join(m.appDir, "gatehouse.previous"), maxUpdateExpandedBytes)
	if err != nil {
		return err
	}
	if err := validateELF(program, releaseArch()); err != nil {
		return err
	}
	if len(snapshot) > maxBackupBytes {
		return errors.New("更新备份超过大小限制")
	}
	id, err := stageID()
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(root, ".pending-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	b := updateBackup{ID: id, Version: from, TargetVersion: to, CreatedAt: time.Now().UTC(),
		Bytes: int64(len(program) + len(snapshot)), ProgramSHA: digestBytes(program), SnapshotSHA: digestBytes(snapshot)}
	if err := atomicWrite(filepath.Join(stage, "gatehouse"), program); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(stage, "snapshot.json"), snapshot); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(stage, "metadata.json"), b); err != nil {
		return err
	}
	if err := syncUpdateDir(stage); err != nil {
		return err
	}
	if err := os.Rename(stage, filepath.Join(root, id)); err != nil {
		return err
	}
	if err := syncUpdateDir(root); err != nil {
		return err
	}
	if err := syncUpdateDir(m.dir); err != nil {
		return err
	}
	list, err := m.listUpdateBackups()
	if err != nil {
		return err
	}
	for i := updateBackupRetention; i < len(list); i++ {
		if err := os.RemoveAll(filepath.Join(root, list[i].ID)); err != nil {
			return err
		}
	}
	return syncUpdateDir(root)
}

func syncUpdateDir(dir string) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (m *Maintenance) loadUpdateBackup(id string) (updateBackup, diskBackup, []byte, error) {
	var snapshot diskBackup
	b, err := m.readUpdateBackupMetadata(id)
	if err != nil {
		return b, snapshot, nil, err
	}
	dir := filepath.Join(m.updateBackupDir(), id)
	program, err := readUpdateFile(filepath.Join(dir, "gatehouse"), maxUpdateExpandedBytes)
	if err != nil || digestBytes(program) != b.ProgramSHA || validateELF(program, releaseArch()) != nil {
		return b, snapshot, nil, errors.New("备份程序已损坏或架构不匹配")
	}
	data, err := readUpdateFile(filepath.Join(dir, "snapshot.json"), maxBackupBytes)
	if err != nil || digestBytes(data) != b.SnapshotSHA || json.Unmarshal(data, &snapshot) != nil || !json.Valid(snapshot.State) || snapshot.Files == nil || b.Bytes != int64(len(data)+len(program)) {
		return b, snapshot, nil, errors.New("备份数据已损坏")
	}
	return b, snapshot, program, nil
}

func (m *Maintenance) scheduleUpdateRollback(id string, current Config, adminPort int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.UpdateHistoryAvailable() || m.busy {
		return errors.New("更新回退需要新版启动器，且不能与其他维护操作同时执行")
	}
	b, snapshot, _, err := m.loadUpdateBackup(id)
	if err != nil {
		return err
	}
	var restored State
	if json.Unmarshal(snapshot.State, &restored) != nil || validateAdminPort(restored.Config, adminPort) != nil {
		return errors.New("备份中的配置与当前管理端口冲突")
	}
	if err := checkRestartPorts(current, restored.Config); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(m.dir, "operation.json"), maintenanceOperation{Kind: "rollback", Version: b.Version, BackupID: id}); err != nil {
		return errors.New("无法安排更新回退")
	}
	m.busy = true
	return nil
}

func (m *Maintenance) lastUpdateResult() *updateResult {
	var result updateResult
	data, err := readUpdateFile(filepath.Join(m.dir, "update-result.json"), 4096)
	if err != nil || json.Unmarshal(data, &result) != nil {
		return nil
	}
	return &result
}

func (m *Maintenance) recordUpdateResult(ok bool, message string) {
	var op maintenanceOperation
	data, err := readUpdateFile(filepath.Join(m.dir, "operation.json"), 4096)
	if errors.Is(err, os.ErrNotExist) {
		data, err = readUpdateFile(filepath.Join(m.dir, "transition.json"), 4096)
	}
	if err != nil || json.Unmarshal(data, &op) != nil || (op.Kind != "rollback" && (op.Kind != "update" || op.FromVersion == "")) {
		return
	}
	if err := writeJSON(filepath.Join(m.dir, "update-result.json"), updateResult{Kind: op.Kind, Version: op.Version, At: time.Now().UTC(), OK: ok, Message: message}); err != nil {
		log.Print("维护结果保存失败")
	}
}
