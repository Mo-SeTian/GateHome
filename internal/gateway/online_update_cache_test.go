package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestOnlineReleaseCacheExpiryAndSettings(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		metadata, _ := json.Marshal(onlineTestMetadata(t, []byte("TEST_ONLY_ARCHIVE")))
		var calls int
		client := &http.Client{Transport: onlineTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return onlineTestClient(metadata).Transport.RoundTrip(r)
		})}
		cache := onlineReleaseCache{}
		state := State{}
		first, err := cache.latest(context.Background(), client, state)
		if err != nil || first.CheckedAt.IsZero() || first.CacheUntil.Sub(first.CheckedAt) != 5*time.Minute {
			t.Fatal("successful check did not provide its freshness window")
		}
		state.Revision++
		second, err := cache.latest(context.Background(), client, state)
		if err != nil || calls != 1 || second.CheckedAt != first.CheckedAt {
			t.Fatal("repeated checks or unrelated config changes bypassed the cache")
		}
		time.Sleep(5*time.Minute + time.Second)
		if _, err := cache.latest(context.Background(), client, state); err != nil || calls != 2 {
			t.Fatal("expired version information was reused")
		}
		for i := 0; i < 5; i++ {
			switch i {
			case 0:
				state.GitHubToken = "TEST_ONLY_GITHUB_TOKEN"
			case 1:
				state.Config.OutboundProxy.Enabled = true
			case 2:
				state.Config.OutboundProxy.URL = "http://proxy.example.test:7890"
			case 3:
				state.Config.OutboundProxy.Username = "test-proxy-user"
			case 4:
				state.ProxyPassword = "TEST_ONLY_PROXY_PASSWORD"
			}
			release, err := cache.latest(context.Background(), client, state)
			encoded, _ := json.Marshal(release)
			if err != nil || calls != i+3 || bytes.Contains(encoded, []byte("TEST_ONLY_")) {
				t.Fatal("connection settings did not invalidate the cache or exposed credentials")
			}
		}
	})
}

func TestOnlineReleaseCacheCoalescesAndSurvivesCallerCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		metadata, _ := json.Marshal(onlineTestMetadata(t, []byte("TEST_ONLY_ARCHIVE")))
		started, finish := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		client := &http.Client{Transport: onlineTestTransport(func(r *http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				close(started)
			}
			select {
			case <-finish:
				return onlineTestClient(metadata).Transport.RoundTrip(r)
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		})}
		cache := onlineReleaseCache{}
		ctx, cancel := context.WithCancel(context.Background())
		first, second := make(chan error, 1), make(chan error, 1)
		go func() { _, err := cache.latest(ctx, client, State{}); first <- err }()
		<-started
		go func() { _, err := cache.latest(context.Background(), client, State{}); second <- err }()
		synctest.Wait()
		cancel()
		if err := <-first; !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled caller did not return promptly")
		}
		close(finish)
		if err := <-second; err != nil || calls.Load() != 1 {
			t.Fatal("concurrent callers did not share the surviving version check")
		}
	})
}

func TestOnlineReleaseCacheTransientFailureRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		metadata, _ := json.Marshal(onlineTestMetadata(t, []byte("TEST_ONLY_ARCHIVE")))
		var calls int
		client := &http.Client{Transport: onlineTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return nil, errors.New("TEST_ONLY_PRIVATE_TRANSPORT_ERROR")
			}
			return onlineTestClient(metadata).Transport.RoundTrip(r)
		})}
		cache := onlineReleaseCache{}
		for i := 0; i < 2; i++ {
			if _, err := cache.latest(context.Background(), client, State{}); err == nil || strings.Contains(err.Error(), "TEST_ONLY_") || calls != 1 {
				t.Fatal("a transient failure bypassed the cache or exposed transport details")
			}
		}
		time.Sleep(11 * time.Second)
		if _, err := cache.latest(context.Background(), client, State{}); err != nil || calls != 2 {
			t.Fatal("checks did not recover after the short failure cache expired")
		}
	})
}

func TestOnlineReleaseCacheRateLimitCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		metadata, _ := json.Marshal(onlineTestMetadata(t, []byte("TEST_ONLY_ARCHIVE")))
		reset := time.Now().Add(90 * time.Second).Truncate(time.Second)
		var calls int
		client := &http.Client{Transport: onlineTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls > 1 {
				return onlineTestClient(metadata).Transport.RoundTrip(r)
			}
			header := make(http.Header)
			header.Set("Retry-After", "10")
			header.Set("X-RateLimit-Remaining", "0")
			header.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
			return &http.Response{StatusCode: 403, Header: header, Body: io.NopCloser(strings.NewReader("TEST_ONLY_PRIVATE_RESPONSE")), Request: r}, nil
		})}
		cache := onlineReleaseCache{}
		_, err := cache.latest(context.Background(), client, State{})
		var limited *githubRateLimitError
		if !errors.As(err, &limited) || !limited.retryAt.Equal(reset.Add(time.Second)) {
			t.Fatal("GitHub reset time was not respected")
		}
		w := httptest.NewRecorder()
		onlineCheckError(w, err)
		job := onlineUpdateJob{}
		job.setFailure(err)
		if !strings.Contains(w.Body.String(), `"retry_at"`) || strings.Contains(w.Body.String(), "TEST_ONLY_") || job.snapshot().RetryAt == nil {
			t.Fatal("rate limit recovery time was missing or exposed response contents")
		}
		time.Sleep(60 * time.Second)
		if _, err := cache.latest(context.Background(), client, State{}); err == nil || calls != 1 {
			t.Fatal("a check ignored the GitHub cooldown")
		}
		time.Sleep(32 * time.Second)
		if _, err := cache.latest(context.Background(), client, State{}); err != nil || calls != 2 {
			t.Fatal("checks did not recover after the rate limit reset")
		}
	})
}

func TestOnlineReleaseCacheDownloadLimitWinsOverInFlightCheck(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		metadata, _ := json.Marshal(onlineTestMetadata(t, []byte("TEST_ONLY_ARCHIVE")))
		started, finish := make(chan struct{}), make(chan struct{})
		var calls int
		client := &http.Client{Transport: onlineTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				close(started)
				<-finish
			}
			return onlineTestClient(metadata).Transport.RoundTrip(r)
		})}
		cache := onlineReleaseCache{}
		result := make(chan error, 1)
		go func() { _, err := cache.latest(context.Background(), client, State{}); result <- err }()
		<-started
		cache.noteLimit(State{}, &githubRateLimitError{retryAt: time.Now().Add(time.Minute)})
		close(finish)
		if err := <-result; err == nil {
			t.Fatal("an earlier version request discarded a download rate limit")
		}
		if _, err := cache.latest(context.Background(), client, State{}); err == nil || calls != 1 {
			t.Fatal("download cooldown was bypassed")
		}
		if _, err := cache.latest(context.Background(), client, State{GitHubToken: "TEST_ONLY_NEW_TOKEN"}); err != nil || calls != 2 {
			t.Fatal("new credentials could not recover from a cached rate limit")
		}
	})
}

func TestOnlineUpdateReusesOnlyVerifiedFreshStage(t *testing.T) {
	m, err := NewMaintenance(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.inspectUpdateVersion(testRelease(t, "99.0.0", releaseArch()), "99.0.0")
	if err != nil {
		t.Fatal(err)
	}
	id := result["id"].(string)
	release := onlineRelease{Version: "99.0.0", digest: strings.Repeat("a", 64)}
	job := onlineUpdateJob{preparedID: id, preparedDigest: release.digest}
	if job.preparedStage(m, release) != id {
		t.Fatal("a previously verified package could not be reused")
	}
	changed := release
	changed.digest = strings.Repeat("b", 64)
	if job.preparedStage(m, changed) != "" {
		t.Fatal("a changed release digest reused the old program")
	}
	path := filepath.Join(m.dir, "update-staged")
	original, _ := os.ReadFile(path)
	os.WriteFile(path, []byte("TEST_ONLY_CORRUPTED_PROGRAM"), 0600)
	if job.preparedStage(m, release) != "" {
		t.Fatal("a corrupted staged program was reused")
	}
	os.WriteFile(path, original, 0600)
	m.stage.Created = time.Now().Add(-16 * time.Minute)
	if job.preparedStage(m, release) != "" {
		t.Fatal("an expired staged program was reused")
	}
	m.stage.Created = time.Now()
	m.stage.Kind = "restore"
	if job.preparedStage(m, release) != "" {
		t.Fatal("a different maintenance operation was reused")
	}
}
