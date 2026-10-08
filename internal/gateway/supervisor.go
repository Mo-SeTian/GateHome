package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// The immutable launcher remains outside the directory writable by the service.
func Supervise(ctx context.Context, data, appDir, admin string) error {
	return SupervisePaths(ctx, legacyStorage(data), appDir, admin)
}

func SupervisePaths(ctx context.Context, paths StoragePaths, appDir, admin string) error {
	if appDir == "" {
		return errors.New("启动程序需要 -managed-root")
	}
	if err := os.MkdirAll(appDir, 0700); err != nil {
		return errors.New("程序目录无法创建")
	}
	binary := filepath.Join(appDir, "gatehouse")
	if _, err := os.Stat(binary); errors.Is(err, os.ErrNotExist) {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if err := copyProgram(exe, binary); err != nil {
			return errors.New("程序初始化失败")
		}
	}
	if os.Getenv("GATEHOUSE_CONTAINER") == "1" {
		if err := syncContainerProgram(ctx, binary); err != nil {
			return err
		}
	}
	m, err := NewMaintenancePaths(paths, appDir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(m.dir, "transition.json")); err == nil {
		if err := m.rollbackTransition(); err != nil {
			return errors.New("中断维护的回滚失败，请检查数据目录")
		}
	}
	transition, err := m.beginTransition()
	if err != nil {
		if transition {
			if err := m.rollbackTransition(); err != nil {
				return errors.New("维护回滚失败")
			}
		} else {
			if err := m.finishTransition(); err != nil {
				return errors.New("维护准备失败且取消操作失败")
			}
			log.Print("维护准备失败，已取消操作并保留原版本")
		}
		transition = false
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		expectedVersion := ""
		if transition {
			b, _ := os.ReadFile(filepath.Join(m.dir, "transition.json"))
			var op maintenanceOperation
			json.Unmarshal(b, &op)
			expectedVersion = op.Version
		}
		args := []string{"-data", paths.Data, "-admin", admin, "-managed-root", appDir}
		if filepath.Clean(paths.Config) != filepath.Clean(paths.Data) {
			args = append(args, "-config", paths.Config)
		}
		if paths.splitLogs() {
			args = append(args, "-log", paths.Log)
		}
		cmd := exec.Command(binary, args...)
		cmd.Env = append(os.Environ(), "GATEHOUSE_SUPERVISED=1", "GATEHOUSE_BACKUP_FILES=1")
		if err := os.Remove(filepath.Join(m.dir, "ready.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("旧启动检查状态清理失败")
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = nil
		if err := cmd.Start(); err != nil {
			if transition {
				if err := m.rollbackTransition(); err != nil {
					return errors.New("更新启动失败且回滚失败")
				}
				transition = false
				continue
			}
			return errors.New("服务子进程启动失败")
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		ready := false
		deadline := time.NewTimer(20 * time.Second)
		ticker := time.NewTicker(100 * time.Millisecond)
		var exited bool
		for !ready && !exited {
			select {
			case <-ctx.Done():
				stopChild(cmd, done)
				deadline.Stop()
				ticker.Stop()
				return nil
			case <-done:
				exited = true
			case <-deadline.C:
				stopChild(cmd, done)
				exited = true
			case <-ticker.C:
				b, _ := os.ReadFile(filepath.Join(m.dir, "ready.json"))
				var value struct {
					PID     int    `json:"pid"`
					Version string `json:"version"`
				}
				if json.Unmarshal(b, &value) == nil && value.PID == cmd.Process.Pid && (expectedVersion == "" || expectedVersion == value.Version) {
					ready = true
				}
			}
		}
		deadline.Stop()
		ticker.Stop()
		if !ready {
			if transition {
				if err := m.rollbackTransition(); err != nil {
					return errors.New("新版本启动检查失败且回滚失败")
				}
				log.Print("维护启动检查失败，已回滚程序和配置")
				transition = false
				continue
			}
			return errors.New("服务启动检查失败，请检查配置或端口占用")
		}
		if transition {
			if err := m.finishTransition(); err != nil {
				stopChild(cmd, done)
				return errors.New("维护完成状态保存失败")
			}
			log.Print("维护完成，新服务已启动")
			transition = false
		}
		select {
		case <-ctx.Done():
			stopChild(cmd, done)
			return nil
		case <-done:
		}
		transition, err = m.beginTransition()
		if err != nil {
			if transition {
				if err := m.rollbackTransition(); err != nil {
					return errors.New("维护失败且回滚失败")
				}
				transition = false
			} else {
				if err := m.finishTransition(); err != nil {
					return errors.New("维护准备失败且取消操作失败")
				}
			}
			log.Print("维护失败，已取消操作并重新启动原版本")
			continue
		}
		if !transition {
			return errors.New("服务子进程退出")
		}
	}
}

// Retain newer Web updates, but replace an older persisted worker on image upgrade.
func syncContainerProgram(ctx context.Context, binary string) error {
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probe, binary, "-version").Output()
	if err != nil {
		return errors.New("持久化程序版本读取失败")
	}
	comparison, err := compareVersions(strings.TrimSpace(string(output)), Version)
	if err != nil {
		return errors.New("持久化程序版本无效")
	}
	if comparison < 0 {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		if err := copyProgram(executable, binary); err != nil {
			return errors.New("镜像升级时程序替换失败")
		}
	}
	return nil
}

func stopChild(cmd *exec.Cmd, done <-chan error) {
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(12 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

func readUpload(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil || int64(len(b)) > max {
		return nil, errors.New("上传文件读取失败或超过大小限制")
	}
	return b, nil
}
