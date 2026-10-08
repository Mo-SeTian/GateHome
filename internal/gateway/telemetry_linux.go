//go:build linux

package gateway

import "os"

func readSystemSample() systemSample {
	result := systemSample{Networks: []networkSample{}}
	data, err := os.ReadFile("/proc/stat")
	total, idle, ok := parseCPU(string(data))
	if err != nil || !ok {
		result.CPUError = "CPU 指标读取失败"
	} else {
		result.CPUTotal, result.CPUIdle = total, idle
	}
	data, err = os.ReadFile("/proc/meminfo")
	memory, used, ok := parseMemory(string(data))
	if err != nil || !ok {
		result.MemoryError = "内存指标读取失败"
	} else {
		result.MemoryTotal, result.MemoryUsed = memory, used
	}
	data, err = os.ReadFile("/proc/net/dev")
	if err != nil {
		result.NetworkError = "网卡计数读取失败"
	} else {
		result.Networks = parseNetworks(string(data))
	}
	return result
}
