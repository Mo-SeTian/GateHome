package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAssistantEndpointCredentialScope(t *testing.T) {
	previous := assistantEndpoint{Name: "local", Base: "https://example.test/v1", Model: "model", Key: "TEST_ONLY_KEY"}
	input := assistantSettings{assistantEndpoint: previous}
	input.Key = ""
	input.Base += "/"
	result, err := input.endpoint(previous)
	if err != nil || result.Key != previous.Key {
		t.Fatal("same normalized endpoint should retain the saved credential")
	}
	input.Base = "https://another.example.test/v1"
	result, err = input.endpoint(previous)
	if err != nil || result.Key != "" {
		t.Fatal("a different endpoint must never inherit the credential")
	}
	input.Base = previous.Base
	input.ClearKey = true
	result, err = input.endpoint(previous)
	if err != nil || result.Key != "" {
		t.Fatal("clearing a saved credential should work without resubmitting it")
	}
	input.Base = "https://example.test/v1?token=TEST_ONLY"
	if _, err := input.endpoint(previous); err == nil {
		t.Fatal("endpoint with query parameters should be rejected")
	}
}

func TestAssistantProposalScopesAndValidatesChanges(t *testing.T) {
	config := DefaultConfig()
	config.Routes = []Route{{ID: "album", GroupID: "default", Host: "album.example.test", Upstream: "http://[::1]:2283", Enabled: true}}
	state := State{Config: config, Revision: 7}
	reply := assistantReply{Action: "route_port", Target: "album", Value: "2284"}
	change := assistantProposal(state, reply)
	if change == nil || change.Revision != 7 || change.Before != "http://[::1]:2283" || change.After != "http://[::1]:2284" {
		t.Fatalf("wrong proposal: %#v", change)
	}
	if state.Config.Routes[0].Upstream != change.Before {
		t.Fatal("a proposal must not mutate the current configuration")
	}
	reply.Value = "65536"
	if assistantProposal(state, reply) != nil {
		t.Fatal("an invalid port should not produce a proposal")
	}
	reply.Target, reply.Value = "missing", "2284"
	if assistantProposal(state, reply) != nil {
		t.Fatal("an unknown target should not produce a proposal")
	}
	change = assistantProposal(state, assistantReply{Action: "health_interval", Value: "90"})
	if change == nil || change.Before != "60" || change.After != "90" {
		t.Fatal("older configurations should preview their effective default interval")
	}
}

func TestAssistantChatIncludesLiveReverseProxyConnections(t *testing.T) {
	type snapshot struct {
		SampledAt time.Time `json:"sampled_at"`
		Traffic   struct {
			Total        proxyTrafficMetrics         `json:"total"`
			RatesSampled bool                        `json:"rates_sampled"`
			Routes       []assistantRouteConnections `json:"routes"`
		} `json:"proxy_traffic"`
	}
	snapshots := make(chan snapshot, 2)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []assistantMessage `json:"messages"`
		}
		var data snapshot
		if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Messages) < 2 {
			http.Error(w, "missing messages", http.StatusBadRequest)
			return
		}
		_, raw, ok := strings.Cut(request.Messages[0].Content, "\n网关快照：")
		if !ok || json.Unmarshal([]byte(raw), &data) != nil {
			http.Error(w, "missing live snapshot", http.StatusBadRequest)
			return
		}
		snapshots <- data
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"已读取实时连接"}}]}`)
	}))
	defer model.Close()

	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()

	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	config := a.store.Snapshot().Config
	group := config.Groups[0]
	config.Routes = []Route{
		{ID: "files", GroupID: group.ID, Name: "文件存储", Host: "files.example.test", Upstream: upstream.URL, Enabled: true},
		{ID: "photos", GroupID: group.ID, Name: "家庭相册", Host: "photos.example.test", Upstream: upstream.URL, Enabled: true},
		{ID: "idle", GroupID: group.ID, Host: "idle.example.test", Upstream: upstream.URL, Enabled: true},
	}
	if err := a.store.Update(config, nil, a.store.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	state := a.store.Snapshot()
	if err := a.proxy.ConfigureState(state); err != nil {
		t.Fatal(err)
	}
	settings := assistantSettings{assistantEndpoint: assistantEndpoint{Name: "测试模型", Base: model.URL, Model: "fixture"}, Revision: state.Revision}
	if w := adminRequest(h, "PUT", "/api/assistant/settings", settings, cookie, ""); w.Code != http.StatusOK {
		t.Fatalf("saving model settings returned HTTP %d", w.Code)
	}

	endpoint, _ := assistantProvider(a.store.Snapshot(), "default")

	proxy := httptest.NewUnstartedServer(a.proxy)
	proxy.Config.ConnContext = ProxyConnContext
	proxy.Start()
	defer proxy.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 3)
	for _, host := range []string{"files.example.test", "files.example.test", "photos.example.test"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		go func() {
			res, err := proxy.Client().Do(req)
			if err == nil {
				err = res.Body.Close()
			}
			done <- err
		}()
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("request did not reach reverse proxy upstream")
		}
	}

	ask := func() snapshot {
		t.Helper()
		body := map[string]any{
			"page":     "/routes",
			"provider": endpoint.ID, "binding": endpoint.binding(), "model": endpoint.Model,
			"messages": []assistantMessage{{Role: "user", Content: "反代的实时连接数有多少，分别是哪些规则的？"}},
		}
		if w := adminRequest(h, "POST", "/api/assistant/chat", body, cookie, ""); w.Code != http.StatusOK {
			t.Fatalf("asking the assistant returned HTTP %d", w.Code)
		}
		return <-snapshots
	}
	live := ask()
	want := []assistantRouteConnections{
		{ID: "files", Name: "文件存储", Group: group.Name, Host: "files.example.test", Active: 2},
		{ID: "photos", Name: "家庭相册", Group: group.Name, Host: "photos.example.test", Active: 1},
		{ID: "idle", Name: "idle.example.test", Group: group.Name, Host: "idle.example.test", Active: 0},
	}
	if live.SampledAt.IsZero() || live.Traffic.RatesSampled || live.Traffic.Total.Active != 3 || !reflect.DeepEqual(live.Traffic.Routes, want) {
		t.Fatalf("assistant did not receive the current connections and rule names before the first rate sample: %+v", live)
	}

	close(release)
	for range 3 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	waitForProxyConnectionsToClose(t, a.proxy)
	idle := ask()
	if idle.Traffic.Total.Active != 0 || len(idle.Traffic.Routes) != 3 {
		t.Fatalf("assistant did not receive the new idle snapshot: %+v", idle)
	}
	for _, route := range idle.Traffic.Routes {
		if route.Active != 0 {
			t.Fatalf("assistant still sees a connection for rule %s", route.ID)
		}
	}
}
