package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

func validStoredMCPToken(token, hash string) bool {
	if token == "" {
		return true // Older installations only retained the hash.
	}
	if len(token) != 70 || !strings.HasPrefix(token, "ghmcp_") {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:]) == hash
}

func (a *Admin) credentialRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/credentials/reveal", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		// MCP grants never authorize reading management credentials.
		if r.Context().Value(mcpInternalKey{}) == true {
			apiError(w, 403, "请在管理控制台登录后查看凭据")
			return
		}
		var input struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		s := a.store.Snapshot()
		value := ""
		switch input.Kind {
		case "ddns":
			value = s.DNSCredentials[input.ID].Token
		case "certificate":
			value = s.CertificateCredentials[input.ID].Token
		case "github":
			if input.ID == "" {
				value = s.GitHubToken
			}
		case "mcp-legacy":
			if input.ID == "" {
				value = s.MCPToken
			}
		case "mcp-client":
			for _, client := range s.MCPClients {
				if client.ID == input.ID && client.RevokedAt == nil {
					value = client.Token
					break
				}
			}
		default:
			apiError(w, 400, "不支持的凭据类型")
			return
		}
		if value == "" {
			apiError(w, 404, "此 Token 未保存原值或已移除；旧 MCP Token 需重新生成后才能查看")
			return
		}
		jsonResponse(w, 200, map[string]string{"value": value})
	}))
}
