package gateway

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type AdminAccessConfig struct {
	Enabled bool     `json:"enabled"`
	Origins []string `json:"origins"`
}

func adminOrigin(value string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(value, "#") || (u.Path != "" && u.Path != "/") || strings.Contains(u.Host, "*") || strings.HasSuffix(u.Host, ":") || len(value) > 320 {
		return "", false
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return "", false
		}
	}
	host := strings.ToLower(u.Host)
	if u.Scheme == "http" && u.Port() == "80" {
		host = strings.TrimSuffix(host, ":80")
	}
	if u.Scheme == "https" && u.Port() == "443" {
		host = strings.TrimSuffix(host, ":443")
	}
	return u.Scheme + "://" + host, true
}

func validateAdminAccess(c AdminAccessConfig) error {
	if len(c.Origins) > 20 || (c.Enabled && len(c.Origins) == 0) {
		return errors.New("启用反代访问时请填写公网访问地址，最多 20 个")
	}
	seen := map[string]bool{}
	for _, value := range c.Origins {
		origin, ok := adminOrigin(value)
		if !ok || seen[origin] {
			return errors.New("公网访问地址须为不重复的 http(s)://域名[:端口]，不含路径、账号或参数")
		}
		seen[origin] = true
	}
	return nil
}

func allowedAdminOrigin(c AdminAccessConfig, value string) bool {
	if !c.Enabled {
		return false
	}
	origin, ok := adminOrigin(value)
	if !ok {
		return false
	}
	for _, value := range c.Origins {
		if configured, ok := adminOrigin(value); ok && configured == origin {
			return true
		}
	}
	return false
}

func (a *Admin) requestOriginAllowed(r *http.Request) bool {
	value := r.Header.Get("Origin")
	if value == "" {
		return true
	}
	origin, ok := adminOrigin(value)
	if !ok {
		return false
	}
	access := a.store.Snapshot().Config.AdminAccess
	if allowedAdminOrigin(access, value) {
		return true
	}
	if access.Enabled {
		source, _ := url.Parse(origin)
		for _, configured := range access.Origins {
			normalized, _ := adminOrigin(configured)
			public, _ := url.Parse(normalized)
			if strings.EqualFold(public.Host, source.Host) {
				return false
			}
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	native, nativeOK := adminOrigin(scheme + "://" + r.Host)
	return nativeOK && origin == native
}

func (a *Admin) secureSession(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	origin, ok := adminOrigin(r.Header.Get("Origin"))
	return ok && strings.HasPrefix(origin, "https://") && allowedAdminOrigin(a.store.Snapshot().Config.AdminAccess, origin)
}
