package gateway

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestIPRegionClassificationAndConcurrentDatabases(t *testing.T) {
	for ip, want := range map[string]string{
		"127.0.0.1": "本机回环", "::1": "本机回环", "::ffff:192.168.1.1": "内网地址",
		"10.0.0.1": "内网地址", "fd00::1": "内网地址", "fe80::1": "链路本地",
		"192.0.2.1": "保留地址", "2001:db8::1": "保留地址", "100.64.0.1": "保留地址",
		"0.0.0.0": "保留地址", "224.0.0.1": "链路本地", "invalid": "归属地未知",
	} {
		if got := ipRegion(ip); got != want {
			t.Fatalf("classification %s: got %q want %q", ip, got, want)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Go(func() {
			for _, ip := range []string{"8.8.8.8", "2001:4860:4860::8888"} {
				if got := ipRegion(ip); got == "归属地未知" || got == "保留地址" || strings.Contains(got, "|0|") {
					t.Errorf("public database lookup failed: %s", ip)
				}
			}
		})
	}
	wg.Wait()
	if ipRegion("::ffff:8.8.8.8") != ipRegion("8.8.8.8") {
		t.Fatal("IPv4 mapped address queried the wrong database")
	}
	for _, item := range []struct {
		data []byte
		sha  string
	}{{regionIPv4.data, "f5ccf366678c91394ac5e469f93b624d5df290e7353c748b920c6b84ffc4e551"}, {regionIPv6.data, "939f6b46bd2b8bec3cf7c5ceb8ba782266ae9b1f35b5ba7916700dec0b7506ed"}} {
		if fmt.Sprintf("%x", sha256.Sum256(item.data)) != item.sha {
			t.Fatal("embedded region database differs from pinned source")
		}
	}
}

func TestRegionEnrichmentDoesNotMutateSavedLogs(t *testing.T) {
	l := &Logs{entries: []LogEntry{{ID: 1, Category: "access", Remote: "192.168.1.1"}}, lastID: 1}
	page := l.Page(LogFilter{Scope: "access"}, 1, 20, 0)
	enrichLogRegions(page.Entries)
	if page.Entries[0].RemoteRegion != "内网地址" || l.entries[0].RemoteRegion != "" {
		t.Fatal("response enrichment changed saved entries")
	}
}
