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
		if !a.requireRecentAdmin(w, r) {
			return
		}
		s := a.store.Snapshot()
		value := ""
		switch input.Kind {
		case "ddns":
			value = s.DNSCredentials[input.ID].Token
		case "ddns-key":
			value = s.DNSCredentials[input.ID].KeyID
		case "certificate":
			value = s.CertificateCredentials[input.ID].Token
		case "certificate-key":
			value = s.CertificateCredentials[input.ID].KeyID
		case "github":
			if input.ID == "" {
				value = s.GitHubToken
			}
		case "outbound-proxy":
			if input.ID == "" {
				value = s.ProxyPassword
			}
		case "alert-wxpusher":
			if input.ID == "" {
				value = s.WxPusher.AppToken
			}
		case "alert-webhook":
			if input.ID == "" {
				value = s.Alerts.URL
			}
		case "alert-webhook-secret":
			if input.ID == "" {
				value = s.Alerts.Secret
			}
		case "webdav":
			if input.ID == "" {
				value = s.WebDAVBackup.Password
			}
		case "webdav-backup":
			if input.ID == "" {
				value = s.WebDAVBackup.BackupPassword
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
		case "mihomo-controller":
			if input.ID == "" && a.mihomo != nil {
				a.mihomo.mu.Lock()
				value = a.mihomo.cfg.ControllerSecret
				a.mihomo.mu.Unlock()
			}
		default:
			apiError(w, 400, "不支持的凭据类型")
			return
		}
		if value == "" {
			apiError(w, 404, "此凭据当前无法查看，请确认它仍在使用")
			return
		}
		jsonResponse(w, 200, map[string]string{"value": value})
	}))
}
