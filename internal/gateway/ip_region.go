package gateway

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
	"net/netip"
	"strings"
	"sync"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

// Compressed assets keep the release package within its update size limit.
//
//go:embed geodb/ip2region_v4.xdb.gz
var regionIPv4Compressed []byte

//go:embed geodb/ip2region_v6.xdb.gz
var regionIPv6Compressed []byte

type regionDatabase struct {
	once sync.Once
	data []byte
}

var regionIPv4, regionIPv6 regionDatabase

func (db *regionDatabase) buffer(compressed []byte, size int) []byte {
	db.once.Do(func() {
		reader, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return
		}
		defer reader.Close()
		data := make([]byte, size)
		if _, err := io.ReadFull(reader, data); err != nil {
			return
		}
		// Read the trailer too, so gzip verifies the embedded data checksum.
		var tail [1]byte
		if n, err := reader.Read(tail[:]); n != 0 || err != io.EOF {
			return
		}
		db.data = data
	})
	return db.data
}

func ipRegion(raw string) string {
	ip, err := netip.ParseAddr(raw)
	if err != nil || ip.Zone() != "" {
		return "归属地未知"
	}
	ip = ip.Unmap()
	switch {
	case ip.IsLoopback():
		return "本机回环"
	case ip.IsPrivate():
		return "内网地址"
	case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast():
		return "链路本地"
	}
	kind, version := "AAAA", xdb.IPv6
	if ip.Is4() {
		kind, version = "A", xdb.IPv4
	}
	if !validPublicIP(ip, kind) {
		return "保留地址"
	}
	var data []byte
	if ip.Is4() {
		data = regionIPv4.buffer(regionIPv4Compressed, 11122501)
	} else {
		data = regionIPv6.buffer(regionIPv6Compressed, 37277813)
	}
	if data == nil {
		return "归属地未知"
	}
	// Each lookup has its own searcher; the shared database bytes are read-only.
	searcher, err := xdb.NewWithBuffer(version, data)
	if err != nil {
		return "归属地未知"
	}
	region, err := searcher.Search(ip.AsSlice())
	if err != nil {
		return "归属地未知"
	}
	parts := strings.Split(region, "|")
	if len(parts) > 4 {
		parts = parts[:4]
	}
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" && part != "0" && (len(labels) == 0 || labels[len(labels)-1] != part) {
			labels = append(labels, part)
		}
	}
	if len(labels) == 0 {
		return "归属地未知"
	}
	return strings.Join(labels, " · ")
}

// Enrich response copies only; geolocation never adds work to proxy requests.
func enrichLogRegions(entries []LogEntry) {
	regions := make(map[string]string)
	for i := range entries {
		if entries[i].Category != "access" || entries[i].Remote == "" {
			continue
		}
		ip := entries[i].Remote
		region, ok := regions[ip]
		if !ok {
			region = ipRegion(ip)
			regions[ip] = region
		}
		entries[i].RemoteRegion = region
	}
}
