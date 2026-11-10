package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLegacyMCPSettingsSavePreservesExplicitWriteRestrictions(t *testing.T) {
	a, h := testAdmin(t)
	yes, no := true, false
	if _, err := a.store.saveMCP(MCPConfig{Enabled: true, Read: true, Write: true, SunPanelWrite: &no, SecurityWrite: &no}, false, a.store.Snapshot().Revision); err != nil {
		t.Fatal("fixture activation failed")
	}
	token, err := a.store.saveMCPClient(MCPClient{Name: "Scoped editor", Config: MCPConfig{Enabled: true, Read: true, Write: true, SunPanelWrite: &no, SecurityWrite: &no}}, false, false, a.store.Snapshot().Revision)
	if err != nil {
		t.Fatal("client setup failed")
	}
	cookie := loginForTest(t, h)
	oldConfig := map[string]any{"enabled": true, "read": true, "write": true, "maintenance": false, "sunpanel_user_id": 0}
	w := adminRequest(h, "PUT", "/api/mcp", map[string]any{"config": oldConfig, "revision": a.store.Snapshot().Revision}, cookie, "")
	if w.Code != 200 || a.store.Snapshot().MCP.canWritePanel() || a.store.Snapshot().MCP.canWriteSecurity() {
		t.Fatal("legacy settings save broadened explicit restrictions")
	}
	row := a.store.Snapshot().MCPClients[0]
	legacyClient := map[string]any{"id": row.ID, "name": row.Name, "config": oldConfig}
	w = adminRequest(h, "POST", "/api/mcp/clients", map[string]any{"client": legacyClient, "revision": a.store.Snapshot().Revision}, cookie, "")
	client, valid := mcpClientForToken(a.store.Snapshot(), "Bearer "+token)
	if w.Code != 200 || !valid || client.Config.canWritePanel() || client.Config.canWriteSecurity() {
		t.Fatal("legacy client save broadened restrictions or invalidated its token")
	}
	row = a.store.Snapshot().MCPClients[0]
	row.Config = MCPConfig{Enabled: true, SunPanelWrite: &yes}
	if _, err := a.store.saveMCPClient(row, false, false, a.store.Snapshot().Revision); err != nil {
		t.Fatal("panel scope setup failed")
	}
	row.Config.SunPanelWrite = nil
	if _, err := a.store.saveMCPClient(row, false, false, a.store.Snapshot().Revision); err != nil {
		t.Fatal("omitted flag lost the only granted scope")
	}
}

func TestMCPScopedWritesKeepLegacyGrantsAndAccountIsolation(t *testing.T) {
	a, _ := testAdmin(t)
	yes, no := true, false
	legacy, err := a.store.saveMCP(MCPConfig{Enabled: true, Read: true, Write: true, SunPanelUserID: 77}, false, a.store.Snapshot().Revision)
	if err != nil {
		t.Fatal("fixture activation failed")
	}
	owners := make(chan uint, 8)
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			UserID uint `json:"userId"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			t.Error("invalid bridge fixture request")
		}
		owners <- input.UserID
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"id":1}}`))
	}))
	defer bridge.Close()
	a.sunPanel = &SunPanel{bridgeURL: bridge.URL, bridgeKey: make([]byte, 32)}
	h := a.Handler()
	create := func(name string, config MCPConfig) (string, MCPClient) {
		token, err := a.store.saveMCPClient(MCPClient{Name: name, Config: config}, false, false, a.store.Snapshot().Revision)
		if err != nil {
			t.Fatal("client setup failed")
		}
		clients := a.store.Snapshot().MCPClients
		return token, clients[len(clients)-1]
	}
	panelToken, _ := create("Panel only", MCPConfig{Enabled: true, Read: true, SunPanelWrite: &yes, SecurityWrite: &no, SunPanelUserID: 42})
	ctx, panel := mcpTestClient(t, h, panelToken)
	group := map[string]any{"title": "TEST_ONLY_GROUP", "description": "", "icon": "", "sort": 0}
	mcpTestCall(t, ctx, panel, "sunpanel_save_group", group, false)
	if <-owners != 42 {
		t.Fatal("client wrote another account")
	}
	state := a.store.Snapshot()
	args := map[string]any{"revision": state.Revision, "section": "sunpanel", "value": state.Config.SunPanel}
	mcpTestCall(t, ctx, panel, "gatehome_update_config", args, true)
	mcpTestCall(t, ctx, panel, "gatehome_maintenance", map[string]any{"operation": "restart", "confirm": true}, true)

	writeToken, row := create("Gateway editor", MCPConfig{Enabled: true, Read: true, Write: true, SunPanelWrite: &no, SecurityWrite: &no})
	_, editor := mcpTestClient(t, h, writeToken)
	args["revision"] = a.store.Snapshot().Revision
	mcpTestCall(t, ctx, editor, "gatehome_update_config", args, false)
	mcpTestCall(t, ctx, editor, "sunpanel_save_group", group, true)
	for _, section := range []string{"firewalls", "admin_access", "trusted_proxies"} {
		args = map[string]any{"revision": a.store.Snapshot().Revision, "section": section, "value": []any{}}
		message := mcpTestCall(t, ctx, editor, "gatehome_update_config", args, true)
		if !strings.Contains(message, "安全策略") {
			t.Fatal("security scope was not enforced before config parsing")
		}
	}
	row.Config.SecurityWrite = &yes
	if _, err := a.store.saveMCPClient(row, false, false, a.store.Snapshot().Revision); err != nil {
		t.Fatal("scope update failed")
	}
	args = map[string]any{"revision": a.store.Snapshot().Revision, "section": "firewalls", "value": []any{}}
	mcpTestCall(t, ctx, editor, "gatehome_update_config", args, false)

	unboundToken, _ := create("Unbound panel", MCPConfig{Enabled: true, SunPanelWrite: &yes, SecurityWrite: &no})
	_, unbound := mcpTestClient(t, h, unboundToken)
	message := mcpTestCall(t, ctx, unbound, "sunpanel_save_group", group, true)
	if !strings.Contains(message, "未绑定") {
		t.Fatal("unbound client inherited the global account")
	}
	_, compatible := mcpTestClient(t, h, legacy)
	mcpTestCall(t, ctx, compatible, "sunpanel_save_group", group, false)
	if <-owners != 77 {
		t.Fatal("legacy account binding changed")
	}
	args["revision"] = a.store.Snapshot().Revision
	mcpTestCall(t, ctx, compatible, "gatehome_update_config", args, false)
	archive, err := encodeBackup(backupPayload{State: a.store.Snapshot(), Certificates: map[string][]byte{}, Files: map[string][]byte{}}, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal("scoped backup failed")
	}
	restored, _, err := decodeBackup(archive, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal("scoped backup restore failed")
	}
	client, ok := mcpClientForToken(restored.State, "Bearer "+panelToken)
	if !ok || !client.Config.canWritePanel() || client.Config.Write || client.Config.SunPanelUserID != 42 {
		t.Fatal("backup lost scoped grants")
	}
}
