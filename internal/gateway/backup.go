package gateway

import (
	"archive/zip"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/scrypt"
)

type backupPayload struct {
	State        State             `json:"state"`
	Certificates map[string][]byte `json:"certificates"`
	Files        map[string][]byte `json:"files"`
}
type diskBackup struct {
	State        json.RawMessage   `json:"state"`
	Certificates map[string][]byte `json:"certificates"`
	Files        map[string][]byte `json:"files"`
}
type backupManifest struct {
	Format     string    `json:"format"`
	Version    string    `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	Encryption string    `json:"encryption"`
	Salt       []byte    `json:"salt"`
	Nonce      []byte    `json:"nonce"`
}

func snapshotBackup(dir string, state State) (backupPayload, error) {
	return snapshotBackupPaths(legacyStorage(dir), state)
}

func snapshotBackupPaths(paths StoragePaths, state State) (backupPayload, error) {
	dir := paths.Data
	b := backupPayload{State: state, Certificates: map[string][]byte{}, Files: map[string][]byte{}}
	for _, name := range []string{"certificates", "logs", "subscriptions", "route-images", "homepage-backgrounds", "page"} {
		info, err := os.Lstat(paths.directory(name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return b, errors.New("备份目录须为普通目录")
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "certificates"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return b, errors.New("证书目录读取失败")
	}
	total := 0
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".pending-") {
			continue
		}
		if !safeCertificateName(e.Name()) || !e.Type().IsRegular() {
			return b, errors.New("证书目录含不支持的文件")
		}
		f, err := os.Open(filepath.Join(dir, "certificates", e.Name()))
		if err != nil {
			return b, errors.New("证书文件读取失败")
		}
		data, err := io.ReadAll(io.LimitReader(f, maxBackupBytes-int64(total)+1))
		f.Close()
		if err != nil || total+len(data) > maxBackupBytes {
			return b, errors.New("证书备份超过大小限制")
		}
		total += len(data)
		b.Certificates[e.Name()] = data
	}
	logNames, err := logFileNames(paths.Log)
	if err != nil {
		return b, errors.New("日志目录读取失败")
	}
	names := []string{}
	for _, name := range logNames {
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
	for id := range homepageImages(state.Config.Homepage) {
		if !seen[id] {
			names = append(names, "route-images/"+id+".img")
			seen[id] = true
		}
	}
	if state.Config.Homepage.Background != "" {
		names = append(names, "homepage-backgrounds/"+state.Config.Homepage.Background+".jpg")
	}
	if state.HomepageData {
		pageNames, err := homepageBackupNames(paths, state)
		if err != nil {
			return b, err
		}
		names = append(names, pageNames...)
	}
	for _, name := range names {
		path := paths.file(name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) && !strings.HasPrefix(name, "route-images/") && !strings.HasPrefix(name, "homepage-backgrounds/") {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return b, errors.New("备份数据文件缺失或不是普通文件")
		}
		f, err := os.Open(path)
		if err != nil {
			return b, errors.New("数据文件读取失败")
		}
		data, err := io.ReadAll(io.LimitReader(f, maxBackupBytes-int64(total)+1))
		f.Close()
		if err != nil || total+len(data) > maxBackupBytes {
			return b, errors.New("备份数据超过 64 MiB 限制")
		}
		total += len(data)
		b.Files[name] = data
	}
	if err := validateBackupFiles(b); err != nil {
		return b, err
	}
	return b, nil
}

func safeCertificateName(name string) bool {
	if len(name) > 320 || filepath.Base(name) != name || strings.ContainsAny(name, "/\\\x00:") || !strings.HasSuffix(name, ".json") {
		return false
	}
	return strings.HasPrefix(name, "staging-") || strings.HasPrefix(name, "production-") || strings.HasPrefix(name, "account-")
}

func encodeBackup(payload backupPayload, password string) ([]byte, error) {
	plain, err := json.Marshal(payload)
	if err != nil || len(plain) > maxBackupBytes {
		return nil, errors.New("备份内容超过 64 MiB")
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	key, err := scrypt.Key([]byte(password), salt, 32768, 8, 1, 32)
	if err != nil {
		return nil, errors.New("备份加密失败")
	}
	defer clear(key)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	format := "gatehouse-backup-v1"
	if payload.Files != nil {
		format = "gatehouse-backup-v2"
	}
	m := backupManifest{Format: format, Version: Version, CreatedAt: time.Now(), Encryption: "AES-256-GCM+scrypt-N32768-r8-p1", Salt: salt, Nonce: nonce}
	metadata, _ := json.Marshal(m)
	encrypted := gcm.Seal(nil, nonce, plain, metadata)
	clear(plain)
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, entry := range []struct {
		name string
		data []byte
	}{{"manifest.json", metadata}, {"backup.enc", encrypted}} {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		h.SetMode(0600)
		f, err := writer.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(entry.data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeBackup(data []byte, password string) (backupPayload, backupManifest, error) {
	var payload backupPayload
	var m backupManifest
	files, err := readZIP(data, maxBackupBytes+1<<20, 2)
	if err != nil {
		return payload, m, err
	}
	metadata := files["manifest.json"]
	if len(files) != 2 || len(metadata) > 4096 || json.Unmarshal(metadata, &m) != nil || (m.Format != "gatehouse-backup-v1" && m.Format != "gatehouse-backup-v2") || m.Encryption != "AES-256-GCM+scrypt-N32768-r8-p1" || len(m.Salt) != 16 || len(m.Nonce) != 12 {
		return payload, m, errors.New("不是受支持的加密备份包")
	}
	comparison, err := compareVersions(m.Version, Version)
	if err != nil || comparison > 0 {
		return payload, m, errors.New("备份来自更高或不支持的版本，请先更新程序")
	}
	key, err := scrypt.Key([]byte(password), m.Salt, 32768, 8, 1, 32)
	if err != nil {
		return payload, m, errors.New("备份解密失败")
	}
	defer clear(key)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, m.Nonce, files["backup.enc"], metadata)
	if err != nil {
		return payload, m, errors.New("备份密码不正确，或文件已损坏")
	}
	defer clear(plain)
	if json.Unmarshal(plain, &payload) != nil {
		return payload, m, errors.New("备份内容格式无效")
	}
	if (m.Format == "gatehouse-backup-v2") != (payload.Files != nil) {
		return payload, m, errors.New("备份格式与数据范围不一致")
	}
	migrateState(&payload.State)
	if err := migrateHomepageBackup(&payload); err != nil {
		return payload, m, err
	}
	if err := ValidateAdminUsername(payload.State.AdminUsername); err != nil {
		return payload, m, errors.New("备份的管理员账号数据无效")
	}
	if err := Validate(payload.State.Config); err != nil {
		return payload, m, errors.New("备份配置无效，无法恢复")
	}
	if _, err := bcrypt.Cost([]byte(payload.State.PasswordHash)); err != nil {
		return payload, m, errors.New("备份的管理员密码数据无效")
	}
	if len(payload.State.CloudflareToken) > 512 || len(payload.State.ProxyPassword) > 255 || strings.ContainsAny(payload.State.CloudflareToken+payload.State.ProxyPassword, "\r\n") {
		return payload, m, errors.New("备份凭据格式无效")
	}
	if err := validateCredentials(payload.State); err != nil {
		return payload, m, errors.New("备份中的 DNS 凭据缺失或格式错误")
	}
	if err := validateHomepageUsers(payload.State); err != nil {
		return payload, m, err
	}
	for name, data := range payload.Certificates {
		if !safeCertificateName(name) || len(data) > 1<<20 || !json.Valid(data) {
			return payload, m, errors.New("备份证书数据无效")
		}
	}
	if err := validateBackupFiles(payload); err != nil {
		return payload, m, err
	}
	return payload, m, nil
}

// Called by the supervisor after the old process has stopped.
func restorePayload(dir string, payload backupPayload) error {
	state, err := json.Marshal(payload.State)
	if err != nil {
		return err
	}
	return restoreFiles(dir, state, payload.Certificates, payload.Files)
}

type restoreTarget struct {
	name, path string
	directory  bool
}

func restoreTargets(paths StoragePaths, full bool, incoming map[string][]byte) []restoreTarget {
	targets := []restoreTarget{{"certificates", paths.directory("certificates"), true}}
	if full {
		for _, name := range []string{"route-images", "homepage-backgrounds", "subscriptions", "page"} {
			targets = append(targets, restoreTarget{name, paths.directory(name), true})
		}
		if paths.splitLogs() {
			names := map[string]bool{"logs/calls.jsonl": true, "logs/calls.jsonl.1": true}
			entries, _ := os.ReadDir(paths.Log)
			for _, entry := range entries {
				name := strings.TrimSuffix(entry.Name(), ".restore-old")
				if _, ok := logFileNumber(name); ok {
					names["logs/"+name] = true
				}
			}
			for name := range incoming {
				if strings.HasPrefix(name, "logs/") && safeBackupFile(name) {
					names[name] = true
				}
			}
			ordered := make([]string, 0, len(names))
			for name := range names {
				ordered = append(ordered, name)
			}
			sort.Strings(ordered)
			for _, name := range ordered {
				targets = append(targets, restoreTarget{name, paths.file(name), false})
			}
		} else {
			targets = append(targets, restoreTarget{"logs", paths.Log, true})
		}
	}
	return targets
}

func restoreFiles(dir string, state []byte, certificates, files map[string][]byte) error {
	return restoreFilesPaths(legacyStorage(dir), state, certificates, files)
}

func restoreFilesPaths(paths StoragePaths, state []byte, certificates, files map[string][]byte) error {
	if !json.Valid(state) {
		return errors.New("配置数据无效")
	}
	stage, err := os.MkdirTemp(paths.Data, ".restore-files-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	logStage := ""
	if files != nil && paths.splitLogs() {
		if err := os.MkdirAll(paths.Log, 0700); err != nil {
			return err
		}
		logStage, err = os.MkdirTemp(paths.Log, ".restore-files-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(logStage)
	}
	staged := func(name string) string {
		if logStage != "" && strings.HasPrefix(name, "logs/") {
			return filepath.Join(logStage, strings.TrimPrefix(name, "logs/"))
		}
		return filepath.Join(stage, filepath.FromSlash(name))
	}
	targets := restoreTargets(paths, files != nil, files)
	for _, target := range targets {
		if target.directory {
			if err := os.Mkdir(staged(target.name), 0700); err != nil {
				return errors.New("恢复目录准备失败")
			}
		}
		if _, err := os.Lstat(target.path + ".restore-old"); !errors.Is(err, os.ErrNotExist) {
			return errors.New("发现未处理的旧恢复目录或文件")
		}
	}
	for name, data := range certificates {
		if !safeCertificateName(name) {
			return errors.New("无效的证书文件名")
		}
		if err := atomicWrite(staged("certificates/"+name), data); err != nil {
			return err
		}
	}
	for name, data := range files {
		if !safeBackupFile(name) {
			return errors.New("无效的备份数据路径")
		}
		if strings.HasPrefix(name, "page/") {
			if err := os.MkdirAll(filepath.Dir(staged(name)), 0700); err != nil {
				return err
			}
		}
		if err := atomicWrite(staged(name), data); err != nil {
			return err
		}
	}
	type replacement struct {
		path   string
		hadOld bool
	}
	replaced := []replacement{}
	committed := false
	defer func() {
		if committed {
			return
		}
		for i := len(replaced) - 1; i >= 0; i-- {
			item := replaced[i]
			os.RemoveAll(item.path)
			if item.hadOld {
				os.Rename(item.path+".restore-old", item.path)
			}
		}
	}()
	for _, target := range targets {
		info, err := os.Lstat(target.path)
		hadOld := err == nil
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if hadOld {
			if info.Mode()&os.ModeSymlink != 0 || (target.directory && !info.IsDir()) || (!target.directory && !info.Mode().IsRegular()) {
				return errors.New("恢复目标类型无效")
			}
			if err := os.Rename(target.path, target.path+".restore-old"); err != nil {
				return err
			}
		}
		replaced = append(replaced, replacement{target.path, hadOld})
		_, err = os.Stat(staged(target.name))
		if errors.Is(err, os.ErrNotExist) && !target.directory {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.Rename(staged(target.name), target.path); err != nil {
			return err
		}
	}
	if err := atomicWrite(paths.file("state.json"), state); err != nil {
		return err
	}
	committed = true
	for _, item := range replaced {
		if item.hadOld {
			if err := os.RemoveAll(item.path + ".restore-old"); err != nil {
				return err
			}
		}
	}
	return nil
}

func snapshotDisk(dir string) (diskBackup, error) {
	return snapshotDiskPaths(legacyStorage(dir))
}

func snapshotDiskPaths(paths StoragePaths) (diskBackup, error) {
	state, err := os.ReadFile(paths.file("state.json"))
	if err != nil || !json.Valid(state) {
		return diskBackup{}, errors.New("当前配置无法备份")
	}
	var typed State
	if json.Unmarshal(state, &typed) != nil {
		return diskBackup{}, errors.New("当前配置无法备份")
	}
	payload, err := snapshotBackupPaths(paths, typed)
	return diskBackup{State: state, Certificates: payload.Certificates, Files: payload.Files}, err
}
