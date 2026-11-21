package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebhookTransitionsSurviveRestart(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	const path = "/api/notifications/webhook"
	for _, method := range []string{"GET", "PUT", "POST"} {
		target := path
		if method == "POST" {
			target += "/test"
		}
		if adminRequest(h, method, target, nil, nil, "").Code != 401 {
			t.Fatal("anonymous webhook access")
		}
	}
	rev := a.store.Snapshot().Revision
	received := make(chan webhookEvent, 10)
	var attempts atomic.Int32
	listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event webhookEvent
		if json.NewDecoder(r.Body).Decode(&event) != nil || event.ID == "" || event.ID != r.Header.Get("Idempotency-Key") || r.Method != "POST" {
			t.Error("invalid webhook envelope")
		}
		received <- event
		if attempts.Add(1) == 1 {
			http.Redirect(w, r, "/elsewhere", 307)
			return
		}
		w.WriteHeader(204)
	}))
	defer listener.Close()
	input := map[string]any{"enabled": true, "url": listener.URL + "/TEST_ONLY_WEBHOOK_PATH?test=fixture", "revision": rev}
	saved := adminRequest(h, "PUT", path, input, cookie, "")
	if saved.Code != 200 || strings.Contains(saved.Body.String(), "TEST_ONLY_") {
		t.Fatal("settings save failed or exposed URL")
	}
	if adminRequest(h, "PUT", path, input, cookie, "").Code != 409 {
		t.Fatal("stale settings accepted")
	}
	if strings.Contains(adminRequest(h, "GET", path, nil, cookie, "").Body.String(), "TEST_ONLY_") {
		t.Fatal("settings exposed URL")
	}

	// The background health scheduler and interactive refresh share the same sample cache.
	route := Route{ID: "test-route", Host: "app.example.test", Upstream: "http://127.0.0.1:1"}
	a.health.results = map[string]serviceHealthResult{}
	a.health.events = map[string]healthEventState{}
	now := time.Now()
	down := serviceHealthResult{CheckedAt: now, State: "timeout", Label: "响应超时"}
	a.recordHealthTransition(route, down)
	a.recordHealthTransition(route, down)
	if len(a.store.Snapshot().Alerts.Pending) != 0 {
		t.Fatal("cached first failure created an alert")
	}
	down.CheckedAt = now.Add(time.Second)
	a.recordHealthTransition(route, down)
	down.CheckedAt = now.Add(2 * time.Second)
	a.recordHealthTransition(route, down)
	if !a.deliverWebhook(context.Background()) || len(received) != 1 {
		t.Fatal("redirect followed or lost failed event")
	}
	if a.store.Snapshot().Alerts.LastError != "Webhook 返回 HTTP 307" {
		t.Fatal("missing sanitized failure status")
	}

	firstAttempt := <-received
	reopened, err := OpenStore(a.store.paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	a = &Admin{store: reopened}
	a.health.results = map[string]serviceHealthResult{}
	a.health.events = map[string]healthEventState{}
	a.recordHealthTransition(route, serviceHealthResult{Healthy: true, CheckedAt: now.Add(3 * time.Second)})
	backup := func(success bool) {
		name, message := "", "TEST_ONLY_PRIVATE_SERVER_DETAIL"
		if success {
			name, message = "fixture.zip", ""
		}
		if err := a.store.recordWebDAVBackup("", name, message, success); err != nil {
			t.Fatal(err)
		}
	}
	backup(false)
	backup(false)
	backup(true)
	backup(true)
	cert := CertificateStatus{ID: "cert-1", Host: "tls.example.test", Ready: true, NotAfter: now.Add(7 * 24 * time.Hour)}
	a.checkCertificateAlerts([]CertificateStatus{cert, cert}, now)
	cert.NotAfter = now.Add(-time.Hour)
	cert.Ready = false
	a.checkCertificateAlerts([]CertificateStatus{cert}, now)
	cert.NotAfter = now.Add(90 * 24 * time.Hour)
	cert.Ready = true
	a.checkCertificateAlerts([]CertificateStatus{cert}, now)
	cert.NotAfter = now
	cert.Staging = true
	a.checkCertificateAlerts([]CertificateStatus{cert}, now)
	want := []string{"upstream/firing", "upstream/resolved", "backup/firing", "backup/resolved", "certificate/firing", "certificate/resolved"}
	if len(a.store.Snapshot().Alerts.Pending) != len(want) {
		t.Fatal("incorrect transition count")
	}
	for i, kind := range want {
		if a.deliverWebhook(context.Background()) {
			t.Fatal("delivery failed")
		}
		event := <-received
		if i == 0 && event.ID != firstAttempt.ID {
			t.Fatal("retry changed event identity")
		}
		if kind != event.Type+"/"+event.Status || strings.Contains(event.Message, "PRIVATE") {
			t.Fatal("incorrect or unsanitized event")
		}
	}
	if len(a.store.Snapshot().Alerts.Pending) != 0 || a.store.Snapshot().Alerts.LastError != "" || a.store.Snapshot().Revision != rev+1 {
		t.Fatal("retry changed identity, revision or left pending work")
	}
}

func TestWebhookDestinationAndQueue(t *testing.T) {
	for _, raw := range []string{"http://example.test", "https://user@example.test", "https://169.254.169.254", "http://[fd00:ec2::254]/", "https://example.test:70000", "https://example.test/#fragment", ""} {
		if validateWebhook(raw, true) == nil {
			t.Fatal("invalid destination accepted")
		}
	}
	for _, raw := range []string{"https://hooks.example.test/path?key=fixture", "http://192.168.1.5:8000/inbox", "http://[::1]/hook"} {
		if err := validateWebhook(raw, true); err != nil {
			t.Fatal("valid destination rejected")
		}
	}
	s := alertQueue{Enabled: true}
	for i := 0; i < webhookQueueLimit+1; i++ {
		s.observe("webdav", "backup", "WebDAV", i%2 == 0, "failure", "recovery", time.Now())
	}
	if len(s.Pending) != webhookQueueLimit || s.Dropped != 1 || s.Pending[0].Status != "resolved" {
		t.Fatal("unexpected queue retention")
	}
}

func TestNotificationHistoryAndChannelSubscriptions(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	const path = "/api/notifications/history"
	if adminRequest(h, "GET", path, nil, nil, "").Code != http.StatusUnauthorized {
		t.Fatal("anonymous access to history")
	}
	setup := func(events []string) {
		input := map[string]any{"enabled": true, "url": "https://fixture.example.test/", "revision": a.store.Snapshot().Revision, "events": events}
		if adminRequest(h, "PUT", "/api/notifications/webhook", input, cookie, "").Code != 200 {
			t.Fatal("could not update subscriptions")
		}
	}
	observe := func(key, kind string, failed bool) {
		a.observeAlert(key, kind, "fixture", failed, "failed", "resolved", time.Now())
	}
	observe("webdav", "backup", true)
	setup([]string{"upstream", "certificate"})
	observe("route/a", "upstream", true)
	observe("cert/a", "certificate", true)
	setup([]string{"upstream"})
	for i := 0; i < 100; i++ {
		observe("route/a", "upstream", true)
	}
	observe("route/a", "upstream", false)
	state := a.store.Snapshot()
	if len(state.Alerts.Pending) != 2 || state.Alerts.Pending[0].Status != "firing" || state.Alerts.Pending[1].Status != "resolved" {
		t.Fatal("filtered queue lost or repeated a transition")
	}
	var events []webhookEvent
	response := adminRequest(h, "GET", path, nil, cookie, "")
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &events) != nil || len(events) != 4 || events[0].Type != "backup" || events[3].Status != "resolved" {
		t.Fatal("history lost independent transitions")
	}
	store, err := OpenStore(a.store.paths.Config)
	if err != nil || len(store.Snapshot().NoticeHistory.Events) != 4 {
		t.Fatal("notification history not persisted")
	}
}
