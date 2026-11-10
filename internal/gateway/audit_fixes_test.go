package gateway

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLogsRedactUpdateAndBackupCredentials(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal("fixture setup failed")
	}
	store.state.GitHubToken = "TEST_ONLY_GITHUB_SECRET"
	store.state.WebDAVBackup.Password = "TEST_ONLY_WEBDAV_SECRET"
	store.state.WebDAVBackup.BackupPassword = "TEST_ONLY_BACKUP_SECRET"
	logs, err := NewLogs(t.TempDir(), store)
	if err != nil {
		t.Fatal("fixture logs failed")
	}
	secrets := []string{store.state.GitHubToken, store.state.WebDAVBackup.Password, store.state.WebDAVBackup.BackupPassword}
	logs.Add(LogEntry{Category: "admin", Message: strings.Join(secrets, " ")})
	data, err := os.ReadFile(logs.path)
	if err != nil {
		t.Fatal("fixture log read failed")
	}
	rows, _, _ := logs.List("", "", 0, 10)
	if len(rows) != 1 {
		t.Fatal("log entry missing")
	}
	for _, secret := range secrets {
		if strings.Contains(string(data), secret) || strings.Contains(rows[0].Message, secret) {
			t.Fatal("saved credential was not redacted from disk and query")
		}
	}
}

func TestPartialJSONDetectionDoesNotBlockOrHideHeaderHits(t *testing.T) {
	p, _, _ := protectionProxy(t, Protection{WAF: WAFPolicy{Mode: "detect", BodyLimit: 65536}})
	body := `{"padding":"` + strings.Repeat("x", 65537) + `"}`
	r := httptest.NewRequest("POST", "http://app.example.com/?q=%3Cscript%3Ealert(1)%3C%2Fscript%3E", strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("User-Agent", "GatehouseTest/1.0")
	r.Header.Set("Accept", "*/*")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != body {
		t.Fatal("partial JSON parsing changed detection-only forwarding")
	}
	coverage, attack := false, false
	for _, hit := range p.logs.entries[len(p.logs.entries)-1].Security {
		coverage = coverage || hit.RuleID == 1002
		attack = attack || hit.RuleID >= 911000
	}
	if !coverage || !attack {
		t.Fatal("partial parsing discarded header/URI detections or its warning")
	}
}

func TestInvalidUpstreamReloadRetainsWorkingRoute(t *testing.T) {
	p, c, _ := protectionProxy(t, Protection{})
	c.Routes[0].Upstream = "http://%zz"
	if p.Configure(c) == nil {
		t.Fatal("malformed upstream accepted")
	}
	if w := protectionRequest(p, "GET", "/", ""); w.Code != 200 {
		t.Fatal("rejected upstream changed the running route")
	}
	if isAdminUpstream(nil, 16666, nil) {
		t.Fatal("nil upstream classified as management")
	}
}

func TestSessionCapacityEvictsEarliestExpiry(t *testing.T) {
	now := time.Now()
	expired, oldest, newest := [32]byte{1}, [32]byte{2}, [32]byte{3}
	sessions := map[[32]byte]time.Time{expired: now.Add(-time.Second), oldest: now.Add(time.Minute), newest: now.Add(time.Hour)}
	pruneSessions(sessions, 3, now)
	if len(sessions) != 2 || sessions[oldest].IsZero() || sessions[newest].IsZero() {
		t.Fatal("expiry cleanup evicted an active session")
	}
	sessions[[32]byte{4}] = now.Add(2 * time.Hour)
	pruneSessions(sessions, 3, now)
	if len(sessions) != 2 || !sessions[oldest].IsZero() || sessions[newest].IsZero() {
		t.Fatal("capacity cleanup did not preserve newer active sessions")
	}
}

func TestSunPanelMaintenanceReturnsWithoutWaitingForSnapshot(t *testing.T) {
	panel := &SunPanel{}
	panel.mu.Lock()
	done := make(chan struct{})
	w := httptest.NewRecorder()
	go func() {
		panel.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/sunpanel/", nil))
		_, err := panel.bridgeCall(context.Background(), "desktop", 1, nil)
		if err == nil {
			t.Error("maintenance bridge request accepted")
		}
		close(done)
	}()
	select {
	case <-done:
		panel.mu.Unlock()
		if w.Code != 503 || w.Header().Get("Retry-After") != "1" {
			t.Fatal("maintenance response missing retry feedback")
		}
	case <-time.After(time.Second):
		panel.mu.Unlock()
		<-done
		t.Fatal("requests waited on the snapshot lifecycle lock")
	}
}

func TestProtectionCapacityReclaimsExpiredWithoutOpeningPolicy(t *testing.T) {
	counters := newProtectionCounters()
	now := time.Now()
	counters.now = func() time.Time { return now }
	counters.lastSweep = now
	for i := 0; i < 32768; i++ {
		counters.windows[fmt.Sprint(i)] = protectionWindow{start: now, expires: now.Add(time.Minute), count: 1}
	}
	expired := counters.windows["1"]
	expired.expires = now.Add(time.Second)
	counters.windows["1"] = expired
	if _, _, available := counters.increment("new", 60, 5); available {
		t.Fatal("unexpired capacity must retain the protection boundary")
	}
	if limited, _, available := counters.increment("0", 60, 5); limited || !available || counters.windows["0"].count != 2 {
		t.Fatal("full capacity changed an existing counter")
	}
	now = now.Add(2 * time.Second)
	if _, _, available := counters.increment("new", 60, 5); !available {
		t.Fatal("expired capacity remained unavailable before the minute sweep")
	}
	if len(counters.windows) != 32768 || counters.windows["0"].count != 2 {
		t.Fatal("reclaim evicted a live counter")
	}
}

func TestWAFDetectsOversizedPrefixAndPreservesFullBody(t *testing.T) {
	p, _, _ := protectionProxy(t, Protection{WAF: WAFPolicy{Mode: "detect", BodyLimit: 65536}})
	body := "q=%3Cscript%3Ealert(1)%3C%2Fscript%3E&padding=" + strings.Repeat("x", 65537)
	w := protectionRequest(p, "POST", "/", body)
	if w.Code != 200 || w.Body.String() != body {
		t.Fatal("detection changed the forwarded oversized request")
	}
	limited, attack := false, false
	for _, hit := range p.logs.entries[len(p.logs.entries)-1].Security {
		limited = limited || hit.RuleID == 1002
		attack = attack || hit.RuleID >= 911000
		if hit.Action != "detect" {
			t.Fatal("detection mode blocked a partial inspection")
		}
	}
	if !limited || !attack {
		t.Fatal("oversized inspection lost its coverage warning or prefix attack")
	}
}
