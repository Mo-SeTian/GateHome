package gateway

import (
	"context"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type IPStatus struct {
	Running bool       `json:"running"`
	IPv4    DetectedIP `json:"ipv4"`
	IPv6    DetectedIP `json:"ipv6"`
}

type ipSettings struct {
	Interface, Mode, IPv4Source, IPv6Source, IPv4Endpoints, IPv6Endpoints string
}

func settingsForIP(g DDNSGroup) ipSettings {
	return ipSettings{g.Interface, g.Mode, ipSource(g, "A"), ipSource(g, "AAAA"), strings.Join(ipEndpoints(g, "A"), "\x00"), strings.Join(ipEndpoints(g, "AAAA"), "\x00")}
}

type ipCheck struct {
	Settings ipSettings
	Status   IPStatus
	Started  time.Time
	Cancel   context.CancelFunc
}

func mergeDetectedIP(previous, sample DetectedIP, err error) DetectedIP {
	now := time.Now()
	if err != nil {
		if !previous.Address.IsValid() {
			previous = sample
		}
		previous.CheckedAt, previous.Error = now, err.Error()
		return previous
	}
	sample.CheckedAt, sample.LastSuccess = now, now
	return sample
}

// Page polling starts at most one check per source every 30 seconds; it never writes DNS.
func (j *Jobs) IPStatus(key string, g DDNSGroup, force bool) IPStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	entry := j.ips[key]
	if entry == nil || entry.Settings != settingsForIP(g) {
		if entry == nil {
			// Bound temporary preview profiles while allowing different tabs to inspect different NICs.
			for len(j.ips) >= 128 {
				oldest := ""
				for name, cached := range j.ips {
					if oldest == "" || cached.Started.Before(j.ips[oldest].Started) {
						oldest = name
					}
				}
				if j.ips[oldest].Cancel != nil {
					j.ips[oldest].Cancel()
				}
				delete(j.ips, oldest)
			}
		}
		if entry != nil && entry.Cancel != nil {
			entry.Cancel()
		}
		entry = &ipCheck{Settings: settingsForIP(g)}
		j.ips[key] = entry
	}
	interval := 30 * time.Second
	if force {
		interval = 5 * time.Second
	}
	if !entry.Status.Running && time.Since(entry.Started) >= interval {
		entry.Started, entry.Status.Running = time.Now(), true
		ctx, cancel := context.WithCancel(j.ipContext)
		entry.Cancel = cancel
		go func() {
			defer cancel()
			j.checkIPs(ctx, key, g, entry)
		}()
	}
	return entry.Status
}

func (j *Jobs) checkIPs(ctx context.Context, key string, g DDNSGroup, entry *ipCheck) {
	var wait sync.WaitGroup
	for _, kind := range []string{"A", "AAAA"} {
		if (kind == "A" && g.Mode == "ipv6") || (kind == "AAAA" && g.Mode == "ipv4") {
			continue
		}
		wait.Add(1)
		go func(kind string) {
			defer wait.Done()
			started := time.Now()
			sample, err := j.discover(ctx, g, kind)
			j.mu.Lock()
			if j.ips[key] == entry {
				if kind == "A" {
					entry.Status.IPv4 = mergeDetectedIP(entry.Status.IPv4, sample, err)
				} else {
					entry.Status.IPv6 = mergeDetectedIP(entry.Status.IPv6, sample, err)
				}
			}
			j.mu.Unlock()
			message := "已获取 " + kind + " 公网地址，来源网卡：" + sample.Interface
			if err != nil {
				message = err.Error()
			}
			target := g.Name
			if target == "" {
				target = "本机公网 IP"
			}
			j.logs.Add(LogEntry{Category: "ddns", Action: "实时 IP 检测", Target: target, OK: err == nil, DurationMS: time.Since(started).Milliseconds(), Message: message})
		}(kind)
	}
	wait.Wait()
	j.mu.Lock()
	if j.ips[key] == entry {
		entry.Status.Running = false
	}
	j.mu.Unlock()
}

func (j *Jobs) recordIP(g DDNSGroup, kind string, sample DetectedIP, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	key := "group:" + g.ID
	entry := j.ips[key]
	if entry == nil {
		entry = &ipCheck{Settings: settingsForIP(g)}
		j.ips[key] = entry
	}
	if entry.Settings != settingsForIP(g) {
		return
	}
	entry.Started = time.Now()
	if kind == "A" {
		entry.Status.IPv4 = mergeDetectedIP(entry.Status.IPv4, sample, err)
	} else {
		entry.Status.IPv6 = mergeDetectedIP(entry.Status.IPv6, sample, err)
	}
}

// Keep the public-IP-only compatibility helper used by discovery tests.
func discoverIP(ctx context.Context, endpoint, kind string) (netip.Addr, error) {
	result, err := queryPublicIP(ctx, endpoint, kind, nil)
	return result.Address, err
}
