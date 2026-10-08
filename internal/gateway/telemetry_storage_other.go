//go:build !linux && !darwin

package gateway

func readStorageSample(name, path string) storageSample {
	return storageSample{Name: name, Error: "存储空间采集支持 Linux / macOS"}
}
