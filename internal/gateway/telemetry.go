package gateway

import (
	"runtime"
	"sync"
	"time"
)

type networkSample struct {
	Name string `json:"name"`
	RX   uint64 `json:"rx_bytes"`
	TX   uint64 `json:"tx_bytes"`
}

type storageSample struct {
	Name      string `json:"name"`
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Available uint64 `json:"available"`
	Error     string `json:"error,omitempty"`
}

type systemSample struct {
	CPUTotal, CPUIdle                   uint64
	MemoryTotal, MemoryUsed             uint64
	Networks                            []networkSample
	CPUError, MemoryError, NetworkError string
}

type telemetryView struct {
	Platform     string          `json:"platform"`
	CPUs         int             `json:"cpus"`
	CPUPercent   *float64        `json:"cpu_percent"`
	MemoryTotal  uint64          `json:"memory_total"`
	MemoryUsed   uint64          `json:"memory_used"`
	CPUError     string          `json:"cpu_error,omitempty"`
	MemoryError  string          `json:"memory_error,omitempty"`
	NetworkError string          `json:"network_error,omitempty"`
	Networks     []networkSample `json:"networks"`
	Interface    string          `json:"interface"`
	RXBytes      uint64          `json:"rx_bytes"`
	TXBytes      uint64          `json:"tx_bytes"`
	RXRate       *float64        `json:"rx_rate"`
	TXRate       *float64        `json:"tx_rate"`
	Storage      []storageSample `json:"storage"`
}

type telemetryCollector struct {
	mu       sync.Mutex
	previous systemSample
	updated  time.Time
	view     telemetryView
	iface    string
}

func (t *telemetryCollector) snapshot(paths StoragePaths, iface string, now time.Time) telemetryView {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.updated.IsZero() && now.Sub(t.updated) < 5*time.Second && iface == t.iface {
		return t.view
	}
	current := readSystemSample()
	view := telemetryView{Platform: runtime.GOOS + " / " + runtime.GOARCH, CPUs: runtime.NumCPU(), MemoryTotal: current.MemoryTotal, MemoryUsed: current.MemoryUsed, CPUError: current.CPUError, MemoryError: current.MemoryError, NetworkError: current.NetworkError, Networks: current.Networks, Storage: []storageSample{}, Interface: iface}
	if current.CPUTotal > t.previous.CPUTotal && !t.updated.IsZero() && current.CPUError == "" {
		total, idle := current.CPUTotal-t.previous.CPUTotal, uint64(0)
		if current.CPUIdle >= t.previous.CPUIdle {
			idle = min(total, current.CPUIdle-t.previous.CPUIdle)
		}
		value := 100 * float64(total-idle) / float64(total)
		view.CPUPercent = &value
	}
	if view.Interface == "" && len(current.Networks) > 0 {
		// Prefer the interface carrying the most observed traffic, rather than
		// adding bridge/veth and physical counters for the same packets twice.
		selected := current.Networks[0]
		for _, n := range current.Networks {
			if n.RX+n.TX > selected.RX+selected.TX {
				selected = n
			}
		}
		view.Interface = selected.Name
	}
	found := false
	for _, n := range current.Networks {
		if n.Name != view.Interface {
			continue
		}
		found = true
		view.RXBytes, view.TXBytes = n.RX, n.TX
		if !t.updated.IsZero() {
			for _, old := range t.previous.Networks {
				if old.Name == n.Name && n.RX >= old.RX && n.TX >= old.TX && now.After(t.updated) {
					rx, tx := float64(n.RX-old.RX)/now.Sub(t.updated).Seconds(), float64(n.TX-old.TX)/now.Sub(t.updated).Seconds()
					view.RXRate, view.TXRate = &rx, &tx
				}
			}
		}
	}
	if !found && view.NetworkError == "" {
		view.NetworkError = "所选网卡当前不可用"
	}
	for _, item := range []struct{ name, path string }{{"配置", paths.Config}, {"日志", paths.Log}, {"数据", paths.Data}} {
		view.Storage = append(view.Storage, readStorageSample(item.name, item.path))
	}
	t.previous, t.updated, t.view, t.iface = current, now, view, iface
	return view
}
