package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func betaTestMetadata(t *testing.T, version string) map[string]any {
	t.Helper()
	metadata := onlineTestMetadata(t, []byte("TEST_ONLY_ARCHIVE"))
	metadata["tag_name"], metadata["prerelease"] = "v"+version, true
	asset := metadata["assets"].([]map[string]any)[0]
	asset["name"] = "gatehouse-" + version + "-update.zip"
	asset["browser_download_url"] = onlineReleaseBase + "download/v" + version + "/" + asset["name"].(string)
	return metadata
}

func TestBetaVersionOrderingAndPackageValidation(t *testing.T) {
	versions := []string{"0.0.65", "0.0.66-beta.1", "0.0.66-beta.2", "0.0.66-beta.10", "0.0.66", "0.0.67-beta.1"}
	for i, a := range versions {
		for j, b := range versions {
			got, err := compareVersions(a, b)
			if err != nil || (got < 0) != (i < j) || (got > 0) != (i > j) {
				t.Fatalf("incorrect ordering of %s and %s", a, b)
			}
		}
	}
	for _, version := range []string{"0.0.66-beta.0", "0.0.66-beta.01", "0.0.66-rc.1", "0.0.66-beta", "v0.0.66-beta.1", "0.0.66-beta.1+build"} {
		if _, err := compareVersions(version, Version); err == nil {
			t.Fatalf("invalid version accepted: %s", version)
		}
	}
	for _, version := range versions {
		got, _, err := inspectRelease(testRelease(t, version, releaseArch()), releaseArch())
		if err != nil || got != version {
			t.Fatalf("valid package %s rejected", version)
		}
	}
}

func TestBetaReleaseSelectionFiltersAndPaginates(t *testing.T) {
	drafts := betaTestMetadata(t, "99.0.0-beta.100")
	drafts["draft"] = true
	incorrectlyPublished := betaTestMetadata(t, "99.0.0-beta.99")
	incorrectlyPublished["prerelease"] = false
	unsupported := betaTestMetadata(t, "99.0.0-rc.1")
	records := []map[string]any{onlineTestMetadata(t, []byte("TEST_ONLY_ARCHIVE")), drafts, incorrectlyPublished, unsupported, betaTestMetadata(t, "99.0.0-beta.2"), betaTestMetadata(t, "99.0.0-beta.10")}
	for len(records) < 100 {
		records = append(records, betaTestMetadata(t, "98.0.0-beta.1"))
	}
	calls := 0
	client := &http.Client{Transport: onlineTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("page") != fmt.Sprint(calls) || r.URL.Query().Get("per_page") != "100" {
			t.Error("pagination invalid")
		}
		page := records
		if calls == 2 {
			page = []map[string]any{betaTestMetadata(t, "99.0.0-beta.11")}
		}
		if calls > 2 {
			t.Fatal("unnecessary page fetched")
		}
		data, _ := json.Marshal(page)
		return onlineTestClient(data).Transport.RoundTrip(r)
	})}
	release, err := latestBetaRelease(context.Background(), client, "")
	if err != nil || calls != 2 || release.Version != "99.0.0-beta.11" || release.Channel != betaChannel || !release.UpdateAvailable {
		t.Fatalf("incorrect beta selected: %v", err)
	}
	data, _ := json.Marshal([]map[string]any{onlineTestMetadata(t, []byte("TEST_ONLY_ARCHIVE")), drafts})
	empty, err := latestBetaRelease(context.Background(), onlineTestClient(data), "")
	if err != nil || empty.Version != "" || empty.Channel != betaChannel || empty.UpdateAvailable {
		t.Fatal("empty beta channel did not return an empty result")
	}
	// Neither a prerelease marked stable nor a beta marked as a normal Release belongs in stable checks.
	for _, record := range []map[string]any{betaTestMetadata(t, "99.0.0-beta.1"), incorrectlyPublished} {
		data, _ := json.Marshal(record)
		if _, err := latestOnlineRelease(context.Background(), onlineTestClient(data), ""); err == nil {
			t.Fatal("beta leaked into stable channel")
		}
	}
	latest := betaTestMetadata(t, "99.0.0-beta.2")
	latest["assets"] = []any{}
	data, _ = json.Marshal([]map[string]any{betaTestMetadata(t, "99.0.0-beta.1"), latest})
	if _, err := latestBetaRelease(context.Background(), onlineTestClient(data), ""); err == nil {
		t.Fatal("incomplete newest beta was silently replaced with an older release")
	}
}

func TestUpdateChannelCacheIsolationAndSharedCooldown(t *testing.T) {
	stable, _ := json.Marshal(onlineTestMetadata(t, []byte("TEST_ONLY_ARCHIVE")))
	beta, _ := json.Marshal([]map[string]any{betaTestMetadata(t, "99.0.0-beta.1")})
	calls := 0
	client := &http.Client{Transport: onlineTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		data := beta
		if strings.HasSuffix(r.URL.Path, "/latest") {
			data = stable
		}
		return onlineTestClient(data).Transport.RoundTrip(r)
	})}
	cache := onlineReleaseCache{}
	for _, channel := range []updateChannel{stableChannel, betaChannel, stableChannel, betaChannel} {
		release, err := cache.latest(context.Background(), client, State{}, channel)
		if err != nil || release.Channel != channel {
			t.Fatal("channel cache contaminated")
		}
	}
	if calls != 2 {
		t.Fatal("channel caches were not reused")
	}
	cache.noteLimit(State{}, &githubRateLimitError{retryAt: time.Now().Add(time.Minute)})
	for _, channel := range []updateChannel{stableChannel, betaChannel} {
		_, err := cache.latest(context.Background(), client, State{}, channel)
		var limit *githubRateLimitError
		if !errors.As(err, &limit) || calls != 2 {
			t.Fatal("switching channel bypassed cooldown")
		}
	}
}

func TestUpdateChannelAPIRejectsInvalidSelection(t *testing.T) {
	a, _ := testAdmin(t)
	h := a.Handler()
	cookie := loginForTest(t, h)
	for _, input := range []map[string]string{{"channel": "nightly"}, {"channel": "stable", "version": "99.0.0-beta.1"}, {"channel": "beta", "version": "99.0.0"}} {
		if w := adminRequest(h, "POST", "/api/maintenance/download-online-update", input, cookie, ""); w.Code != 400 {
			t.Fatal("invalid version/channel accepted")
		}
	}
	if w := adminRequest(h, "POST", "/api/maintenance/check-online-update", map[string]string{"channel": "nightly"}, cookie, ""); w.Code != 400 {
		t.Fatal("unknown channel triggered a check")
	}
}
