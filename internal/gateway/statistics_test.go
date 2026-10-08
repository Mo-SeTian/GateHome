package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStatisticsClassifiesFirewallSeparatelyFromUpstream403(t *testing.T) {
	now := time.Now()
	l := &Logs{entries: []LogEntry{
		{ID: 1, Time: now.Add(-time.Hour), Category: "access", Rule: "one/nas.example.com", Status: 200},
		{ID: 2, Time: now.Add(-time.Hour), Category: "access", Rule: "one/nas.example.com", Status: 403, Outcome: "blocked"},
		{ID: 3, Time: now.Add(-time.Hour), Category: "access", Rule: "one/nas.example.com", Status: 403, Outcome: "forwarded"},
		{ID: 4, Time: now.Add(-time.Hour), Category: "access", Rule: "two/nas.example.com", Status: 302},
		{ID: 5, Time: now.Add(-time.Hour), Category: "access", Rule: "one/nas.example.com", Status: 403, Message: "反代组：默认组；防火墙拒绝来源 IP"},
		{ID: 6, Time: now.Add(-25 * time.Hour), Category: "access", Status: 200},
		{ID: 7, Time: now.Add(time.Hour), Category: "access", Status: 200},
		{ID: 8, Time: now, Category: "admin", Status: 200},
	}}
	s := l.Statistics(24, "", now)
	if s.Total != 5 || s.Normal != 2 || s.Blocked != 2 || s.Failed != 1 || len(s.Trend) != 24 {
		t.Fatal("incorrect classification or time filtering")
	}
	if len(s.Rules) != 2 || s.Rules[0].Rule != "one/nas.example.com" || s.Statuses[3].Count != 3 {
		t.Fatal("incorrect rule or status aggregation")
	}
	filtered := l.Statistics(24, "one/nas.example.com", now)
	if filtered.Total != 4 || filtered.Normal != 1 || len(filtered.Rules) != 1 {
		t.Fatal("same-domain rules mixed")
	}
	weekly := l.Statistics(168, "", now)
	if weekly.Total != 6 || len(weekly.Trend) != 7 {
		t.Fatal("daily buckets incorrect")
	}
	total := 0
	for _, bucket := range s.Trend {
		total += bucket.Total
	}
	if total != s.Total {
		t.Fatal("trend dropped counts")
	}
}

func TestProxyLogsCaptureRealFirewallOutcome(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) }))
	defer upstream.Close()
	c := DefaultConfig()
	c.Firewalls = []Firewall{{ID: "deny", Name: "deny", DefaultAction: "deny", Groups: []FirewallGroup{}}}
	route := testRoute("nas.example.com", upstream.URL)
	c.Routes = []Route{route}
	p := NewProxy(c, nil)
	l, err := NewLogs(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p.SetLogs(l)
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://nas.example.com", nil))
	c.Routes[0].FirewallID = "deny"
	p.Configure(c)
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://nas.example.com", nil))
	s := l.Statistics(24, "", time.Now())
	if s.Total != 2 || s.Blocked != 1 || s.Failed != 1 {
		t.Fatal("upstream 403 counted as firewall block")
	}
}

func TestStatisticsAPIRequiresAuthAndValidRange(t *testing.T) {
	a, h := testAdmin(t)
	l, err := NewLogs(t.TempDir(), a.store)
	if err != nil {
		t.Fatal(err)
	}
	a.SetLogs(l)
	l.Add(LogEntry{Category: "access", Status: 200, Rule: "default/nas.example.com", OK: true})
	if w := adminRequest(h, "GET", "/api/statistics", nil, nil, ""); w.Code != 401 {
		t.Fatal("unauthenticated statistics disclosed")
	}
	cookie := loginForTest(t, h)
	for _, query := range []string{"hours=0", "hours=999999", "hours=invalid"} {
		if w := adminRequest(h, "GET", "/api/statistics?"+query, nil, cookie, ""); w.Code != 400 {
			t.Fatal("invalid range accepted")
		}
	}
	w := adminRequest(h, "GET", "/api/statistics?hours=24&rule=default%2Fnas.example.com", nil, cookie, "")
	var s AccessStatistics
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &s) != nil || s.Total != 1 || s.Normal != 1 {
		t.Fatal("statistics API incorrect")
	}
}

func TestStatisticsAuthAndFirewallDetailsAreDistinctFromBackendErrors(t *testing.T) {
	now := time.Now()
	hit := &FirewallHit{ID: "policy", Name: "Original name", Kind: "group", Group: "Blocked range", Order: 2, Match: "include", Reason: "Matched second group"}
	l := &Logs{entries: []LogEntry{
		{Time: now, Category: "access", Rule: "default/nas.example", Status: 303, Outcome: "auth_success", AuthResult: "auth_success"},
		{Time: now, Category: "access", Rule: "default/nas.example", Status: 401, Outcome: "auth_failed", AuthResult: "auth_failed"},
		{Time: now, Category: "access", Rule: "default/nas.example", Status: 403, Outcome: "ip_frozen", AuthResult: "auth_failed", FreezeCreated: true},
		{Time: now, Category: "access", Rule: "default/nas.example", Status: 403, Outcome: "ip_frozen"},
		{Time: now, Category: "access", Rule: "default/nas.example", Status: 403, Outcome: "blocked", Firewall: hit},
		{Time: now, Category: "access", Rule: "default/nas.example", Status: 401, Outcome: "forwarded"},
		{Time: now, Category: "access", Rule: "default/nas.example", Status: 401, Outcome: "auth_required", AuthResult: "auth_required"},
		{Time: now, Category: "access", Rule: "default/nas.example", Status: 429, Outcome: "auth_rate_limited", AuthResult: "auth_rate_limited"},
		{Time: now, Category: "access", Rule: "other/nas.example", Status: 403, Outcome: "blocked", Firewall: hit},
	}}
	s := l.Statistics(24, "default/nas.example", now)
	if s.Total != 8 || s.Total != s.Normal+s.Blocked+s.Failed || s.AuthSuccess != 1 || s.AuthFailed != 2 || s.AuthRequired != 1 || s.AuthLimited != 1 || s.FreezeCreated != 1 || s.FrozenBlocked != 2 || s.FirewallBlocked != 1 || s.Blocked != 3 {
		t.Fatal("authentication events confused with backend status or double counted")
	}
	if len(s.Firewalls) != 1 || s.Firewalls[0].Order != 2 || s.Firewalls[0].Name != "Original name" || s.Firewalls[0].Count != 1 {
		t.Fatal("rule-filtered security records or firewall snapshots incorrect")
	}
	l.entries = nil
	for i := 0; i < 125; i++ {
		l.entries = append(l.entries, LogEntry{ID: int64(i + 1), Time: now, Category: "access", Status: 403, Outcome: "blocked", Firewall: hit})
	}
	s = l.Statistics(24, "", now)
	if s.Firewalls[0].Count != 125 {
		t.Fatal("recent records cap dropped aggregate counts or newest records")
	}
}
