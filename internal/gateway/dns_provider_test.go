package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyDNSAccountMigratesIntoGroupsOnce(t *testing.T) {
	c := DefaultConfig()
	c.Zone = "example.com"
	g := testDDNSGroup()
	g.Provider = ""
	g.Zone = ""
	g.Enabled = true
	c.DDNS.Groups = []DDNSGroup{g}
	dir := t.TempDir()
	if err := writeJSON(filepath.Join(dir, "state.json"), State{Config: c, CloudflareToken: "TEST_ONLY_LEGACY_TOKEN", Revision: 4}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		store, err := OpenStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		s := store.Snapshot()
		if s.Revision != 5 || s.Config.Zone != "" || s.CloudflareToken != "" || s.Config.DDNS.Groups[0].Provider != "cloudflare" || s.Config.DDNS.Groups[0].Zone != "example.com" || s.DNSCredentials[g.ID].Token != "TEST_ONLY_LEGACY_TOKEN" {
			t.Fatal("global credentials were lost, exposed globally, or migration repeated")
		}
	}
}

func TestLegacyCertificateAccountMigration(t *testing.T) {
	for _, count := range []int{0, 2} {
		t.Run(map[int]string{0: "certificate-only", 2: "multiple-groups"}[count], func(t *testing.T) {
			c := DefaultConfig()
			c.Zone = "example.com"
			c.ACME.Enabled, c.ACME.AcceptTerms = true, true
			c.ACME.Requests = nil // On-disk versions before 0.0.5 had no manual requests.
			c.ACME.Email = "admin@example.com"
			r := testRoute("nas.example.com", "http://localhost:5000")
			r.TLS = true
			r.Enabled = count == 0 // Temporarily disabled routes must retain their certificate account too.
			c.Routes = []Route{r}
			for i := 0; i < count; i++ {
				g := testDDNSGroup()
				g.Provider, g.Zone = "", ""
				if i == 1 {
					g.ID, g.Hosts = "photos", []string{"photos.example.com"}
				}
				c.DDNS.Groups = append(c.DDNS.Groups, g)
			}
			dir := t.TempDir()
			if err := writeJSON(filepath.Join(dir, "state.json"), State{Config: c, CloudflareToken: "TEST_ONLY_LEGACY_TOKEN", Revision: 4}); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				store, err := OpenStore(dir)
				if err != nil {
					t.Fatal(err)
				}
				s := store.Snapshot()
				g, err := s.Config.CertificateDNSGroup(r.Host)
				if err != nil || s.Revision != 5 || s.DNSCredentials[g.ID].Token != "TEST_ONLY_LEGACY_TOKEN" {
					t.Fatal("certificate account was lost or ambiguous after migration")
				}
				if len(s.Config.ACME.Requests) != 1 || s.Config.ACME.Requests[0].Domains[0] != r.Host || s.CertificateCredentials[s.Config.ACME.Requests[0].ID].Token != "TEST_ONLY_LEGACY_TOKEN" {
					t.Fatal("legacy certificate task did not retain independent credentials")
				}
			}
		})
	}
}

func TestGroupTokenAPIIsolationAndSecretRedaction(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	c := DefaultConfig()
	one := testDDNSGroup()
	two := one
	two.ID = "other"
	two.Zone = "example.net"
	two.Hosts = []string{"nas.example.net"}
	c.DDNS.Groups = []DDNSGroup{one, two}
	w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 1, "dns_tokens": map[string]string{one.ID: "TEST_ONLY_FIRST_TOKEN", two.ID: "TEST_ONLY_SECOND_TOKEN"}}, cookie, "")
	if w.Code != 200 {
		t.Fatal("per-group credentials save failed")
	}
	for _, token := range []string{"TEST_ONLY_FIRST_TOKEN", "TEST_ONLY_SECOND_TOKEN"} {
		if strings.Contains(w.Body.String(), token) {
			t.Fatal("DNS group token disclosed")
		}
	}
	c.DDNS.Groups = append([]DDNSGroup(nil), c.DDNS.Groups...)
	c.DDNS.Groups[0].Name = "改名"
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 2}, cookie, "")
	if w.Code != 200 || a.store.Snapshot().DNSCredentials[one.ID].Token != "TEST_ONLY_FIRST_TOKEN" {
		t.Fatal("omitted token was overwritten")
	}
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 3, "dns_tokens": map[string]string{one.ID: ""}}, cookie, "")
	s := a.store.Snapshot()
	if w.Code != 200 || s.DNSCredentials[one.ID].Token != "" || s.DNSCredentials[two.ID].Token != "TEST_ONLY_SECOND_TOKEN" {
		t.Fatal("clearing one group changed another account")
	}
	c.DDNS.Groups = append([]DDNSGroup(nil), c.DDNS.Groups...)
	c.DDNS.Groups[0].Enabled = true
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 4}, cookie, "")
	if w.Code != 400 {
		t.Fatal("enabled group without its own credentials accepted")
	}
}

func TestDDNSUsesEachGroupsZoneAndToken(t *testing.T) {
	seen := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/zones" {
			zone := r.URL.Query().Get("name")
			want := map[string]string{"example.com": "Bearer TEST_ONLY_ONE", "example.net": "Bearer TEST_ONLY_TWO"}[zone]
			if want == "" || r.Header.Get("Authorization") != want {
				t.Error("group used another account or zone")
			}
			seen[zone]++
			json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []map[string]string{{"id": zone, "name": zone}}})
			return
		}
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []any{}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{}})
	}))
	defer server.Close()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	j := NewJobs(store, nil)
	j.provider = func(s State, g DDNSGroup, logs *Logs) (dnsProvider, error) {
		p, err := newDNSProvider(s, g, logs)
		if err == nil {
			p.(cloudflareProvider).BaseURL = server.URL
		}
		return p, err
	}
	j.discover = func(context.Context, DDNSGroup, string) (DetectedIP, error) {
		return DetectedIP{Address: netip.MustParseAddr("8.8.8.8"), Interface: "test0"}, nil
	}
	one := testDDNSGroup()
	one.Mode = "ipv4"
	two := one
	two.ID = "other"
	two.Zone = "example.net"
	two.Hosts = []string{"nas.example.net"}
	s := State{Config: DefaultConfig(), CloudflareToken: "TEST_ONLY_WRONG_GLOBAL", DNSCredentials: map[string]DNSCredential{one.ID: {Token: "TEST_ONLY_ONE"}, two.ID: {Token: "TEST_ONLY_TWO"}}}
	for _, g := range []DDNSGroup{one, two} {
		if _, err := j.syncDNS(context.Background(), s, g); err != nil {
			t.Fatal("isolated group sync failed")
		}
	}
	if seen[one.Zone] != 1 || seen[two.Zone] != 1 {
		t.Fatal("both group zones were not synchronized")
	}
}

func TestCertificateDNSGroupSelection(t *testing.T) {
	c := DefaultConfig()
	one := testDDNSGroup()
	two := one
	two.ID = "other"
	two.Zone = "example.net"
	two.Hosts = []string{"nas.example.net"}
	c.DDNS.Groups = []DDNSGroup{one, two}
	for host, id := range map[string]string{"nas.example.com": one.ID, "photos.example.net": two.ID} {
		g, err := c.CertificateDNSGroup(host)
		if err != nil || g.ID != id {
			t.Fatal("certificate chose another zone's account")
		}
	}
	duplicate := one
	duplicate.ID = "duplicate"
	c.DDNS.Groups = append(c.DDNS.Groups, duplicate)
	if _, err := c.CertificateDNSGroup("nas.example.com"); err == nil {
		t.Fatal("ambiguous accounts were silently selected")
	}
	c.ACME.DNSGroups["nas.example.com"] = one.ID
	g, err := c.CertificateDNSGroup("nas.example.com")
	if err != nil || g.ID != one.ID {
		t.Fatal("explicit certificate binding ignored")
	}
	c.ACME.DNSGroups["nas.example.com"] = two.ID
	if _, err := c.CertificateDNSGroup("nas.example.com"); err == nil {
		t.Fatal("certificate used a group outside its zone")
	}
}

func TestBackupAndLogsProtectGroupCredentials(t *testing.T) {
	a, _ := testAdmin(t)
	s := a.store.Snapshot()
	s.Config = DefaultConfig()
	g := testDDNSGroup()
	s.Config.DDNS.Groups = []DDNSGroup{g}
	s.DNSCredentials = map[string]DNSCredential{g.ID: {Token: "TEST_ONLY_GROUP_SECRET"}}
	data, err := encodeBackup(backupPayload{State: s, Certificates: map[string][]byte{}}, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("TEST_ONLY_GROUP_SECRET")) {
		t.Fatal("group credential leaked in backup")
	}
	decoded, _, err := decodeBackup(data, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil || decoded.State.DNSCredentials[g.ID].Token != s.DNSCredentials[g.ID].Token {
		t.Fatal("group credentials did not survive backup")
	}
	token := s.DNSCredentials[g.ID].Token
	if err := a.store.UpdateDNS(s.Config, map[string]*string{g.ID: &token}, nil, nil, 1); err != nil {
		t.Fatal(err)
	}
	l, err := NewLogs(t.TempDir(), a.store)
	if err != nil {
		t.Fatal(err)
	}
	l.Add(LogEntry{Category: "ddns", Message: "TEST_ONLY_GROUP_SECRET"})
	rows, _, _ := l.List("", "", 0, 10)
	if strings.Contains(rows[0].Message, "TEST_ONLY_GROUP_SECRET") {
		t.Fatal("group credential appeared in logs")
	}
}
