package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSubscriptionParserAndIndexedMatch(t *testing.T) {
	input := "\ufeff# IPv4 / IPv6\r\n1.2.3.4/24 # canonicalize\n1.2.3.0/24\n2001:db8::/32\n::ffff:192.0.2.1/120\n198.51.100.9\n// comment\n; comment\n"
	prefixes, err := parseSubscription(strings.NewReader(input))
	if err != nil || len(prefixes) != 4 {
		t.Fatal("IP / CIDR parsing, comments or deduplication failed")
	}
	index := newPrefixIndex(prefixes)
	for _, tc := range []struct {
		address string
		match   bool
	}{{"1.2.3.1", true}, {"1.2.4.1", false}, {"2001:db8::1", true}, {"2001:db9::1", false}, {"192.0.2.8", true}, {"::ffff:192.0.2.8", true}, {"198.51.100.9", true}, {"198.51.100.8", false}} {
		if index.contains(netip.MustParseAddr(tc.address)) != tc.match {
			t.Fatalf("wrong indexed match for %s", tc.address)
		}
	}
	for _, input := range []string{"", "# only comments", "<html>error</html>", "1.2.3.0/24\ninvalid", "1.2.3.0 1.2.3.255", "::ffff:192.0.2.1/80", strings.Repeat("x", maxSubscriptionBytes+1)} {
		if _, err := parseSubscription(strings.NewReader(input)); err == nil {
			t.Fatal("empty, invalid or oversized subscription accepted")
		}
	}
}

func newTestSubscriptions(t *testing.T, dir string, d Subscription, config *Config) (*Store, *Subscriptions) {
	t.Helper()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	c.Subscriptions = []Subscription{d}
	if config != nil {
		c = *config
	}
	if err := store.Update(c, nil, store.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	s, err := NewSubscriptions(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	return store, s
}

func TestSubscriptionRefreshHotApplyCacheAndFailure(t *testing.T) {
	var response atomic.Value
	response.Store("198.51.100.0/24\n2001:db8::/32\n")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, response.Load().(string))
	}))
	defer server.Close()
	dir := t.TempDir()
	d := Subscription{ID: "cn", Name: "测试订阅", URL: server.URL, Enabled: true, Interval: 300}
	r := testRoute("nas.example.com", "http://127.0.0.1:1")
	c := DefaultConfig()
	c.Subscriptions = []Subscription{d}
	testFirewall(&c, &r, "allow", []string{"192.0.2.0/24"}, []string{d.ID})
	c.Routes = []Route{r}
	store, s := newTestSubscriptions(t, dir, d, &c)
	s.client = server.Client()
	p := NewProxy(store.Snapshot().Config, s)
	request := func(addr string) int {
		req := httptest.NewRequest("GET", "http://nas.example.com", nil)
		req.RemoteAddr = addr
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		return w.Code
	}
	if request("192.0.2.1:1000") != 403 {
		t.Fatal("uninitialized subscription must fail closed, even with a manual match")
	}
	s.refresh(context.Background(), d)
	state := s.Status()[0]
	if !state.Ready || state.Running || state.Count != 2 || state.LastSuccess.IsZero() {
		t.Fatal("successful refresh not published")
	}
	for _, addr := range []string{"192.0.2.1:1000", "198.51.100.1:1000", "[2001:db8::1]:1000"} {
		if request(addr) != 502 { // An allowed request reaches the deliberately unavailable upstream.
			t.Fatal("manual and subscription union did not apply without reconfiguring proxy")
		}
	}
	if request("203.0.113.1:1000") != 403 {
		t.Fatal("unlisted IP admitted")
	}
	for _, content := range []string{"", "198.51.100.0/24\n<html>error</html>"} {
		response.Store(content)
		s.refresh(context.Background(), d)
		if v := s.Status()[0]; !v.Ready || v.Count != 2 || !v.LastSuccess.Equal(state.LastSuccess) || v.Running {
			t.Fatal("failed download replaced last good cache")
		}
		if request("198.51.100.1:1000") != 502 {
			t.Fatal("cached list stopped working after failed refresh")
		}
	}
	restored, err := NewSubscriptions(dir, store)
	if err != nil || !restored.Status()[0].Ready || restored.Status()[0].Count != 2 {
		t.Fatal("persisted subscription cache was not restored")
	}
	info, err := os.Stat(filepath.Join(dir, "subscriptions", d.ID+".json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("cache must be private")
	}
	response.Store("203.0.113.0/24\n")
	s.refresh(context.Background(), d)
	if request("198.51.100.1:1000") != 403 || request("203.0.113.1:1000") != 502 {
		t.Fatal("refreshed list was not hot applied")
	}
	c = DefaultConfig()
	testFirewall(&c, &r, "deny", []string{"192.0.2.0/24"}, []string{d.ID})
	c.Subscriptions, c.Routes = []Subscription{d}, []Route{r}
	p.Configure(c)
	if request("203.0.113.1:1000") != 403 || request("198.51.100.1:1000") != 502 || request("192.0.2.1:1000") != 403 {
		t.Fatal("blacklist did not block manual and subscribed addresses")
	}
	d.URL = server.URL + "/changed"
	c.Subscriptions = []Subscription{d}
	if err := store.Update(c, nil, store.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	s.Reload()
	p.Configure(c)
	if s.Status()[0].Ready || request("198.51.100.1:1000") != 403 {
		t.Fatal("changed source reused an unrelated cache or failed open")
	}
}

func TestSubscriptionRefreshCannotApplyStaleDownload(t *testing.T) {
	for _, changeURL := range []bool{true, false} {
		t.Run(map[bool]string{true: "changed URL", false: "disabled"}[changeURL], func(t *testing.T) {
			started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-release
				io.WriteString(w, "198.51.100.0/24\n")
			}))
			defer server.Close()
			dir := t.TempDir()
			d := Subscription{ID: "cn", Name: "测试订阅", URL: server.URL, Enabled: true, Interval: 300}
			store, s := newTestSubscriptions(t, dir, d, nil)
			s.client = server.Client()
			go func() { s.refresh(context.Background(), d); close(done) }()
			<-started
			c := DefaultConfig()
			changed := d
			if changeURL {
				changed.URL += "/new"
			} else {
				changed.Enabled = false
			}
			c.Subscriptions = []Subscription{changed}
			if err := store.Update(c, nil, store.Snapshot().Revision); err != nil {
				t.Error(err)
			}
			s.Reload()
			close(release)
			<-done
			v := s.Status()[0]
			if v.Ready || v.Running || v.Count != 0 {
				t.Fatal("obsolete download changed new or disabled subscription")
			}
			if _, err := os.Stat(filepath.Join(dir, "subscriptions", d.ID+".json")); !os.IsNotExist(err) {
				t.Fatal("obsolete download persisted cache")
			}
		})
	}
}

func TestSubscriptionHTTPFailureAndRedirect(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/list", 302)
			return
		}
		http.Error(w, "TEST_ONLY_PRIVATE_SERVER_ERROR", 503)
	}))
	defer server.Close()
	_, s := newTestSubscriptions(t, t.TempDir(), Subscription{ID: "test", Name: "测试", URL: server.URL, Enabled: true, Interval: 300}, nil)
	s.client.Transport = server.Client().Transport
	for _, path := range []string{"/list", "/redirect"} {
		if _, err := s.fetch(context.Background(), server.URL+path); err == nil || strings.Contains(err.Error(), "TEST_ONLY_PRIVATE_SERVER_ERROR") {
			t.Fatal("failed or redirected response accepted, or private error leaked")
		}
	}
}

func TestMultipleSubscriptionsRequireAllCachesAndMergeMatches(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			io.WriteString(w, "198.51.100.0/24\n")
		} else {
			io.WriteString(w, "203.0.113.0/24\n")
		}
	}))
	defer server.Close()
	one := Subscription{ID: "one", Name: "第一份", URL: server.URL + "/first", Enabled: true, Interval: 300}
	two := Subscription{ID: "two", Name: "第二份", URL: server.URL + "/second", Enabled: true, Interval: 300}
	store, s := newTestSubscriptions(t, t.TempDir(), one, nil)
	c := DefaultConfig()
	c.Subscriptions = []Subscription{one, two}
	r := testRoute("nas.example.com", "http://127.0.0.1:1")
	testFirewall(&c, &r, "allow", nil, []string{one.ID, two.ID})
	c.Routes = []Route{r}
	if err := store.Update(c, nil, store.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	s.Reload()
	s.client = server.Client()
	p := NewProxy(c, s)
	request := func(addr string) int {
		req := httptest.NewRequest("GET", "http://nas.example.com", nil)
		req.RemoteAddr = addr + ":1000"
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		return w.Code
	}
	s.refresh(context.Background(), one)
	if request("198.51.100.1") != 403 {
		t.Fatal("partially initialized subscriptions failed open")
	}
	s.refresh(context.Background(), two)
	if request("198.51.100.1") != 502 || request("203.0.113.1") != 502 || request("192.0.2.1") != 403 {
		t.Fatal("subscription lists were not combined")
	}
}
