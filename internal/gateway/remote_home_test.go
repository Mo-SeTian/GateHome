package gateway

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteHomeRequiresAdminAndOrigin(t *testing.T) {
	_, h := testAdmin(t)
	for _, op := range []string{"settings", "add", "edit", "remove"} {
		if w := adminRequest(h, "POST", "/api/remote-home/"+op, map[string]any{}, nil, ""); w.Code != 401 {
			t.Fatalf("%s without session: HTTP %d", op, w.Code)
		}
	}
	cookie := loginForTest(t, h)
	if w := adminRequest(h, "POST", "/api/remote-home/settings", map[string]any{}, cookie, "https://unrelated.example"); w.Code != 403 {
		t.Fatalf("unexpected origin: HTTP %d", w.Code)
	}
}

func TestWireGuardAssistantOnlyExposesOperationalFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/status" {
			t.Errorf("unexpected host operation %s %s", r.Method, r.URL.Path)
		}
		jsonResponse(w, 200, map[string]any{
			"running": true, "settings": map[string]string{"lan": "192.168.2.0/24", "private": "OMIT"},
			"devices":           []map[string]any{{"name": "phone", "handshake": 123, "key": "OMIT"}},
			"server_public_key": "OMIT", "profile": "OMIT",
		})
	}))
	defer server.Close()
	original := wireGuardClient
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	wireGuardClient = &http.Client{Transport: transport}
	t.Cleanup(func() { wireGuardClient = original; transport.CloseIdleConnections() })
	data, err := json.Marshal(wireGuardAssistant(context.Background()))
	if err != nil || strings.Contains(string(data), "OMIT") || !strings.Contains(string(data), `"handshake":123`) || !strings.Contains(string(data), "192.168.2.0/24") {
		t.Fatal("assistant projection omitted operational data or included unapproved fields")
	}
}
