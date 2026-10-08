//go:build !linux

package gateway

func readSystemSample() systemSample {
	return systemSample{Networks: []networkSample{}, CPUError: "CPU 指标采集支持 Linux", MemoryError: "内存指标采集支持 Linux", NetworkError: "网卡流量采集支持 Linux"}
}
