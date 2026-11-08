package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const onlineReleaseCacheTTL = 5 * time.Minute

type githubRateLimitError struct{ retryAt time.Time }

func (e *githubRateLimitError) Error() string {
	return "GitHub 请求额度暂时用尽；可在更新页面配置 GitHub Token 提高 API 额度"
}

func githubRetryTime(header http.Header) time.Time {
	now := time.Now()
	var retry time.Time
	if seconds, err := strconv.ParseInt(header.Get("Retry-After"), 10, 64); err == nil && seconds >= 0 {
		retry = now.Add(time.Duration(seconds)*time.Second + time.Second)
	} else if at, err := http.ParseTime(header.Get("Retry-After")); err == nil {
		retry = at.Add(time.Second)
	}
	if header.Get("X-RateLimit-Remaining") == "0" {
		if seconds, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			if at := time.Unix(seconds, 0).Add(time.Second); at.After(retry) {
				retry = at
			}
		}
	}
	if !retry.After(now) {
		retry = now.Add(time.Minute)
	}
	return retry
}

type onlineReleaseCacheEntry struct {
	key     [32]byte
	release onlineRelease
	err     error
	expires time.Time
}

// One bounded entry per channel and one shared GitHub cooldown.
type onlineReleaseCache struct {
	mu           sync.Mutex
	stable, beta onlineReleaseCacheEntry
	limitKey     [32]byte
	limit        *githubRateLimitError
	flight       singleflight.Group
}

func onlineReleaseCacheKey(state State) [32]byte {
	settings, _ := json.Marshal([]any{state.Config.OutboundProxy, state.ProxyPassword, state.GitHubToken})
	return sha256.Sum256(settings)
}

func (c *onlineReleaseCache) noteLimit(state State, err error) {
	var limited *githubRateLimitError
	if !errors.As(err, &limited) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := onlineReleaseCacheKey(state)
	if c.limitKey == key && c.limit != nil && c.limit.retryAt.After(limited.retryAt) {
		return
	}
	c.limitKey, c.limit = key, limited
}

func (c *onlineReleaseCache) entry(channel updateChannel) *onlineReleaseCacheEntry {
	if channel == betaChannel {
		return &c.beta
	}
	return &c.stable
}

func (c *onlineReleaseCache) cached(key [32]byte, channel updateChannel) (onlineRelease, error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.limitKey == key && c.limit != nil && time.Now().Before(c.limit.retryAt) {
		return onlineRelease{}, c.limit, true
	}
	entry := c.entry(channel)
	return entry.release, entry.err, entry.key == key && time.Now().Before(entry.expires)
}

func (c *onlineReleaseCache) latest(ctx context.Context, client *http.Client, state State, channel updateChannel) (onlineRelease, error) {
	key := onlineReleaseCacheKey(state)
	if release, err, ok := c.cached(key, channel); ok {
		return release, err
	}
	result := c.flight.DoChan(hex.EncodeToString(key[:])+string(channel), func() (any, error) {
		if release, err, ok := c.cached(key, channel); ok {
			return release, err
		}
		// A disconnected caller must not cancel a check shared by other callers.
		checkCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		check := latestOnlineRelease
		if channel == betaChannel {
			check = latestBetaRelease
		}
		release, err := check(checkCtx, client, state.GitHubToken)
		now := time.Now()
		expires := now.Add(onlineReleaseCacheTTL)
		if err == nil {
			release.CheckedAt, release.CacheUntil = now, expires
		} else {
			expires = now.Add(10 * time.Second)
			c.noteLimit(state, err)
		}
		c.mu.Lock()
		if c.limitKey == key && c.limit != nil && time.Now().Before(c.limit.retryAt) {
			limited := c.limit
			c.mu.Unlock()
			return onlineRelease{}, limited
		}
		*c.entry(channel) = onlineReleaseCacheEntry{key: key, release: release, err: err, expires: expires}
		c.mu.Unlock()
		return release, err
	})
	select {
	case <-ctx.Done():
		return onlineRelease{}, ctx.Err()
	case result := <-result:
		return result.Val.(onlineRelease), result.Err
	}
}

func onlineCheckError(w http.ResponseWriter, err error) {
	var limited *githubRateLimitError
	if errors.As(err, &limited) {
		if logged, ok := w.(*loggedResponseWriter); ok {
			logged.errorMessage = err.Error()
		}
		jsonResponse(w, 502, map[string]any{"error": err.Error(), "retry_at": limited.retryAt})
		return
	}
	apiError(w, 502, err.Error())
}
