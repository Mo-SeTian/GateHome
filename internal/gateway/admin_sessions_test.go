package gateway

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestManagementSessionRevocationAndRecentVerification(t *testing.T) {
	a, h := testAdmin(t)
	first, second := loginForTest(t, h), loginForTest(t, h)
	w := adminRequest(h, "GET", "/api/account/sessions", nil, first, "")
	var list struct {
		Current  string
		Sessions []adminSession
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Sessions) != 2 {
		t.Fatal("active sessions missing")
	}
	for _, session := range list.Sessions {
		if session.Started.IsZero() || session.LastSeen.Before(session.Started) || session.Method != "password" {
			t.Fatal("session metadata missing")
		}
		if session.ID != list.Current && adminRequest(h, "DELETE", "/api/account/sessions/"+session.ID, map[string]any{}, first, "").Code != 204 {
			t.Fatal("unable to revoke individual session")
		}
	}
	if adminRequest(h, "GET", "/api/account", nil, second, "").Code != 401 ||
		adminRequest(h, "GET", "/api/account", nil, first, "").Code != 200 {
		t.Fatal("wrong session revoked")
	}

	key := sha256.Sum256([]byte(first.Value))
	a.mu.Lock()
	session := a.sessions[key]
	session.Verified = time.Now().Add(-recentAdminWindow)
	a.sessions[key] = session
	a.mu.Unlock()
	w = adminRequest(h, "POST", "/api/credentials/reveal", map[string]string{"kind": "github"}, first, "")
	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("stale verification returned %d", w.Code)
	}
	if adminRequest(h, "POST", "/api/account/verify", map[string]string{"password": "TEST_ONLY_ADMIN_PASSWORD"}, first, "").Code != http.StatusNoContent {
		t.Fatal("password did not renew the current session")
	}
	if adminRequest(h, "POST", "/api/credentials/reveal", map[string]string{"kind": "github"}, first, "").Code != http.StatusNotFound {
		t.Fatal("fresh verification was not reused")
	}

}

func TestExposureRequiresSessionAndShowsManagementListener(t *testing.T) {
	a, h := testAdmin(t)
	a.adminAddress = "127.0.0.1:16666"
	if adminRequest(h, "GET", "/api/exposure", nil, nil, "").Code != 401 {
		t.Fatal("unauthenticated exposure summary")
	}
	w := adminRequest(h, "GET", "/api/exposure", nil, loginForTest(t, h), "")
	var data struct{ Entries []exposureEntry }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &data) != nil || len(data.Entries) == 0 ||
		data.Entries[0].Address != "127.0.0.1:16666" || data.Entries[0].Transport != "HTTP" {
		t.Fatal("management exposure does not describe the running listener")
	}
}
