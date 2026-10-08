package gateway

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"time"
)

func (a *Admin) SetMaintenance(m *Maintenance) { a.maintenance = m }

func (a *Admin) maintenanceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/maintenance/restart", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct{}
		if !decodeBody(w, r, &input) {
			return
		}
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		if a.maintenance == nil {
			apiError(w, 409, "启动管理程序未初始化")
			return
		}
		if a.maintenance.Available() {
			if err := checkRestartPorts(a.ports, a.store.Snapshot().Config); err != nil {
				apiError(w, 409, err.Error())
				return
			}
		}
		if err := a.maintenance.scheduleRestart(); err != nil {
			apiError(w, 409, err.Error())
			return
		}
		jsonResponse(w, 202, map[string]string{"message": "重启已安排，请稍后刷新并重新登录"})
		_ = http.NewResponseController(w).Flush()
		a.maintenance.requestRestart()
	}))
	mux.HandleFunc("GET /api/maintenance", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		available := a.maintenance != nil && a.maintenance.Available()
		jsonResponse(w, 200, map[string]any{"version": Version, "platform": runtime.GOOS + "/" + runtime.GOARCH, "can_apply": available, "busy": a.maintenance.Busy(), "max_update_bytes": maxUpdateBytes, "max_backup_bytes": maxBackupBytes + (1 << 20)})
	}))
	mux.HandleFunc("POST /api/maintenance/backup", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Password string `json:"password"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		if a.maintenance == nil || a.maintenance.Busy() {
			apiError(w, 409, "维护功能不可用或正在执行操作")
			return
		}
		// Keep both rotated log files at the same complete-record snapshot.
		if a.logs != nil {
			a.logs.mu.Lock()
		}
		payload, err := snapshotBackupPaths(a.store.paths, a.store.Snapshot())
		if a.logs != nil {
			a.logs.mu.Unlock()
		}
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		data, err := encodeBackup(payload, input.Password)
		input.Password = ""
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "gatehouse-backup-" + time.Now().UTC().Format("20060102-150405") + ".zip"}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(200)
		_, _ = w.Write(data)
	}))
	for _, kind := range []string{"update", "backup"} {
		mux.HandleFunc("POST /api/maintenance/inspect-"+kind, a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
			a.updateMu.Lock()
			defer a.updateMu.Unlock()
			if a.maintenance == nil {
				apiError(w, 409, "维护功能未初始化")
				return
			}
			data, password, err := maintenanceUpload(w, r)
			if err != nil {
				apiError(w, 400, err.Error())
				return
			}
			var result map[string]any
			if kind == "update" {
				result, err = a.maintenance.inspectUpdate(data)
			} else {
				result, err = a.maintenance.inspectBackup(data, password, a.adminPort)
			}
			password = ""
			if err != nil {
				apiError(w, 400, err.Error())
				return
			}
			jsonResponse(w, 200, result)
		}))
	}
	for _, kind := range []string{"update", "restore"} {
		mux.HandleFunc("POST /api/maintenance/apply-"+kind, a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				ID string `json:"id"`
			}
			if !decodeBody(w, r, &input) {
				return
			}
			a.updateMu.Lock()
			defer a.updateMu.Unlock()
			if a.maintenance == nil {
				apiError(w, 409, "维护功能未初始化")
				return
			}
			if err := a.maintenance.schedule(input.ID, kind); err != nil {
				apiError(w, 400, err.Error())
				return
			}
			jsonResponse(w, 202, map[string]string{"message": "维护操作已接受，服务即将重启；请稍后刷新并重新登录"})
			if controller := http.NewResponseController(w); controller.Flush() != nil { /* The operation remains accepted if the client disconnects. */
			}
			a.maintenance.requestRestart()
		}))
	}
}

func checkRestartPorts(current, next Config) error {
	owned := map[int]bool{}
	for _, g := range current.Groups {
		if g.Enabled {
			owned[g.HTTPPort], owned[g.HTTPSPort] = true, true
		}
	}
	for _, g := range next.Groups {
		if !g.Enabled {
			continue
		}
		for _, port := range []int{g.HTTPPort, g.HTTPSPort} {
			if port == 0 || owned[port] {
				continue
			}
			listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
			if err != nil {
				return fmt.Errorf("端口 %d 无法监听，请检查占用或权限；当前服务继续运行", port)
			}
			listener.Close()
		}
	}
	return nil
}

func maintenanceUpload(w http.ResponseWriter, r *http.Request) ([]byte, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpdateBytes+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, "", errors.New("请通过文件表单上传 ZIP")
	}
	var data []byte
	password := ""
	seen := map[string]bool{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", errors.New("上传读取失败")
		}
		name := part.FormName()
		if seen[name] {
			part.Close()
			return nil, "", errors.New("上传字段重复")
		}
		seen[name] = true
		switch name {
		case "file":
			data, err = readUpload(part, maxUpdateBytes)
		case "password":
			var b []byte
			b, err = io.ReadAll(part)
			password = string(b)
			clear(b)
		default:
			err = errors.New("不支持的上传字段")
		}
		part.Close()
		if err != nil {
			return nil, "", err
		}
	}
	if len(data) == 0 {
		return nil, "", errors.New("请选择 ZIP 文件")
	}
	return data, password, nil
}

// A readiness marker contains no credential and is only visible inside the data directory.
func MarkServiceReady(m *Maintenance) error { return m.markReady() }
