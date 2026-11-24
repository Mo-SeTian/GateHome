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
	for _, section := range []string{"firewalls", "admin_access", "trusted_proxies", "subscriptions", "outbound_proxy"} {
		args = map[string]any{"revision": a.store.Snapshot().Revision, "section": section, "value": []any{}}
		if section == "outbound_proxy" {
			args["value"] = map[string]any{"enabled": true, "url": "http://127.0.0.1:7890"}
		}
		message := mcpTestCall(t, ctx, editor, "gatehome_update_config", args, true)
		if !strings.Contains(message, "安全策略") {
			t.Fatalf("security scope was not enforced for %s: %s", section, message)
		}
	}

	// Setup a route protected by a firewall and route auth; non-security MCP editor must not weaken or remove protection.
	cfgWithSec := a.store.Snapshot().Config
	cfgWithSec.Firewalls = []Firewall{{ID: "fw-1", Name: "Strict", DefaultAction: "allow"}}
	cfgWithSec.Routes = []Route{{
		ID:         "route-sec-1",
		GroupID:    "default",
		Name:       "Protected App",
		Host:       "sec.example.test",
		Upstream:   "http://127.0.0.1:8080",
		Enabled:    true,
		FirewallID: "fw-1",
		Auth:       RouteAuthConfig{Enabled: true, Username: "alice"},
	}}
	secPass := "TEST_ONLY_SERVICE_PASSWORD"
	if err := a.store.UpdateRouteCredentials(cfgWithSec, nil, nil, nil, nil, map[string]*string{"default/sec.example.test": &secPass}, nil, a.store.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	curCfg := a.store.Snapshot().Config
	// Try clearing FirewallID on existing route.
	badRoutes1 := append([]Route(nil), curCfg.Routes...)
	badRoutes1[0].FirewallID = ""
	if msg := mcpTestCall(t, ctx, editor, "gatehome_update_config", map[string]any{"revision": a.store.Snapshot().Revision, "section": "routes", "value": badRoutes1}, true); !strings.Contains(msg, "安全策略") {
		t.Fatalf("expected FirewallID clear to be rejected, got: %s", msg)
	}
	// Try disabling route auth or deleting+recreating without protection.
	badRoutes2 := append([]Route(nil), curCfg.Routes...)
	badRoutes2[0].ID = "route-new-id"
	badRoutes2[0].Auth = RouteAuthConfig{Enabled: false}
	if msg := mcpTestCall(t, ctx, editor, "gatehome_update_config", map[string]any{"revision": a.store.Snapshot().Revision, "section": "routes", "value": badRoutes2}, true); !strings.Contains(msg, "安全策略") {
		t.Fatalf("expected Auth disable via delete+recreate to be rejected, got: %s", msg)
	}
	// Editing non-security fields (Name/Upstream) on the same protected route must still succeed.
	okRoutes := append([]Route(nil), curCfg.Routes...)
	okRoutes[0].Name = "Renamed App"
	mcpTestCall(t, ctx, editor, "gatehome_update_config", map[string]any{"revision": a.store.Snapshot().Revision, "section": "routes", "value": okRoutes}, false)
	if a.store.Snapshot().Config.Routes[0].Name != "Renamed App" || a.store.Snapshot().Config.Routes[0].FirewallID != "fw-1" {
		t.Fatalf("non-security route update failed or lost protection")
	}

	row.Config.SecurityWrite = &yes
	if _, err := a.store.saveMCPClient(row, false, false, a.store.Snapshot().Revision); err != nil {
		t.Fatal("scope update failed")
	}
	args = map[string]any{"revision": a.store.Snapshot().Revision, "section": "firewalls", "value": []Firewall{{ID: "fw-1", Name: "Strict v2", DefaultAction: "allow"}}}
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
