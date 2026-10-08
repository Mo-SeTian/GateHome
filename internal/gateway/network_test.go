package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestIPSourceValidationAndAddressSelection(t *testing.T) {
	g := testDDNSGroup()
	if err := validateIPSources(g); err != nil || ipSource(g, "A") != "url" || ipSource(g, "AAAA") != "url" {
		t.Fatal("legacy source defaults changed")
	}
	g.IPv6Source = "interface"
	if validateIPSources(g) == nil {
		t.Fatal("direct source without a selected interface accepted")
	}
	g.Interface = "eth0"
	if validateIPSources(g) != nil {
		t.Fatal("mixed IPv4 query and IPv6 interface sources rejected")
	}
	g.IPv4Source = "invalid"
	if validateIPSources(g) == nil {
		t.Fatal("unknown source accepted")
	}
	addresses := []netip.Addr{netip.MustParseAddr("192.168.1.2"), netip.MustParseAddr("fe80::1"), netip.MustParseAddr("fd00::1"), netip.MustParseAddr("2606:4700:4700::1111")}
	if _, err := selectInterfaceIP(addresses, "A", true); err == nil {
		t.Fatal("private IPv4 selected for DDNS")
	}
	if ip, err := selectInterfaceIP(addresses, "A", false); err != nil || ip.String() != "192.168.1.2" {
		t.Fatal("NAT source address rejected for an outbound query")
	}
	if ip, err := selectInterfaceIP(addresses, "AAAA", true); err != nil || ip.String() != "2606:4700:4700::1111" {
		t.Fatal("public IPv6 was not selected")
	}
}

func TestQueryReportsActualLocalInterfaceAndMissingInterfaceFails(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		io.WriteString(w, "8.8.8.8")
	}))
	defer server.Close()
	sample, err := queryPublicIP(context.Background(), server.URL, "A", nil)
	if err != nil || sample.Address.String() != "8.8.8.8" || sample.Interface == "" || sample.LocalAddress == "" {
		t.Fatal("actual connection source was not reported")
	}
	g := testDDNSGroup()
	g.Interface, g.IPv4URL = "gatehouse-missing-test", server.URL
	if _, err := detectIP(context.Background(), g, "A"); err == nil || requests.Load() != 1 {
		t.Fatal("missing interface silently fell back to automatic routing")
	}
}

func waitIPCheck(t *testing.T, jobs *Jobs, key string, g DDNSGroup) IPStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := jobs.IPStatus(key, g, false)
		if !status.Running {
			return status
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("IP check did not finish")
	return IPStatus{}
}

func TestIPPollingCacheSwitchAndStaleResults(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	j := NewJobs(store, nil)
	var calls atomic.Int32
	j.discover = func(ctx context.Context, g DDNSGroup, kind string) (DetectedIP, error) {
		calls.Add(1)
		ip := "8.8.8.8"
		if g.Interface == "new0" {
			ip = "8.8.4.4"
		}
		return DetectedIP{Address: netip.MustParseAddr(ip), Interface: g.Interface}, nil
	}
	g := testDDNSGroup()
	g.Mode, g.Interface = "ipv4", "old0"
	j.IPStatus("preview", g, false)
	first := waitIPCheck(t, j, "preview", g)
	for i := 0; i < 20; i++ {
		j.IPStatus("preview", g, true)
	}
	if calls.Load() != 1 || !first.IPv4.Address.IsValid() {
		t.Fatal("polling or repeated refresh bypassed the detection cooldown")
	}
	j.mu.Lock()
	oldEntry := j.ips["preview"]
	j.mu.Unlock()
	oldGroup := g
	g.Interface = "new0"
	j.IPStatus("preview", g, false)
	second := waitIPCheck(t, j, "preview", g)
	if calls.Load() != 2 || second.IPv4.Address.String() != "8.8.4.4" || second.IPv4.Interface != "new0" {
		t.Fatal("source switch reused the old interface result")
	}
	j.checkIPs(context.Background(), "preview", oldGroup, oldEntry)
	if current := j.IPStatus("preview", g, false); current.IPv4.Address != second.IPv4.Address || current.Running {
		t.Fatal("late results from the old source overwrote the selected source")
	}
}

func TestIPFamiliesUpdateIndependentlyAndFailureKeepsLastValidAddress(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	j := NewJobs(store, nil)
	release := make(chan struct{})
	j.discover = func(ctx context.Context, g DDNSGroup, kind string) (DetectedIP, error) {
		if kind == "AAAA" {
			<-release
			return DetectedIP{}, errors.New("测试 IPv6 不可用")
		}
		return DetectedIP{Address: netip.MustParseAddr("8.8.8.8"), Interface: "test0"}, nil
	}
	g := testDDNSGroup()
	j.IPStatus("preview", g, false)
	deadline := time.Now().Add(2 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		status := j.IPStatus("preview", g, false)
		if status.IPv4.Address.IsValid() && status.Running {
			ready = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	if !ready {
		t.Fatal("pending IPv6 prevented IPv4 from appearing")
	}
	status := waitIPCheck(t, j, "preview", g)
	if !status.IPv4.Address.IsValid() || status.IPv6.Error == "" {
		t.Fatal("family failure was hidden or discarded the other address")
	}
	failed := mergeDetectedIP(status.IPv4, DetectedIP{}, errors.New("测试查询失败"))
	if failed.Address != status.IPv4.Address || failed.LastSuccess != status.IPv4.LastSuccess || failed.Error == "" {
		t.Fatal("last valid IP was discarded or failure looked successful")
	}
}

func TestIPPreviewAPIIsAuthenticatedAndNeverWritesDNS(t *testing.T) {
	a, h := testAdmin(t)
	var dnsCalls, discoveries atomic.Int32
	a.jobs.provider = func(State, DDNSGroup, *Logs) (dnsProvider, error) {
		dnsCalls.Add(1)
		return nil, errors.New("DNS must not be called")
	}
	a.jobs.discover = func(ctx context.Context, g DDNSGroup, kind string) (DetectedIP, error) {
		discoveries.Add(1)
		ip := "8.8.8.8"
		if kind == "AAAA" {
			ip = "2606:4700:4700::1111"
		}
		return DetectedIP{Address: netip.MustParseAddr(ip), Interface: "test0"}, nil
	}
	if w := adminRequest(h, "GET", "/api/ddns/network", nil, nil, ""); w.Code != 401 {
		t.Fatal("unauthenticated IP access")
	}
	cookie := loginForTest(t, h)
	revision := a.store.Snapshot().Revision
	for _, method := range []string{"GET", "POST"} {
		if w := adminRequest(h, method, "/api/ddns/network", map[string]string{}, cookie, ""); w.Code != 200 {
			t.Fatal("IP preview API failed")
		}
	}
	waitIPCheck(t, a.jobs, "preview::url:url", DDNSGroup{Mode: "dual"})
	if dnsCalls.Load() != 0 || discoveries.Load() != 2 || a.store.Snapshot().Revision != revision {
		t.Fatal("IP preview changed DNS/configuration or repeatedly queried")
	}
	if w := adminRequest(h, "GET", "/api/ddns/network?group_id=missing", nil, cookie, ""); w.Code != 404 {
		t.Fatal("unknown group accepted")
	}
	if w := adminRequest(h, "POST", "/api/ddns/network", map[string]string{"ipv6_source": "interface"}, cookie, ""); w.Code != 400 {
		t.Fatal("direct preview without an interface accepted")
	}
	r := httptest.NewRequest("POST", "http://localhost:16666/api/ddns/network", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("IP refresh without CSRF header accepted")
	}
}

func TestIPSourceSettingsPersistAndBackUp(t *testing.T) {
	a, _ := testAdmin(t)
	c := DefaultConfig()
	g := testDDNSGroup()
	g.Interface, g.IPv4Source, g.IPv6Source = "eth0", "url", "interface"
	g.IPv4URLs = []string{"http://first.example.test/ip", "https://second.example.test/ip"}
	c.DDNS.Groups = []DDNSGroup{g}
	if err := a.store.UpdateDNS(c, nil, nil, nil, a.store.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(filepath.Dir(a.store.path))
	if err != nil || reopened.Snapshot().Config.DDNS.Groups[0].Interface != g.Interface {
		t.Fatal("network source settings were not persisted")
	}
	data, err := encodeBackup(backupPayload{State: reopened.Snapshot(), Certificates: map[string][]byte{}}, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	restored, _, err := decodeBackup(data, "TEST_ONLY_BACKUP_PASSWORD")
	if err != nil || settingsForIP(restored.State.Config.DDNS.Groups[0]) != settingsForIP(g) {
		t.Fatal("backup lost the selected interface or family sources")
	}
}
