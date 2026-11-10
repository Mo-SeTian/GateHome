package gateway

import (
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestTrustedProxySettingsSurvivePersistenceAndBackup(t *testing.T) {
	a, _ := testAdmin(t)
	before := a.store.Snapshot()
	config := before.Config
	config.TrustedProxies = []string{"192.0.2.10/32", "2001:db8:1::/64"}
	if a.store.Update(config, nil, before.Revision) != nil {
		t.Fatal("trusted proxies could not be saved")
	}
	reopened, err := OpenStorePaths(a.store.paths)
	if err != nil {
		t.Fatal("saved configuration could not be reopened")
	}
	archive, err := encodeBackup(backupPayload{State: reopened.Snapshot(), Certificates: map[string][]byte{}, Files: map[string][]byte{}}, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal("backup creation failed")
	}
	restored, _, err := decodeBackup(archive, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil || !reflect.DeepEqual(restored.State.Config.TrustedProxies, config.TrustedProxies) || restored.State.PasswordHash != before.PasswordHash {
		t.Fatal("backup lost trusted proxies or changed existing account data")
	}
}

func TestTrustedProxyChainUsesClosestUntrustedHop(t *testing.T) {
	trusted, err := parseTrustedProxies([]string{"192.0.2.10", "2001:db8:1::/64"})
	if err != nil {
		t.Fatal("fixture proxy list rejected")
	}
	for _, test := range []struct{ peer, chain, want string }{
		{"192.0.2.10:1234", "198.51.100.1", "198.51.100.1:1234"},
		{"192.0.2.10:1234", "garbage, 127.0.0.1, 198.51.100.1", "198.51.100.1:1234"},
		{"192.0.2.10:1234", "2001:db8:2::1, 2001:db8:1::2", "[2001:db8:2::1]:1234"},
		{"192.0.2.11:1234", "198.51.100.1", "192.0.2.11:1234"},
		{"192.0.2.10:1234", "invalid", "192.0.2.10:1234"},
		{"192.0.2.10:1234", "", "192.0.2.10:1234"},
		{"192.0.2.10:1234", strings.Repeat("invalid,", 100) + "198.51.100.2", "198.51.100.2:1234"},
	} {
		r := httptest.NewRequest("GET", "http://example.test", nil)
		r.RemoteAddr = test.peer
		r.Header.Set("X-Forwarded-For", test.chain)
		if got := proxySourceRequest(r, trusted).RemoteAddr; got != test.want {
			t.Fatal("forwarded chain crossed its trust boundary")
		}
		if r.RemoteAddr != test.peer {
			t.Fatal("shared input request was mutated")
		}
	}
	for _, value := range []string{"0.0.0.0/0", "::/0", "https://proxy.test", "192.0.2.10:80", "fe80::1%eth0"} {
		c := DefaultConfig()
		c.TrustedProxies = []string{value}
		if Validate(c) == nil {
			t.Fatal("invalid trusted source persisted")
		}
	}
}

func TestTrustedProxyFreezesClientsWithoutFreezingTheirProxy(t *testing.T) {
	policy := Protection{Rate: RatePolicy{Enabled: true, Requests: 1, WindowSeconds: 60, Path: "/api"}, Freeze: FreezePolicy{Enabled: true, Failures: 2, WindowSeconds: 300, Seconds: 3600}}
	p, c, _ := protectionProxy(t, policy)
	c.TrustedProxies = []string{"192.0.2.10/32"}
	if p.Configure(c) != nil {
		t.Fatal("fixture configuration rejected")
	}
	request := func(client string) int {
		r := httptest.NewRequest("GET", "http://app.example.com/api", nil)
		r.RemoteAddr = "192.0.2.10:1234"
		r.Header.Add("X-Forwarded-For", "127.0.0.1")
		r.Header.Add("X-Forwarded-For", client)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, r)
		return w.Code
	}
	for _, want := range []int{200, 429, 403, 403} {
		if got := request("198.51.100.1"); got != want {
			t.Fatal("client rate/freeze policy changed")
		}
	}
	if request("198.51.100.2") != 200 {
		t.Fatal("another client inherited the proxy freeze")
	}
	blocks, _ := p.defense.list()
	if len(blocks) != 1 || blocks[0].IP != "198.51.100.1" {
		t.Fatal("proxy or spoofed prefix was frozen")
	}
}
