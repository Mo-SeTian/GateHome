package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestCredentialRevealRequiresAdminSessionAndExplicitRequest(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	s := a.store.Snapshot()
	s.Config.DDNS.Groups = []DDNSGroup{testDDNSGroup()}
	s.Config.ACME.Requests = []CertificateRequest{{ID: "cert", Provider: "cloudflare", Domains: []string{"example.test"}}}
	s.Config.OutboundProxy.URL = "http://127.0.0.1:7890"
	dns, cert, github := "TEST_ONLY_DDNS_TOKEN", "TEST_ONLY_CERT_TOKEN", "TEST_ONLY_GITHUB_TOKEN"
	proxy, storage, encryption := "TEST_ONLY_PROXY_PASSWORD", "TEST_ONLY_STORAGE_PASSWORD", "TEST_ONLY_BACKUP_PASSWORD"
	if err := a.store.UpdateRouteCredentials(s.Config, map[string]*string{"home": &dns}, map[string]*string{"cert": &cert}, nil, &proxy, nil, &github, s.Revision); err != nil {
		t.Fatal("credential fixture failed")
	}
	if err := a.store.saveWebDAVBackup(WebDAVBackupConfig{URL: "https://storage.example.test/dav/"}, &storage, &encryption, a.store.Snapshot().Revision); err != nil {
		t.Fatal("WebDAV credential fixture failed")
	}
	a.logs, _ = NewLogs(t.TempDir(), a.store)
	before := a.store.Snapshot()
	for _, fixture := range []struct{ kind, id, value string }{
		{"ddns", "home", dns}, {"certificate", "cert", cert}, {"github", "", github},
		{"outbound-proxy", "", proxy}, {"webdav", "", storage}, {"webdav-backup", "", encryption},
	} {
		body := map[string]string{"kind": fixture.kind, "id": fixture.id}
		if adminRequest(h, "POST", "/api/credentials/reveal", body, nil, "").Code != 401 {
			t.Fatal("credential reveal accepted without a session")
		}
		if adminRequest(h, "POST", "/api/credentials/reveal", body, cookie, "https://other.example.test").Code != 403 {
			t.Fatal("cross-origin credential read accepted")
		}
		w := adminRequest(h, "POST", "/api/credentials/reveal", body, cookie, "")
		var result map[string]string
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result["value"] != fixture.value || len(result) != 1 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("explicit credential response incorrect or cacheable")
		}
	}
	for _, body := range []map[string]string{{"kind": "password"}, {"kind": "ddns", "id": "missing"}, {"kind": "github", "id": "other"}, {"kind": "mcp-legacy"}, {"kind": "mcp-client", "id": "missing"}} {
		if adminRequest(h, "POST", "/api/credentials/reveal", body, cookie, "").Code < 400 {
			t.Fatal("unsupported or missing credential accepted")
		}
	}
	for _, internal := range []bool{false, true} {
		r := httptest.NewRequest("POST", "http://localhost:16666/api/credentials/reveal", strings.NewReader(`{"kind":"github"}`))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(cookie)
		if internal {
			r.Header.Set("X-Gatehouse-Request", "1")
			r = r.WithContext(context.WithValue(r.Context(), mcpInternalKey{}, true))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("credential reveal bypassed CSRF or MCP boundary")
		}
	}
	if adminRequest(h, "GET", "/api/credentials/reveal", nil, cookie, "").Code < 400 || !reflect.DeepEqual(before, a.store.Snapshot()) {
		t.Fatal("credential read changed configuration or accepted GET")
	}
	public := adminRequest(h, "GET", "/api/config", nil, cookie, "")
	webDAV := adminRequest(h, "GET", "/api/backups/webdav", nil, cookie, "")
	logs, _, _ := a.logs.List("admin", "", 0, 100)
	logBytes, _ := json.Marshal(logs)
	for _, value := range []string{dns, cert, github, proxy, storage, encryption} {
		if bytes.Contains(public.Body.Bytes(), []byte(value)) || bytes.Contains(webDAV.Body.Bytes(), []byte(value)) || bytes.Contains(logBytes, []byte(value)) {
			t.Fatal("credential escaped into ordinary responses or logs")
		}
	}
}

func TestSavedMCPTokensRevealPersistRotateRevokeAndRedact(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	legacy, err := a.store.saveMCP(MCPConfig{Enabled: true, Read: true}, false, a.store.Snapshot().Revision)
	if err != nil {
		t.Fatal("legacy MCP fixture failed")
	}
	clientToken, err := a.store.saveMCPClient(MCPClient{Name: "Reveal fixture", Config: MCPConfig{Enabled: true, Read: true}}, false, false, a.store.Snapshot().Revision)
	if err != nil {
		t.Fatal("client fixture failed")
	}
	client := a.store.Snapshot().MCPClients[0]
	read := func(kind, id, expected string, code int) {
		t.Helper()
		w := adminRequest(h, "POST", "/api/credentials/reveal", map[string]string{"kind": kind, "id": id}, cookie, "")
		var result map[string]string
		json.Unmarshal(w.Body.Bytes(), &result)
		if w.Code != code || (code == 200 && result["value"] != expected) {
			t.Fatal("stored MCP token read returned unexpected result")
		}
	}
	read("mcp-legacy", "", legacy, 200)
	read("mcp-client", client.ID, clientToken, 200)
	public := adminRequest(h, "GET", "/api/mcp", nil, cookie, "")
	for _, value := range []string{legacy, clientToken, client.Hash, a.store.Snapshot().MCPTokenHash} {
		if strings.Contains(public.Body.String(), value) || strings.Contains(a.mcpSafeText(value), value) {
			t.Fatal("MCP public response or tool text disclosed credentials")
		}
	}
	if !publicMCPClients(a.store.Snapshot())[0].Revealable {
		t.Fatal("reveal availability missing")
	}
	reopened, err := OpenStorePaths(a.store.paths)
	if err != nil || reopened.Snapshot().MCPToken != legacy || reopened.Snapshot().MCPClients[0].Token != clientToken {
		t.Fatal("stored MCP tokens did not survive reopening")
	}
	old := a.store.Snapshot()
	old.MCPToken = ""
	old.MCPClients = append([]MCPClient{}, old.MCPClients...)
	old.MCPClients[0].Token = ""
	if validateCredentials(old) != nil || !validMCPToken(old, "Bearer "+clientToken) || publicMCPClients(old)[0].Revealable {
		t.Fatal("legacy hash-only token compatibility changed")
	}
	bad := a.store.Snapshot()
	bad.MCPToken = "TEST_ONLY_TAMPERED_TOKEN"
	if validateCredentials(bad) == nil {
		t.Fatal("mismatched MCP raw token and hash accepted")
	}
	client.Name = "Edited fixture"
	if _, err := a.store.saveMCPClient(client, false, false, a.store.Snapshot().Revision); err != nil {
		t.Fatal("editing client failed")
	}
	read("mcp-client", client.ID, clientToken, 200)
	rotated, err := a.store.saveMCPClient(client, true, false, a.store.Snapshot().Revision)
	if err != nil || validMCPToken(a.store.Snapshot(), "Bearer "+clientToken) {
		t.Fatal("rotating client did not invalidate old credentials")
	}
	read("mcp-client", client.ID, rotated, 200)
	a.logs, _ = NewLogs(t.TempDir(), a.store)
	a.logs.Add(LogEntry{Category: "admin", Message: legacy + " " + rotated})
	rows, _, _ := a.logs.List("admin", "", 0, 10)
	data, _ := json.Marshal(rows)
	if bytes.Contains(data, []byte(legacy)) || bytes.Contains(data, []byte(rotated)) {
		t.Fatal("MCP tokens appeared in logs")
	}
	if _, err := a.store.saveMCPClient(client, false, true, a.store.Snapshot().Revision); err != nil {
		t.Fatal("revoking client failed")
	}
	read("mcp-client", client.ID, "", 404)
}

func TestSunPanelInitialPasswordRequiresManagementSession(t *testing.T) {
	_, handler := testAdmin(t)
	const path = "/api/sunpanel/admin-setup"
	if adminRequest(handler, "GET", path, nil, nil, "").Code != 401 || adminRequest(handler, "POST", path, map[string]any{}, nil, "").Code != 401 {
		t.Fatal("operator setup must require a management session")
	}
	if adminRequest(handler, "POST", path, map[string]any{}, loginForTest(t, handler), "https://other.example.test").Code != 403 {
		t.Fatal("operator setup accepted a cross-origin request")
	}
	req := httptest.NewRequest("GET", path, nil)
	req = req.WithContext(context.WithValue(req.Context(), mcpInternalKey{}, true))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("operator setup was accessible to MCP")
	}
}
