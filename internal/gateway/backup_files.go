package gateway

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"slices"
	"strings"

	"sun-panel/lib/imageasset"
)

func isBackupImage(name string) bool {
	return strings.HasPrefix(name, "route-images/") || strings.HasPrefix(name, "icons/")
}

func safeBackupFile(name string) bool {
	if rel, ok := strings.CutPrefix(name, "icons/"); ok {
		return safeIconPath(rel)
	}
	if strings.HasPrefix(name, "sunpanel/") {
		return safeSunPanelBackupFile(name)
	}
	if strings.HasPrefix(name, "mihomo/") {
		return safeMihomoBackupFile(name)
	}
	if strings.HasPrefix(name, "logs/") {
		_, ok := logFileNumber(strings.TrimPrefix(name, "logs/"))
		return ok
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
	if err == nil {
		return format == "png" && config.Width > 0 && config.Height > 0 && config.Width <= 128 && config.Height <= 128
	}
	return imageasset.Validate("image.svg", data)
}

func validateBackupImage(name string, data []byte) error {
	if id, ok := strings.CutPrefix(name, "route-images/"); ok {
		if !validRouteImage(strings.TrimSuffix(id, ".img"), data) {
			return errors.New("备份中的反代图片损坏或无效")
		}
		return nil
	}
	if len(data) == 0 || len(data) > maxImageUpload {
		return errors.New("备份中的图标大小无效")
	}
	if _, _, err := iconDimensions(bytes.NewReader(data), iconFormat(name)); err != nil {
		return errors.New("备份中的图标损坏或无效")
	}
	return nil
}

// Drop unusable optional assets before validating a complete backup. Invalid
// paths and non-image data are still checked by validateBackupFiles.
func omitUnavailableBackupImages(payload *backupPayload) {
	if payload.Files == nil {
		return
	}
	skipped := map[string]struct{}{}
	for name, data := range payload.Files {
		if isBackupImage(name) && safeBackupFile(name) && validateBackupImage(name, data) != nil {
			delete(payload.Files, name)
			skipped[name] = struct{}{}
		}
	}
	copied := false
	for i, route := range payload.State.Config.Routes {
		if route.Image == "" {
			continue
		}
		name := routeImageBackupPath(route.Image)
		if !safeBackupFile(name) || payload.Files[name] != nil {
			continue
		}
		if !copied {
			payload.State.Config.Routes = slices.Clone(payload.State.Config.Routes)
			copied = true
		}
		payload.State.Config.Routes[i].Image = ""
		skipped[name] = struct{}{}
	}
	payload.SkippedImages += len(skipped)
}

func validateBackupFiles(payload backupPayload) error {
	for name, data := range payload.Files {
		if !safeBackupFile(name) {
			return errors.New("备份含不支持的数据路径")
		}
		switch {
		case isBackupImage(name):
			if err := validateBackupImage(name, data); err != nil {
				return err
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
		case strings.HasPrefix(name, "mihomo/"):
			if err := validateMihomoBackupFile(name, data); err != nil {
				return err
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
		if r.Image != "" && payload.Files[routeImageBackupPath(r.Image)] == nil {
			return errors.New("备份缺少反代规则使用的图片")
		}
	}
	return nil
}

func backupFileCounts(payload backupPayload) (images, logs, caches int) {
	for name, data := range payload.Files {
		switch {
		case isBackupImage(name):
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
