package gateway

import (
	"io"
	"strings"
	"sync/atomic"
	"time"
)

const forwardTrafficInterval = 5 * time.Second

type forwardCounters struct {
	upload, download atomic.Uint64
	active           atomic.Int64
	latest           forwardTrafficStatus // protected by PortForwarder.sampleMu
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
	counter *atomic.Uint64
}

func (w forwardTrafficWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.counter.Add(uint64(n))
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
		counts := trafficBytes{Upload: b.traffic.upload.Swap(0), Download: b.traffic.download.Swap(0)}
		b.traffic.latest.UploadBPS = float64(counts.Upload) / elapsed
		b.traffic.latest.DownloadBPS = float64(counts.Download) / elapsed
		b.traffic.latest.Sampled = true
		p.traffic.addBytes(b.rule.ID+"/"+b.rule.Protocol, b.rule.Name, counts, now)
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
