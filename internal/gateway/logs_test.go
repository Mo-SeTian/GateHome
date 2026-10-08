package gateway

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogsRedactFilterRotateAndRestore(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	c.OutboundProxy = OutboundProxyConfig{URL: "http://127.0.0.1:8080", Username: "test"}
	token, password := "TEST_ONLY_CLOUDFLARE_SECRET", "TEST_ONLY_PROXY_SECRET"
	if err := store.UpdateWithProxy(c, &token, &password, 0); err != nil {
		t.Fatal(err)
	}
	l, err := NewLogs(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	l.Add(LogEntry{Category: "ddns", Target: "https://user:TEST_ONLY_URL_SECRET@example.com/list?token=TEST_ONLY_QUERY", Path: "/api/token/TEST_ONLY_PATH_SECRET?password=TEST_ONLY_QUERY", Message: token + password, OK: false})
	data, err := os.ReadFile(l.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{token, password, "TEST_ONLY_URL_SECRET", "TEST_ONLY_QUERY", "TEST_ONLY_PATH_SECRET"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("log disclosed a credential")
		}
	}
	l.size = maxLogBytes
	l.Add(LogEntry{Category: "access", OK: true, Status: 200})
	l.Add(LogEntry{Category: "admin", OK: false, Status: 401})
	rows, next, bad := l.List("", "", 0, 2)
	if len(rows) != 2 || next == 0 || bad || rows[0].ID <= rows[1].ID {
		t.Fatal("log pagination or order incorrect")
	}
	older, _, _ := l.List("", "", next, 2)
	if len(older) != 1 || older[0].Category != "ddns" {
		t.Fatal("pagination lost rows")
	}
	restored, err := NewLogs(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	rows, _, _ = restored.List("access", "success", 0, 50)
	if len(rows) != 1 || rows[0].Status != 200 {
		t.Fatal("rotated logs were not restored")
	}
	info, err := os.Stat(l.path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("logs must be private")
	}
}

func TestProjectAndRuleLogFiltersDoNotMixGroups(t *testing.T) {
	l, err := NewLogs(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Second)
	l.Add(LogEntry{Category: "admin", OK: true, Path: "/api/config", Method: "PUT", Status: 200})
	l.Add(LogEntry{Category: "access", Rule: "one/nas.example.com", Target: "nas.example.com", Path: "/photos", Remote: "192.0.2.1", Method: "GET", Status: 200, OK: true})
	l.Add(LogEntry{Category: "access", Rule: "two/nas.example.com", Target: "nas.example.com", Path: "/photos", Remote: "192.0.2.2", Method: "POST", Status: 403, OK: false})
	project, _, _ := l.Query(LogFilter{Scope: "project"}, 0, 50)
	if len(project) != 1 || project[0].Category != "admin" {
		t.Fatal("access logs mixed into project logs")
	}
	f := LogFilter{Scope: "access", Rule: "two/nas.example.com", Method: "POST", Status: 403, Search: "192.0.2.2", From: start, To: time.Now().Add(time.Second), Result: "error"}
	rows, _, _ := l.Query(f, 0, 50)
	if len(rows) != 1 || rows[0].Rule != f.Rule {
		t.Fatal("rule, method, status, IP or time filter failed")
	}
	f.Rule = "one/nas.example.com"
	rows, _, _ = l.Query(f, 0, 50)
	if len(rows) != 0 {
		t.Fatal("same domain from another group leaked into rule filter")
	}
	f.Rule = "two/nas.example.com"
	f.To = start
	rows, _, _ = l.Query(f, 0, 50)
	if len(rows) != 0 {
		t.Fatal("time filter ignored")
	}
}

func TestConcurrentLogWritesAndAPIAuth(t *testing.T) {
	a, h := testAdmin(t)
	l, err := NewLogs(t.TempDir(), a.store)
	if err != nil {
		t.Fatal(err)
	}
	a.SetLogs(l)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); l.Add(LogEntry{Category: "access", OK: true, Status: 200}) }()
	}
	wg.Wait()
	if w := adminRequest(h, "GET", "/api/logs", nil, nil, ""); w.Code != 401 {
		t.Fatal("unauthenticated logs exposed")
	}
	cookie := loginForTest(t, h)
	w := adminRequest(h, "GET", "/api/logs?scope=access&category=access&result=success", nil, cookie, "")
	var result struct {
		Entries []LogEntry `json:"entries"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Entries) != 50 {
		t.Fatal("log API failed")
	}
	if strings.Contains(w.Body.String(), "TEST_ONLY_ADMIN_PASSWORD") {
		t.Fatal("login password in logs")
	}
}
