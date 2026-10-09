package gateway

import (
	"encoding/json"
	"errors"
	"strings"
)

func safeHomepageBackupFile(name string) bool {
	parts := strings.Split(name, "/")
	if len(parts) < 3 || parts[0] != "page" || (parts[1] != homepageAdminSpace && !homepageUserID.MatchString(parts[1])) {
		return false
	}
	if len(parts) == 3 {
		return parts[2] == "config.json"
	}
	if len(parts) != 4 {
		return false
	}
	return (parts[2] == "route-images" && strings.HasSuffix(parts[3], ".img") && routeImageID.MatchString(strings.TrimSuffix(parts[3], ".img"))) || (parts[2] == "homepage-backgrounds" && strings.HasSuffix(parts[3], ".jpg") && routeImageID.MatchString(strings.TrimSuffix(parts[3], ".jpg")))
}

func homepageBackupNames(paths StoragePaths, state State) ([]string, error) {
	names := []string{}
	for _, owner := range homepageSpaceIDs(state) {
		document, err := readHomepageDocument(paths.Data, owner)
		if err != nil {
			return nil, err
		}
		prefix := "page/" + owner + "/"
		names = append(names, prefix+"config.json")
		for id := range homepageImages(document.Homepage) {
			names = append(names, prefix+"route-images/"+id+".img")
		}
		if document.Homepage.Background != "" {
			names = append(names, prefix+"homepage-backgrounds/"+document.Homepage.Background+".jpg")
		}
	}
	return names, nil
}

func migrateHomepageBackup(payload *backupPayload) error {
	if payload.State.HomepageData || payload.Files == nil {
		return nil
	}
	home := payload.State.Config.Homepage
	if !legacyHomepageContent(home) {
		home = defaultHomepage()
	}
	for id := range homepageImages(home) {
		content := payload.Files["route-images/"+id+".img"]
		if !validRouteImage(id, content) {
			return errors.New("旧备份缺少首页图标")
		}
		payload.Files["page/admin/route-images/"+id+".img"] = content
	}
	if home.Background != "" {
		content := payload.Files["homepage-backgrounds/"+home.Background+".jpg"]
		if !validHomepageBackground(home.Background, content) {
			return errors.New("旧备份缺少首页背景")
		}
		payload.Files["page/admin/homepage-backgrounds/"+home.Background+".jpg"] = content
	}
	normalizeHomepage(&home)
	content, err := json.Marshal(homepageDocument{Homepage: home, Revision: 1})
	if err != nil {
		return err
	}
	payload.Files["page/admin/config.json"] = content
	payload.State.Config.Homepage = HomepageConfig{Enabled: payload.State.Config.Homepage.Enabled, Port: payload.State.Config.Homepage.Port}
	payload.State.HomepageData = true
	return nil
}

func validateHomepageBackup(payload backupPayload) error {
	owners := map[string]bool{}
	for _, id := range homepageSpaceIDs(payload.State) {
		owners[id] = true
	}
	for name, content := range payload.Files {
		if !strings.HasPrefix(name, "page/") {
			continue
		}
		parts := strings.Split(name, "/")
		if !safeHomepageBackupFile(name) || !owners[parts[1]] {
			return errors.New("备份含无效的首页用户目录")
		}
		if len(parts) == 4 {
			id := strings.TrimSuffix(strings.TrimSuffix(parts[3], ".img"), ".jpg")
			valid := validRouteImage(id, content)
			if parts[2] == "homepage-backgrounds" {
				valid = validHomepageBackground(id, content)
			}
			if !valid {
				return errors.New("备份中的首页图片无效")
			}
		}
	}
	if !payload.State.HomepageData || payload.Files == nil {
		return nil
	}
	for id := range owners {
		prefix := "page/" + id + "/"
		var document homepageDocument
		content := payload.Files[prefix+"config.json"]
		if len(content) > 1<<20 || json.Unmarshal(content, &document) != nil || document.Revision < 1 || validateHomepageContent(document.Homepage) != nil {
			return errors.New("备份缺少首页用户配置或配置无效")
		}
		for image := range homepageImages(document.Homepage) {
			if payload.Files[prefix+"route-images/"+image+".img"] == nil {
				return errors.New("备份缺少首页用户使用的图标")
			}
		}
		if image := document.Homepage.Background; image != "" && payload.Files[prefix+"homepage-backgrounds/"+image+".jpg"] == nil {
			return errors.New("备份缺少首页用户使用的背景")
		}
	}
	return nil
}

func homepageBackupCounts(payload backupPayload) (desktops, groups int) {
	for name, content := range payload.Files {
		if !strings.HasPrefix(name, "page/") || !strings.HasSuffix(name, "/config.json") {
			continue
		}
		var document homepageDocument
		if json.Unmarshal(content, &document) == nil {
			desktops++
			groups += len(document.Homepage.Groups)
		}
	}
	if desktops == 0 {
		groups = len(payload.State.Config.Homepage.Groups)
	}
	return desktops, groups
}
