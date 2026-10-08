package gateway

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestRouteCredentialsAPIValidationRedactionAndPersistence(t *testing.T) {
	a, h := testAdmin(t)
	adminCookie := loginForTest(t, h)
	c := a.store.Snapshot().Config
	r := testRoute("nas.example.test", "http://127.0.0.1:17777")
	r.Auth = RouteAuthConfig{Enabled: true, Username: "nas-visitor"}
	c.Routes = []Route{r}
	save := func(cfg Config, passwords map[string]*string) int {
		t.Helper()
		w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": cfg, "revision": a.store.Snapshot().Revision, "route_passwords": passwords}, adminCookie, "")
		return w.Code
	}
	if save(c, nil) != 400 {
		t.Fatal("authentication enabled without a service password")
	}
	password := testRoutePassword
	if save(c, map[string]*string{routeKey(r): &password}) != 200 {
		t.Fatal("service credential save failed")
	}
	s := a.store.Snapshot()
	hash := s.RoutePasswordHashes[routeKey(r)]
	if hash == password || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		t.Fatal("service password was not independently hashed")
	}
	for _, method := range []string{"GET", "PUT"} {
		var body any
		if method == "PUT" {
			body = map[string]any{"config": c, "revision": a.store.Snapshot().Revision}
		}
		w := adminRequest(h, method, "/api/config", body, adminCookie, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), password) || strings.Contains(w.Body.String(), hash) || strings.Contains(w.Body.String(), "route_password_hashes") {
			t.Fatal("service credentials leaked in admin response")
		}
		if !strings.Contains(w.Body.String(), `"route_passwords_configured"`) {
			t.Fatal("credential status missing")
		}
	}
	reopened, err := OpenStore(filepath.Dir(a.store.path))
	if err != nil || reopened.Snapshot().RoutePasswordHashes[routeKey(r)] != hash {
		t.Fatal("service credentials lost across restart")
	}
	data, err := encodeBackup(backupPayload{State: a.store.Snapshot()}, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	payload, _, err := decodeBackup(data, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil || payload.State.RoutePasswordHashes[routeKey(r)] != hash || payload.State.Config.Routes[0].Auth.Username != "nas-visitor" {
		t.Fatal("encrypted backup lost service credentials")
	}
	short := "short"
	if save(c, map[string]*string{routeKey(r): &short}) != 400 || save(c, map[string]*string{"missing/route": &password}) != 400 {
		t.Fatal("invalid service credential accepted")
	}
	c.Routes = append([]Route(nil), c.Routes...)
	c.Routes[0].Auth.Username = "renamed-visitor"
	if save(c, nil) != 400 {
		t.Fatal("account change silently reused old password")
	}
	if save(c, map[string]*string{routeKey(r): &password}) != 200 {
		t.Fatal("account and password replacement failed")
	}
	empty := ""
	if save(c, map[string]*string{routeKey(r): &empty}) != 400 {
		t.Fatal("enabled verification lost its password")
	}
	c.Routes[0].Auth.Enabled = false
	if save(c, map[string]*string{routeKey(r): &empty}) != 200 || a.store.Snapshot().RoutePasswordHashes[routeKey(r)] != "" {
		t.Fatal("disabled credential could not be cleared")
	}
	c.Routes = nil
	if save(c, nil) != 200 || len(a.store.Snapshot().RoutePasswordHashes) != 0 {
		t.Fatal("deleted route retained password hash")
	}
	// Service sessions and credential writes cannot grant administrative access.
	if adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": a.store.Snapshot().Revision, "route_passwords": map[string]*string{}}, &http.Cookie{Name: routeCookiePrefix + "fake", Value: "TEST_ONLY_SESSION"}, "").Code != 401 {
		t.Fatal("service cookie granted config write access")
	}
}

func TestRouteAuthConfigValidation(t *testing.T) {
	for _, auth := range []RouteAuthConfig{{Enabled: true}, {Enabled: true, Username: " padded "}, {Username: "line\nbreak"}, {Username: strings.Repeat("a", 65)}} {
		if validateRouteAuth(auth) == nil {
			t.Fatal("invalid service username accepted")
		}
	}
	for _, auth := range []RouteAuthConfig{{}, {Enabled: true, Username: "访客"}, {Username: "nas-user"}} {
		if validateRouteAuth(auth) != nil {
			t.Fatal("valid service username rejected")
		}
	}
	c := DefaultConfig()
	r := testRoute("nas.example.test", "http://127.0.0.1:17777")
	r.Auth = RouteAuthConfig{Enabled: true, Username: "visitor"}
	c.Routes = []Route{r}
	p := NewProxy(c, nil)
	if routeAuthRequest(p, "default", "GET", "http://nas.example.test/", nil).Code != 401 {
		t.Fatal("missing server credential did not fail closed")
	}
}
