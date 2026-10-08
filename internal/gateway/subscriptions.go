package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxSubscriptionBytes = 8 << 20
const maxSubscriptionEntries = 200000

type prefixBucket struct {
	bits    int
	entries map[netip.Prefix]struct{}
}

type prefixIndex struct{ v4, v6 []prefixBucket }

func newPrefixIndex(prefixes []netip.Prefix) prefixIndex {
	v4, v6 := map[int]map[netip.Prefix]struct{}{}, map[int]map[netip.Prefix]struct{}{}
	for _, p := range prefixes {
		family := v6
		if p.Addr().Is4() {
			family = v4
		}
		if family[p.Bits()] == nil {
			family[p.Bits()] = map[netip.Prefix]struct{}{}
		}
		family[p.Bits()][p] = struct{}{}
	}
	index := prefixIndex{}
	for bits, entries := range v4 {
		index.v4 = append(index.v4, prefixBucket{bits, entries})
	}
	for bits, entries := range v6 {
		index.v6 = append(index.v6, prefixBucket{bits, entries})
	}
	return index
}

func (i prefixIndex) contains(addr netip.Addr) bool {
	addr = addr.Unmap()
	buckets := i.v6
	if addr.Is4() {
		buckets = i.v4
	}
	for _, b := range buckets {
		if _, ok := b.entries[netip.PrefixFrom(addr, b.bits).Masked()]; ok {
			return true
		}
	}
	return false
}

func parseSubscription(reader io.Reader) ([]netip.Prefix, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxSubscriptionBytes+1))
	if err != nil {
		return nil, errors.New("订阅内容读取失败")
	}
	if len(data) > maxSubscriptionBytes {
		return nil, errors.New("订阅文件超过 8 MiB")
	}
	scanner := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff")))
	scanner.Buffer(make([]byte, 1024), 4096)
	seen := map[netip.Prefix]bool{}
	prefixes := []netip.Prefix{}
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
			continue
		}
		p, err := parsePrefix(line)
		if err != nil {
			return nil, errors.New("订阅内容含无效行，需每行一个 IP 或 CIDR（支持 # 注释）")
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		prefixes = append(prefixes, p)
		if len(prefixes) > maxSubscriptionEntries {
			return nil, errors.New("订阅超过 200000 条记录")
		}
	}
	if scanner.Err() != nil {
		return nil, errors.New("订阅行过长或读取失败")
	}
	if len(prefixes) == 0 {
		return nil, errors.New("订阅没有有效 IP / CIDR，保留已有列表")
	}
	return prefixes, nil
}

type SubscriptionStatus struct {
	ID          string    `json:"id"`
	Count       int       `json:"count"`
	Ready       bool      `json:"ready"`
	Running     bool      `json:"running"`
	LastRun     time.Time `json:"last_run"`
	LastSuccess time.Time `json:"last_success"`
	Message     string    `json:"message"`
}

type subscriptionEntry struct {
	source string
	index  prefixIndex
	status SubscriptionStatus
}

type subscriptionCache struct {
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetched_at"`
	CIDRs     []string  `json:"cidrs"`
}

type subscriptionRef struct{ id, source string }

type Subscriptions struct {
	mu      sync.Mutex
	store   *Store
	dir     string
	client  *http.Client
	entries atomic.Value
	queue   chan string
	logs    *Logs
}

func NewSubscriptions(dir string, store *Store) (*Subscriptions, error) {
	dir = filepath.Join(dir, "subscriptions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Subscriptions{store: store, dir: dir, queue: make(chan string, 100), client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return errors.New("订阅链接不允许重定向，请使用最终 HTTPS 地址")
	}}}
	s.entries.Store(map[string]subscriptionEntry{})
	s.Reload()
	return s, nil
}

func (s *Subscriptions) Reload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.entries.Load().(map[string]subscriptionEntry)
	next := map[string]subscriptionEntry{}
	for _, definition := range s.store.Snapshot().Config.Subscriptions {
		entry, exists := old[definition.ID]
		if exists && entry.source == definition.URL {
			if !definition.Enabled && entry.status.Running {
				entry.status.Running = false
				entry.status.Message = "订阅已停用，保留现有列表"
			}
			next[definition.ID] = entry
			continue
		}
		entry = subscriptionEntry{source: definition.URL, status: SubscriptionStatus{ID: definition.ID, Message: "尚未拉取有效列表"}}
		data, err := os.ReadFile(filepath.Join(s.dir, definition.ID+".json"))
		if err == nil && len(data) <= 2*maxSubscriptionBytes {
			var cache subscriptionCache
			if json.Unmarshal(data, &cache) == nil && cache.URL == definition.URL {
				if prefixes, err := parseSubscription(strings.NewReader(strings.Join(cache.CIDRs, "\n"))); err == nil {
					entry.index = newPrefixIndex(prefixes)
					entry.status = SubscriptionStatus{ID: definition.ID, Ready: true, Count: len(prefixes), LastSuccess: cache.FetchedAt, Message: "已加载上次成功的列表"}
				}
			}
		}
		next[definition.ID] = entry
	}
	s.entries.Store(next)
}

func (s *Subscriptions) Status() []SubscriptionStatus {
	entries := s.entries.Load().(map[string]subscriptionEntry)
	result := []SubscriptionStatus{}
	for _, definition := range s.store.Snapshot().Config.Subscriptions {
		entry := entries[definition.ID]
		if entry.source != definition.URL {
			result = append(result, SubscriptionStatus{ID: definition.ID, Message: "等待加载"})
		} else {
			result = append(result, entry.status)
		}
	}
	return result
}

func (s *Subscriptions) Trigger(id string) bool {
	select {
	case s.queue <- id:
		return true
	default:
		return false
	}
}

func (s *Subscriptions) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			id := ""
			select {
			case <-ctx.Done():
				return
			case id = <-s.queue:
			case <-ticker.C:
			}
			for _, definition := range s.store.Snapshot().Config.Subscriptions {
				if ctx.Err() != nil {
					return
				}
				if !definition.Enabled || (id != "" && id != definition.ID) {
					continue
				}
				entry := s.entries.Load().(map[string]subscriptionEntry)[definition.ID]
				last := entry.status.LastRun
				if last.IsZero() {
					last = entry.status.LastSuccess
				}
				if id == "" && !last.IsZero() && time.Since(last) < time.Duration(definition.Interval)*time.Second {
					continue
				}
				if !last.IsZero() && time.Since(last) < 5*time.Second {
					continue
				}
				s.refresh(ctx, definition)
			}
		}
	}()
	s.Trigger("")
}

func (s *Subscriptions) setEntry(definition Subscription, change func(*subscriptionEntry)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := false
	for _, d := range s.store.Snapshot().Config.Subscriptions {
		if d.ID == definition.ID && d.URL == definition.URL && d.Enabled {
			current = true
			break
		}
	}
	if !current {
		return false
	}
	old := s.entries.Load().(map[string]subscriptionEntry)
	next := make(map[string]subscriptionEntry, len(old))
	for id, entry := range old {
		next[id] = entry
	}
	entry := next[definition.ID]
	if entry.source != definition.URL {
		return false
	}
	change(&entry)
	next[definition.ID] = entry
	s.entries.Store(next)
	return true
}

func (s *Subscriptions) refresh(ctx context.Context, definition Subscription) {
	if !s.setEntry(definition, func(e *subscriptionEntry) {
		e.status.Running = true
		e.status.LastRun = time.Now()
		e.status.Message = "正在拉取订阅"
	}) {
		return
	}
	started := time.Now()
	defer func() {
		entry := s.entries.Load().(map[string]subscriptionEntry)[definition.ID]
		if entry.source == definition.URL {
			s.logs.Add(LogEntry{Category: "subscriptions", Action: "更新订阅", Target: definition.Name, OK: entry.status.Ready && !entry.status.LastSuccess.Before(started), DurationMS: time.Since(started).Milliseconds(), Message: entry.status.Message})
		}
	}()
	prefixes, err := s.fetch(ctx, definition.URL)
	if err != nil {
		s.setEntry(definition, func(e *subscriptionEntry) { e.status.Running = false; e.status.Message = err.Error() })
		return
	}
	s.setEntry(definition, func(e *subscriptionEntry) {
		cidrs := make([]string, len(prefixes))
		for i, p := range prefixes {
			cidrs[i] = p.String()
		}
		now := time.Now()
		if err := writeJSON(filepath.Join(s.dir, definition.ID+".json"), subscriptionCache{URL: definition.URL, FetchedAt: now, CIDRs: cidrs}); err != nil {
			e.status.Running = false
			e.status.Message = "订阅缓存保存失败，保留现有列表"
			return
		}
		e.index = newPrefixIndex(prefixes)
		e.status.Running, e.status.Ready, e.status.Count = false, true, len(prefixes)
		e.status.LastSuccess, e.status.Message = now, "订阅已更新，访问规则即时生效"
	})
}

func (s *Subscriptions) fetch(ctx context.Context, source string) ([]netip.Prefix, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", source, nil)
	if err != nil {
		return nil, errors.New("订阅请求构建失败")
	}
	req.Header.Set("Accept", "text/plain")
	client := s.client
	if client.Transport == nil {
		client = outboundClient(s.store.Snapshot(), 30*time.Second)
		defer closeClient(client)
	}
	started := time.Now()
	status := 0
	ok := false
	defer func() {
		message := "订阅 HTTP 请求失败"
		if ok {
			message = "订阅 HTTP 请求成功"
		}
		s.logs.Add(LogEntry{Category: "subscriptions", Action: "下载订阅", Target: source, Method: "GET", Status: status, OK: ok, DurationMS: time.Since(started).Milliseconds(), Message: message})
	}()
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("订阅连接失败，请检查 HTTPS、网络及链接是否重定向")
	}
	defer resp.Body.Close()
	status = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("订阅服务器未返回 HTTP 200，保留现有列表")
	}
	ok = true
	return parseSubscription(resp.Body)
}

func (s *Subscriptions) SetLogs(logs *Logs) { s.logs = logs }
