package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	trafficRetention  = 30 * 24 * time.Hour
	maxTrafficDaySize = 4 << 20
)

type trafficBytes struct {
	Upload   uint64 `json:"upload_bytes"`
	Download uint64 `json:"download_bytes"`
}

type trafficRuleDay struct {
	Host  string
	Hours [24]trafficBytes
}

type trafficDay map[string]*trafficRuleDay

type Traffic struct {
	mu                   sync.Mutex
	flushMu              sync.Mutex
	dir                  string
	days                 map[time.Time]trafficDay
	dirty                map[time.Time]bool
	readError, saveError bool
}

func OpenTraffic(dir string) (*Traffic, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return nil, errors.New("流量统计路径须为普通目录")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	t := &Traffic{dir: dir, days: make(map[time.Time]trafficDay), dirty: make(map[time.Time]bool)}
	cutoff := time.Now().UTC().Add(-trafficRetention).Truncate(24 * time.Hour)
	for _, entry := range entries {
		day, err := time.Parse(time.DateOnly, strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if day.Before(cutoff) {
			t.dirty[day] = true // Remove expired files on the first flush.
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxTrafficDaySize {
			t.readError = true
			continue
		}
		var data trafficDay
		raw, err := os.ReadFile(t.path(day))
		if err != nil || json.Unmarshal(raw, &data) != nil || data == nil {
			t.readError = true
			continue
		}
		t.days[day] = data
	}
	return t, nil
}

func (t *Traffic) path(day time.Time) string {
	return filepath.Join(t.dir, day.Format(time.DateOnly)+".json")
}

func (t *Traffic) add(route *Route, upload, download int, at time.Time) {
	if t == nil || route.ID == "" || (upload == 0 && download == 0) {
		return
	}
	at = at.UTC()
	day := at.Truncate(24 * time.Hour)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.days[day] == nil {
		t.days[day] = make(trafficDay)
	}
	entry := t.days[day][route.ID]
	if entry == nil {
		entry = &trafficRuleDay{}
		t.days[day][route.ID] = entry
	}
	entry.Host = route.Host
	entry.Hours[at.Hour()].Upload += uint64(upload)
	entry.Hours[at.Hour()].Download += uint64(download)
	t.dirty[day] = true
}

func (t *Traffic) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				_ = t.flush(now)
			}
		}
	}()
}

func (t *Traffic) Flush() error { return t.flush(time.Now()) }

func (t *Traffic) flush(now time.Time) error {
	t.flushMu.Lock()
	defer t.flushMu.Unlock()
	cutoff := now.UTC().Add(-trafficRetention).Truncate(24 * time.Hour)
	t.mu.Lock()
	for day := range t.days {
		if day.Before(cutoff) {
			delete(t.days, day)
			t.dirty[day] = true
		}
	}
	snapshot := make(map[time.Time]map[string]trafficRuleDay, len(t.dirty))
	for day := range t.dirty {
		if day.Before(cutoff) {
			snapshot[day] = nil
			continue
		}
		copyDay := make(map[string]trafficRuleDay, len(t.days[day]))
		for id, entry := range t.days[day] {
			if entry != nil {
				copyDay[id] = *entry
			}
		}
		snapshot[day] = copyDay
	}
	clear(t.dirty)
	t.mu.Unlock()
	var failure error
	for day, entries := range snapshot {
		var err error
		if entries == nil {
			err = os.Remove(t.path(day))
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		} else {
			var data []byte
			data, err = json.Marshal(entries)
			if err == nil && len(data) > maxTrafficDaySize {
				err = errors.New("单日流量记录超过大小限制")
			}
			if err == nil {
				err = atomicWrite(t.path(day), data)
			}
		}
		if err != nil {
			failure = err
			t.mu.Lock()
			t.dirty[day] = true
			t.mu.Unlock()
		}
	}
	t.mu.Lock()
	t.saveError = failure != nil
	t.mu.Unlock()
	return failure
}

type trafficRuleTotal struct {
	ID   string `json:"id"`
	Host string `json:"host"`
	trafficBytes
}

type trafficReport struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	Rule string    `json:"rule,omitempty"`
	trafficBytes
	Rules        []trafficRuleTotal `json:"rules"`
	StorageError bool               `json:"storage_error"`
}

func (t *Traffic) report(from, to time.Time, id string) trafficReport {
	result := trafficReport{From: from, To: to, Rule: id, Rules: []trafficRuleTotal{}}
	totals := make(map[string]trafficRuleTotal)
	t.mu.Lock()
	result.StorageError = t.readError || t.saveError
	for hour := from; hour.Before(to); hour = hour.Add(time.Hour) {
		for ruleID, rule := range t.days[hour.Truncate(24*time.Hour)] {
			if rule == nil || (id != "" && id != ruleID) {
				continue
			}
			b := rule.Hours[hour.Hour()]
			if b.Upload == 0 && b.Download == 0 {
				continue
			}
			row := totals[ruleID]
			row.ID, row.Host = ruleID, rule.Host
			row.Upload += b.Upload
			row.Download += b.Download
			totals[ruleID] = row
			result.Upload += b.Upload
			result.Download += b.Download
		}
	}
	t.mu.Unlock()
	for _, row := range totals {
		result.Rules = append(result.Rules, row)
	}
	sort.Slice(result.Rules, func(i, j int) bool {
		a, b := result.Rules[i], result.Rules[j]
		if a.Upload+a.Download != b.Upload+b.Download {
			return a.Upload+a.Download > b.Upload+b.Download
		}
		return a.ID < b.ID
	})
	return result
}

type trafficRequestBody struct {
	io.ReadCloser
	traffic *Traffic
	route   *Route
}

func (b trafficRequestBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.traffic.add(b.route, n, 0, time.Now())
	return n, err
}
