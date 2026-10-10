package gateway

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SunPanelConfig struct {
	Enabled     bool   `json:"enabled"`
	Port        int    `json:"port"`
	LaunchURL   string `json:"launch_url,omitempty"`
	ExternalURL string `json:"external_url,omitempty"`
}

func validateSunPanelListener(c SunPanelConfig, groups []ProxyGroup) error {
	for _, address := range []struct {
		name, value string
		allowPath   bool
	}{{"内网", c.LaunchURL, true}, {"外网", c.ExternalURL, false}} {
		if address.value == "" {
			continue
		}
		u, err := url.Parse(address.value)
		if err != nil || len(address.value) > 2048 || strings.ContainsAny(address.value, " \\\t\r\n") || u.User != nil {
			return fmt.Errorf("Sun-Panel %s地址须为有效地址，不能包含账号密码或空白字符", address.name)
		}
		absolute := (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) && u.Hostname() != ""
		localPath := address.allowPath && strings.HasPrefix(address.value, "/") && !strings.HasPrefix(address.value, "//") && u.Scheme == "" && u.Host == ""
		if !absolute && !localPath {
			if address.allowPath {
				return errors.New("Sun-Panel 内网地址须为 HTTP(S) 地址或以 / 开头的本站路径")
			}
			return errors.New("Sun-Panel 外网地址须为完整 HTTP(S) 地址")
		}
		if u.Port() != "" {
			port, err := strconv.Atoi(u.Port())
			if err != nil || port < 1 || port > 65535 {
				return fmt.Errorf("Sun-Panel %s地址中的端口须为 1–65535", address.name)
			}
		}
	}
	if c.Port == 0 && !c.Enabled {
		return nil
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("Sun-Panel 端口须为 1–65535")
	}
	if c.Enabled {
		for _, g := range groups {
			if c.Port == g.HTTPPort || c.Port == g.HTTPSPort {
				return errors.New("Sun-Panel 端口不能与业务端口相同")
			}
		}
	}
	return nil
}

// The worker shares the release binary, but isolates upstream globals and cwd.
type SunPanel struct {
	failure chan error
	mu      sync.Mutex
	dir     string
	cmd     *exec.Cmd
	done    chan error
	proxy   *httputil.ReverseProxy
}

func NewSunPanel(paths StoragePaths) *SunPanel {
	return &SunPanel{dir: paths.sunPanelDir(), failure: make(chan error, 1)}
}

func (s *SunPanel) Start() error { s.mu.Lock(); defer s.mu.Unlock(); return s.start() }
func (s *SunPanel) start() error {
	if s.cmd != nil {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	dir, err := filepath.Abs(s.dir)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		return err
	}
	defer listener.Close()
	file, err := listener.File()
	if err != nil {
		return err
	}
	defer file.Close()
	target, _ := url.Parse("http://" + listener.Addr().String())
	cmd := exec.Command(exe, "-sunpanel-worker", dir)
	configureSunPanelProcess(cmd)
	cmd.ExtraFiles = []*os.File{file}
	// Upstream logs may contain account data; never forward them to GateHome logs.
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return errors.New("Sun-Panel 子进程启动失败")
	}
	s.cmd, s.done = cmd, make(chan error, 1)
	done := s.done
	go func() {
		err := cmd.Wait()
		done <- err
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.cmd == cmd {
			select {
			case s.failure <- errors.New("Sun-Panel 子进程意外退出"):
			default:
			}
		}
	}()
	client := &http.Client{Timeout: 300 * time.Millisecond}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-s.done:
			s.cmd = nil
			return errors.New("Sun-Panel 初始化失败，请检查 sunpanel 目录权限")
		default:
		}
		response, err := client.Get(target.String() + "/sunpanel/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 204 {
				s.proxy = httputil.NewSingleHostReverseProxy(target)
				s.proxy.ErrorLog = nil
				s.proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
					http.Error(w, "Sun-Panel 暂时不可用", 503)
				}
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.stop()
	return errors.New("Sun-Panel 启动超时")
}
func (s *SunPanel) stop() {
	if s.cmd != nil {
		stopChild(s.cmd, s.done)
		s.cmd = nil
		s.proxy = nil
	}
}
func (s *SunPanel) Stop() { s.mu.Lock(); defer s.mu.Unlock(); s.stop() }
func (s *SunPanel) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		proxy := s.proxy
		s.mu.Unlock()
		if proxy == nil {
			http.Error(w, "Sun-Panel 未运行或正在备份", 503)
			return
		}
		// Never forward GateHome's session to the vendored service.
		r = r.Clone(r.Context())
		stripSessionCookie(r, "gatehouse_session")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
		proxy.ServeHTTP(w, r)
	})
}

func (s *SunPanel) snapshot(paths StoragePaths, state State) (backupPayload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	running := s.cmd != nil
	if running {
		s.stop()
	}
	payload, err := snapshotBackupPaths(paths, state)
	if running {
		if startErr := s.start(); startErr != nil {
			return payload, fmt.Errorf("备份后重启失败：%w", startErr)
		}
	}
	return payload, err
}

func (m *Maintenance) checkSunPanelFiles() error {
	if !m.sunPanelFiles {
		return errors.New("请先更新 Linux 启动器或重建 Docker 镜像，确保 Sun-Panel 支持恢复与回滚")
	}
	return nil
}

func (s *SunPanel) Failures() <-chan error { return s.failure }
