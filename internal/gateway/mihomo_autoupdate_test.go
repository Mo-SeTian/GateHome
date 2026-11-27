package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testValidSubYAML1 = `proxies:
  - name: Node-1
    type: ss
    server: 1.1.1.1
    port: 8388
    cipher: aes-256-gcm
    password: pass
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - Node-1
rules:
  - MATCH,PROXY
`

const testValidSubYAML2 = `proxies:
  - name: Node-A
    type: ss
    server: 2.2.2.2
    port: 8388
    cipher: aes-256-gcm
    password: pass
  - name: Node-B
    type: ss
    server: 3.3.3.3
    port: 8388
    cipher: aes-256-gcm
    password: pass
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - Node-A
      - Node-B
rules:
  - MATCH,PROXY
`

func TestMihomoAutoUpdateValidationTickRollbackAndAlerts(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	a.logs, _ = NewLogs(t.TempDir(), a.store)
	a.SetLogs(a.logs)

	// Enable mihomo alert notifications in webhook settings
	if w := adminRequest(h, "PUT", "/api/notifications/webhook", map[string]any{
		"enabled":  true,
		"url":      "https://example.com/hook",
		"events":   []string{"mihomo"},
		"revision": 1,
	}, cookie, ""); w.Code != 200 {
		t.Fatalf("enable mihomo alert failed: %d", w.Code)
	}

	// 1. Validation: local YAML cannot set auto_update_interval > 0
	badLocal := adminRequest(h, "POST", "/api/mihomo/subscriptions", map[string]any{
		"name":                 "LocalYAML",
		"yaml_content":         testValidSubYAML1,
		"auto_update_interval": 360,
	}, cookie, "")
	if badLocal.Code != 400 {
		t.Fatalf("expected 400 for local YAML with auto_update_interval, got %d", badLocal.Code)
	}

	// 2. Out-of-range interval rejected
	badRange := adminRequest(h, "POST", "/api/mihomo/subscriptions", map[string]any{
		"name":                 "RemoteBadInterval",
		"url":                  "https://sub.example.test/clash.yaml",
		"auto_update_interval": 30,
	}, cookie, "")
	if badRange.Code != 400 {
		t.Fatalf("expected 400 for 30m interval (<60m), got %d", badRange.Code)
	}

	// 3. Add remote subscription via test fetcher with 360m interval
	var fetchMode atomic.Int32 // 0=ok v1, 1=ok v2, 2=fetch fail, 3=invalid yaml (triggers rollback)
	var fetchCalls atomic.Int32
	a.mihomo.mu.Lock()
	a.mihomo.fetchSubFn = func(_ context.Context, _ string) ([]byte, int64, int64, int64, int64, error) {
		fetchCalls.Add(1)
		switch fetchMode.Load() {
		case 0:
			return []byte(testValidSubYAML1), 100, 100, 1000, time.Now().Add(10 * 24 * time.Hour).Unix(), nil
		case 1:
			// Trigger both expiry <= 3d and quota <= 10%
			return []byte(testValidSubYAML2), 450, 500, 1000, time.Now().Add(48 * time.Hour).Unix(), nil
		case 2:
			return nil, 0, 0, 0, 0, errors.New("upstream timeout")
		case 3:
			// Structurally invalid YAML that causes synthesizeAndWriteLocked to fail
			return []byte(":::not-yaml::: ["), 0, 0, 0, 0, nil
		default:
			return []byte(testValidSubYAML1), 0, 0, 0, 0, nil
		}
	}
	a.mihomo.mu.Unlock()

	addW := adminRequest(h, "POST", "/api/mihomo/subscriptions", map[string]any{
		"name":                 "主力订阅",
		"url":                  "https://sub.example.test/clash.yaml?token=secret",
		"auto_update_interval": 360,
	}, cookie, "")
	if addW.Code != 200 {
		t.Fatalf("add subscription failed: %d %s", addW.Code, addW.Body.String())
	}
	var st mihomoStatusResponse
	if err := json.Unmarshal(addW.Body.Bytes(), &st); err != nil || len(st.Subscriptions) != 1 {
		t.Fatalf("decode status failed: err=%v st=%+v", err, st)
	}
	subID := st.Subscriptions[0].ID
	if st.Subscriptions[0].AutoUpdateInterval != 360 || st.Subscriptions[0].NextUpdateAt.IsZero() {
		t.Fatalf("expected auto_update_interval=360 and next_update_at set, got %+v", st.Subscriptions[0])
	}

	// 4. Tick before interval is due -> does NOT fetch
	beforeCalls := fetchCalls.Load()
	now := time.Now()
	a.mihomo.RunAutoUpdateTick(context.Background(), now.Add(30*time.Minute))
	if fetchCalls.Load() != beforeCalls {
		t.Fatalf("auto update ran before interval due: calls %d -> %d", beforeCalls, fetchCalls.Load())
	}

	// 5. Simulate failure when due -> sets LastError, triggers mihomo alert, respects 15m failure cooldown
	fetchMode.Store(2)
	dueTime := now.Add(7 * time.Hour)
	a.mihomo.RunAutoUpdateTick(context.Background(), dueTime)
	if fetchCalls.Load() != beforeCalls+1 {
		t.Fatalf("expected auto update fetch when due")
	}
	snap := a.mihomo.statusSnapshot()
	if snap.Subscriptions[0].LastError == "" {
		t.Fatal("expected LastError after failed auto update")
	}
	// Within 15m cooldown -> skipped
	a.mihomo.RunAutoUpdateTick(context.Background(), dueTime.Add(5*time.Minute))
	if fetchCalls.Load() != beforeCalls+1 {
		t.Fatal("15m failure cooldown did not suppress retry")
	}

	// 6. Invalid YAML synthesis failure rolls back subscription YAML on disk cleanly
	fetchMode.Store(3)
	a.mihomo.RunAutoUpdateTick(context.Background(), dueTime.Add(20*time.Minute))
	snap = a.mihomo.statusSnapshot()
	if snap.Subscriptions[0].NodeCount != 1 || !strings.Contains(snap.Subscriptions[0].LastError, "保留原订阅") {
		t.Fatalf("expected rollback message and preserved node count 1, got %+v", snap.Subscriptions[0])
	}

	// 7. Recovery with v2 YAML (2 nodes, expiry < 3d, quota remaining 5%) -> resolves sub error & fires expire + quota alerts
	fetchMode.Store(1)
	a.mihomo.RunAutoUpdateTick(context.Background(), dueTime.Add(40*time.Minute))
	snap = a.mihomo.statusSnapshot()
	if snap.Subscriptions[0].NodeCount != 2 || snap.Subscriptions[0].LastError != "" {
		t.Fatalf("expected recovered 2 nodes & empty LastError, got %+v", snap.Subscriptions[0])
	}

	pending := a.store.Snapshot().Alerts.Pending
	var hasSubFiring, hasSubResolved, hasExpire, hasQuota bool
	for _, ev := range pending {
		if ev.Type != "mihomo" {
			continue
		}
		if strings.Contains(ev.Message, "更新失败") && ev.Status == "firing" {
			hasSubFiring = true
		}
		if strings.Contains(ev.Message, "更新已恢复") && ev.Status == "resolved" {
			hasSubResolved = true
		}
		if strings.Contains(ev.Message, "到期") && ev.Status == "firing" {
			hasExpire = true
		}
		if strings.Contains(ev.Message, "剩余流量") && ev.Status == "firing" {
			hasQuota = true
		}
	}
	if !hasSubFiring || !hasSubResolved || !hasExpire || !hasQuota {
		t.Fatalf("missing expected mihomo alert lifecycle events: firing=%v resolved=%v expire=%v quota=%v events=%+v",
			hasSubFiring, hasSubResolved, hasExpire, hasQuota, pending)
	}

	// 8. Delete subscription prunes its active alert keys
	delW := adminRequest(h, "DELETE", "/api/mihomo/subscriptions/"+subID, nil, cookie, "")
	if delW.Code != 200 {
		t.Fatalf("delete subscription failed: %d", delW.Code)
	}
	active := a.store.Snapshot().NoticeHistory.Active
	if active["mihomo-sub/"+subID] || active["mihomo-expire/"+subID] || active["mihomo-quota/"+subID] {
		t.Fatalf("expected deleted subscription alert keys cleared, got %+v", active)
	}
}
