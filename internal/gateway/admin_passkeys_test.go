package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestAdminPasskeysListOriginValidationAndAccountRevocation(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)

	// 1. Unauthenticated GET /api/account/passkeys rejected; authenticated returns empty list.
	if w := adminRequest(h, "GET", passkeyPath, nil, nil, ""); w.Code != 401 {
		t.Fatalf("expected 401 for unauthenticated passkey list, got %d", w.Code)
	}
	w := adminRequest(h, "GET", passkeyPath, nil, cookie, "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("expected 200 [] for empty passkey list, got %d %s", w.Code, w.Body.String())
	}

	// 2. passkeyOrigin rejects IP literals, non-HTTPS non-localhost origins, and cross-site fetches.
	for _, tc := range []struct {
		name      string
		origin    string
		fetchSite string
		host      string
		wantRP    string
	}{
		{"localhost allowed", "http://localhost:16666", "same-origin", "localhost:16666", "localhost"},
		{"ip literal rejected", "http://127.0.0.1:16666", "same-origin", "127.0.0.1:16666", ""},
		{"plain http non-localhost rejected", "http://admin.example.test:16666", "same-origin", "admin.example.test:16666", ""},
		{"cross-site rejected", "http://localhost:16666", "cross-site", "localhost:16666", ""},
	} {
		req := httptest.NewRequest("POST", passkeyPath+"/login/begin", nil)
		req.Host = tc.host
		req.Header.Set("Origin", tc.origin)
		if tc.fetchSite != "" {
			req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
		}
		_, gotRP := a.passkeyOrigin(req)
		if gotRP != tc.wantRP {
			t.Fatalf("%s: passkeyOrigin rp=%q want %q", tc.name, gotRP, tc.wantRP)
		}
	}

	// 3. Begin login returns 404 when no passkeys exist for RP; invalid finish returns 400.
	beginReq := httptest.NewRequest("POST", passkeyPath+"/login/begin", nil)
	beginReq.Host = "localhost:16666"
	beginReq.Header.Set("Origin", "http://localhost:16666")
	beginReq.Header.Set("Content-Type", "application/json")
	beginReq.Header.Set("X-Gatehouse-Request", "1")
	beginW := httptest.NewRecorder()
	h.ServeHTTP(beginW, beginReq)
	if beginW.Code != 404 {
		t.Fatalf("expected 404 when no passkeys exist, got %d %s", beginW.Code, beginW.Body.String())
	}

	finishReq := httptest.NewRequest("POST", passkeyPath+"/login/finish", strings.NewReader(`{}`))
	finishReq.Host = "localhost:16666"
	finishReq.Header.Set("Origin", "http://localhost:16666")
	finishReq.Header.Set("Content-Type", "application/json")
	finishReq.Header.Set("X-Gatehouse-Request", "1")
	finishW := httptest.NewRecorder()
	h.ServeHTTP(finishW, finishReq)
	if finishW.Code != 400 {
		t.Fatalf("expected 400 for finish without flow cookie, got %d", finishW.Code)
	}

	// 4. Seed a passkey, initiate login begin to create an in-flight passkeyFlow,
	//    then change admin account credentials and verify passkeyFlows was cleared.
	if !a.store.savePasskeys([]byte("user-1"), []adminPasskey{{
		RPID:    "localhost",
		Created: time.Now(),
		Credential: webauthn.Credential{
			ID:        []byte("cred-1"),
			PublicKey: []byte("pub-1"),
		},
	}}) {
		t.Fatal("failed to seed passkey fixture")
	}
	beginReq2 := httptest.NewRequest("POST", passkeyPath+"/login/begin", nil)
	beginReq2.Host = "localhost:16666"
	beginReq2.Header.Set("Origin", "http://localhost:16666")
	beginReq2.Header.Set("Content-Type", "application/json")
	beginReq2.Header.Set("X-Gatehouse-Request", "1")
	beginW2 := httptest.NewRecorder()
	h.ServeHTTP(beginW2, beginReq2)
	if beginW2.Code != 200 {
		t.Fatalf("expected 200 for seeded passkey begin login, got %d %s", beginW2.Code, beginW2.Body.String())
	}
	var pkCookie *http.Cookie
	for _, c := range beginW2.Result().Cookies() {
		if c.Name == passkeyCookie {
			pkCookie = c
			break
		}
	}
	if pkCookie == nil {
		t.Fatal("expected passkey challenge cookie")
	}

	// Changing admin username must clear in-flight passkeyFlows alongside sessions.
	accW := adminRequest(h, "PUT", "/api/account", map[string]string{
		"username":         "newadmin",
		"current_password": "TEST_ONLY_ADMIN_PASSWORD",
	}, cookie, "")
	if accW.Code != 200 {
		t.Fatalf("expected PUT /api/account 200, got %d %s", accW.Code, accW.Body.String())
	}
	a.mu.Lock()
	remainingFlows := len(a.passkeyFlows)
	a.mu.Unlock()
	if remainingFlows != 0 {
		t.Fatalf("expected passkeyFlows to be cleared on account change, got %d", remainingFlows)
	}

	// Finishing with the pre-change passkey cookie must now be rejected cleanly.
	finishReq2 := httptest.NewRequest("POST", passkeyPath+"/login/finish", strings.NewReader(`{}`))
	finishReq2.Host = "localhost:16666"
	finishReq2.Header.Set("Origin", "http://localhost:16666")
	finishReq2.Header.Set("Content-Type", "application/json")
	finishReq2.Header.Set("X-Gatehouse-Request", "1")
	finishReq2.AddCookie(pkCookie)
	finishW2 := httptest.NewRecorder()
	h.ServeHTTP(finishW2, finishReq2)
	if finishW2.Code != 400 {
		t.Fatalf("expected 400 after invalidated passkey flow, got %d", finishW2.Code)
	}
	var errPayload map[string]string
	_ = json.Unmarshal(finishW2.Body.Bytes(), &errPayload)
	if !strings.Contains(errPayload["error"], "失效") {
		t.Fatalf("unexpected error payload after invalidated passkey flow: %+v", errPayload)
	}
}
