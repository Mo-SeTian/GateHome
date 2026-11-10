package gateway

import (
	"encoding/json"
	"testing"
)

func TestMCPPanelOnlyGlobalAuthorizationSurvivesSaveRestartAndBackup(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	yes, no := true, false
	before := a.store.Snapshot()
	config := MCPConfig{Enabled: true, SunPanelWrite: &yes, SecurityWrite: &no}
	w := adminRequest(h, "PUT", "/api/mcp", map[string]any{"config": config, "revision": before.Revision}, cookie, "")
	if w.Code != 200 {
		t.Fatal("panel-only global grant could not be saved")
	}
	reopened, err := OpenStorePaths(a.store.paths)
	if err != nil {
		t.Fatal("saved panel-only grant prevented configuration loading")
	}
	state := reopened.Snapshot()
	if !state.MCP.canWritePanel() || state.MCP.Read || state.MCP.Write || state.MCP.Maintenance || state.PasswordHash != before.PasswordHash {
		t.Fatal("configuration reload changed grants or the existing account")
	}
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": state.Config, "revision": state.Revision}, cookie, "")
	if w.Code != 200 {
		t.Fatal("panel-only global grant prevented an ordinary config save")
	}
	archive, err := encodeBackup(backupPayload{State: a.store.Snapshot(), Certificates: map[string][]byte{}, Files: map[string][]byte{}}, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal("backup encoding failed")
	}
	restored, _, err := decodeBackup(archive, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil || !restored.State.MCP.canWritePanel() || restored.State.MCP.Read || restored.State.MCP.Write || restored.State.PasswordHash != before.PasswordHash {
		t.Fatal("backup inspection rejected or changed the saved grant/account")
	}
	config.SunPanelWrite = &no
	w = adminRequest(h, "PUT", "/api/mcp", map[string]any{"config": config, "revision": a.store.Snapshot().Revision}, cookie, "")
	if w.Code != 400 {
		t.Fatal("enabled MCP without an effective permission was accepted")
	}
	invalid := a.store.Snapshot()
	invalid.MCP = config
	if validateCredentials(invalid) == nil {
		t.Fatal("credential validation no longer enforces an effective permission")
	}
}

func TestMCPConfigWriteResponseRespectsReadGrant(t *testing.T) {
	for _, dedicated := range []bool{false, true} {
		for _, read := range []bool{false, true} {
			name := map[bool]string{false: "legacy", true: "dedicated"}[dedicated] + map[bool]string{false: "/write-only", true: "/read-write"}[read]
			t.Run(name, func(t *testing.T) {
				a, h := testAdmin(t)
				no := false
				grant := MCPConfig{Enabled: true, Read: read, Write: true, SunPanelWrite: &no, SecurityWrite: &no}
				token, err := a.store.saveMCP(grant, false, a.store.Snapshot().Revision)
				if err != nil {
					t.Fatal("MCP fixture activation failed")
				}
				if dedicated {
					token, err = a.store.saveMCPClient(MCPClient{Name: "TEST_ONLY_EDITOR", Config: grant}, false, false, a.store.Snapshot().Revision)
					if err != nil {
						t.Fatal("client creation failed")
					}
				}
				ctx, client := mcpTestClient(t, h, token)
				mcpTestCall(t, ctx, client, "gatehome_get_config", mcpEmpty{}, !read)
				state := a.store.Snapshot()
				text := mcpTestCall(t, ctx, client, "gatehome_update_config", map[string]any{"revision": state.Revision, "section": "dashboard", "value": state.Config.Dashboard}, false)
				var result map[string]json.RawMessage
				if json.Unmarshal([]byte(text), &result) != nil {
					t.Fatal("invalid config write response")
				}
				var revision int
				if json.Unmarshal(result["revision"], &revision) != nil || revision != state.Revision+1 {
					t.Fatal("write response lost the saved revision")
				}
				if read {
					if result["config"] == nil {
						t.Fatal("read/write client lost its compatible full response")
					}
				} else {
					var ok bool
					if len(result) != 2 || json.Unmarshal(result["ok"], &ok) != nil || !ok {
						t.Fatal("write-only response disclosed fields other than success and revision")
					}
				}
			})
		}
	}
}
