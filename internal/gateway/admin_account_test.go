package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAdminAccountMigrationAndBackup(t *testing.T) {
	a, _ := testAdmin(t)
	previous := a.store.Snapshot()
	data, _ := os.ReadFile(a.store.path)
	var fields map[string]json.RawMessage
	json.Unmarshal(data, &fields)
	delete(fields, "admin_username")
	legacy, _ := json.Marshal(fields)
	if atomicWrite(a.store.path, legacy) != nil {
		t.Fatal("legacy fixture failed")
	}
	migrated, err := OpenStorePaths(a.store.paths)
	if err != nil || migrated.Snapshot().AdminUsername != "admin" || migrated.Snapshot().PasswordHash != previous.PasswordHash || !reflect.DeepEqual(migrated.Snapshot().Config, previous.Config) {
		t.Fatal("legacy migration changed password or business configuration")
	}
	a.store = migrated
	h := a.Handler()
	loginForTest(t, h)
	if a.store.SetAdminAccount("TEST_ONLY_OWNER", previous.PasswordHash) != nil {
		t.Fatal("account setup failed")
	}
	payload := backupPayload{State: a.store.Snapshot(), Certificates: map[string][]byte{}}
	backup, err := encodeBackup(payload, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	restored, _, err := decodeBackup(backup, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil || restored.State.AdminUsername != "TEST_ONLY_OWNER" || restored.State.PasswordHash != previous.PasswordHash {
		t.Fatal("administrator account missing from backup")
	}
}

func TestAdminAccountChangeRevokesSessionsAndKeepsServiceAccounts(t *testing.T) {
	a, _ := testAdmin(t)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("TEST_ONLY_SERVICE_OK")) }))
	defer backend.Close()
	c := a.store.Snapshot().Config
	c.Routes = []Route{{GroupID: "default", Host: "nas.example.test", Upstream: backend.URL, Enabled: true, Auth: RouteAuthConfig{Enabled: true, Username: "TEST_ONLY_VISITOR"}}}
	servicePassword := "TEST_ONLY_SERVICE_PASSWORD"
	if a.store.UpdateRouteCredentials(c, nil, nil, nil, nil, map[string]*string{routeKey(c.Routes[0]): &servicePassword}, nil, a.store.Snapshot().Revision) != nil {
		t.Fatal("service credentials setup failed")
	}
	a.proxy.ConfigureState(a.store.Snapshot())
	h := a.Handler()
	first := loginForTest(t, h)
	second := loginForTest(t, h)
	serviceCookie := routeLoginTest(t, a.proxy, "default", "nas.example.test", "TEST_ONLY_VISITOR", servicePassword)
	before := a.store.Snapshot()
	newPassword := "TEST_ONLY_NEW_ADMIN_PASSWORD"
	w := adminRequest(h, "PUT", "/api/account", map[string]string{"username": "TEST_ONLY_OWNER", "current_password": "TEST_ONLY_ADMIN_PASSWORD", "new_password": newPassword, "confirm_password": newPassword}, first, "")
	if w.Code != 200 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatal("management account change failed or cookie not cleared")
	}
	after := a.store.Snapshot()
	if after.AdminUsername != "TEST_ONLY_OWNER" || after.PasswordHash == before.PasswordHash || after.Revision != before.Revision+1 {
		t.Fatal("administrator credentials not updated atomically")
	}
	if !reflect.DeepEqual(after.Config, before.Config) || !reflect.DeepEqual(after.RoutePasswordHashes, before.RoutePasswordHashes) || !reflect.DeepEqual(after.DNSCredentials, before.DNSCredentials) {
		t.Fatal("administrator change altered independent service data")
	}
	for _, cookie := range []*http.Cookie{first, second} {
		if adminRequest(h, "GET", "/api/config", nil, cookie, "").Code != 401 {
			t.Fatal("administrator session remained valid after credentials changed")
		}
	}
	for _, credentials := range []map[string]string{{"password": newPassword}, {"username": "admin", "password": newPassword}, {"username": "TEST_ONLY_OWNER", "password": "TEST_ONLY_ADMIN_PASSWORD"}, {"username": "TEST_ONLY_VISITOR", "password": servicePassword}} {
		if adminRequest(h, "POST", "/api/login", credentials, nil, "").Code != 401 {
			t.Fatal("password-only, stale or service credentials logged into management")
		}
	}
	login := adminRequest(h, "POST", "/api/login", map[string]string{"username": "TEST_ONLY_OWNER", "password": newPassword}, nil, "")
	if login.Code != 200 {
		t.Fatal("new management account cannot log in")
	}
	if w := routeAuthRequest(a.proxy, "default", "GET", "http://nas.example.test/private", nil, serviceCookie); w.Code != 200 {
		t.Fatal("management change invalidated service session")
	}
	reopened, err := OpenStorePaths(a.store.paths)
	if err != nil || reopened.Snapshot().AdminUsername != after.AdminUsername || reopened.Snapshot().PasswordHash != after.PasswordHash {
		t.Fatal("management credentials not persisted")
	}
	for _, secret := range []string{newPassword, "TEST_ONLY_ADMIN_PASSWORD", servicePassword, after.PasswordHash} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("account response leaked password data")
		}
	}
}

func TestAdminAccountChangesRequireCurrentPasswordAndCSRF(t *testing.T) {
	for _, test := range []struct {
		name   string
		body   map[string]string
		status int
	}{
		{"wrong current", map[string]string{"username": "new-owner", "current_password": "TEST_ONLY_WRONG"}, 400},
		{"empty current", map[string]string{"username": "new-owner"}, 400},
		{"empty username", map[string]string{"current_password": "TEST_ONLY_ADMIN_PASSWORD"}, 400},
		{"whitespace", map[string]string{"username": " new-owner", "current_password": "TEST_ONLY_ADMIN_PASSWORD"}, 400},
		{"control", map[string]string{"username": "owner\n", "current_password": "TEST_ONLY_ADMIN_PASSWORD"}, 400},
		{"short password", map[string]string{"username": "new-owner", "current_password": "TEST_ONLY_ADMIN_PASSWORD", "new_password": "short", "confirm_password": "short"}, 400},
		{"mismatch", map[string]string{"username": "new-owner", "current_password": "TEST_ONLY_ADMIN_PASSWORD", "new_password": "TEST_ONLY_NEW_PASSWORD", "confirm_password": "TEST_ONLY_DIFFERENT"}, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, h := testAdmin(t)
			cookie := loginForTest(t, h)
			before := a.store.Snapshot()
			w := adminRequest(h, "PUT", "/api/account", test.body, cookie, "")
			if w.Code != test.status || !reflect.DeepEqual(before, a.store.Snapshot()) {
				t.Fatal("invalid management change mutated persistent state")
			}
			if adminRequest(h, "GET", "/api/account", nil, cookie, "").Code != 200 {
				t.Fatal("failed change revoked current session")
			}
		})
	}
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	before := a.store.Snapshot()
	body := map[string]string{"username": "new-owner", "current_password": "TEST_ONLY_ADMIN_PASSWORD"}
	if adminRequest(h, "PUT", "/api/account", body, nil, "").Code != 401 || adminRequest(h, "PUT", "/api/account", body, cookie, "https://evil.example").Code != 403 {
		t.Fatal("account change lacked authentication or origin validation")
	}
	serviceCookie := &http.Cookie{Name: routeCookiePrefix + "test", Value: "TEST_ONLY_FAKE_SESSION"}
	if adminRequest(h, "PUT", "/api/account", body, serviceCookie, "").Code != 401 || !reflect.DeepEqual(before, a.store.Snapshot()) {
		t.Fatal("service session changed management account")
	}
	a.store.path = filepath.Join(t.TempDir(), "missing", "state.json")
	if adminRequest(h, "PUT", "/api/account", body, cookie, "").Code != 400 || !reflect.DeepEqual(before, a.store.Snapshot()) || adminRequest(h, "GET", "/api/account", nil, cookie, "").Code != 200 {
		t.Fatal("failed disk write changed credentials or revoked sessions")
	}
}

func TestAdminUsernameOnlyChangeRetainsPassword(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	oldHash := a.store.Snapshot().PasswordHash
	body := map[string]string{"username": "TEST_ONLY_CHANGED_NAME", "current_password": "TEST_ONLY_ADMIN_PASSWORD"}
	if adminRequest(h, "PUT", "/api/account", body, cookie, "").Code != 200 || a.store.Snapshot().PasswordHash != oldHash {
		t.Fatal("username-only change replaced password")
	}
	if adminRequest(h, "POST", "/api/login", map[string]string{"username": "TEST_ONLY_CHANGED_NAME", "password": "TEST_ONLY_ADMIN_PASSWORD"}, nil, "").Code != 200 {
		t.Fatal("username-only account could not log in with original password")
	}
}
