package gateway

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestIPMonitorConfigPersistsAndProvidesNetworkDefault(t *testing.T) {
	a, _ := testAdmin(t)
	s := a.store.Snapshot()
	monitor := IPMonitorConfig{Interface: "TEST_ONLY_MISSING_IFACE", IPv4Source: "interface", IPv6Source: "interface"}
	s.Config.DDNS.IPMonitor = monitor
	h := a.Handler()
	cookie := loginForTest(t, h)
	w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": s.Config, "revision": s.Revision}, cookie, "")
	if w.Code != 200 {
		t.Fatalf("monitor configuration rejected: %d", w.Code)
	}
	reopened, err := OpenStore(filepath.Dir(a.store.path))
	if err != nil || reopened.Snapshot().Config.DDNS.IPMonitor != monitor {
		t.Fatal("monitor configuration lost after reload")
	}
	w = adminRequest(h, "GET", "/api/ddns/network", nil, cookie, "")
	var result struct {
		Interface  string `json:"interface"`
		IPv4Source string `json:"ipv4_source"`
		IPv6Source string `json:"ipv6_source"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Interface != monitor.Interface || result.IPv4Source != "interface" || result.IPv6Source != "interface" {
		t.Fatal("network endpoint ignored the saved default")
	}
	w = adminRequest(h, "GET", "/api/ddns/network?group_id=&interface=TEST_ONLY_OVERRIDE&ipv4_source=interface&ipv6_source=interface", nil, cookie, "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Interface != "TEST_ONLY_OVERRIDE" {
		t.Fatal("explicit preview did not override saved defaults")
	}
	snapshot, err := snapshotDiskPaths(a.store.paths)
	var restored State
	if err != nil || json.Unmarshal(snapshot.State, &restored) != nil || restored.Config.DDNS.IPMonitor != monitor {
		t.Fatal("saved detection settings omitted from backups")
	}
}

func TestIPMonitorConfigurationValidationAndGroupRemoval(t *testing.T) {
	c := DefaultConfig()
	for _, monitor := range []IPMonitorConfig{{IPv4Source: "invalid"}, {IPv6Source: "interface"}, {Interface: "bad/interface"}, {GroupID: "missing"}} {
		c.DDNS.IPMonitor = monitor
		if Validate(c) == nil {
			t.Fatal("invalid saved detection configuration accepted")
		}
	}
	c.DDNS.Groups = []DDNSGroup{{ID: "test-dns", Name: "Test", Provider: "cloudflare", Zone: "example.test", Hosts: []string{"nas.example.test"}, Interval: 300, Mode: "dual", Interface: "TEST_ONLY_MISSING_IFACE", IPv4Source: "interface", IPv6Source: "interface"}}
	c.DDNS.IPMonitor = IPMonitorConfig{GroupID: "test-dns"}
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	c.DDNS.Groups = []DDNSGroup{}
	if Validate(c) == nil {
		t.Fatal("deleted detection group reference accepted")
	}
	c.DDNS.IPMonitor = IPMonitorConfig{}
	if err := Validate(c); err != nil {
		t.Fatal("clearing a deleted group did not restore the default detector")
	}
}

func TestExplicitIPPreviewDoesNotInheritSavedGroup(t *testing.T) {
	a, _ := testAdmin(t)
	state := a.store.Snapshot()
	group := testDDNSGroup()
	group.Mode = "ipv4"
	group.Interface = "TEST_ONLY_SAVED_IFACE"
	group.IPv4Source, group.IPv6Source = "interface", "interface"
	state.Config.DDNS.Groups = []DDNSGroup{group}
	state.Config.DDNS.IPMonitor = IPMonitorConfig{GroupID: group.ID}
	if err := a.store.Update(state.Config, nil, state.Revision); err != nil {
		t.Fatal(err)
	}
	h := a.Handler()
	cookie := loginForTest(t, h)
	w := adminRequest(h, "POST", "/api/ddns/network", map[string]string{
		"interface": "TEST_ONLY_PREVIEW_IFACE", "ipv4_source": "interface", "ipv6_source": "interface",
	}, cookie, "")
	var result struct {
		Interface string `json:"interface"`
		Mode      string `json:"mode"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil ||
		result.Interface != "TEST_ONLY_PREVIEW_IFACE" || result.Mode != "dual" {
		t.Fatal("explicit preview inherited the persisted group's interface or mode")
	}
	if a.store.Snapshot().Config.DDNS.IPMonitor.GroupID != group.ID {
		t.Fatal("temporary preview overwrote the persisted selection")
	}
}
