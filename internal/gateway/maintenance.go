package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type maintenanceOperation struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}
type maintenanceStage struct {
	ID, Kind, Version, Digest string
	Created                   time.Time
	Payload                   *backupPayload
}
type Maintenance struct {
	mu            sync.Mutex
	dir, appDir   string
	paths         StoragePaths
	supervised    bool
	backupFiles   bool
	homepageFiles bool
	stage         *maintenanceStage
	busy          bool
	restart       chan struct{}
}

func NewMaintenance(data, appDir string) (*Maintenance, error) {
	return NewMaintenancePaths(legacyStorage(data), appDir)
}

func NewMaintenancePaths(paths StoragePaths, appDir string) (*Maintenance, error) {
	dir := filepath.Join(paths.Data, "maintenance")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Maintenance{dir: dir, appDir: appDir, paths: paths, supervised: os.Getenv("GATEHOUSE_SUPERVISED") == "1", backupFiles: os.Getenv("GATEHOUSE_BACKUP_FILES") == "1", homepageFiles: os.Getenv("GATEHOUSE_HOMEPAGE_FILES") == "1", restart: make(chan struct{}, 1)}, nil
}

func (m *Maintenance) Available() bool {
	return m.appDir != "" && m.supervised && runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")
}
func (m *Maintenance) Busy() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.busy
}
func (m *Maintenance) RestartSignal() <-chan struct{} { return m.restart }
func (m *Maintenance) requestRestart() {
	select {
	case m.restart <- struct{}{}:
	default:
	}
}

func (m *Maintenance) inspectUpdate(data []byte) (map[string]any, error) {
	return m.inspectUpdateVersion(data, "")
}

func (m *Maintenance) inspectUpdateVersion(data []byte, expectedVersion string) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy {
		return nil, errors.New("正在执行维护操作")
	}
	version, binary, err := inspectRelease(data, releaseArch())
	if err != nil {
		return nil, err
	}
	if expectedVersion != "" && version != expectedVersion {
		return nil, errors.New("更新包版本与 GitHub 正式版本不一致，请重新检查版本")
	}
	id, err := stageID()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(binary)
	if err := atomicWrite(filepath.Join(m.dir, "update-staged"), binary); err != nil {
		return nil, errors.New("更新暂存失败")
	}
	m.stage = &maintenanceStage{ID: id, Kind: "update", Version: version, Digest: hex.EncodeToString(sum[:]), Created: time.Now()}
	comparison, _ := compareVersions(version, Version)
	message := "校验通过，应用后将重启服务"
	if comparison <= 0 {
		message = "更新包版本不高于当前版本，无需更新"
	} else if !m.Available() {
		message = "更新包校验通过；应用更新需使用 Linux 安装脚本或新版 Docker 启动方式"
	}
	compatible := m.checkHomepageFiles(nil)
	if compatible != nil && m.Available() {
		message = compatible.Error()
	}
	return map[string]any{"id": id, "version": version, "can_apply": m.Available() && comparison > 0 && compatible == nil, "message": message}, nil
}

func (m *Maintenance) inspectBackup(data []byte, password string, adminPort int) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy {
		return nil, errors.New("正在执行维护操作")
	}
	payload, manifest, err := decodeBackup(data, password)
	if err != nil {
		return nil, err
	}
	if err := validateAdminPort(payload.State.Config, adminPort); err != nil {
		return nil, errors.New("备份业务端口与当前管理端口冲突")
	}
	id, err := stageID()
	if err != nil {
		return nil, err
	}
	// A single private staging slot bounds disk and memory use; passwords are not stored.
	if err := writeJSON(filepath.Join(m.dir, "restore-staged.json"), payload); err != nil {
		return nil, errors.New("恢复暂存失败")
	}
	m.stage = &maintenanceStage{ID: id, Kind: "restore", Version: Version, Created: time.Now()}
	c := payload.State.Config
	images, logs, caches := backupFileCounts(payload)
	compatible := m.checkHomepageFiles(&c.Homepage)
	canApply := m.Available() && (payload.Files == nil || m.backupFiles) && compatible == nil
	message := "恢复会覆盖当前配置和管理员账户；完成后使用备份时的管理员账号和管理密码登录"
	if payload.Files != nil && !m.backupFiles && m.Available() {
		message = "当前启动器不支持完整数据恢复。请使用此版本安装脚本更新启动器，再重新检查备份；仅网页更新程序不会更新启动器。"
	}
	if compatible != nil && m.Available() {
		message = compatible.Error()
	}
	return map[string]any{"id": id, "version": manifest.Version, "created_at": manifest.CreatedAt, "can_apply": canApply, "routes": len(c.Routes), "groups": len(c.Groups), "homepage_groups": len(c.Homepage.Groups), "ddns_groups": len(c.DDNS.Groups), "firewalls": len(c.Firewalls), "subscriptions": len(c.Subscriptions), "certificates": len(payload.Certificates), "images": images, "log_entries": logs, "subscription_caches": caches, "includes_files": payload.Files != nil, "token_configured": payload.State.HasDNSToken(), "message": message}, nil
}

func stageID() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}

func (m *Maintenance) schedule(id, kind string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.Available() {
		return errors.New("此部署尚未启用自动维护，请使用 Linux 安装脚本或更新后的 Docker 部署")
	}
	if m.busy {
		return errors.New("正在执行维护操作")
	}
	if m.stage == nil || m.stage.ID != id || m.stage.Kind != kind || time.Since(m.stage.Created) > 15*time.Minute {
		return errors.New("暂存包不存在或已过期，请重新上传并检查")
	}
	if kind == "update" {
		cmp, err := compareVersions(m.stage.Version, Version)
		if err != nil || cmp <= 0 {
			return errors.New("只可更新到更高版本")
		}
	}
	if kind == "restore" {
		var payload diskBackup
		data, err := os.ReadFile(filepath.Join(m.dir, "restore-staged.json"))
		if err != nil || json.Unmarshal(data, &payload) != nil {
			return errors.New("恢复暂存数据无效")
		}
		if payload.Files != nil && !m.backupFiles {
			return errors.New("请先使用此版本安装脚本更新启动器，才能完整恢复图片、日志和订阅缓存")
		}
		var state State
		if json.Unmarshal(payload.State, &state) != nil {
			return errors.New("恢复配置无效")
		}
		if err := m.checkHomepageFiles(&state.Config.Homepage); err != nil {
			return err
		}
	} else if err := m.checkHomepageFiles(nil); err != nil {
		return err
	}
	op := maintenanceOperation{Kind: kind, Version: m.stage.Version, Digest: m.stage.Digest}
	if err := writeJSON(filepath.Join(m.dir, "operation.json"), op); err != nil {
		return errors.New("维护操作写入失败")
	}
	m.busy = true
	return nil
}

func (m *Maintenance) scheduleRestart() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.Available() {
		return errors.New("此部署未启用启动管理程序，请使用安装脚本或新版 Docker 启动方式")
	}
	if m.busy {
		return errors.New("正在执行维护操作")
	}
	if err := m.checkHomepageFiles(nil); err != nil {
		return err
	}
	// Reapply the running program using the update protocol understood by 0.0.1 launchers.
	binary, err := os.ReadFile(filepath.Join(m.appDir, "gatehouse"))
	if err != nil {
		return errors.New("当前程序读取失败")
	}
	if err := validateELF(binary, releaseArch()); err != nil {
		return err
	}
	sum := sha256.Sum256(binary)
	if err := atomicWrite(filepath.Join(m.dir, "update-staged"), binary); err != nil {
		return errors.New("重启暂存失败")
	}
	if err := writeJSON(filepath.Join(m.dir, "operation.json"), maintenanceOperation{Kind: "update", Version: Version, Digest: hex.EncodeToString(sum[:])}); err != nil {
		return errors.New("重启请求写入失败")
	}
	m.busy = true
	return nil
}

func (m *Maintenance) markReady() error {
	return writeJSON(filepath.Join(m.dir, "ready.json"), map[string]any{"pid": os.Getpid(), "version": Version})
}

func copyProgram(source, destination string) error {
	b, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := atomicWrite(destination, b); err != nil {
		return err
	}
	return os.Chmod(destination, 0750)
}

func (m *Maintenance) beginTransition() (bool, error) {
	path := filepath.Join(m.dir, "operation.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var op maintenanceOperation
	if json.Unmarshal(b, &op) != nil || (op.Kind != "update" && op.Kind != "restore") {
		return false, errors.New("维护请求无效")
	}
	previous, err := snapshotDiskPaths(m.paths)
	if err != nil {
		return false, err
	}
	if err := writeJSON(filepath.Join(m.dir, "rollback.json"), previous); err != nil {
		return false, err
	}
	if err := copyProgram(filepath.Join(m.appDir, "gatehouse"), filepath.Join(m.appDir, "gatehouse.previous")); err != nil {
		return false, err
	}
	// Persist the transaction before any mutation, so interrupted startups can roll back.
	if err := writeJSON(filepath.Join(m.dir, "transition.json"), op); err != nil {
		return false, err
	}
	if op.Kind == "update" {
		binary, err := os.ReadFile(filepath.Join(m.dir, "update-staged"))
		if err != nil {
			return true, err
		}
		sum := sha256.Sum256(binary)
		if hex.EncodeToString(sum[:]) != op.Digest {
			return true, errors.New("暂存程序校验失败")
		}
		if err := validateELF(binary, releaseArch()); err != nil {
			return true, err
		}
		if err := copyProgram(filepath.Join(m.dir, "update-staged"), filepath.Join(m.appDir, "gatehouse")); err != nil {
			return true, err
		}
	} else {
		b, err := os.ReadFile(filepath.Join(m.dir, "restore-staged.json"))
		if err != nil {
			return true, err
		}
		var restored diskBackup
		if json.Unmarshal(b, &restored) != nil {
			return true, errors.New("暂存备份无效")
		}
		var oldRevision struct {
			Revision int `json:"revision"`
		}
		if json.Unmarshal(previous.State, &oldRevision) != nil {
			return true, errors.New("原配置版本无效")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(restored.State, &fields) != nil {
			return true, errors.New("恢复配置无效")
		}
		fields["revision"], _ = json.Marshal(oldRevision.Revision + 1)
		state, _ := json.Marshal(fields)
		if err := restoreFilesPaths(m.paths, state, restored.Certificates, restored.Files); err != nil {
			return true, err
		}
	}
	return true, nil
}

func (m *Maintenance) rollbackTransition() error {
	b, err := os.ReadFile(filepath.Join(m.dir, "rollback.json"))
	if err != nil {
		return err
	}
	var previous diskBackup
	if json.Unmarshal(b, &previous) != nil {
		return errors.New("回滚数据无效")
	}
	for _, target := range restoreTargets(m.paths, true, nil) {
		if err := os.RemoveAll(target.path + ".restore-old"); err != nil {
			return err
		}
	}
	if err := restoreFilesPaths(m.paths, previous.State, previous.Certificates, previous.Files); err != nil {
		return err
	}
	if err := copyProgram(filepath.Join(m.appDir, "gatehouse.previous"), filepath.Join(m.appDir, "gatehouse")); err != nil {
		return err
	}
	return m.finishTransition()
}

func (m *Maintenance) finishTransition() error {
	for _, name := range []string{"operation.json", "transition.json", "rollback.json", "restore-staged.json", "update-staged"} {
		if err := os.Remove(filepath.Join(m.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
