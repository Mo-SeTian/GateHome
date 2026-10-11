package gateway

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestServiceHealthTLSRedirectAndHTTPStates(t *testing.T) {
	var secureCalls atomic.Int32
	secure := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { secureCalls.Add(1); w.WriteHeader(200) }))
	secure.Config.ErrorLog = log.New(io.Discard, "", 0)
	secure.StartTLS()
	defer secure.Close()
	result := probeServiceHealth(context.Background(), secure.URL)
	if result.State != "tls_untrusted" || result.TLSVerified || result.Healthy {
		t.Fatal("untrusted TLS accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, secure.URL, 302)
		case "/auth":
			w.WriteHeader(401)
		case "/error":
			w.WriteHeader(503)
		case "/head":
			if r.Method == "HEAD" {
				w.WriteHeader(405)
			} else {
				w.WriteHeader(200)
			}
		default:
			w.WriteHeader(200)
		}
	}))
	defer server.Close()
	for _, test := range []struct {
		path, state string
		healthy     bool
	}{{"/", "healthy", true}, {"/redirect", "redirect", true}, {"/auth", "auth_required", true}, {"/error", "http_error", false}, {"/head", "healthy", true}} {
		row := probeServiceHealth(context.Background(), server.URL+test.path)
		if row.State != test.state || row.Healthy != test.healthy {
			t.Fatal("HTTP health classification incorrect")
		}
	}
	if secureCalls.Load() != 0 {
		t.Fatal("redirect reached untrusted HTTPS handler")
	}
}

func TestServiceHealthCacheDeduplicatesAndCancels(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer server.Close()
	a, _ := testAdmin(t)
	var work sync.WaitGroup
	for i := 0; i < 12; i++ {
		work.Add(1)
		go func() {
			defer work.Done()
			if !a.cachedServiceHealth(context.Background(), server.URL, time.Minute).Healthy {
				t.Error("health request failed")
			}
		}()
	}
	work.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate probes not collapsed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := a.cachedServiceHealth(ctx, server.URL+"/cancel", 0)
	if result.State != "cancelled" {
		t.Fatal("cancelled probe reported success")
	}
}

func TestServiceHealthNotificationsAreDeduplicated(t *testing.T) {
	a, _ := testAdmin(t)
	route := Route{ID: "fixture-route", Upstream: "http://127.0.0.1/"}
	a.cachedServiceHealth(context.Background(), "http://127.0.0.1:1/", time.Minute)
	down := serviceHealthResult{State: "connection_error", Label: "连接失败", CheckedAt: time.Now().UTC()}
	a.recordHealthTransition(route, down)
	a.recordHealthTransition(route, down)
	if a.health.events[route.ID].failures != 1 {
		t.Fatal("same observation counted twice")
	}
	down.CheckedAt = down.CheckedAt.Add(time.Second)
	a.recordHealthTransition(route, down)
	if !a.health.events[route.ID].down {
		t.Fatal("two failures did not mark outage")
	}
	recovered := serviceHealthResult{State: "healthy", Healthy: true, CheckedAt: down.CheckedAt.Add(time.Second)}
	a.recordHealthTransition(route, recovered)
	if a.health.events[route.ID].down || len(a.health.notices) != 2 || a.health.notices[0].Healthy || !a.health.notices[1].Healthy {
		t.Fatal("deduplicated outage/recovery notices incorrect")
	}
}

func TestPanelHealthRequiresLoginAndSavedOwnedIDs(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sunpanel/api/user/getInfo" {
			if r.Header.Get("token") != "TEST_ONLY_PANEL_SESSION" {
				jsonResponse(w, 200, map[string]any{"code": 1001})
				return
			}
			jsonResponse(w, 200, map[string]any{"code": 0, "data": map[string]uint{"id": 7}})
			return
		}
		var input struct {
			UserID uint `json:"userId"`
		}
		json.NewDecoder(r.Body).Decode(&input)
		if input.UserID != 7 {
			t.Error("worker request used another account")
		}
		jsonResponse(w, 200, map[string]any{"data": map[string]any{"items": []map[string]any{{"id": 11, "url": target.URL, "lanUrl": target.URL}}}})
	}))
	defer worker.Close()
	a, _ := testAdmin(t)
	a.sunPanel = &SunPanel{bridgeURL: worker.URL, bridgeKey: make([]byte, 32)}
	call := func(ids string, token string) map[string]any {
		request := httptest.NewRequest("POST", "/sunpanel/api/site/health", strings.NewReader(ids))
		request.Header.Set("token", token)
		response := httptest.NewRecorder()
		a.sunPanelSiteHealth(response, request)
		var result map[string]any
		json.Unmarshal(response.Body.Bytes(), &result)
		return result
	}
	if call(`{"ids":[11]}`, "")["code"] == float64(0) {
		t.Fatal("anonymous probing allowed")
	}
	if targetCalls.Load() != 0 {
		t.Fatal("target contacted before authentication")
	}
	if len(call(`{"ids":[999],"userId":99}`, "TEST_ONLY_PANEL_SESSION")["data"].([]any)) != 0 || targetCalls.Load() != 0 {
		t.Fatal("unowned URL probed")
	}
	result := call(`{"ids":[11],"url":"http://169.254.169.254/"}`, "TEST_ONLY_PANEL_SESSION")
	if result["code"] != float64(0) || targetCalls.Load() != 1 || result["data"].([]any)[0].(map[string]any)["url"] != target.URL {
		t.Fatal("saved owned URL not used or duplicate probe")
	}
}
