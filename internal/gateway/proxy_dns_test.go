package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"testing"
)

func TestProxyBindingAddsDNSDomainsAtomically(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	c := DefaultConfig()
	g := testDDNSGroup()
	g.Hosts = []string{"manual.example.com"}
	c.DDNS.Groups = []DDNSGroup{g}
	c.Groups[0].DomainSuffix, c.Groups[0].DDNSGroupID = "example.com", g.ID
	c.Routes = []Route{testRoute("nas.example.com", "http://localhost:7777")}
	w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 1, "dns_tokens": map[string]string{g.ID: "TEST_ONLY_BOUND_DNS_TOKEN"}}, cookie, "")
	if w.Code != 200 {
		t.Fatal("bound route save failed")
	}
	state := a.store.Snapshot()
	if !slices.Equal(state.Config.DDNS.Groups[0].Hosts, []string{"manual.example.com", "nas.example.com"}) || state.DNSCredentials[g.ID].Token != "TEST_ONLY_BOUND_DNS_TOKEN" {
		t.Fatal("binding changed manual hosts or credentials, or did not add route")
	}
	if !slices.Equal(c.DDNS.Groups[0].Hosts, g.Hosts) {
		t.Fatal("binding mutated the submitted config")
	}
	// Re-saving the original payload must not duplicate the route domain.
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 2}, cookie, "")
	if w.Code != 200 || len(a.store.Snapshot().Config.DDNS.Groups[0].Hosts) != 2 {
		t.Fatal("duplicate bound domain")
	}
	c.Routes[0].Host = "photos.example.com"
	c.DDNS.Groups[0].Hosts = slices.Clone(state.Config.DDNS.Groups[0].Hosts)
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 3}, cookie, "")
	if w.Code != 200 {
		t.Fatal("editing bound domain failed")
	}
	state = a.store.Snapshot()
	if !slices.Equal(state.Config.DDNS.Groups[0].Hosts, []string{"manual.example.com", "nas.example.com", "photos.example.com"}) {
		t.Fatal("edit lost existing DDNS domains")
	}
	// Invalid zone changes and DNS conflicts must roll back the entire save.
	c.Routes[0].Host = "photos.other.example"
	w = adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": 4}, cookie, "")
	if w.Code != 400 || a.store.Snapshot().Revision != 4 {
		t.Fatal("foreign-zone route was applied")
	}
	c = state.Config
	c.DDNS.Groups = nil
	if Validate(c) == nil {
		t.Fatal("bound DDNS group could be removed")
	}
	c = state.Config
	conflict := g
	conflict.ID = "conflict"
	conflict.Hosts = []string{"photos.example.com"}
	c.DDNS.Groups = append(slices.Clone(c.DDNS.Groups), conflict)
	if a.store.Update(c, nil, 4) == nil || a.store.Snapshot().Revision != 4 {
		t.Fatal("duplicate DNS task applied")
	}
}

func TestProxyDNSBindingUsesBothAddressFamilies(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	g := testDDNSGroup()
	g.Enabled = true
	g.Hosts = []string{"example.com"}
	c.DDNS.Groups = []DDNSGroup{g}
	c.Groups[0].DomainSuffix, c.Groups[0].DDNSGroupID = "example.com", g.ID
	c.Routes = []Route{testRoute("nas.example.com", "http://localhost:5000")}
	token := "TEST_ONLY_BOUND_DNS_TOKEN"
	if err := store.UpdateDNS(c, map[string]*string{g.ID: &token}, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	writes := []DNSRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("bound group credential was not used")
		}
		if r.URL.Path == "/zones" {
			json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []map[string]string{{"id": "zone", "name": "example.com"}}})
			return
		}
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []any{}})
			return
		}
		var record cfRecord
		json.NewDecoder(r.Body).Decode(&record)
		writes = append(writes, DNSRecord{Host: record.Name, Type: record.Type})
		json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{}})
	}))
	defer server.Close()
	jobs := NewJobs(store, nil)
	jobs.provider = func(s State, g DDNSGroup, _ *Logs) (dnsProvider, error) {
		cf := NewCloudflare(s.DNSCredentials[g.ID].Token)
		cf.BaseURL = server.URL
		return cloudflareProvider{cf}, nil
	}
	jobs.discover = func(_ context.Context, _ DDNSGroup, kind string) (DetectedIP, error) {
		ip := "8.8.8.8"
		if kind == "AAAA" {
			ip = "2606:4700:4700::1111"
		}
		return DetectedIP{Address: netip.MustParseAddr(ip)}, nil
	}
	state := store.Snapshot()
	if _, err := jobs.syncDNS(context.Background(), state, state.Config.DDNS.Groups[0]); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"A", "AAAA"} {
		if !slices.Contains(writes, DNSRecord{Host: "nas.example.com", Type: kind}) {
			t.Fatal("proxy domain was not synchronized")
		}
	}
}
