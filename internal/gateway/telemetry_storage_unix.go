//go:build linux || darwin

package gateway

import "golang.org/x/sys/unix"

func readStorageSample(name, path string) storageSample {
	result := storageSample{Name: name}
	var stat unix.Statfs_t
	if unix.Statfs(path, &stat) != nil {
		result.Error = "文件系统空间读取失败"
		return result
	}
	result.Total = stat.Blocks * uint64(stat.Bsize)
	result.Used = (stat.Blocks - stat.Bfree) * uint64(stat.Bsize)
	result.Available = stat.Bavail * uint64(stat.Bsize)
	return result
}
