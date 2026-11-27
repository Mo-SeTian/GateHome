package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestExtendedWebhookFormatsAndSignatures(t *testing.T) {
	event := webhookEvent{
		ID:       "evt-test-1",
		Type:     "mihomo",
		Status:   "firing",
		Resource: "主力机场",
		Message:  "订阅剩余流量不足 10%",
		Time:     time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC),
	}

	t.Run("generic-hmac", func(t *testing.T) {
		var gotSig string
		var gotBody []byte
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotSig = r.Header.Get("X-GateHome-Signature")
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(200)
		}))
		defer srv.Close()

		s := webhookState{URL: srv.URL, Format: "generic", Secret: "TEST_GENERIC_SECRET"}
		if err := sendWebhook(context.Background(), srv.Client(), s, event); err != nil {
			t.Fatal(err)
		}
		mac := hmac.New(sha256.New, []byte("TEST_GENERIC_SECRET"))
		mac.Write(gotBody)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if gotSig != want {
			t.Fatalf("generic HMAC mismatch: got %q want %q", gotSig, want)
		}
	})

	t.Run("wecom", func(t *testing.T) {
		failMode := false
		var gotMsgType, gotContent string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotMsgType, _ = body["msgtype"].(string)
			md, _ := body["markdown"].(map[string]any)
			gotContent, _ = md["content"].(string)
			if failMode {
				_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 93000, "errmsg": "invalid"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok"})
		}))
		defer srv.Close()

		s := webhookState{URL: srv.URL, Format: "wecom"}
		if err := sendWebhook(context.Background(), srv.Client(), s, event); err != nil {
			t.Fatal(err)
		}
		if gotMsgType != "markdown" || !strings.Contains(gotContent, "主力机场") {
			t.Fatalf("unexpected wecom payload: msgtype=%q content=%q", gotMsgType, gotContent)
		}
		failMode = true
		if err := sendWebhook(context.Background(), srv.Client(), s, event); err == nil {
			t.Fatal("wecom non-zero errcode must fail")
		}
	})

	t.Run("feishu", func(t *testing.T) {
		failMode := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			ts, _ := body["timestamp"].(string)
			sig, _ := body["sign"].(string)
			mac := hmac.New(sha256.New, []byte(ts+"\nTEST_FS_SECRET"))
			want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
			if sig != want || body["msg_type"] != "post" {
				t.Fatalf("feishu signature or msg_type invalid: got %q want %q type=%v", sig, want, body["msg_type"])
			}
			if failMode {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 19021})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		}))
		defer srv.Close()

		s := webhookState{URL: srv.URL, Format: "feishu", Secret: "TEST_FS_SECRET"}
		if err := sendWebhook(context.Background(), srv.Client(), s, event); err != nil {
			t.Fatal(err)
		}
		failMode = true
		if err := sendWebhook(context.Background(), srv.Client(), s, event); err == nil {
			t.Fatal("feishu non-zero code must fail")
		}
	})

	t.Run("dingtalk", func(t *testing.T) {
		failMode := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ts := r.URL.Query().Get("timestamp")
			sig := r.URL.Query().Get("sign")
			mac := hmac.New(sha256.New, []byte("SEC_TEST_DT"))
			mac.Write([]byte(ts + "\nSEC_TEST_DT"))
			want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
			if r.URL.Query().Get("access_token") != "abc" || sig != want {
				t.Fatalf("dingtalk sign or token invalid: query=%v sig=%q want=%q", r.URL.Query(), sig, want)
			}
			if failMode {
				_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 310000})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0})
		}))
		defer srv.Close()

		s := webhookState{URL: srv.URL + "/robot/send?access_token=abc", Format: "dingtalk", Secret: "SEC_TEST_DT"}
		if err := sendWebhook(context.Background(), srv.Client(), s, event); err != nil {
			t.Fatal(err)
		}
		failMode = true
		if err := sendWebhook(context.Background(), srv.Client(), s, event); err == nil {
			t.Fatal("dingtalk non-zero errcode must fail")
		}
	})

	t.Run("telegram", func(t *testing.T) {
		failMode := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["chat_id"] != "-100999" || !strings.Contains(body["text"].(string), "主力机场") {
				t.Fatalf("unexpected telegram body: %+v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": !failMode})
		}))
		defer srv.Close()

		s := webhookState{URL: srv.URL, Format: "telegram", ChatID: "-100999"}
		if err := sendWebhook(context.Background(), srv.Client(), s, event); err != nil {
			t.Fatal(err)
		}
		failMode = true
		if err := sendWebhook(context.Background(), srv.Client(), s, event); err == nil {
			t.Fatal("telegram ok:false must fail")
		}
	})

	t.Run("bark", func(t *testing.T) {
		failMode := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["group"] != "GateHome" {
				t.Fatalf("missing bark group: %+v", body)
			}
			if failMode {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 400})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200})
		}))
		defer srv.Close()

		s := webhookState{URL: srv.URL, Format: "bark"}
		if err := sendWebhook(context.Background(), srv.Client(), s, event); err != nil {
			t.Fatal(err)
		}
		failMode = true
		if err := sendWebhook(context.Background(), srv.Client(), s, event); err == nil {
			t.Fatal("bark non-200 code must fail")
		}
	})

	t.Run("gotify", func(t *testing.T) {
		var gotKey string
		var gotPriority float64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotKey = r.Header.Get("X-Gotify-Key")
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotPriority, _ = body["priority"].(float64)
			w.WriteHeader(200)
		}))
		defer srv.Close()

		s := webhookState{URL: srv.URL, Format: "gotify", Secret: "GOTIFY_APP_TOKEN"}
		if err := sendWebhook(context.Background(), srv.Client(), s, event); err != nil {
			t.Fatal(err)
		}
		if gotKey != "GOTIFY_APP_TOKEN" || gotPriority != 8 {
			t.Fatalf("unexpected gotify headers/priority: key=%q prio=%v", gotKey, gotPriority)
		}
	})
}

func TestExtendedWebhookConfigRevealRedactionAndNewEvents(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	a.logs, _ = NewLogs(t.TempDir(), a.store)

	// 1. Default format/events backward compatibility & Telegram chat_id requirement
	w := adminRequest(h, "PUT", "/api/notifications/webhook", map[string]any{
		"enabled":  true,
		"format":   "telegram",
		"url":      "https://api.telegram.org/bot123/sendMessage",
		"revision": 1,
	}, cookie, "")
	if w.Code != 400 {
		t.Fatalf("expected 400 when telegram lacks chat_id, got %d", w.Code)
	}

	w = adminRequest(h, "PUT", "/api/notifications/webhook", map[string]any{
		"enabled":  true,
		"format":   "invalid-chan",
		"url":      "https://example.com/hook",
		"revision": 1,
	}, cookie, "")
	if w.Code != 400 {
		t.Fatalf("expected 400 for invalid format, got %d", w.Code)
	}

	// 2. Save DingTalk with secret and all 6 events
	dtSecret := "SEC_TEST_ONLY_DINGTALK_SECRET"
	w = adminRequest(h, "PUT", "/api/notifications/webhook", map[string]any{
		"enabled":  true,
		"format":   "dingtalk",
		"url":      "https://oapi.dingtalk.com/robot/send?access_token=t",
		"secret":   dtSecret,
		"events":   []string{"upstream", "backup", "certificate", "ddns", "mihomo", "security"},
		"revision": 1,
	}, cookie, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), dtSecret) {
		t.Fatalf("save failed or leaked secret: code=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"secret_configured":true`) || !strings.Contains(w.Body.String(), `"format":"dingtalk"`) {
		t.Fatalf("missing format/secret_configured in response: %s", w.Body.String())
	}

	// Reveal alert-webhook-secret returns exact secret
	rw := adminRequest(h, "POST", "/api/credentials/reveal", map[string]string{"kind": "alert-webhook-secret"}, cookie, "")
	if rw.Code != 200 || !strings.Contains(rw.Body.String(), dtSecret) {
		t.Fatalf("reveal alert-webhook-secret failed: %d %s", rw.Code, rw.Body.String())
	}

	// Logs redact alert-webhook-secret
	a.logs.Add(LogEntry{Category: "admin", Message: "secret=" + dtSecret})
	logRows, _, _ := a.logs.List("admin", "", 0, 10)
	if len(logRows) == 0 || strings.Contains(logRows[0].Message, dtSecret) || logRows[0].Message != "secret=<REDACTED>" {
		t.Fatalf("logs did not redact webhook secret: %+v", logRows)
	}

	// 3. DDNS result hook fires & resolves alerts
	g := testDDNSGroup()
	a.jobs.onDDNSResult(g, false, "连接超时", time.Now())
	a.jobs.onDDNSResult(g, true, "完成", time.Now().Add(time.Minute))

	pending := a.store.Snapshot().Alerts.Pending
	if len(pending) != 2 || pending[0].Type != "ddns" || pending[0].Status != "firing" || pending[1].Status != "resolved" {
		t.Fatalf("expected DDNS firing+resolved alerts, got %+v", pending)
	}

	// 4. Security IP freeze hook fires with 5m cooldown dedupe
	route := testRoute("app.example.com", "http://127.0.0.1:8080")
	route.Auth = RouteAuthConfig{Enabled: true, Username: "u", FailureLimit: 2, FreezeSeconds: 60}
	now := time.Now()
	a.proxy.defense.record(route, "203.0.113.10", false)
	_, blocked, created := a.proxy.defense.record(route, "203.0.113.10", false)
	if !blocked || !created {
		t.Fatal("expected IPDefense to freeze client after 2 failures")
	}
	// Repeat deduped within 5m
	a.emitIncidentAlert("ip-freeze:"+routeKey(route)+"|203.0.113.10", "security", "203.0.113.10", "duplicate", now.Add(time.Minute))
	pending = a.store.Snapshot().Alerts.Pending
	secCount := 0
	for _, ev := range pending {
		if ev.Type == "security" {
			secCount++
		}
	}
	if secCount != 1 {
		t.Fatalf("expected exactly 1 deduped security alert, got %d (%+v)", secCount, pending)
	}

	// Event filter respects event selection
	if err := validateEventSelection([]string{"upstream", "ddns", "mihomo", "security"}); err != nil {
		t.Fatalf("valid extended events rejected: %v", err)
	}
	if err := validateEventSelection([]string{"unknown"}); err == nil {
		t.Fatal("unknown event type accepted")
	}
	_ = netip.IPv4Unspecified()
	_ = slices.Contains([]string{}, "")
	_ = errors.New("")
}
