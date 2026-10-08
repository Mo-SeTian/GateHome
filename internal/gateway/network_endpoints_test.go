package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPublicIPResponseFormats(t *testing.T) {
	for _, test := range []struct{ body, kind, want string }{
		{"8.8.8.8\n", "A", "8.8.8.8"},
		{"<html><body>Current IP Address: 8.8.8.8</body></html>", "A", "8.8.8.8"},
		{"当前 IP：8.8.8.8 来自于：中国", "A", "8.8.8.8"},
		{`{"ip":"8.8.8.8"}`, "A", "8.8.8.8"},
		{`{"ip":"2606:4700:4700::1111"}`, "AAAA", "2606:4700:4700::1111"},
		{"IP:2606:4700:4700::1111", "AAAA", "2606:4700:4700::1111"},
		{"2606:4700::", "AAAA", "2606:4700::"},
		{"8.8.8.8 8.8.8.8", "A", "8.8.8.8"},
		{"8.8.8.8 1.1.1.1", "A", ""},
		{"192.168.1.2", "A", ""},
		{"100.64.1.2", "A", ""},
		{"8.8.8.8", "AAAA", ""},
		{"fe80::1", "AAAA", ""},
		{"fd00::1", "AAAA", ""},
		{"请求失败", "A", ""},
	} {
		ip, err := parsePublicIPResponse([]byte(test.body), test.kind)
		if test.want == "" {
			if err == nil {
				t.Fatal("invalid or ambiguous IP response accepted")
			}
		} else if err != nil || ip.String() != test.want {
			t.Fatal("supported IP response format rejected", test.kind)
		}
	}
}

func TestIPEndpointsFailOverInOrderAndStopAfterSuccess(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/unavailable":
			w.WriteHeader(503)
		case "/private":
			io.WriteString(w, `{"ip":"192.168.1.2"}`)
		case "/good":
			io.WriteString(w, "<body>Current IP Address: 8.8.8.8</body>")
		default:
			t.Error("a later endpoint was called after success")
		}
	}))
	defer server.Close()
	urls := []string{server.URL + "/unavailable", server.URL + "/private", server.URL + "/good?key=TEST_ONLY_QUERY", server.URL + "/unused"}
	sample, err := queryPublicIPs(context.Background(), urls, "A", nil)
	if err != nil || sample.Address.String() != "8.8.8.8" || sample.Endpoint != server.URL+"/good" || sample.Attempts != 3 || sample.Interface == "" {
		t.Fatal("ordered fallback or source metadata failed")
	}
	if !reflect.DeepEqual(paths, []string{"/unavailable", "/private", "/good"}) {
		t.Fatal("endpoints were not tried in configured order")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := queryPublicIPs(ctx, urls, "A", nil); err == nil || len(paths) != 3 {
		t.Fatal("canceled detection continued querying endpoints")
	}
}

func TestIPResponseSizeLimitDoesNotAcceptTruncatedData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "8.8.8.8"+strings.Repeat(" ", 64<<10)) }))
	defer server.Close()
	if _, err := queryPublicIP(context.Background(), server.URL, "A", nil); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestIPEndpointDefaultsLegacyCompatibilityAndValidation(t *testing.T) {
	g := testDDNSGroup()
	if !reflect.DeepEqual(ipEndpoints(g, "A"), []string{g.IPv4URL}) {
		t.Fatal("legacy endpoint changed")
	}
	g.IPv4URL, g.IPv6URL = "", ""
	if len(ipEndpoints(g, "A")) != 6 || len(ipEndpoints(g, "AAAA")) != 4 || ipEndpoints(g, "A")[0] != "https://ddns.oray.com/checkip" {
		t.Fatal("requested defaults missing")
	}
	c := DefaultConfig()
	c.DDNS.Groups = []DDNSGroup{g}
	if err := Validate(c); err != nil {
		t.Fatal("default HTTP/HTTPS interface list rejected", err)
	}
	g.IPv4URLs = []string{"http://example.test/ip", "https://example.test/another"}
	if err := validateIPEndpoints(g); err != nil {
		t.Fatal("custom HTTP/HTTPS list rejected")
	}
	for _, urls := range [][]string{{}, {"file:///tmp/ip"}, {"https://user:TEST_ONLY@example.test/ip"}, {"https://example.test/ip#part"}, {"https://example.test/ip", "https://example.test/ip"}, make([]string, 21)} {
		g.IPv4URLs = urls
		if validateIPEndpoints(g) == nil {
			t.Fatal("invalid interface list accepted")
		}
	}
}

func TestIPCacheInvalidatesWhenEndpointOrderChanges(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	j := NewJobs(store, nil)
	var calls atomic.Int32
	j.discover = func(ctx context.Context, g DDNSGroup, kind string) (DetectedIP, error) {
		calls.Add(1)
		return DetectedIP{Address: netip.MustParseAddr("8.8.8.8"), Endpoint: ipEndpoints(g, kind)[0]}, nil
	}
	g := testDDNSGroup()
	g.Mode = "ipv4"
	g.IPv4URLs = []string{"https://one.example.test/ip", "https://two.example.test/ip"}
	j.IPStatus("group:"+g.ID, g, false)
	first := waitIPCheck(t, j, "group:"+g.ID, g)
	g.IPv4URLs = []string{g.IPv4URLs[1], g.IPv4URLs[0]}
	j.IPStatus("group:"+g.ID, g, false)
	second := waitIPCheck(t, j, "group:"+g.ID, g)
	if calls.Load() != 2 || first.IPv4.Endpoint == second.IPv4.Endpoint {
		t.Fatal("changed endpoint order reused stale source result")
	}
}

func TestNetworkAPIReturnsIPForEveryDDNSGroupWithoutDNSWrites(t *testing.T) {
	a, h := testAdmin(t)
	c := a.store.Snapshot().Config
	first := testDDNSGroup()
	second := first
	second.ID = "second"
	second.Name = "另一个组"
	second.Hosts = []string{"photos.example.com"}
	c.DDNS.Groups = []DDNSGroup{first, second}
	if err := a.store.UpdateDNS(c, nil, nil, nil, a.store.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	var dnsCalls atomic.Int32
	a.jobs.provider = func(State, DDNSGroup, *Logs) (dnsProvider, error) {
		dnsCalls.Add(1)
		t.Error("preview must not call DNS provider")
		return nil, nil
	}
	a.jobs.discover = func(_ context.Context, g DDNSGroup, kind string) (DetectedIP, error) {
		ip := "8.8.8.8"
		if g.ID == second.ID {
			ip = "1.1.1.1"
		}
		if kind == "AAAA" {
			ip = "2606:4700:4700::1111"
		}
		return DetectedIP{Address: netip.MustParseAddr(ip)}, nil
	}
	cookie := loginForTest(t, h)
	revision := a.store.Snapshot().Revision
	w := adminRequest(h, "GET", "/api/ddns/network", nil, cookie, "")
	if w.Code != 200 {
		t.Fatal("group IP API failed")
	}
	for _, g := range c.DDNS.Groups {
		waitIPCheck(t, a.jobs, "group:"+g.ID, g)
	}
	w = adminRequest(h, "GET", "/api/ddns/network", nil, cookie, "")
	var response struct {
		Groups map[string]IPStatus `json:"groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Groups) != 2 || response.Groups[first.ID].IPv4.Address.String() != "8.8.8.8" || response.Groups[second.ID].IPv4.Address.String() != "1.1.1.1" || !response.Groups[first.ID].IPv6.Address.Is6() || dnsCalls.Load() != 0 || a.store.Snapshot().Revision != revision {
		t.Fatal("group IP cards missing results or preview changed DNS/configuration")
	}
}
