package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPClientsIsolationExpiryAndRevocation(t *testing.T) {
	a, h := testAdmin(t)
	legacy, err := a.store.saveMCP(MCPConfig{Enabled: true, Read: true}, false, a.store.Snapshot().Revision)
	if err != nil {
		t.Fatal("setup failed")
	}
	create := func(name string, write bool) string {
		token, err := a.store.saveMCPClient(MCPClient{Name: name, Config: MCPConfig{Enabled: true, Read: true, Write: write}}, false, false, a.store.Snapshot().Revision)
		if err != nil {
			t.Fatal("client creation failed")
		}
		return token
	}
	readToken, writeToken := create("Reader", false), create("Editor", true)
	ctx, reader := mcpTestClient(t, h, readToken)
	_, editor := mcpTestClient(t, h, writeToken)
	state := a.store.Snapshot()
	args := map[string]any{"revision": state.Revision, "section": "sunpanel", "value": state.Config.SunPanel}
	mcpTestCall(t, ctx, reader, "gatehome_update_config", args, true)
	mcpTestCall(t, ctx, editor, "gatehome_update_config", args, false)
	mcpTestCall(t, ctx, reader, "gatehome_maintenance", map[string]any{"operation": "restart", "confirm": true}, true)
	if !validMCPToken(a.store.Snapshot(), "Bearer "+legacy) {
		t.Fatal("legacy token compatibility lost")
	}
	rows := publicMCPClients(a.store.Snapshot())
	data, _ := json.Marshal(rows)
	for _, row := range a.store.Snapshot().MCPClients {
		if strings.Contains(string(data), row.Hash) {
			t.Fatal("public list disclosed hash")
		}
	}
	row := a.store.Snapshot().MCPClients[0]
	row.Config.SunPanelUserID = 42
	_, err = a.store.saveMCPClient(row, false, false, a.store.Snapshot().Revision)
	if err != nil {
		t.Fatal("binding update failed")
	}
	owner, ok := mcpClientForToken(a.store.Snapshot(), "Bearer "+readToken)
	if !ok || owner.Config.SunPanelUserID != 42 {
		t.Fatal("binding not per client")
	}
	stale := a.store.Snapshot().Revision - 1
	if _, err = a.store.saveMCPClient(row, false, false, stale); err == nil {
		t.Fatal("stale settings accepted")
	}
	expired := time.Now().Add(-time.Second)
	copyState := a.store.Snapshot()
	copyState.MCPClients = append([]MCPClient{}, copyState.MCPClients...)
	copyState.MCPClients[0].ExpiresAt = &expired
	if validMCPToken(copyState, "Bearer "+readToken) {
		t.Fatal("expired token accepted")
	}
	_, err = a.store.saveMCPClient(row, false, true, a.store.Snapshot().Revision)
	if err != nil || validMCPToken(a.store.Snapshot(), "Bearer "+readToken) || !validMCPToken(a.store.Snapshot(), "Bearer "+writeToken) {
		t.Fatal("revocation did not isolate clients")
	}
	_, err = reader.CallTool(context.Background(), &mcp.CallToolParams{Name: "gatehome_get_config", Arguments: mcpEmpty{}})
	if err == nil {
		t.Fatal("revoked session accepted")
	}
}

func TestMCPOperationSummaryExcludesValues(t *testing.T) {
	summary := mcpOperationSummary("gatehome_update_config", map[string]any{"section": "routes", "value": map[string]string{"password": "TEST_ONLY_SECRET", "url": "https://example.test/private"}})
	if summary != "gatehome_update_config · section=routes" {
		t.Fatal("operation summary includes unexpected values")
	}
}

func TestMCPClientCanBePausedWithoutRunningPanel(t *testing.T) {
	a, h := testAdmin(t)
	if _, err := a.store.saveMCP(MCPConfig{Enabled: true, Read: true}, false, a.store.Snapshot().Revision); err != nil {
		t.Fatal("fixture setup failed")
	}
	token, err := a.store.saveMCPClient(MCPClient{Name: "Offline panel", Config: MCPConfig{Enabled: true, Read: true, SunPanelUserID: 7}}, false, false, a.store.Snapshot().Revision)
	if err != nil {
		t.Fatal("fixture setup failed")
	}
	row := publicMCPClients(a.store.Snapshot())[0]
	row.Config.Enabled = false
	body, _ := json.Marshal(map[string]any{"client": row, "revision": a.store.Snapshot().Revision})
	request := httptest.NewRequest("POST", "/api/mcp/clients", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Gatehouse-Request", "1")
	request = request.WithContext(context.WithValue(request.Context(), mcpInternalKey{}, true))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != 200 || validMCPToken(a.store.Snapshot(), "Bearer "+token) {
		t.Fatal("pausing a disconnected binding failed")
	}
}

func TestMCPClientsSurviveEncryptedBackupWithoutPlaintextTokens(t *testing.T) {
	a, _ := testAdmin(t)
	if _, err := a.store.saveMCP(MCPConfig{Enabled: true, Read: true}, false, a.store.Snapshot().Revision); err != nil {
		t.Fatal("fixture setup failed")
	}
	expires := time.Now().UTC().Add(24 * time.Hour)
	token, err := a.store.saveMCPClient(MCPClient{Name: "Backup fixture", Config: MCPConfig{Enabled: true, Read: true}, ExpiresAt: &expires}, false, false, a.store.Snapshot().Revision)
	if err != nil {
		t.Fatal("client creation failed")
	}
	stored, err := os.ReadFile(a.store.path)
	if err != nil || bytes.Contains(stored, []byte(token)) {
		t.Fatal("client credential stored in plaintext")
	}
	archive, err := encodeBackup(backupPayload{State: a.store.Snapshot(), Certificates: map[string][]byte{}, Files: map[string][]byte{}}, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil || bytes.Contains(archive, []byte(token)) {
		t.Fatal("encrypted backup failed or disclosed token")
	}
	restored, _, err := decodeBackup(archive, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil || len(restored.State.MCPClients) != 1 || !validMCPToken(restored.State, "Bearer "+token) || !restored.State.MCPClients[0].ExpiresAt.Equal(expires) {
		t.Fatal("client authorization or expiry lost on restore")
	}
}
