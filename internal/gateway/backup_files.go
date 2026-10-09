package gateway

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"strings"
)

func safeBackupFile(name string) bool {
	if strings.HasPrefix(name, "page/") {
		return safeHomepageBackupFile(name)
	}
	if strings.HasPrefix(name, "logs/") {
		_, ok := logFileNumber(strings.TrimPrefix(name, "logs/"))
		return ok
	}
	if strings.HasPrefix(name, "homepage-backgrounds/") && strings.HasSuffix(name, ".jpg") {
		return routeImageID.MatchString(strings.TrimSuffix(strings.TrimPrefix(name, "homepage-backgrounds/"), ".jpg"))
	}
	if strings.HasPrefix(name, "route-images/") && strings.HasSuffix(name, ".img") {
		return routeImageID.MatchString(strings.TrimSuffix(strings.TrimPrefix(name, "route-images/"), ".img"))
	}
	if strings.HasPrefix(name, "subscriptions/") && strings.HasSuffix(name, ".json") {
		return idPattern.MatchString(strings.TrimSuffix(strings.TrimPrefix(name, "subscriptions/"), ".json"))
	}
	return false
}

func validRouteImage(id string, data []byte) bool {
	if !routeImageID.MatchString(id) || len(data) == 0 || len(data) > maxRouteImage {
		return false
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != id {
		return false
	}
	if validDiscoveryICO(data) {
		return len(data) <= 64<<10
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	return err == nil && format == "png" && config.Width > 0 && config.Height > 0 && config.Width <= 128 && config.Height <= 128
}

func validateBackupFiles(payload backupPayload) error {
	for name, data := range payload.Files {
		if !safeBackupFile(name) {
			return errors.New("备份含不支持的数据路径")
		}
		switch {
		case strings.HasPrefix(name, "route-images/"):
			id := strings.TrimSuffix(strings.TrimPrefix(name, "route-images/"), ".img")
			if !validRouteImage(id, data) {
				return errors.New("备份中的反代图片损坏或无效")
			}
		case strings.HasPrefix(name, "homepage-backgrounds/"):
			id := strings.TrimSuffix(strings.TrimPrefix(name, "homepage-backgrounds/"), ".jpg")
			if !validHomepageBackground(id, data) {
				return errors.New("备份中的首页背景损坏或无效")
			}
		case strings.HasPrefix(name, "logs/"):
			if len(data) > maxLogBytes {
				return errors.New("备份日志超过大小限制")
			}
			scanner := bufio.NewScanner(bytes.NewReader(data))
			scanner.Buffer(make([]byte, 4096), 64<<10)
			for scanner.Scan() {
				var entry LogEntry
				if json.Unmarshal(scanner.Bytes(), &entry) != nil {
					return errors.New("备份日志数据无效")
				}
			}
			if scanner.Err() != nil {
				return errors.New("备份日志数据无效")
			}
		case strings.HasPrefix(name, "subscriptions/"):
			var cache subscriptionCache
			if len(data) > maxSubscriptionBytes || json.Unmarshal(data, &cache) != nil {
				return errors.New("备份订阅缓存无效")
			}
			if _, err := parseSubscription(strings.NewReader(strings.Join(cache.CIDRs, "\n"))); err != nil {
				return errors.New("备份订阅缓存无效")
			}
		}
	}
	for _, r := range payload.State.Config.Routes {
		if r.Image != "" && payload.Files["route-images/"+r.Image+".img"] == nil {
			return errors.New("备份缺少反代规则使用的图片")
		}
	}
	for id := range homepageImages(payload.State.Config.Homepage) {
		if payload.Files["route-images/"+id+".img"] == nil {
			return errors.New("备份缺少首页图片")
		}
	}
	if id := payload.State.Config.Homepage.Background; id != "" && payload.Files["homepage-backgrounds/"+id+".jpg"] == nil {
		return errors.New("备份缺少首页背景图片")
	}
	return validateHomepageBackup(payload)
}

func backupFileCounts(payload backupPayload) (images, logs, caches int) {
	for name, data := range payload.Files {
		switch {
		case strings.HasPrefix(name, "route-images/"), strings.HasPrefix(name, "homepage-backgrounds/"), strings.HasPrefix(name, "page/") && (strings.Contains(name, "/route-images/") || strings.Contains(name, "/homepage-backgrounds/")):
			images++
		case strings.HasPrefix(name, "logs/"):
			logs += bytes.Count(data, []byte("\n"))
			if len(data) > 0 && data[len(data)-1] != '\n' {
				logs++
			}
		case strings.HasPrefix(name, "subscriptions/"):
			caches++
		}
	}
	return
}
