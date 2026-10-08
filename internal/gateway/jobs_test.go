package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"testing"
)

func testDDNSGroup() DDNSGroup {
	return DDNSGroup{Provider: "cloudflare", Zone: "example.com", ID: "home", Name: "家庭 DDNS", Interval: 300, Mode: "dual", Hosts: []string{"nas.example.com"}, IPv4URL: "https://api4.ipify.org", IPv6URL: "https://api6.ipify.org"}
}

func TestDDNSGroupsExpandValidateAndMigrate(t *testing.T) {
	c := DefaultConfig()
	c.Zone = "example.com"
	g := testDDNSGroup()
	g.Enabled = true
	c.DDNS.Groups = []DDNSGroup{g}
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	if len(g.Records()) != 2 || !c.DDNSEnabled() {
		t.Fatal("dual-stack group did not expand")
	}
	other := g
	other.ID = "other"
	other.Mode = "ipv4"
	c.DDNS.Groups = append(c.DDNS.Groups, other)
	if Validate(c) == nil {
		t.Fatal("conflicting groups accepted")
	}
	c.DDNS.Groups[0].Mode = "ipv6"
	if err := Validate(c); err != nil {
		t.Fatal("separate IPv4 and IPv6 groups rejected")
	}
	for _, mode := range []string{"ipv4", "ipv6", "dual"} {
		g.Mode = mode
		if got := len(g.Records()); got != map[string]int{"ipv4": 1, "ipv6": 1, "dual": 2}[mode] {
			t.Fatal("wrong record expansion")
		}
	}
	dir := t.TempDir()
	c = DefaultConfig()
	c.Zone = "example.com"
	c.DDNS = DDNSConfig{Enabled: true, Interval: 600, IPv4URL: "https://api4.ipify.org", IPv6URL: "https://api6.ipify.org", Records: []DNSRecord{{Host: "nas.example.com", Type: "A"}, {Host: "nas.example.com", Type: "AAAA"}, {Host: "photos.example.com", Type: "A"}}}
	if err := writeJSON(filepath.Join(dir, "state.json"), State{Config: c, CloudflareToken: "TEST_ONLY_TOKEN", Revision: 4}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		store, err := OpenStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		state := store.Snapshot()
		if state.Revision != 5 || len(state.Config.DDNS.Groups) != 2 || len(state.Config.DDNS.Groups[1].Records()) != 2 || state.Config.DDNS.Groups[0].Interval != 600 || !state.Config.DDNSEnabled() {
			t.Fatal("old records or interval did not migrate once")
		}
	}
}

func TestDDNSPartialFailureDoesNotStopOtherFamily(t *testing.T) {
	writes := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		writes = append(writes, record.Type+"/"+record.Name)
		json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{}})
	}))
	defer server.Close()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logs, err := NewLogs(t.TempDir(), store)
	if err != nil {
		t.Fatal(err)
	}
	j := NewJobs(store, nil)
	j.SetLogs(logs)
	j.provider = func(s State, g DDNSGroup, logs *Logs) (dnsProvider, error) {
		cf := NewCloudflare("TEST_ONLY_TOKEN")
		cf.BaseURL = server.URL
		cf.logs = logs
		return cloudflareProvider{cf}, nil
	}
	calls := map[string]int{}
	j.discover = func(ctx context.Context, g DDNSGroup, kind string) (DetectedIP, error) {
		calls[kind]++
		if kind == "AAAA" {
			return DetectedIP{}, errors.New("测试 IPv6 不可用")
		}
		return DetectedIP{Address: netip.MustParseAddr("8.8.8.8"), Interface: "test0"}, nil
	}
	g := testDDNSGroup()
	g.Enabled = true
	g.Hosts = append(g.Hosts, "photos.example.com")
	c := DefaultConfig()
	c.Zone = "example.com"
	c.DDNS.Groups = []DDNSGroup{g}
	s := State{Config: c}
	j.runDDNS(context.Background(), s, g)
	j.ddnsSummary(c)
	if len(writes) != 2 || calls["A"] != 1 || calls["AAAA"] != 1 {
		t.Fatal("partial failure blocked IPv4 or repeated address discovery")
	}
	status, _ := j.Snapshot()
	if status["ddns:home"].Running || !status["ddns:home"].LastSuccess.IsZero() || status["ddns"].LastSuccess.After(status["ddns"].LastRun) {
		t.Fatal("partial failure reported success")
	}
	rows, _, _ := logs.List("ddns", "error", 0, 50)
	if len(rows) < 3 {
		t.Fatal("per-record and group failures were not logged")
	}
}
