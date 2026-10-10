package gateway

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestGitHubTokenConfigPersistenceAndRedaction(t *testing.T) {
	a, h := testAdmin(t)
	cookie := loginForTest(t, h)
	token := "TEST_ONLY_GITHUB_TOKEN"
	put := func(value *string) {
		t.Helper()
		state := a.store.Snapshot()
		body := map[string]any{"config": state.Config, "revision": state.Revision}
		if value != nil {
			body["github_token"] = *value
		}
		w := adminRequest(h, "PUT", "/api/config", body, cookie, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), token) {
			t.Fatal("GitHub token save or redaction failed")
		}
	}
	put(&token)
	put(nil)
	if a.store.Snapshot().GitHubToken != token {
		t.Fatal("unrelated config save lost GitHub token")
	}
	w := adminRequest(h, "GET", "/api/config", nil, cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"github_token_configured":true`) || strings.Contains(w.Body.String(), token) {
		t.Fatal("config did not hide saved GitHub token")
	}
	store, err := OpenStorePaths(a.store.paths)
	if err != nil || store.Snapshot().GitHubToken != token {
		t.Fatal("GitHub token did not survive reopening config")
	}
	info, err := os.Stat(a.store.path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("saved GitHub token must use private config permissions")
	}
	for _, invalid := range []string{strings.Repeat("x", 513), "TEST_ONLY_\nTOKEN", "TEST_ONLY_ TOKEN", "TEST_ONLY_\x00TOKEN"} {
		state := a.store.Snapshot()
		w := adminRequest(h, "PUT", "/api/config", map[string]any{"config": state.Config, "revision": state.Revision, "github_token": invalid}, cookie, "")
		if w.Code != 400 || a.store.Snapshot().Revision != state.Revision || strings.Contains(w.Body.String(), invalid) {
			t.Fatal("invalid GitHub token accepted or exposed")
		}
	}
	empty := ""
	put(&empty)
	w = adminRequest(h, "GET", "/api/config", nil, cookie, "")
	if a.store.Snapshot().GitHubToken != "" || !strings.Contains(w.Body.String(), `"github_token_configured":false`) {
		t.Fatal("GitHub token was not cleared")
	}
}

func TestGitHubTokenErrorsAndHostRestriction(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		client := onlineTestClient(nil)
		client.Transport = onlineTestTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{"X-Ratelimit-Remaining": {"0"}}, Body: io.NopCloser(strings.NewReader("TEST_ONLY_GITHUB_TOKEN")), Request: r}, nil
		})
		_, err := latestOnlineRelease(context.Background(), client, "TEST_ONLY_GITHUB_TOKEN")
		if err == nil || strings.Contains(err.Error(), "TEST_ONLY_") || !strings.Contains(err.Error(), "GitHub") {
			t.Fatal("GitHub token or rate limit error was not reported safely")
		}
		if status == 401 && !strings.Contains(err.Error(), "无效或已过期") {
			t.Fatal("invalid GitHub token was not explained")
		}
		if status != 401 && !strings.Contains(err.Error(), "额度暂时用尽") {
			t.Fatal("rate limit was not explained")
		}
	}
	client := onlineUpdateClient(State{})
	client.Transport = onlineTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() == "api.github.com" {
			if r.Header.Get("Authorization") == "" {
				t.Fatal("API request did not authenticate")
			}
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://github.com/test-download"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatal("GitHub token leaked to a non-API host")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
	})
	if _, err := fetchOnlineUpdate(context.Background(), client, onlineReleaseAPI, 1024, "TEST_ONLY_GITHUB_TOKEN"); err != nil {
		t.Fatal("trusted download redirect failed")
	}
	if _, err := fetchOnlineUpdate(context.Background(), client, "https://example.com/test", 1024, "TEST_ONLY_GITHUB_TOKEN"); err == nil {
		t.Fatal("token request to an untrusted host accepted")
	}
}
