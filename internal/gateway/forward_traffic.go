package gateway

import (
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const forwardTrafficInterval = 5 * time.Second

type forwardCounters struct {
	mu     sync.Mutex
	hours  map[time.Time]trafficBytes
	active atomic.Int64
	latest forwardTrafficStatus // protected by PortForwarder.sampleMu
}

func (c *forwardCounters) record(at time.Time, counts trafficBytes) {
	if counts == (trafficBytes{}) {
		return
	}
	hour := at.UTC().Truncate(time.Hour)
	c.mu.Lock()
	if c.hours == nil {
		c.hours = make(map[time.Time]trafficBytes)
	}
	bucket := c.hours[hour]
	bucket.Upload += counts.Upload
	bucket.Download += counts.Download
	c.hours[hour] = bucket
	c.mu.Unlock()
}

func (c *forwardCounters) drain() map[time.Time]trafficBytes {
	c.mu.Lock()
	hours := c.hours
	c.hours = nil
	c.mu.Unlock()
	return hours
}

type forwardTrafficStatus struct {
	ID          string  `json:"id"`
	Protocol    string  `json:"protocol"`
	UploadBPS   float64 `json:"upload_bytes_per_second"`
	DownloadBPS float64 `json:"download_bytes_per_second"`
	Active      int64   `json:"active"`
	Sampled     bool    `json:"sampled"`
}

type forwardTrafficWriter struct {
	io.Writer
	counters *forwardCounters
	upload   bool
}

func (w forwardTrafficWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if w.upload {
		w.counters.record(time.Now(), trafficBytes{Upload: uint64(n)})
	} else {
		w.counters.record(time.Now(), trafficBytes{Download: uint64(n)})
	}
	return n, err
}

func (p *PortForwarder) runTrafficSampler() {
	defer p.wg.Done()
	ticker := time.NewTicker(forwardTrafficInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.sampleTraffic(time.Now())
		}
	}
}

func (p *PortForwarder) sampleTraffic(now time.Time) {
	p.sampleMu.Lock()
	defer p.sampleMu.Unlock()
	elapsed := now.Sub(p.sampledAt).Seconds()
	if elapsed <= 0 {
		return
	}
	p.sampledAt = now
	for _, b := range p.bindings {
		var totals trafficBytes
		for hour, counts := range b.traffic.drain() {
			totals.Upload += counts.Upload
			totals.Download += counts.Download
			p.traffic.addBytes(b.rule.ID+"/"+b.rule.Protocol, b.rule.Name, counts, hour)
		}
		b.traffic.latest.UploadBPS = float64(totals.Upload) / elapsed
		b.traffic.latest.DownloadBPS = float64(totals.Download) / elapsed
		b.traffic.latest.Sampled = true
	}
}

func (p *PortForwarder) trafficStatus() []forwardTrafficStatus {
	result := []forwardTrafficStatus{}
	if p == nil {
		return result
	}
	p.sampleMu.Lock()
	defer p.sampleMu.Unlock()
	for _, b := range p.bindings {
		row := b.traffic.latest
		row.ID, row.Protocol = b.rule.ID, b.rule.Protocol
		row.Active = b.traffic.active.Load()
		result = append(result, row)
	}
	return result
}

func (t *Traffic) forwardReport(from, to time.Time, id, protocol string) trafficReport {
	report := t.reportMatching(from, to, id, func(key string) bool {
		ruleID, kind, ok := strings.Cut(key, "/")
		return ok && (kind == "tcp" || kind == "udp") && (id == "" || id == ruleID) && (protocol == "" || protocol == kind)
	})
	for i := range report.Rules {
		row := &report.Rules[i]
		row.ID, row.Protocol, _ = strings.Cut(row.ID, "/")
	}
	return report
}
