//go:build linux

package gateway

import (
	"testing"
	"time"
)

func TestLinuxTelemetryReadsRuntime(t *testing.T) {
	sample := readSystemSample()
	if sample.CPUError != "" || sample.MemoryError != "" || sample.NetworkError != "" || sample.CPUTotal == 0 || sample.MemoryTotal == 0 || sample.MemoryUsed > sample.MemoryTotal {
		t.Fatal("Linux runtime resources are unavailable or inconsistent")
	}
	dir := t.TempDir()
	paths := StoragePaths{Config: dir, Log: dir, Data: dir}
	collector := telemetryCollector{}
	first := collector.snapshot(paths, "", time.Now())
	if first.CPUPercent != nil || len(first.Storage) != 3 || first.Storage[0].Total == 0 {
		t.Fatal("first sample fabricated CPU use or storage missing")
	}
	time.Sleep(30 * time.Millisecond)
	collector.updated = collector.updated.Add(-6 * time.Second)
	second := collector.snapshot(paths, "", time.Now())
	if second.CPUPercent == nil || *second.CPUPercent < 0 || *second.CPUPercent > 100 {
		t.Fatal("Linux CPU delta unavailable or outside range")
	}
	if len(second.Networks) > 0 && (second.Interface == "" || second.RXRate == nil || second.TXRate == nil) {
		t.Fatal("Linux network samples did not produce rates")
	}
}
