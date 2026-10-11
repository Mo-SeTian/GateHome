package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

type MCPConfig struct {
	Enabled        bool `json:"enabled"`
	Read           bool `json:"read"`
	Write          bool `json:"write"`
	Maintenance    bool `json:"maintenance"`
	SunPanelUserID uint `json:"sunpanel_user_id"`
}

type mcpInternalKey struct{}

func validMCPToken(state State, header string) bool {
	token, ok := strings.CutPrefix(header, "Bearer ")
	sum := sha256.Sum256([]byte(token))
	return state.MCP.Enabled && ok && len(token) == 70 && strings.HasPrefix(token, "ghmcp_") && len(state.MCPTokenHash) == 64 && subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(state.MCPTokenHash)) == 1
}

func (s *Store) saveMCP(config MCPConfig, rotate bool, revision int) (string, error) {
	if config.Enabled && !config.Read && !config.Write && !config.Maintenance {
		return "", errors.New("启用 MCP 时请至少授权一项权限")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Revision != revision {
		return "", errors.New("配置已变化，请刷新后重试")
	}
	next := s.state
	next.MCP = config
	token := ""
	if rotate || (config.Enabled && next.MCPTokenHash == "") {
		secret := make([]byte, 32)
		rand.Read(secret)
		token = "ghmcp_" + hex.EncodeToString(secret)
		sum := sha256.Sum256([]byte(token))
		next.MCPTokenHash = hex.EncodeToString(sum[:])
	}
	next.Revision++
	if err := writeJSON(s.path, next); err != nil {
		return "", errors.New("MCP 设置保存失败，请检查配置目录权限")
	}
	s.state = next
	return token, nil
}

func (a *Admin) mcpSettingsRoutes(mux *http.ServeMux) {
	status := func(w http.ResponseWriter, token string) {
		state := a.store.Snapshot()
		jsonResponse(w, 200, map[string]any{"config": state.MCP, "token_configured": state.MCPTokenHash != "", "revision": state.Revision, "token": token})
	}
	mux.HandleFunc("GET /api/mcp", a.requireAuth(func(w http.ResponseWriter, r *http.Request) { status(w, "") }))
	mux.HandleFunc("GET /api/mcp/sunpanel-users", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if a.sunPanel == nil {
			apiError(w, 503, "Sun-Panel 未运行，请启用并重启服务后刷新账号")
			return
		}
		data, err := a.sunPanel.bridgeCall(r.Context(), "users", 0, nil)
		if err != nil {
			apiError(w, 503, err.Error())
			return
		}
		jsonResponse(w, 200, data)
	}))
	mux.HandleFunc("PUT /api/mcp", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Config   MCPConfig `json:"config"`
			Revision int       `json:"revision"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		if input.Config.SunPanelUserID != 0 && input.Config.SunPanelUserID != a.store.Snapshot().MCP.SunPanelUserID {
			if a.sunPanel == nil {
				apiError(w, 400, "请先启用 Sun-Panel 并刷新账号")
				return
			}
			if _, err := a.sunPanel.bridgeCall(r.Context(), "desktop", input.Config.SunPanelUserID, nil); err != nil {
				apiError(w, 400, "所选 Sun-Panel 账号不可用，请刷新账号后重试")
				return
			}
		}
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		if a.maintenance.Busy() {
			apiError(w, 409, "维护执行中，请稍后修改 MCP 设置")
			return
		}
		token, err := a.store.saveMCP(input.Config, false, input.Revision)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		status(w, token)
	}))
	mux.HandleFunc("POST /api/mcp/rotate-token", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Revision int `json:"revision"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		if a.maintenance.Busy() {
			apiError(w, 409, "维护执行中，请稍后重置 MCP 令牌")
			return
		}
		token, err := a.store.saveMCP(a.store.Snapshot().MCP, true, input.Revision)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		status(w, token)
	}))
}
