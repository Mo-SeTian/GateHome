package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type mihomoTrafficSample struct {
	pid  int
	up   int64
	down int64
	at   time.Time
}

type mihomoCPUSample struct {
	pid       int
	procTicks uint64
	at        time.Time
}

type mihomoTelemetryResponse struct {
	SampledAt         time.Time `json:"sampled_at"`
	Running           bool      `json:"running"`
	PID               int       `json:"pid,omitempty"`
	StartedAt         time.Time `json:"started_at,omitempty"`
	UptimeSeconds     int64     `json:"uptime_seconds"`
	UpRate            int64     `json:"up_rate"`
	DownRate          int64     `json:"down_rate"`
	UploadTotal       int64     `json:"upload_total"`
	DownloadTotal     int64     `json:"download_total"`
	ActiveConnections int       `json:"active_connections"`
	MemoryRSSBytes    uint64    `json:"memory_rss_bytes"`
	HostMemTotalBytes uint64    `json:"host_mem_total_bytes"`
	MemoryPercent     float64   `json:"memory_percent"`
	HeapInuseBytes    uint64    `json:"heap_inuse_bytes"`
	CPUPercent        float64   `json:"cpu_percent"`
	GroupCount        int       `json:"group_count"`
	NodeCount         int       `json:"node_count"`
	RuleSetCount      int       `json:"rule_set_count"`
	CustomRuleCount   int       `json:"custom_rule_count"`
	TotalRulesCount   int       `json:"total_rules_count"`
	RulesLive         bool      `json:"rules_live"`
}

func (m *mihomoManager) resetTelemetrySamples() {
	m.telemetryMu.Lock()
	m.lastTrafficSample = mihomoTrafficSample{}
	m.lastCPUSample = mihomoCPUSample{}
	m.telemetryMu.Unlock()
}

var (
	darwinHostMemOnce sync.Once
	darwinHostMemSize uint64
)

func detectHostTotalMemory() uint64 {
	switch runtime.GOOS {
	case "linux":
		raw, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			return 0
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if kb, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
						return kb * 1024
					}
				}
				break
			}
		}
	case "darwin":
		darwinHostMemOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, "sysctl", "-n", "hw.memsize").Output()
			if err == nil {
				if val, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64); err == nil {
					darwinHostMemSize = val
				}
			}
		})
		return darwinHostMemSize
	}
	return 0
}

func (m *mihomoManager) sampleMihomoProcessStats(pid int) (cpuPct float64, rssBytes uint64, hostTotal uint64) {
	hostTotal = detectHostTotalMemory()
	if pid <= 0 {
		return 0, 0, hostTotal
	}
	switch runtime.GOOS {
	case "linux":
		// Read VmRSS from /proc/<pid>/status
		if statusRaw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status"); err == nil {
			for _, line := range strings.Split(string(statusRaw), "\n") {
				if strings.HasPrefix(line, "VmRSS:") {
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						if kb, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
							rssBytes = kb * 1024
						}
					}
					break
				}
			}
		}
		// Read utime + stime ticks from /proc/<pid>/stat (after last ')' to handle arbitrary comm)
		if statRaw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
			s := string(statRaw)
			if rparen := strings.LastIndexByte(s, ')'); rparen >= 0 && rparen+1 < len(s) {
				fields := strings.Fields(s[rparen+1:])
				if len(fields) > 12 {
					utime, err1 := strconv.ParseUint(fields[11], 10, 64)
					stime, err2 := strconv.ParseUint(fields[12], 10, 64)
					if err1 == nil && err2 == nil {
						totalTicks := utime + stime
						now := time.Now()
						m.telemetryMu.Lock()
						prev := m.lastCPUSample
						m.lastCPUSample = mihomoCPUSample{pid: pid, procTicks: totalTicks, at: now}
						m.telemetryMu.Unlock()
						if prev.pid == pid && !prev.at.IsZero() && totalTicks >= prev.procTicks {
							dt := now.Sub(prev.at).Seconds()
							if dt >= 0.2 {
								cpuSec := float64(totalTicks-prev.procTicks) / 100.0
								cpuPct = math.Round((cpuSec/dt)*1000) / 10
							}
						}
					}
				}
			}
		}
	case "darwin":
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "%cpu=,rss=").Output()
		if err == nil {
			fields := strings.Fields(strings.TrimSpace(string(out)))
			if len(fields) >= 2 {
				if c, err := strconv.ParseFloat(fields[0], 64); err == nil && c >= 0 {
					cpuPct = math.Round(c*10) / 10
				}
				if kb, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					rssBytes = kb * 1024
				}
			}
		}
	}
	return cpuPct, rssBytes, hostTotal
}

func (m *mihomoManager) fetchMihomoMemoryInuse(ctx context.Context, ctrlURL, secret string) uint64 {
	if ctrlURL == "" {
		return 0
	}
	reqCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, ctrlURL+"/memory", nil)
	if err != nil {
		return 0
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := m.localClient(1500 * time.Millisecond).Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	// Mihomo /memory streams one JSON object per second; read only the first line.
	reader := bufio.NewReaderSize(resp.Body, 4096)
	line, err := reader.ReadBytes('\n')
	if len(line) == 0 && err != nil {
		return 0
	}
	var payload struct {
		Inuse uint64 `json:"inuse"`
	}
	if json.Unmarshal(line, &payload) == nil {
		return payload.Inuse
	}
	return 0
}

// Telemetry samples Mihomo traffic rates, process CPU/RSS, heap memory, connections, and rule counts.
func (m *mihomoManager) Telemetry(ctx context.Context) mihomoTelemetryResponse {
	now := time.Now()
	m.mu.Lock()
	running := m.running
	hasOverride := m.ctrlBaseURL != ""
	pid := m.pid
	startedAt := m.startedAt
	ctrlURL, secret := m.controllerURLAndSecretLocked()
	ruleSetCount := len(m.cfg.CustomRuleSets)
	customRuleCount := len(m.cfg.CustomRules)
	customGroups := slicesCloneGroups(m.cfg.CustomGroups)
	rtPath := m.runtimePath()
	m.mu.Unlock()

	var uptimeSec int64
	if running && !startedAt.IsZero() {
		if d := int64(now.Sub(startedAt).Seconds()); d >= 0 {
			uptimeSec = d
		}
	}

	res := mihomoTelemetryResponse{
		SampledAt:         now,
		Running:           running,
		PID:               pid,
		StartedAt:         startedAt,
		UptimeSeconds:     uptimeSec,
		RuleSetCount:      ruleSetCount,
		CustomRuleCount:   customRuleCount,
		HostMemTotalBytes: detectHostTotalMemory(),
	}

	// Rule counts (uses live /providers/rules when running, offline files otherwise)
	liveRules, providers := m.GetRuleSetStats()
	res.RulesLive = liveRules
	totalRules := customRuleCount
	for _, info := range providers {
		if info.RuleCount != nil && *info.RuleCount > 0 {
			totalRules += *info.RuleCount
		}
	}
	res.TotalRulesCount = totalRules

	if running || hasOverride {
		// Connections, cumulative traffic, and rate delta
		var snap MihomoConnectionsSnapshot
		var connErr error
		if running {
			snap, connErr = m.GetConnections()
		} else if hasOverride {
			var payload struct {
				DownloadTotal int64                  `json:"downloadTotal"`
				UploadTotal   int64                  `json:"uploadTotal"`
				Memory        int64                  `json:"memory"`
				Connections   []MihomoConnectionItem `json:"connections"`
			}
			connErr = m.doControllerJSONWithTimeout(http.MethodGet, ctrlURL+"/connections", secret, nil, &payload, 2*time.Second)
			if connErr == nil {
				snap = MihomoConnectionsSnapshot{
					DownloadTotal: payload.DownloadTotal,
					UploadTotal:   payload.UploadTotal,
					Memory:        payload.Memory,
					Connections:   payload.Connections,
				}
			}
		}
		if connErr == nil {
			res.UploadTotal = snap.UploadTotal
			res.DownloadTotal = snap.DownloadTotal
			res.ActiveConnections = len(snap.Connections)
			if snap.Memory > 0 {
				res.HeapInuseBytes = uint64(snap.Memory)
			}

			sampleKey := pid
			if sampleKey == 0 && hasOverride {
				sampleKey = -1
			}
			m.telemetryMu.Lock()
			prev := m.lastTrafficSample
			m.lastTrafficSample = mihomoTrafficSample{
				pid:  sampleKey,
				up:   snap.UploadTotal,
				down: snap.DownloadTotal,
				at:   now,
			}
			m.telemetryMu.Unlock()

			if prev.pid == sampleKey && !prev.at.IsZero() {
				dt := now.Sub(prev.at).Seconds()
				if dt >= 0.2 {
					if snap.UploadTotal >= prev.up {
						res.UpRate = int64(float64(snap.UploadTotal-prev.up) / dt)
					}
					if snap.DownloadTotal >= prev.down {
						res.DownRate = int64(float64(snap.DownloadTotal-prev.down) / dt)
					}
				}
			}
		}

		if running && pid > 0 {
			cpuPct, rssBytes, hostTotal := m.sampleMihomoProcessStats(pid)
			res.CPUPercent = cpuPct
			res.MemoryRSSBytes = rssBytes
			if hostTotal > 0 {
				res.HostMemTotalBytes = hostTotal
			}
			if rssBytes > 0 && hostTotal > 0 {
				res.MemoryPercent = math.Round((float64(rssBytes)/float64(hostTotal))*1000) / 10
			}
		}

		if res.HeapInuseBytes == 0 && ctrlURL != "" {
			res.HeapInuseBytes = m.fetchMihomoMemoryInuse(ctx, ctrlURL, secret)
		}

		if groups, _, nodes, _, err := m.GetGroupsAndDelays(); err == nil || len(groups) > 0 || len(nodes) > 0 {
			res.GroupCount = len(groups)
			res.NodeCount = len(nodes)
			return res
		}
	}

	runtimeBytes, _ := os.ReadFile(rtPath)
	yamlGroups, yamlNodes := extractYAMLGroupsAndNodes(runtimeBytes, customGroups)
	res.GroupCount = len(yamlGroups)
	res.NodeCount = len(yamlNodes)
	return res
}

func slicesCloneGroups(in []MihomoCustomGroup) []MihomoCustomGroup {
	if len(in) == 0 {
		return nil
	}
	out := make([]MihomoCustomGroup, len(in))
	copy(out, in)
	return out
}
