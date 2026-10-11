package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Hash is stored locally and in encrypted backups, never in management responses.
type MCPClient struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Config    MCPConfig  `json:"config"`
	Hash      string     `json:"hash,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

func mcpClientForToken(state State, header string) (MCPClient, bool) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !state.MCP.Enabled || !ok || len(token) != 70 || !strings.HasPrefix(token, "ghmcp_") {
		return MCPClient{}, false
	}
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])
	for _, client := range state.MCPClients {
		if client.Config.Enabled && client.RevokedAt == nil && (client.ExpiresAt == nil || time.Now().Before(*client.ExpiresAt)) && len(client.Hash) == 64 && subtle.ConstantTimeCompare([]byte(hash), []byte(client.Hash)) == 1 {
			return client, true
		}
	}
	if len(state.MCPTokenHash) == 64 && subtle.ConstantTimeCompare([]byte(hash), []byte(state.MCPTokenHash)) == 1 {
		return MCPClient{ID: "legacy", Name: "兼容 Token", Config: state.MCP}, true
	}
	return MCPClient{}, false
}

func publicMCPClients(state State) []MCPClient {
	clients := append([]MCPClient{}, state.MCPClients...)
	for i := range clients {
		clients[i].Hash = ""
	}
	return clients
}

func (s *Store) saveMCPClient(client MCPClient, rotate, revoke bool, revision int) (string, error) {
	client.Name = strings.TrimSpace(client.Name)
	if !revoke && (client.Name == "" || len([]rune(client.Name)) > 40 || strings.ContainsAny(client.Name, "\r\n") || strings.Contains(client.Name, "ghmcp_") || (!client.Config.Read && !client.Config.Write && !client.Config.Maintenance)) {
		return "", errors.New("请填写客户端名称并至少授权一项权限")
	}
	if !revoke && client.ExpiresAt != nil && !client.ExpiresAt.After(time.Now()) {
		return "", errors.New("有效期必须晚于当前时间")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Revision != revision {
		return "", errors.New("配置已变化，请刷新后重试")
	}
	next := s.state
	next.MCPClients = append([]MCPClient{}, next.MCPClients...)
	index := -1
	for i, row := range next.MCPClients {
		if row.ID == client.ID {
			index = i
			break
		}
	}
	if client.ID != "" && index < 0 {
		return "", errors.New("客户端不存在，请刷新")
	}
	if index >= 0 {
		prior := next.MCPClients[index]
		if prior.RevokedAt != nil {
			return "", errors.New("已撤销的客户端不可重新启用，请创建新 Token")
		}
		client.Hash, client.CreatedAt = prior.Hash, prior.CreatedAt
	} else {
		if revoke || len(next.MCPClients) >= 100 {
			return "", errors.New("无法创建更多客户端")
		}
		id := make([]byte, 12)
		if _, err := rand.Read(id); err != nil {
			return "", errors.New("客户端创建失败")
		}
		client.ID, client.CreatedAt, rotate = hex.EncodeToString(id), time.Now().UTC(), true
	}
	token := ""
	if revoke {
		now := time.Now().UTC()
		client = next.MCPClients[index]
		client.RevokedAt = &now
		client.Hash = ""
		client.Config.Enabled = false
	} else if rotate {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return "", errors.New("Token 创建失败")
		}
		token = "ghmcp_" + hex.EncodeToString(secret)
		sum := sha256.Sum256([]byte(token))
		client.Hash = hex.EncodeToString(sum[:])
	}
	if index < 0 {
		next.MCPClients = append(next.MCPClients, client)
	} else {
		next.MCPClients[index] = client
	}
	next.Revision++
	if writeJSON(s.path, next) != nil {
		return "", errors.New("客户端设置保存失败，请检查目录权限")
	}
	s.state = next
	return token, nil
}

func (a *Admin) mcpClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/mcp/clients", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Client   MCPClient `json:"client"`
			Revision int       `json:"revision"`
			Rotate   bool      `json:"rotate"`
			Revoke   bool      `json:"revoke"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		priorBinding := uint(0)
		for _, client := range a.store.Snapshot().MCPClients {
			if client.ID == input.Client.ID {
				priorBinding = client.Config.SunPanelUserID
				break
			}
		}
		if !input.Revoke && input.Client.Config.SunPanelUserID != 0 && input.Client.Config.SunPanelUserID != priorBinding {
			if a.sunPanel == nil {
				apiError(w, 400, "请先启用 Sun-Panel")
				return
			}
			if _, err := a.sunPanel.bridgeCall(r.Context(), "desktop", input.Client.Config.SunPanelUserID, nil); err != nil {
				apiError(w, 400, "所选 Sun-Panel 账号不可用，请刷新后重试")
				return
			}
		}
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		if a.maintenance.Busy() {
			apiError(w, 409, "维护执行中，请稍后修改客户端")
			return
		}
		token, err := a.store.saveMCPClient(input.Client, input.Rotate, input.Revoke, input.Revision)
		if err != nil {
			apiError(w, 400, err.Error())
			return
		}
		state := a.store.Snapshot()
		jsonResponse(w, 200, map[string]any{"config": state.MCP, "token_configured": state.MCPTokenHash != "", "clients": publicMCPClients(state), "revision": state.Revision, "token": token, "guide": mcpManagementGuide})
	}))
}
