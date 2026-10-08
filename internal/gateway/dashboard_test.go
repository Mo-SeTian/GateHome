package gateway

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDashboardSettingsPersistAndBackup(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	c := a.store.Snapshot().Config
	c.Dashboard = DashboardConfig{Widgets: []string{"map", "traffic", "cpu"}, NetworkInterface: "eth0", MapProvince: "江苏"}
	w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": c, "revision": a.store.Snapshot().Revision}, cookie, "")
	if w.Code != 200 {
		t.Fatal("valid dashboard settings rejected")
	}
	reopened, err := OpenStorePaths(a.store.paths)
	if err != nil || !reflect.DeepEqual(reopened.Snapshot().Config.Dashboard, c.Dashboard) {
		t.Fatal("dashboard settings were not persisted")
	}
	payload := backupPayload{State: a.store.Snapshot(), Certificates: map[string][]byte{}}
	data, err := encodeBackup(payload, "TEST_ONLY_BACKUP")
	if err != nil {
		t.Fatal(err)
	}
	restored, _, err := decodeBackup(data, "TEST_ONLY_BACKUP")
	if err != nil || !reflect.DeepEqual(restored.State.Config.Dashboard, c.Dashboard) {
		t.Fatal("dashboard missing from backup")
	}
	for _, bad := range []DashboardConfig{{Widgets: []string{"unknown"}}, {Widgets: []string{"cpu", "cpu"}}, {NetworkInterface: "../private"}, {MapProvince: "invalid"}} {
		if validateDashboard(bad) == nil {
			t.Fatal("invalid dashboard settings accepted")
		}
	}
	if validateDashboard(DashboardConfig{}) != nil || validateDashboard(DashboardConfig{Widgets: []string{}}) != nil {
		t.Fatal("legacy or intentionally empty layout rejected")
	}
	if w := adminRequest(h, http.MethodGet, "/api/dashboard", nil, cookie, ""); w.Code != 200 || strings.Contains(w.Body.String(), "TEST_ONLY_FAKE_TOKEN") || strings.Contains(w.Body.String(), "password_hash") {
		t.Fatal("dashboard failed or exposed credentials")
	}
}

func TestDashboardRegionsUseRetainedRecordsAndDistinctIPs(t *testing.T) {
	now := time.Now()
	entries := []LogEntry{
		{ID: 1, Category: "access", Remote: "TEST_OLD", Time: now.Add(-25 * time.Hour)},
		{ID: 2, Category: "admin", Remote: "TEST_ADMIN", Time: now},
		{ID: 3, Category: "access", Remote: "192.0.2.1", Time: now.Add(-time.Second)},
		{ID: 4, Category: "access", Remote: "192.0.2.1", Time: now},
		{ID: 5, Category: "access", Remote: "192.0.2.2", Time: now, Outcome: "blocked"},
		{ID: 6, Category: "access", Remote: "127.0.0.1", Time: now},
	}
	calls := 0
	result := summarizeDashboardRegions(entries, now, func(ip string) string {
		calls++
		if ip == "127.0.0.1" {
			return "本机回环"
		}
		return "中国 · 江苏省 · 南京市"
	})
	if result.Sample != 4 || result.UniqueIPs != 3 || result.Unmapped != 1 || calls != 3 || len(result.Visits) != 4 {
		t.Fatal("region window, cache or unique IP counting incorrect")
	}
	if result.Provinces[0].Name != "江苏" || result.Provinces[0].Requests != 3 || result.Provinces[0].IPs != 2 || !result.Visits[1].Blocked {
		t.Fatal("province counts or blocked map visit incorrect")
	}
	for _, region := range []string{"中国 · 内蒙古自治区", "中国 · 广西壮族自治区", "中国 · 宁夏回族自治区", "中国 · 新疆维吾尔自治区", "中国 · 香港特别行政区"} {
		if provinceFromRegion(region) == "" {
			t.Fatal("autonomous region not recognized")
		}
	}
	if provinceFromRegion("日本 · 东京") != "" {
		t.Fatal("foreign address was plotted in China")
	}
}

func TestSystemTelemetryParsers(t *testing.T) {
	total, idle, ok := parseCPU("cpu 10 20 30 40 50 60 70 80 90 100\ncpu0 1 2 3 4")
	if !ok || total != 360 || idle != 90 {
		t.Fatal("CPU guest counters double-counted or idle misread")
	}
	for _, bad := range []string{"", "cpu invalid 0 1 2", "cpu0 1 2 3 4"} {
		if _, _, ok := parseCPU(bad); ok {
			t.Fatal("invalid CPU sample accepted")
		}
	}
	total, used, ok := parseMemory("MemTotal: 2048 kB\nMemAvailable: 512 kB\n")
	if !ok || total != 2048*1024 || used != 1536*1024 {
		t.Fatal("cached memory counted incorrectly")
	}
	_, used, ok = parseMemory("MemTotal: 2048 kB\nMemFree: 256 kB\nBuffers: 256 kB\nCached: 512 kB\n")
	if !ok || used != 1024*1024 {
		t.Fatal("old Linux memory fallback incorrect")
	}
	rows := parseNetworks("lo: 1 0 0 0 0 0 0 0 2 0 0 0 0 0 0 0\neth0: 123 0 0 0 0 0 0 0 456 0 0 0 0 0 0 0\ninvalid: nan\n")
	if len(rows) != 1 || rows[0].Name != "eth0" || rows[0].RX != 123 || rows[0].TX != 456 {
		t.Fatal("network byte columns or loopback exclusion incorrect")
	}
}
