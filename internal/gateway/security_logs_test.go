package gateway

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"
)

func TestSecurityLogScopesAndFilters(t *testing.T) {
	now := time.Now()
	policy := &FirewallHit{ID: "policy", Name: "Original policy", Reason: "Original reason"}
	l := &Logs{entries: []LogEntry{
		{Category: "admin"},
		{Category: "access", Outcome: "forwarded", Status: 200},
		{Category: "access", Outcome: "forwarded", Status: 403},
		{Category: "access", Outcome: "blocked", Status: 403, Firewall: policy},
		{Category: "access", Outcome: "forwarded", Status: 403, Firewall: policy, Security: []SecurityHit{{Engine: "waf", RuleID: 941100, Action: "detect"}}},
		{Category: "access", Outcome: "blocked", Status: 403, Firewall: policy, Security: []SecurityHit{{Engine: "waf", RuleID: 942100, Action: "block"}, {Engine: "custom", RuleID: 10000, Action: "detect"}}},
		{Category: "access", Outcome: "auth_failed", AuthResult: "auth_failed"},
		{Category: "access", Outcome: "ip_frozen"},
		{Category: "access", Outcome: "auth_required", AuthResult: "auth_required"},
		{Category: "access", Outcome: "forwarded", Security: []SecurityHit{{Engine: "custom", RuleID: 10000, Name: "Observe crawler", Action: "detect"}}},
		{Category: "access", Outcome: "auth_rate_limited", AuthResult: "auth_rate_limited"},
		{Category: "admin", Security: []SecurityHit{{Engine: "waf", RuleID: 941100}}},
	}}
	for i := range l.entries {
		l.entries[i].ID = int64(i + 1)
		l.entries[i].Time = now
		l.entries[i].Rule = "home/app.example.test"
		l.entries[i].Remote = "2001:4860:0:0:0:0:0:8888"
	}
	l.lastID = int64(len(l.entries))
	for _, tc := range []struct {
		filter LogFilter
		want   int
	}{
		{LogFilter{Scope: "firewall"}, 5}, {LogFilter{Scope: "security"}, 7},
		{LogFilter{Scope: "firewall", Decision: "detect"}, 2}, {LogFilter{Scope: "firewall", Decision: "block"}, 3},
		{LogFilter{Scope: "firewall", Engine: "ip"}, 1}, {LogFilter{Scope: "firewall", Engine: "freeze"}, 1},
		{LogFilter{Scope: "security", Engine: "auth"}, 2},
		{LogFilter{Scope: "firewall", Engine: "waf", RuleID: 941100}, 1},
		{LogFilter{Scope: "firewall", Engine: "waf", RuleID: 10000}, 0},
		{LogFilter{Scope: "firewall", Firewall: "policy", Search: "original reason"}, 3},
		{LogFilter{Scope: "firewall", IP: "2001:4860::8888", Rule: "home/app.example.test"}, 5},
		{LogFilter{Scope: "firewall", IP: "8.8.8.8"}, 0},
		{LogFilter{Scope: "firewall", From: now.Add(time.Second)}, 0},
		{LogFilter{Scope: "firewall", To: now.Add(-time.Second)}, 0},
		{LogFilter{Scope: "firewall", Search: "observe crawler"}, 1},
	} {
		if got := l.Page(tc.filter, 1, 20, 0).Total; got != tc.want {
			t.Errorf("filter %+v: total %d want %d", tc.filter, got, tc.want)
		}
	}
}

func TestSecurityLogPaginationSnapshotAndAPI(t *testing.T) {
	a, handler := testAdmin(t)
	l, err := NewLogs(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a.SetLogs(l)
	for i := 0; i < 125; i++ {
		l.Add(LogEntry{Category: "access", Remote: "192.168.1.1", Outcome: "blocked", Firewall: &FirewallHit{ID: "policy"}})
	}
	filter := LogFilter{Scope: "firewall"}
	first := l.Page(filter, 1, 20, 0)
	l.Add(LogEntry{Category: "access", Remote: "8.8.8.8", Outcome: "blocked"})
	ids := map[int64]bool{}
	for page := 1; page <= first.Pages; page++ {
		result := l.Page(filter, page, 20, first.Through)
		if result.Total != 125 {
			t.Fatal("new entries changed the paging snapshot")
		}
		for _, entry := range result.Entries {
			if ids[entry.ID] || entry.ID > first.Through {
				t.Fatal("duplicate or new entry in paged snapshot")
			}
			ids[entry.ID] = true
		}
	}
	if len(ids) != 125 || l.Page(filter, 1, 20, 0).Total != 126 {
		t.Fatal("paged results lost retained events or refresh stayed stale")
	}
	for _, scope := range []string{"access", "firewall", "security"} {
		if adminRequest(handler, "GET", "/api/logs?scope="+scope+"&page=1&size=20", nil, nil, "").Code != 401 {
			t.Fatal("unauthenticated event API disclosed logs")
		}
	}
	cookie := loginForTest(t, handler)
	for _, invalid := range []string{"ip=invalid", "ip=fe80%3A%3A1%25eth0", "engine=waf%7Ccustom", "decision=allow", "rule_id=0", "rule_id=1000000000", "size=10", "page=0", "through=-1", "from=invalid", "from=2026-10-09T00:00:00Z&to=2026-10-08T00:00:00Z"} {
		query := url.Values{"scope": {"firewall"}, "page": {"1"}, "size": {"20"}}
		extra, _ := url.ParseQuery(invalid)
		for key, values := range extra {
			query[key] = values
		}
		if w := adminRequest(handler, "GET", "/api/logs?"+query.Encode(), nil, cookie, ""); w.Code != 400 {
			t.Errorf("invalid filter accepted: %s", invalid)
		}
	}
	for _, size := range []string{"20", "50", "100", "200"} {
		w := adminRequest(handler, "GET", "/api/logs?scope=firewall&page=1&size="+size+"&ip=::ffff:192.168.1.1&firewall=policy", nil, cookie, "")
		var page LogPage
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || page.Total != 125 || page.Entries[0].RemoteRegion != "内网地址" {
			t.Fatal("filtered paging or response location enrichment failed")
		}
	}
}

func TestDetectLogRetainsHistoricalFirewallIdentity(t *testing.T) {
	p, config, _ := protectionProxy(t, Protection{Rules: []HTTPRule{{Name: "Observe path", Target: "path", Pattern: "^/watch", Action: "detect"}}})
	protectionRequest(p, "GET", "/watch", "")
	config.Firewalls[0].Name = "Renamed policy"
	if err := p.Configure(config); err != nil {
		t.Fatal(err)
	}
	page := p.logs.Page(LogFilter{Scope: "firewall", Firewall: "secure", Decision: "detect"}, 1, 20, 0)
	if page.Total != 1 || page.Entries[0].Firewall.Name != "测试防护" {
		t.Fatal("detect-only event lost its original firewall identity")
	}
}
