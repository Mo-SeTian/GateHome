package gateway

import (
	"encoding/json"
	"strconv"
	"testing"
)

func TestLogPagesKeepSnapshotAndFilters(t *testing.T) {
	l, err := NewLogs(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 53; i++ {
		l.Add(LogEntry{Category: "access", Rule: "one/nas.example.com", Status: 200, OK: true, Method: "GET"})
		l.Add(LogEntry{Category: "access", Rule: "two/nas.example.com", Status: 403, Method: "POST"})
		l.Add(LogEntry{Category: "admin", Status: 200, OK: true})
	}
	filter := LogFilter{Scope: "access", Rule: "one/nas.example.com", Method: "GET", Result: "success", Status: 200}
	first := l.Page(filter, 1, 20, 0)
	if first.Total != 53 || first.Pages != 3 || len(first.Entries) != 20 {
		t.Fatal("filtered page metadata incorrect")
	}
	l.Add(LogEntry{Category: "access", Rule: filter.Rule, Status: 200, OK: true, Method: "GET"})
	seen := map[int64]bool{}
	for page := 1; page <= 3; page++ {
		result := l.Page(filter, page, 20, first.Through)
		if result.Total != 53 {
			t.Fatal("new rows shifted snapshot pages")
		}
		for _, row := range result.Entries {
			if seen[row.ID] || row.Rule != filter.Rule {
				t.Fatal("duplicate page row or wrong rule")
			}
			seen[row.ID] = true
		}
	}
	if len(seen) != 53 {
		t.Fatal("page lost rows")
	}
	if refreshed := l.Page(filter, 1, 20, 0); refreshed.Total != 54 || refreshed.Entries[0].ID <= first.Through {
		t.Fatal("refresh did not include new logs")
	}
	if last := l.Page(filter, 100, 20, first.Through); last.Page != 3 || len(last.Entries) != 13 {
		t.Fatal("page beyond end was not clamped")
	}
	if empty := l.Page(LogFilter{Rule: "missing"}, 2, 20, 0); empty.Page != 1 || empty.Total != 0 || len(empty.Entries) != 0 {
		t.Fatal("empty page incorrect")
	}
}

func TestLogPageAPIValidatesSizeAndSnapshot(t *testing.T) {
	a, h := testAdmin(t)
	l, err := NewLogs(t.TempDir(), a.store)
	if err != nil {
		t.Fatal(err)
	}
	a.SetLogs(l)
	for i := 0; i < 71; i++ {
		l.Add(LogEntry{Category: "access", Status: 200, OK: true})
	}
	cookie := loginForTest(t, h)
	for _, size := range []int{20, 50, 100, 200} {
		w := adminRequest(h, "GET", "/api/logs?scope=access&page=1&size="+strconv.Itoa(size), nil, cookie, "")
		var result LogPage
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Entries) != min(size, 71) || result.Total != 71 {
			t.Fatal("log page API failed")
		}
	}
	for _, query := range []string{"page=0&size=20", "page=1&size=5000", "page=1&size=abc", "page=1&size=20&through=-1", "page=1&size=20&through=abc"} {
		if w := adminRequest(h, "GET", "/api/logs?"+query, nil, cookie, ""); w.Code != 400 {
			t.Fatal("invalid pagination parameters accepted")
		}
	}
}
