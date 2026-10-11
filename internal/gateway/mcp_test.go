package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpTestTransport struct{ token string }

func (t mcpTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(r)
}

func mcpTestClient(t *testing.T, h http.Handler, token string) (context.Context, *mcp.ClientSession) {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: mcpTestTransport{token}}}, nil)
	if err != nil {
		t.Fatal("MCP initialization failed")
	}
	t.Cleanup(func() { session.Close() })
	return ctx, session
}

func mcpTestCall(t *testing.T, ctx context.Context, s *mcp.ClientSession, name string, args any, wantError bool) string {
	t.Helper()
	result, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil || result.IsError != wantError || len(result.Content) != 1 {
		t.Fatalf("unexpected MCP result for %s", name)
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("MCP content type invalid")
	}
	return content.Text
}

func TestMCPProtocolPermissionsAndRevocation(t *testing.T) {
	a, h := testAdmin(t)
	token, err := a.store.saveMCP(MCPConfig{Enabled: true, Read: true}, false, a.store.Snapshot().Revision)
	if err != nil {
		t.Fatal(err)
	}
	ctx, client := mcpTestClient(t, h, token)
	list, err := client.ListTools(ctx, nil)
	if err != nil || len(list.Tools) < 25 {
		t.Fatal("MCP tools discovery incomplete")
	}
	for _, tool := range list.Tools {
		if tool.Name == "gatehome_get_config" && !tool.Annotations.ReadOnlyHint {
			t.Fatal("missing read-only annotation")
		}
	}
	text := mcpTestCall(t, ctx, client, "gatehome_get_config", mcpEmpty{}, false)
	for _, secret := range []string{"TEST_ONLY_FAKE_TOKEN", token, a.store.Snapshot().MCPTokenHash, a.store.Snapshot().PasswordHash} {
		if strings.Contains(text, secret) {
			t.Fatal("MCP disclosed credential")
		}
	}
	mcpTestCall(t, ctx, client, "gatehome_update_config", map[string]any{"revision": a.store.Snapshot().Revision, "section": "sunpanel", "value": a.store.Snapshot().Config.SunPanel}, true)
	mcpTestCall(t, ctx, client, "gatehome_maintenance", map[string]any{"operation": "restart", "confirm": true}, true)
	mcpTestCall(t, ctx, client, "sunpanel_get_desktop", mcpEmpty{}, true)
	state := a.store.Snapshot()
	state.MCP.Write = true
	if _, err := a.store.saveMCP(state.MCP, false, state.Revision); err != nil {
		t.Fatal(err)
	}
	state = a.store.Snapshot()
	value := state.Config.SunPanel
	value.LaunchURL = "https://panel.example.test/"
	mcpTestCall(t, ctx, client, "gatehome_update_config", map[string]any{"revision": state.Revision, "section": "sunpanel", "value": value}, false)
	if a.store.Snapshot().Config.SunPanel.LaunchURL != value.LaunchURL || a.store.Snapshot().CloudflareToken != "TEST_ONLY_FAKE_TOKEN" {
		t.Fatal("configuration write failed or changed credential")
	}
	mcpTestCall(t, ctx, client, "gatehome_update_config", map[string]any{"revision": state.Revision, "section": "sunpanel", "value": state.Config.SunPanel}, true)
	mcpTestCall(t, ctx, client, "gatehome_update_config", map[string]any{"revision": a.store.Snapshot().Revision, "section": "mcp", "value": map[string]bool{"maintenance": true}}, true)
	state = a.store.Snapshot()
	state.MCP.Read = false
	if _, err := a.store.saveMCP(state.MCP, false, state.Revision); err != nil {
		t.Fatal(err)
	}
	mcpTestCall(t, ctx, client, "gatehome_get_config", mcpEmpty{}, true)
	state = a.store.Snapshot()
	newToken, err := a.store.saveMCP(state.MCP, true, state.Revision)
	if err != nil || validMCPToken(a.store.Snapshot(), "Bearer "+token) || !validMCPToken(a.store.Snapshot(), "Bearer "+newToken) {
		t.Fatal("token rotation did not revoke old token")
	}
	if _, err := client.ListTools(ctx, nil); err == nil {
		t.Fatal("revoked client remained authenticated")
	}
}

func TestMCPManagementGuide(t *testing.T) {
	a, h := testAdmin(t)
	token, err := a.store.saveMCP(MCPConfig{Enabled: true, Read: true}, false, a.store.Snapshot().Revision)
	if err != nil {
		t.Fatal(err)
	}
	ctx, client := mcpTestClient(t, h, token)
	if client.InitializeResult().Instructions != mcpManagementGuide {
		t.Fatal("MCP initialization omitted the editing guide")
	}
	prompts, err := client.ListPrompts(ctx, nil)
	if err != nil || len(prompts.Prompts) != 1 || prompts.Prompts[0].Name != "gatehome_management_guide" {
		t.Fatal("management prompt discovery failed")
	}
	prompt, err := client.GetPrompt(ctx, &mcp.GetPromptParams{Name: "gatehome_management_guide"})
	if err != nil || len(prompt.Messages) != 1 || prompt.Messages[0].Role != "user" {
		t.Fatal("management prompt retrieval failed")
	}
	text, ok := prompt.Messages[0].Content.(*mcp.TextContent)
	if !ok || text.Text != mcpManagementGuide || strings.Contains(text.Text, token) {
		t.Fatal("management prompt differs or disclosed a token")
	}
	tools, err := client.ListTools(ctx, nil)
	if err != nil {
		t.Fatal("tool discovery failed")
	}
	available := map[string]bool{}
	for _, tool := range tools.Tools {
		available[tool.Name] = true
	}
	for _, name := range regexp.MustCompile(`\b(?:gatehome|sunpanel)_[a-z_]+\b`).FindAllString(mcpManagementGuide, -1) {
		if !available[name] {
			t.Fatalf("management guide references an unavailable tool: %s", name)
		}
	}
	w := adminRequest(h, "GET", "/api/mcp", nil, loginForTest(t, h), "")
	var settings struct {
		Guide string `json:"guide"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &settings) != nil || settings.Guide != text.Text {
		t.Fatal("web guide differs from the MCP prompt")
	}
}

func TestMCPSettingsSecretStorageAndHTTPBoundary(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	if adminRequest(h, "GET", "/api/mcp", nil, nil, "").Code != 401 {
		t.Fatal("MCP settings lack session authorization")
	}
	w := adminRequest(h, "PUT", "/api/mcp", map[string]any{"revision": a.store.Snapshot().Revision, "config": MCPConfig{Enabled: true, Read: true}}, cookie, "")
	var response struct {
		Token string `json:"token"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Token == "" {
		t.Fatal("MCP activation failed")
	}
	contents, err := os.ReadFile(a.store.path)
	if err != nil || strings.Contains(string(contents), response.Token) {
		t.Fatal("plaintext MCP token persisted")
	}
	payload, err := snapshotBackupPaths(a.store.paths, a.store.Snapshot())
	if err != nil {
		t.Fatal("MCP backup snapshot failed")
	}
	archive, err := encodeBackup(payload, "TEST_ONLY_MCP_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal("MCP backup encoding failed")
	}
	decoded, _, err := decodeBackup(archive, "TEST_ONLY_MCP_BACKUP_PASSWORD")
	if err != nil || !validMCPToken(decoded.State, "Bearer "+response.Token) || !decoded.State.MCP.Read {
		t.Fatal("MCP authorization was not preserved in backup")
	}
	w = adminRequest(h, "GET", "/api/mcp", nil, cookie, "")
	if strings.Contains(w.Body.String(), response.Token) || strings.Contains(w.Body.String(), a.store.Snapshot().MCPTokenHash) {
		t.Fatal("MCP settings repeated credential")
	}
	for _, test := range []struct {
		path, origin string
		want         int
	}{{"/mcp", "https://evil.example.test", 403}, {"/api/config", "", 401}, {"/mcp", "", 405}} {
		r := httptest.NewRequest("GET", "http://localhost:16666"+test.path, nil)
		r.Header.Set("Authorization", "Bearer "+response.Token)
		if test.origin != "" {
			r.Header.Set("Origin", test.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatalf("unexpected auth boundary status on %s: %d", test.path, w.Code)
		}
	}
	state := a.store.Snapshot()
	state.MCP.Enabled = false
	if _, err := a.store.saveMCP(state.MCP, false, state.Revision); err != nil {
		t.Fatal(err)
	}
	if validMCPToken(a.store.Snapshot(), "Bearer "+response.Token) {
		t.Fatal("disabled MCP accepted token")
	}
}

func TestMCPTokenCreationBeforeActivation(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	w := adminRequest(h, "POST", "/api/mcp/rotate-token", map[string]any{"revision": a.store.Snapshot().Revision}, cookie, "")
	var response struct {
		Token string `json:"token"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Token == "" {
		t.Fatal("API token creation failed")
	}
	token := response.Token
	state := a.store.Snapshot()
	if state.MCP.Enabled || state.MCP.Read || state.MCP.Write || state.MCP.Maintenance || validMCPToken(state, "Bearer "+token) {
		t.Fatal("token creation enabled service or granted permissions")
	}
	contents, err := os.ReadFile(a.store.path)
	if err != nil || strings.Contains(string(contents), token) {
		t.Fatal("API token persisted in plaintext")
	}
	w = adminRequest(h, "PUT", "/api/mcp", map[string]any{"revision": state.Revision, "config": MCPConfig{Enabled: true, Read: true}}, cookie, "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Token != "" {
		t.Fatal("activation regenerated or repeated the existing token")
	}
	if !validMCPToken(a.store.Snapshot(), "Bearer "+token) {
		t.Fatal("activation did not accept the API token created earlier")
	}
}

func TestMCPRedactionPreservesCredentialURLsOnEdit(t *testing.T) {
	a, _ := testAdmin(t)
	old := map[string]any{"id": "route", "url": "https://service.example.test/?token=TEST_ONLY_URL_CREDENTIAL&view=home"}
	old["api_key"] = map[string]any{"value": "TEST_ONLY_NESTED_CREDENTIAL"}
	old["fragment"] = "https://service.example.test/#access_token=TEST_ONLY_FRAGMENT_CREDENTIAL"
	encoded, _ := json.Marshal(old)
	var safe any
	json.Unmarshal(encoded, &safe)
	safe = mcpRedact(safe)
	masked, _ := json.Marshal(safe)
	if strings.Contains(string(masked), "TEST_ONLY_URL_CREDENTIAL") {
		t.Fatal("URL query credential disclosed")
	}
	for _, secret := range []string{"TEST_ONLY_NESTED_CREDENTIAL", "TEST_ONLY_FRAGMENT_CREDENTIAL"} {
		if strings.Contains(string(masked), secret) {
			t.Fatal("nested or fragment credential disclosed")
		}
	}
	restored, err := mcpRestoreRedactions(safe, old)
	if err != nil || restored.(map[string]any)["url"] != old["url"] {
		t.Fatal("masked URL credential was not preserved")
	}
	if !reflect.DeepEqual(restored.(map[string]any)["api_key"], old["api_key"]) {
		t.Fatal("nested credential not preserved")
	}
	if restored.(map[string]any)["fragment"] != old["fragment"] {
		t.Fatal("masked fragment credential not preserved")
	}
	if _, err := mcpRestoreRedactions("<REDACTED>", "different"); err == nil {
		t.Fatal("redaction placeholder accepted as configuration")
	}
	text := a.mcpSafeText("GET https://example.test/?api_key=TEST_ONLY_URL_CREDENTIAL Bearer TEST_ONLY_BEARER_CREDENTIAL TEST_ONLY_FAKE_TOKEN")
	for _, secret := range []string{"TEST_ONLY_URL_CREDENTIAL", "TEST_ONLY_BEARER_CREDENTIAL", "TEST_ONLY_FAKE_TOKEN"} {
		if strings.Contains(text, secret) {
			t.Fatal("log credential disclosed")
		}
	}
}

func TestMCPBridgeHiddenFromPublicSunPanel(t *testing.T) {
	panel := (&SunPanel{}).Handler()
	for _, path := range []string{"/sunpanel/integration/mcp", "/sunpanel//integration/mcp", "/sunpanel/integration%2fmcp", "/sunpanel/integration/mcp/.."} {
		w := httptest.NewRecorder()
		panel.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code != 404 {
			t.Fatalf("internal bridge exposed via %s", path)
		}
	}
}
