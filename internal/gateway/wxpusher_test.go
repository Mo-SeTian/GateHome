package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type wxTestTransport func(*http.Request) (*http.Response, error)

func (f wxTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWxPusherChecksTargetAcknowledgment(t *testing.T) {
	settings := wxPusherState{AppToken: "AT_FIXTURE", UID: "UID_FIXTURE"}
	for _, tc := range []struct {
		response string
		accepted bool
	}{
		{`{"code":1000,"data":[{"uid":"UID_FIXTURE","code":1000}]}`, true},
		{`{"code":1000,"data":[{"uid":"UID_FIXTURE","code":1001}]}`, false},
		{`{"code":1000,"data":[{"uid":"UID_OTHER","code":1000}]}`, false},
		{`{"code":1001,"msg":"AT_FIXTURE"}`, false},
	} {
		client := &http.Client{Transport: wxTestTransport(func(r *http.Request) (*http.Response, error) {
			var body struct {
				AppToken, Content string
				ContentType       int
				UIDs              []string
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || r.Method != "POST" || r.URL.String() != wxPusherEndpoint ||
				body.AppToken != settings.AppToken || body.ContentType != 1 || len(body.UIDs) != 1 || body.UIDs[0] != settings.UID ||
				!strings.Contains(body.Content, "nas.example.test") || strings.Contains(body.Content, settings.AppToken) {
				t.Fatal("incorrect WxPusher request")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.response))}, nil
		})}
		err := sendWxPusher(context.Background(), client, settings, webhookEvent{Resource: "nas.example.test", Message: "上游失败", Time: time.Now()})
		if (err == nil) != tc.accepted || (err != nil && strings.Contains(err.Error(), settings.AppToken)) {
			t.Fatal("incorrect or unsafe acknowledgment")
		}
	}
}

func TestWxPusherSettingsAndEventFiltering(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	const path = "/api/notifications/wxpusher"
	revision := a.store.Snapshot().Revision
	body := map[string]any{"enabled": true, "uid": "UID_ONLY_FOR_TEST", "app_token": "AT_ONLY_FOR_TEST", "events": []string{"upstream"}, "revision": revision}
	if adminRequest(h, "PUT", path, body, nil, "").Code != 401 {
		t.Fatal("anonymous push settings access")
	}
	saved := adminRequest(h, "PUT", path, body, cookie, "")
	if saved.Code != 200 || strings.Contains(saved.Body.String(), "AT_ONLY_FOR_TEST") {
		t.Fatal("saving failed or exposed token")
	}
	now := time.Now()
	for _, failed := range []bool{true, true, false, false} {
		a.observeAlert("route/fixture", "upstream", "fixture", failed, "failure", "recovery", now)
		a.observeAlert("webdav", "backup", "WebDAV", failed, "failure", "recovery", now)
	}
	state := a.store.Snapshot()
	if len(state.Alerts.Pending) != 0 || len(state.WxPusher.Pending) != 2 || state.WxPusher.Pending[0].Status != "firing" || state.WxPusher.Pending[1].Status != "resolved" {
		t.Fatal("push queue ignored event filter or duplicated a transition")
	}
	delete(body, "app_token")
	body["events"] = []string{"upstream", "backup"}
	body["revision"] = state.Revision
	if adminRequest(h, "PUT", path, body, cookie, "").Code != 200 {
		t.Fatal("changing event filter failed")
	}
	state = a.store.Snapshot()
	if state.WxPusher.AppToken != "AT_ONLY_FOR_TEST" || len(state.WxPusher.Pending) != 2 {
		t.Fatal("editing event filter lost the token or pending notifications")
	}
}
