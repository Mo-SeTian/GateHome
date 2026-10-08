package gateway

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"runtime"
	"strings"
)

const maxUpdateBytes = 128 << 20
const maxBackupBytes = 64 << 20

type releaseFile struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type releaseManifest struct {
	Format  string                 `json:"format"`
	Version string                 `json:"version"`
	Files   map[string]releaseFile `json:"files"`
}

func readZIP(data []byte, maxExpanded int64, maxEntries int) (map[string][]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("ZIP 文件无效")
	}
	if len(r.File) > maxEntries {
		return nil, errors.New("ZIP 文件数量超过限制")
	}
	result := map[string][]byte{}
	seen := map[string]bool{}
	total := int64(0)
	for _, f := range r.File {
		name := strings.TrimSuffix(f.Name, "/")
		if name == "" || path.Clean(name) != name || path.IsAbs(name) || strings.ContainsAny(name, "\\\x00:") || name == ".." || strings.HasPrefix(name, "../") || seen[name] || f.Mode()&0170000 != 0 {
			return nil, errors.New("ZIP 含重复路径、特殊文件或不安全路径")
		}
		seen[name] = true
		if f.FileInfo().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() {
			return nil, errors.New("ZIP 只允许普通文件")
		}
		if f.UncompressedSize64 > uint64(maxExpanded-total) {
			return nil, errors.New("ZIP 解压大小超过限制")
		}
		reader, err := f.Open()
		if err != nil {
			return nil, errors.New("ZIP 内容读取失败")
		}
		b, err := io.ReadAll(io.LimitReader(reader, maxExpanded-total+1))
		reader.Close()
		if err != nil || int64(len(b)) > maxExpanded-total {
			return nil, errors.New("ZIP 内容损坏或超过限制")
		}
		total += int64(len(b))
		result[name] = b
	}
	return result, nil
}

func inspectRelease(data []byte, arch string) (string, []byte, error) {
	files, err := readZIP(data, 128<<20, 1000)
	if err != nil {
		return "", nil, err
	}
	var manifest releaseManifest
	if len(files["manifest.json"]) > 1<<20 || json.Unmarshal(files["manifest.json"], &manifest) != nil || manifest.Format != "gatehouse-update-v1" {
		return "", nil, errors.New("不是 Gatehouse 更新包")
	}
	if _, err := compareVersions(manifest.Version, Version); err != nil {
		return "", nil, err
	}
	if len(manifest.Files) != len(files)-1 {
		return "", nil, errors.New("更新包清单与文件数量不符")
	}
	for name, b := range files {
		if name == "manifest.json" {
			continue
		}
		entry, ok := manifest.Files[name]
		sum := sha256.Sum256(b)
		if !ok || entry.Size != int64(len(b)) || entry.SHA256 != hex.EncodeToString(sum[:]) {
			return "", nil, errors.New("更新包校验失败，文件不完整或已改变")
		}
	}
	binary, ok := files["dist/gatehouse-linux-"+arch]
	if !ok {
		return "", nil, errors.New("更新包不包含此主机架构的 Linux 程序")
	}
	if err := validateELF(binary, arch); err != nil {
		return "", nil, err
	}
	return manifest.Version, binary, nil
}

func validateELF(data []byte, arch string) error {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return errors.New("更新程序不是有效的 Linux ELF 文件")
	}
	defer f.Close()
	want := elf.EM_NONE
	switch arch {
	case "amd64":
		want = elf.EM_X86_64
	case "arm64":
		want = elf.EM_AARCH64
	}
	if want == elf.EM_NONE || f.Machine != want || f.Class != elf.ELFCLASS64 || (f.Type != elf.ET_EXEC && f.Type != elf.ET_DYN) {
		return errors.New("更新程序架构不匹配")
	}
	return nil
}

func releaseArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}
