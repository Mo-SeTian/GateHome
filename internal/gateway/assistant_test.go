package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func readSSEEvents(t *testing.T, body io.Reader) [][2]string {
	t.Helper()
	var events [][2]string
	scanner := bufio.NewScanner(body)
	var ev, data string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if ev != "" || data != "" {
				events = append(events, [2]string{ev, data})
				ev, data = "", ""
			}
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			ev = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

func TestAssistantStreamingProtocolsJSONExtractionAndRedaction(t *testing.T) {
	secretKey := "sk-stream-secret-9988"
	for _, tc := range []struct {
		name     string
		protocol string
		chunks   []string
		wantText string
		wantLink string
	}{
		{
			name:     "chat JSON envelope action before text and split secret",
			protocol: "chat",
			chunks: []string{
				`data: {"choices":[{"delta":{"content":"{\"action\":\"health_settings\",\"text\":\"你好\\n"}}]}`,
				`data: {"choices":[{"delta":{"content":"密钥 sk-st"}}]}`,
				fmt.Sprintf(`data: {"choices":[{"delta":{"content":"%s 表情\\uD83D\\uDE00\"}"}}]}`, "ream-secret-9988"),
				`data: [DONE]`,
			},
			wantText: "你好\n密钥 <REDACTED> 表情😀",
			wantLink: "/routes",
		},
		{
			name:     "responses fenced plain text",
			protocol: "responses",
			chunks: []string{
				`data: {"type":"response.output_text.delta","delta":"### 诊断结论\n"}`,
				`data: {"type":"response.output_text.delta","delta":"一切正常"}`,
				`data: {"type":"response.completed","response":{"output":[{"content":[{"type":"output_text","text":"ignored"}]}]}}`,
			},
			wantText: "### 诊断结论\n一切正常",
		},
		{
			name:     "anthropic text_delta streaming",
			protocol: "anthropic",
			chunks: []string{
				`data: {"type":"content_block_start","content_block":{"type":"text","text":""}}`,
				`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"{\"text\":\"已检查"}}`,
				`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"运行态\"}"}}`,
				`data: {"type":"message_stop"}`,
			},
			wantText: "已检查运行态",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				flusher := w.(http.Flusher)
				for _, line := range tc.chunks {
					_, _ = fmt.Fprintf(w, "%s\n\n", line)
					flusher.Flush()
				}
			}))
			defer upstream.Close()

			a, h := testAdmin(t)
			cookie := loginForTest(t, h)
			state := a.store.Snapshot()
			settings := assistantSettings{assistantEndpoint: assistantEndpoint{
				Name: "流式测试", Protocol: tc.protocol, Base: upstream.URL, Model: "test-model", Key: secretKey,
			}, Revision: state.Revision}
			if w := adminRequest(h, "PUT", "/api/assistant/settings", settings, cookie, ""); w.Code != http.StatusOK {
				t.Fatalf("PUT settings failed: %d", w.Code)
			}
			endpoint, _ := assistantProvider(a.store.Snapshot(), "default")
			body := map[string]any{
				"page": "/runtime", "provider": endpoint.ID, "binding": endpoint.binding(),
				"model": endpoint.Model, "stream": true,
				"messages": []assistantMessage{{Role: "user", Content: "请检查状态"}},
			}
			w := adminRequest(h, "POST", "/api/assistant/chat", body, cookie, "")
			if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
				t.Fatalf("expected SSE 200 response, got %d %q", w.Code, w.Header().Get("Content-Type"))
			}
			events := readSSEEvents(t, w.Body)
			if len(events) < 2 {
				t.Fatalf("expected multiple SSE events, got %v", events)
			}
			var streamed strings.Builder
			var donePayload map[string]any
			for _, ev := range events {
				var obj map[string]any
				if err := json.Unmarshal([]byte(ev[1]), &obj); err != nil {
					t.Fatalf("invalid SSE JSON %q: %v", ev[1], err)
				}
				switch ev[0] {
				case "delta":
					streamed.WriteString(obj["text"].(string))
				case "done":
					donePayload = obj
				}
			}
			if streamed.String() != tc.wantText {
				t.Fatalf("streamed deltas = %q, want %q", streamed.String(), tc.wantText)
			}
			if strings.Contains(streamed.String(), secretKey) {
				t.Fatal("leaked API key in streamed deltas")
			}
			if donePayload == nil || donePayload["text"] != tc.wantText {
				t.Fatalf("unexpected done payload: %#v", donePayload)
			}
			if tc.wantLink != "" {
				link, _ := donePayload["link"].(map[string]any)
				if link == nil || link["path"] != tc.wantLink {
					t.Fatalf("expected link %q in done payload, got %#v", tc.wantLink, donePayload)
				}
			}
		})
	}
}

func TestAssistantSSRFProtectionAnthropicNormalizationAndBackupValidation(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	a.adminPort = 18999

	meta := assistantSettings{assistantEndpoint: assistantEndpoint{
		Name: "ssrf", Base: "http://169.254.169.254/latest", Model: "m",
	}}
	if w := adminRequest(h, "POST", "/api/assistant/models", meta, cookie, ""); w.Code != 502 {
		t.Fatalf("metadata SSRF expected HTTP 502, got %d: %s", w.Code, w.Body.String())
	}

	selfTarget := assistantSettings{assistantEndpoint: assistantEndpoint{
		Name: "self", Base: "http://127.0.0.1:18999/v1", Model: "m",
	}}
	if w := adminRequest(h, "POST", "/api/assistant/test", selfTarget, cookie, ""); w.Code != 400 {
		t.Fatalf("admin self-probe expected HTTP 400, got %d", w.Code)
	}
	u, _ := url.Parse("http://0.0.0.0:18999/v1")
	if !isForbiddenAssistantTarget(u, 18999, 0, localAdminAddresses()) {
		t.Fatal("0.0.0.0 admin port should be blocked for assistant")
	}

	normalized, err := normalizeAnthropicMessages([]assistantMessage{
		{Role: "assistant", Content: "stale 1"},
		{Role: "assistant", Content: "stale 2"},
		{Role: "user", Content: "u1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u3"},
	})
	if err != nil || len(normalized) != 3 || normalized[0].Role != "user" || normalized[0].Content != "u1\n\nu2" {
		t.Fatalf("unexpected Anthropic normalization: %#v err=%v", normalized, err)
	}

	badState := State{
		Config:             DefaultConfig(),
		AssistantProviders: []assistantEndpoint{{ID: "new", Name: "x", Base: "https://api.example.test/v1", Model: "m"}},
	}
	if err := validateCredentials(badState); err == nil {
		t.Fatal("reserved provider ID in backup state must be rejected")
	}
	badAssistant := State{
		Config:    DefaultConfig(),
		Assistant: assistantEndpoint{Name: "x", Base: "ftp://bad", Model: "m"},
	}
	if err := validateCredentials(badAssistant); err == nil {
		t.Fatal("invalid assistant URL in backup state must be rejected")
	}
}
