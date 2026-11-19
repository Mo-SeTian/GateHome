package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTrafficCountsForwardedBodiesOnly(t *testing.T) {
	const upload = "streamed upload"
	const download = "upstream response"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != upload || len(r.TransferEncoding) == 0 {
			t.Error("chunked request did not arrive intact")
		}
		w.WriteHeader(http.StatusForbidden) // Upstream errors still transfer data.
		_, _ = io.WriteString(w, download)
	}))
	defer upstream.Close()
	c := DefaultConfig()
	c.Firewalls = []Firewall{{ID: "deny", Name: "deny", DefaultAction: "deny"}}
	route := testRoute("nas.example.com", upstream.URL)
	route.ID = "nas"
	c.Routes = []Route{route}
	p := NewProxy(c, nil)
	m, err := OpenTraffic(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p.SetTraffic(m)
	proxy := httptest.NewServer(p)
	defer proxy.Close()
	send := func() int {
		t.Helper()
		req, err := http.NewRequest("POST", proxy.URL, io.NopCloser(strings.NewReader(upload)))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = route.Host
		res, err := proxy.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		return res.StatusCode
	}
	if status := send(); status != http.StatusForbidden {
		t.Fatalf("upstream response lost: %d", status)
	}
	c.Routes[0].FirewallID = "deny"
	if err := p.Configure(c); err != nil || send() != http.StatusForbidden {
		t.Fatal("expected a locally blocked request")
	}
	upstream.Close()
	// Use a bodyless request to check that a locally generated 502 adds no download.
	if err := p.Configure(Config{Groups: c.Groups, Routes: []Route{route}}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest("GET", "http://"+route.Host, nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected a locally generated 502, got %d", w.Code)
	}
	now := time.Now().UTC()
	report := m.report(now.Truncate(time.Hour).Add(-time.Hour), now, route.ID)
	if report.Upload != uint64(len(upload)) || report.Download != uint64(len(download)) {
		t.Fatalf("unexpected transfer counts: %+v", report.trafficBytes)
	}
}

func TestTrafficPersistsAndFiltersByRuleAndHour(t *testing.T) {
	dir := t.TempDir()
	m, err := OpenTraffic(dir)
	if err != nil {
		t.Fatal(err)
	}
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	first, second := &Route{ID: "one", Host: "nas.example.com"}, &Route{ID: "two", Host: "nas.example.com"}
	m.add(first, 11, 24, hour.Add(-time.Second))
	m.add(first, 14, 31, hour)
	m.add(first, 21, 39, hour.Add(time.Hour))
	m.add(second, 10, 60, hour.Add(59*time.Minute))
	expired := hour.Add(-32 * 24 * time.Hour).Truncate(24 * time.Hour)
	if err := os.WriteFile(m.path(expired), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Flush(); err != nil {
		t.Fatal(err)
	}
	m, err = OpenTraffic(dir)
	if err != nil {
		t.Fatal(err)
	}
	all := m.report(hour, hour.Add(time.Hour), "")
	if all.StorageError || all.Upload != 24 || all.Download != 91 || len(all.Rules) != 2 || all.Rules[0].ID != "two" {
		t.Fatalf("aggregate did not survive restart or split same-domain rules: %+v", all)
	}
	filtered := m.report(hour, hour.Add(time.Hour), "one")
	if filtered.Upload != 14 || filtered.Download != 31 || len(filtered.Rules) != 1 {
		t.Fatalf("time or rule filter leaked other traffic: %+v", filtered)
	}
	if err := m.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.path(expired)); !os.IsNotExist(err) {
		t.Fatal("expired traffic data was retained")
	}
}

func TestForwardTrafficKeepsWriteHourAcrossSamples(t *testing.T) {
	history, err := OpenTraffic(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	boundary := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	b := &forwardBinding{rule: PortForward{ID: "hourly", Name: "Hourly", Protocol: "tcp"}}
	p := &PortForwarder{
		traffic: history, bindings: []*forwardBinding{b}, sampledAt: boundary.Add(-3 * time.Second),
	}
	b.traffic.record(boundary.Add(-time.Second), trafficBytes{Upload: 500, Download: 100})
	b.traffic.record(boundary, trafficBytes{Upload: 250, Download: 50})
	p.sampleTraffic(boundary.Add(2 * time.Second))

	for _, tc := range []struct {
		from, to time.Time
		want     trafficBytes
	}{
		{boundary.Add(-time.Hour), boundary, trafficBytes{Upload: 500, Download: 100}},
		{boundary, boundary.Add(time.Hour), trafficBytes{Upload: 250, Download: 50}},
	} {
		report := history.forwardReport(tc.from, tc.to, "hourly", "tcp")
		if report.trafficBytes != tc.want || len(report.Rules) != 1 {
			t.Fatalf("unexpected history for %v: %+v", tc.from, report)
		}
	}
	status := p.trafficStatus()[0]
	if status.UploadBPS != 150 || status.DownloadBPS != 30 {
		t.Fatalf("the live sample lost bytes across the boundary: %+v", status)
	}
	p.sampleTraffic(boundary.Add(7 * time.Second))
	report := history.forwardReport(boundary.Add(-time.Hour), boundary.Add(time.Hour), "hourly", "tcp")
	if report.Upload != 750 || report.Download != 150 || p.trafficStatus()[0].UploadBPS != 0 {
		t.Fatalf("second sample repeated counted traffic: %+v", report)
	}
}

func TestTrafficAPIRequiresAuthAndValidRange(t *testing.T) {
	a, h := testAdmin(t)
	m, err := OpenTraffic(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.proxy.SetTraffic(m)
	const path = "/api/statistics/traffic"
	if w := adminRequest(h, "GET", path, nil, nil, ""); w.Code != 401 {
		t.Fatal("unauthenticated traffic data disclosed")
	}
	cookie := loginForTest(t, h)
	for _, query := range []string{"hours=0", "hours=721", "from=invalid", "rule=some%2Fhost", "from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z"} {
		if w := adminRequest(h, "GET", path+"?"+query, nil, cookie, ""); w.Code != 400 {
			t.Fatalf("invalid range or rule accepted: %s", query)
		}
	}
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	m.add(&Route{ID: "nas", Host: "nas.example.com"}, 14, 31, hour.Add(5*time.Minute))
	q := url.Values{"rule": {"nas"}, "from": {hour.Add(time.Minute).Format(time.RFC3339)}, "to": {hour.Add(30 * time.Minute).Format(time.RFC3339)}}
	w := adminRequest(h, "GET", path+"?"+q.Encode(), nil, cookie, "")
	var data trafficReport
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &data) != nil || data.Rule != "nas" || data.Upload != 14 || data.Download != 31 || !data.From.Equal(hour) || !data.To.Equal(hour.Add(time.Hour)) {
		t.Fatalf("filtered traffic response incorrect: HTTP %d, %+v", w.Code, data)
	}
}
