package gateway

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"slices"
	"time"
)

const healthHistoryWindow = 60 * time.Minute

type healthSample struct {
	Time    time.Time `json:"checked_at"`
	Healthy bool      `json:"healthy"`
	Label   string    `json:"label"`
	MS      int64     `json:"duration_ms"`
}

type routeHealthHistory struct {
	Upstream [32]byte       `json:"upstream_hash"`
	Samples  []healthSample `json:"samples"`
}

// Called with the health cache locked.
func (a *Admin) loadHealthHistory() {
	cache := &a.health
	if cache.history != nil {
		return
	}
	cache.history = make(map[string]routeHealthHistory)
	if a.store == nil {
		return
	}
	f, err := os.Open(a.store.paths.file("service-health-history.json"))
	if err != nil {
		return
	}
	defer f.Close()
	var saved map[string]routeHealthHistory
	if json.NewDecoder(io.LimitReader(f, 2<<20)).Decode(&saved) == nil && saved != nil {
		cache.history = saved
	}
}

func (a *Admin) recordHealthSample(route Route, result serviceHealthResult) {
	a.loadHealthHistory()
	cache := &a.health
	hash := sha256.Sum256([]byte(route.Upstream))
	history := cache.history[route.ID]
	if history.Upstream != hash {
		history = routeHealthHistory{Upstream: hash}
	}
	samples := history.Samples
	if len(samples) > 0 {
		last := samples[len(samples)-1].Time
		if !result.CheckedAt.After(last) {
			return
		}
		if last.Truncate(time.Minute) == result.CheckedAt.Truncate(time.Minute) {
			samples = samples[:len(samples)-1]
		}
	}
	cutoff := result.CheckedAt.Truncate(time.Minute).Add(time.Minute - healthHistoryWindow)
	for len(samples) > 0 && samples[0].Time.Before(cutoff) {
		samples = samples[1:]
	}
	history.Samples = append(samples, healthSample{result.CheckedAt, result.Healthy, result.Label, result.DurationMS})
	cache.history[route.ID] = history
	cache.historyDirty = true
}

func (a *Admin) flushHealthHistory() {
	if a.store == nil {
		return
	}
	config := a.store.Snapshot().Config
	cache := &a.health
	cache.mu.Lock()
	if !cache.historyDirty {
		cache.mu.Unlock()
		return
	}
	valid := make(map[string]bool)
	for _, route := range config.Routes {
		valid[route.ID] = true
	}
	for id := range cache.history {
		if !valid[id] {
			delete(cache.history, id)
		}
	}
	raw, err := json.Marshal(cache.history)
	cache.historyDirty = false
	cache.mu.Unlock()
	if err == nil {
		err = atomicWrite(a.store.paths.file("service-health-history.json"), raw)
	}
	if err != nil {
		cache.mu.Lock()
		cache.historyDirty = true
		cache.mu.Unlock()
		if a.logs != nil {
			a.logs.Add(LogEntry{Category: "admin", Action: "保存反代在线历史", Message: "无法保存在线历史，请检查数据目录权限"})
		}
	}
}

func (a *Admin) routeHistory(w http.ResponseWriter, r *http.Request) {
	for _, route := range a.store.Snapshot().Config.Routes {
		if route.ID != r.PathValue("id") {
			continue
		}
		a.health.mu.Lock()
		a.loadHealthHistory()
		history := a.health.history[route.ID]
		var samples []healthSample
		if history.Upstream == sha256.Sum256([]byte(route.Upstream)) {
			samples = slices.Clone(history.Samples)
		}
		a.health.mu.Unlock()
		now := time.Now().UTC()
		samples = slices.DeleteFunc(samples, func(p healthSample) bool {
			return p.Time.Before(now.Truncate(time.Minute).Add(time.Minute-healthHistoryWindow)) || p.Time.After(now)
		})
		jsonResponse(w, 200, map[string]any{"now": now, "samples": samples})
		return
	}
	apiError(w, 404, "反代规则不存在")
}
