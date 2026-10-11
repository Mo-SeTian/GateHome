package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

type WebDAVBackupConfig struct {
	URL             string `json:"url"`
	Username        string `json:"username"`
	Keep            int    `json:"keep"`
	ScheduleEnabled bool   `json:"schedule_enabled"`
	ScheduleMode    string `json:"schedule_mode"`
	Time            string `json:"time"`
	Timezone        string `json:"timezone"`
	Weekday         int    `json:"weekday"`
}
type WebDAVBackupState struct {
	Config         WebDAVBackupConfig `json:"config"`
	Password       string             `json:"password,omitempty"`
	BackupPassword string             `json:"backup_password,omitempty"`
	InstanceID     string             `json:"instance_id,omitempty"`
	LastSlot       string             `json:"last_slot,omitempty"`
	LastAttempt    time.Time          `json:"last_attempt,omitempty"`
	LastSuccess    time.Time          `json:"last_success,omitempty"`
	LastFile       string             `json:"last_file,omitempty"`
	LastError      string             `json:"last_error,omitempty"`
}
type remoteBackupJob struct {
	Phase      string    `json:"phase"`
	Name       string    `json:"name,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}
type remoteBackupRuntime struct {
	mu   sync.Mutex
	job  remoteBackupJob
	ctx  context.Context
	busy bool
}

func validateWebDAVBackupState(state WebDAVBackupState) error {
	if state.Config.URL == "" && !state.Config.ScheduleEnabled && state.InstanceID == "" {
		return nil
	}
	if err := validateWebDAVConfig(normalizeWebDAVConfig(state.Config)); err != nil {
		return err
	}
	if !regexp.MustCompile(`^[a-f0-9]{24}$`).MatchString(state.InstanceID) {
		return errors.New("WebDAV 备份标识无效")
	}
	if state.Config.URL != "" && state.BackupPassword == "" {
		return errors.New("WebDAV 备份加密密码缺失")
	}
	return nil
}

func normalizeWebDAVConfig(config WebDAVBackupConfig) WebDAVBackupConfig {
	config.URL = strings.TrimSpace(config.URL)
	if config.Keep == 0 {
		config.Keep = 7
	}
	if config.ScheduleMode == "" {
		config.ScheduleMode = "daily"
	}
	if config.Time == "" {
		config.Time = "03:00"
	}
	if config.Timezone == "" {
		config.Timezone = "Asia/Shanghai"
	}
	return config
}
func validateWebDAVConfig(config WebDAVBackupConfig) error {
	if config.Keep < 1 || config.Keep > 100 {
		return errors.New("保留份数需为 1–100")
	}
	if config.ScheduleMode != "daily" && config.ScheduleMode != "weekly" || config.Weekday < 0 || config.Weekday > 6 {
		return errors.New("备份计划无效")
	}
	if len(config.Time) != 5 {
		return errors.New("备份时间需为 HH:MM")
	}
	if _, err := time.Parse("15:04", config.Time); err != nil {
		return errors.New("备份时间需为 HH:MM")
	}
	if _, err := time.LoadLocation(config.Timezone); err != nil {
		return errors.New("请填写有效时区，例如 Asia/Shanghai 或 UTC")
	}
	if len(config.Username) > 255 || strings.ContainsAny(config.Username, "\r\n") {
		return errors.New("WebDAV 账号无效")
	}
	if config.URL == "" {
		if config.ScheduleEnabled {
			return errors.New("定时备份需要先配置 WebDAV")
		}
		return nil
	}
	_, err := webDAVBase(config.URL)
	return err
}

// The latest due slot is stable across restarts and independent of the host timezone.
func webDAVScheduleSlot(config WebDAVBackupConfig, now time.Time) (string, time.Time) {
	location, _ := time.LoadLocation(config.Timezone)
	if location == nil {
		return "", time.Time{}
	}
	local := now.In(location)
	parts := strings.Split(config.Time, ":")
	if len(parts) != 2 {
		return "", time.Time{}
	}
	hour, _ := strconv.Atoi(parts[0])
	minute, _ := strconv.Atoi(parts[1])
	slot := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, location)
	if config.ScheduleMode == "weekly" {
		days := (int(local.Weekday()) - config.Weekday + 7) % 7
		slot = slot.AddDate(0, 0, -days)
	}
	if slot.After(local) {
		if config.ScheduleMode == "weekly" {
			slot = slot.AddDate(0, 0, -7)
		} else {
			slot = slot.AddDate(0, 0, -1)
		}
	}
	key := config.Timezone + "|" + config.ScheduleMode + "|" + config.Time + "|" + strconv.Itoa(config.Weekday) + "|" + slot.Format("2006-01-02")
	return key, slot
}
func webDAVNextRun(state WebDAVBackupState, now time.Time) *time.Time {
	config := normalizeWebDAVConfig(state.Config)
	if !config.ScheduleEnabled {
		return nil
	}
	key, slot := webDAVScheduleSlot(config, now)
	if key == "" {
		return nil
	}
	if key != state.LastSlot && state.LastSlot != "" {
		return &now
	}
	if config.ScheduleMode == "weekly" {
		slot = slot.AddDate(0, 0, 7)
	} else {
		slot = slot.AddDate(0, 0, 1)
	}
	return &slot
}
func (s *Store) saveWebDAVBackup(config WebDAVBackupConfig, password, backupPassword *string, revision int) error {
	config = normalizeWebDAVConfig(config)
	if err := validateWebDAVConfig(config); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Revision != revision {
		return errors.New("配置已变化，请刷新后重试")
	}
	next := s.state
	prior := next.WebDAVBackup
	if config.URL != prior.Config.URL || config.Username != prior.Config.Username {
		prior.Password = ""
	}
	if password != nil {
		prior.Password = *password
	}
	if backupPassword != nil {
		prior.BackupPassword = *backupPassword
	}
	if config.URL != "" && prior.BackupPassword == "" {
		return errors.New("远程备份包含配置与凭据，请设置备份加密密码")
	}
	if prior.InstanceID == "" {
		id := make([]byte, 12)
		if _, err := rand.Read(id); err != nil {
			return errors.New("备份标识创建失败")
		}
		prior.InstanceID = hex.EncodeToString(id)
	}
	if config.ScheduleEnabled && (!prior.Config.ScheduleEnabled || config.ScheduleMode != prior.Config.ScheduleMode || config.Time != prior.Config.Time || config.Timezone != prior.Config.Timezone || config.Weekday != prior.Config.Weekday) {
		prior.LastSlot, _ = webDAVScheduleSlot(config, time.Now())
	}
	prior.Config = config
	next.WebDAVBackup = prior
	next.Revision++
	if writeJSON(s.path, next) != nil {
		return errors.New("WebDAV 设置保存失败，请检查配置目录权限")
	}
	s.state = next
	return nil
}
func (s *Store) recordWebDAVBackup(slot, name, message string, success bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state
	if slot != "" {
		next.WebDAVBackup.LastSlot = slot
	}
	if name == "" && message == "" && !success {
		next.WebDAVBackup.LastAttempt = time.Now().UTC()
	} else {
		next.WebDAVBackup.LastError = message
		if success {
			next.WebDAVBackup.LastSuccess = time.Now().UTC()
			next.WebDAVBackup.LastFile = name
		}
	}
	if writeJSON(s.path, next) != nil {
		return errors.New("备份运行记录保存失败，请检查目录权限")
	}
	s.state = next
	return nil
}
func (a *Admin) backupSnapshot() (backupPayload, error) {
	state := a.store.Snapshot()
	if err := checkBackupCapacity(a.store.paths, state); err != nil {
		return backupPayload{}, err
	}
	if a.logs != nil {
		a.logs.mu.Lock()
		defer a.logs.mu.Unlock()
	}
	if a.sunPanel != nil {
		return a.sunPanel.snapshot(a.store.paths, state)
	}
	return snapshotBackupPaths(a.store.paths, state)
}
func (a *Admin) setRemoteBackupPhase(phase, name, message string) {
	a.webDAV.mu.Lock()
	a.webDAV.job.Phase = phase
	a.webDAV.job.Name = name
	a.webDAV.job.Error = message
	a.webDAV.mu.Unlock()
}
func (a *Admin) startWebDAVBackup(slot string) error {
	a.webDAV.mu.Lock()
	defer a.webDAV.mu.Unlock()
	if a.webDAV.busy {
		return errors.New("WebDAV 操作正在执行，请稍后重试")
	}
	state := a.store.Snapshot()
	if state.WebDAVBackup.Config.URL == "" || state.WebDAVBackup.BackupPassword == "" {
		return errors.New("请先保存 WebDAV 与备份加密密码")
	}
	if a.maintenance == nil || a.maintenance.Busy() {
		return errors.New("维护正在执行或备份功能未初始化")
	}
	if slot == "" && state.WebDAVBackup.Config.ScheduleEnabled {
		slot, _ = webDAVScheduleSlot(normalizeWebDAVConfig(state.WebDAVBackup.Config), time.Now())
	}
	if err := a.store.recordWebDAVBackup(slot, "", "", false); err != nil {
		return err
	}
	a.webDAV.busy = true
	a.webDAV.job = remoteBackupJob{Phase: "snapshot", StartedAt: time.Now().UTC()}
	parent := a.webDAV.ctx
	if parent == nil {
		parent = context.Background()
	}
	go func() {
		ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
		defer cancel()
		name, message, success := "", "", false
		defer func() {
			if err := a.store.recordWebDAVBackup("", name, message, success); err != nil {
				message = err.Error()
			}
			a.webDAV.mu.Lock()
			a.webDAV.busy = false
			a.webDAV.job.Name = name
			a.webDAV.job.Error = message
			a.webDAV.job.FinishedAt = time.Now().UTC()
			if message != "" {
				a.webDAV.job.Phase = "failed"
			} else {
				a.webDAV.job.Phase = "done"
			}
			a.webDAV.mu.Unlock()
			if a.logs != nil {
				a.logs.Add(LogEntry{Category: "admin", Action: "WebDAV 备份", Target: name, OK: success, Message: map[bool]string{true: "整站备份上传并校验完成", false: "WebDAV 备份未完成"}[success]})
			}
		}()
		a.updateMu.Lock()
		if a.maintenance.Busy() {
			a.updateMu.Unlock()
			message = "维护正在执行，请稍后重试"
			return
		}
		payload, err := a.backupSnapshot()
		a.updateMu.Unlock()
		if err != nil {
			message = err.Error()
			return
		}
		a.setRemoteBackupPhase("encrypt", "", "")
		archive, err := encodeBackup(payload, state.WebDAVBackup.BackupPassword)
		if err != nil {
			message = err.Error()
			return
		}
		if ctx.Err() != nil {
			message = "备份已中断，请重新执行"
			return
		}
		client, err := newWebDAVClient(state)
		if err != nil {
			message = err.Error()
			return
		}
		defer client.close()
		nonce := make([]byte, 6)
		if _, err := rand.Read(nonce); err != nil {
			message = "备份名称生成失败"
			return
		}
		filename := "gatehome-backup-" + state.WebDAVBackup.InstanceID + "-" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(nonce) + ".zip"
		a.setRemoteBackupPhase("upload", filename, "")
		name, err = client.upload(ctx, filename, archive, normalizeWebDAVConfig(state.WebDAVBackup.Config).Keep)
		if err != nil {
			message = err.Error()
			return
		}
		success = true
	}()
	return nil
}
func (a *Admin) StartWebDAVBackups(ctx context.Context) {
	a.webDAV.mu.Lock()
	a.webDAV.ctx = ctx
	a.webDAV.mu.Unlock()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		check := func() {
			state := a.store.Snapshot().WebDAVBackup
			config := normalizeWebDAVConfig(state.Config)
			if !config.ScheduleEnabled {
				return
			}
			slot, _ := webDAVScheduleSlot(config, time.Now())
			if slot != "" && slot != state.LastSlot {
				a.startWebDAVBackup(slot)
			}
		}
		check()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				check()
			}
		}
	}()
}
func (a *Admin) webDAVResponse(w http.ResponseWriter) {
	state := a.store.Snapshot()
	settings := state.WebDAVBackup
	a.webDAV.mu.Lock()
	job, busy := a.webDAV.job, a.webDAV.busy
	a.webDAV.mu.Unlock()
	jsonResponse(w, 200, map[string]any{"config": normalizeWebDAVConfig(settings.Config), "revision": state.Revision, "password_configured": settings.Password != "", "backup_password_configured": settings.BackupPassword != "", "last_attempt": settings.LastAttempt, "last_success": settings.LastSuccess, "last_file": settings.LastFile, "last_error": settings.LastError, "next_run": webDAVNextRun(settings, time.Now()), "job": job, "busy": busy})
}
func (a *Admin) webDAVBackupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/backups/webdav", a.requireAuth(func(w http.ResponseWriter, r *http.Request) { a.webDAVResponse(w) }))
	mux.HandleFunc("PUT /api/backups/webdav", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Config         WebDAVBackupConfig `json:"config"`
			Password       *string            `json:"password"`
			BackupPassword *string            `json:"backup_password"`
			Revision       int                `json:"revision"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		a.webDAV.mu.Lock()
		defer a.webDAV.mu.Unlock()
		if a.webDAV.busy || a.maintenance.Busy() {
			apiError(w, 409, "正在备份或维护，请稍后保存")
			return
		}
		if err := a.store.saveWebDAVBackup(input.Config, input.Password, input.BackupPassword, input.Revision); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]any{"saved": true})
	}))
	mux.HandleFunc("POST /api/backups/webdav/run", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct{}
		if !decodeBody(w, r, &input) {
			return
		}
		if err := a.startWebDAVBackup(""); err != nil {
			apiError(w, 409, err.Error())
			return
		}
		jsonResponse(w, 202, map[string]string{"message": "整站备份已开始"})
	}))
	mux.HandleFunc("GET /api/backups/webdav/files", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		client, err := newWebDAVClient(a.store.Snapshot())
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		defer client.close()
		files, err := client.list(r.Context())
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		jsonResponse(w, 200, files)
	}))
	mux.HandleFunc("POST /api/backups/webdav/test", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct{}
		if !decodeBody(w, r, &input) {
			return
		}
		client, err := newWebDAVClient(a.store.Snapshot())
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		defer client.close()
		if err := client.ensureCollection(r.Context(), client.base, 12); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		if _, err := client.list(r.Context()); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]string{"message": "WebDAV 目录连接成功；上传与删除权限在备份时校验"})
	}))
	mux.HandleFunc("POST /api/backups/webdav/inspect", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Name     string  `json:"name"`
			Password *string `json:"password"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		state := a.store.Snapshot()
		client, err := newWebDAVClient(state)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		defer client.close()
		data, err := client.get(r.Context(), input.Name)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		password := state.WebDAVBackup.BackupPassword
		if input.Password != nil {
			password = *input.Password
		}
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		if a.maintenance == nil || a.maintenance.Busy() {
			apiError(w, 409, "维护功能不可用或正在执行")
			return
		}
		preview, err := a.maintenance.inspectBackup(data, password, a.adminPort)
		password = ""
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		jsonResponse(w, 200, preview)
	}))
	mux.HandleFunc("POST /api/backups/webdav/download", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Name string `json:"name"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		client, err := newWebDAVClient(a.store.Snapshot())
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		defer client.close()
		data, err := client.get(r.Context(), input.Name)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+input.Name+`"`)
		w.Write(data)
	}))
}
