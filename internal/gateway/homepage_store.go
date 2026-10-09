package gateway

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

const homepageAdminSpace = "admin"

var homepageUserID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type HomepageUser struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	Enabled      bool   `json:"enabled"`
}

type HomepageWidgets struct {
	Clock  bool `json:"clock"`
	Search bool `json:"search"`
}

type homepageDocument struct {
	Homepage HomepageConfig `json:"homepage"`
	Revision int            `json:"revision"`
}

type homepageStore struct {
	mu        sync.RWMutex
	data      string
	documents map[string]homepageDocument
}

func homepageSpaceIDs(state State) []string {
	ids := []string{homepageAdminSpace}
	for id := range state.HomepageUsers {
		ids = append(ids, id)
	}
	return ids
}

func normalizeHomepage(c *HomepageConfig) {
	c.Enabled, c.Port = false, 0
	if c.Groups == nil {
		c.Groups = []HomepageGroup{}
	}
	for i := range c.Groups {
		for j := range c.Groups[i].Pages {
			if c.Groups[i].Pages[j].Links == nil {
				c.Groups[i].Pages[j].Links = []HomepageLink{}
			}
		}
	}
	if c.SearchEngines == nil {
		c.SearchEngines = defaultHomepage().SearchEngines
	}
	if c.Widgets == nil {
		c.Widgets = &HomepageWidgets{Clock: true, Search: true}
	}
}

func cloneHomepage(c HomepageConfig) HomepageConfig {
	content, _ := json.Marshal(c)
	var clone HomepageConfig
	_ = json.Unmarshal(content, &clone)
	return clone
}

func validateHomepageContent(c HomepageConfig) error {
	if c.Enabled || c.Port != 0 {
		return errors.New("首页开关和端口由 GateHome 管理")
	}
	c.Port = 16680
	return validateHomepage(c, nil)
}

func homepageSpaceDirectory(data, id string) (string, error) {
	if id != homepageAdminSpace && !homepageUserID.MatchString(id) {
		return "", errors.New("首页用户目录无效")
	}
	base := filepath.Join(data, "page")
	dir := filepath.Join(base, id)
	for _, path := range []string{base, dir} {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("首页目录须为普通目录")
		}
	}
	return dir, nil
}

func readHomepageDocument(data, id string) (homepageDocument, error) {
	var document homepageDocument
	dir, err := homepageSpaceDirectory(data, id)
	if err != nil {
		return document, err
	}
	path := filepath.Join(dir, "config.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return document, errors.New("首页配置文件无效或超过 1 MiB")
	}
	content, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(content, &document) != nil || document.Revision < 1 {
		return document, errors.New("首页配置无法读取")
	}
	normalizeHomepage(&document.Homepage)
	if err := validateHomepageContent(document.Homepage); err != nil {
		return document, err
	}
	return document, nil
}

func openHomepageStore(data string, state State) (*homepageStore, error) {
	base := filepath.Join(data, "page")
	if err := os.MkdirAll(base, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(base)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("首页数据目录无效")
	}
	s := &homepageStore{data: data, documents: map[string]homepageDocument{}}
	for _, id := range homepageSpaceIDs(state) {
		initial := defaultHomepage()
		if id != homepageAdminSpace {
			initial.Public = false
		} else if legacyHomepageContent(state.Config.Homepage) {
			initial = state.Config.Homepage
		}
		if err := s.ensure(id, initial); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func legacyHomepageContent(c HomepageConfig) bool {
	return c.Title != "" || c.Background != "" || c.CustomCSS != "" || len(c.Groups) != 0 || len(c.SearchEngines) != 0
}

func (s *homepageStore) ensure(id string, initial HomepageConfig) error {
	if id != homepageAdminSpace && !homepageUserID.MatchString(id) {
		return errors.New("首页用户 ID 无效")
	}
	dir := filepath.Join(s.data, "page", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if _, err := homepageSpaceDirectory(s.data, id); err != nil {
		return err
	}
	path := filepath.Join(dir, "config.json")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		// Existing homepage assets are copied before the legacy configuration is removed.
		for image := range homepageImages(initial) {
			content, err := readRouteImage(s.data, image)
			if err != nil {
				return errors.New("旧首页图标无法迁移，请恢复缺失的图片后再试")
			}
			if err := storeHomepageAsset(dir, "route-images", image+".img", content); err != nil {
				return err
			}
		}
		if initial.Background != "" {
			content, err := readHomepageBackground(s.data, initial.Background)
			if err != nil {
				return errors.New("旧首页背景无法迁移")
			}
			if err := storeHomepageAsset(dir, "homepage-backgrounds", initial.Background+".jpg", content); err != nil {
				return err
			}
		}
		normalizeHomepage(&initial)
		if err := validateHomepageContent(initial); err != nil {
			return err
		}
		if err := writeJSON(path, homepageDocument{Homepage: initial, Revision: 1}); err != nil {
			return err
		}
	}
	document, err := readHomepageDocument(s.data, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.documents[id] = document
	s.mu.Unlock()
	return nil
}

func storeHomepageAsset(dir, folder, name string, content []byte) error {
	parent := filepath.Join(dir, folder)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("首页图片目录无效")
	}
	return atomicWrite(filepath.Join(parent, name), content)
}

func (s *homepageStore) snapshot(id string) (homepageDocument, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	document, ok := s.documents[id]
	return document, ok
}

func (s *homepageStore) update(id string, c HomepageConfig, revision int) (homepageDocument, error) {
	if err := validateHomepageContent(c); err != nil {
		return homepageDocument{}, err
	}
	normalizeHomepage(&c)
	dir, err := homepageSpaceDirectory(s.data, id)
	if err != nil {
		return homepageDocument{}, err
	}
	for image := range homepageImages(c) {
		if _, err := readRouteImage(dir, image); err != nil {
			return homepageDocument{}, errors.New("首页图标不存在，请重新上传或导入")
		}
	}
	if c.Background != "" {
		if _, err := readHomepageBackground(dir, c.Background); err != nil {
			return homepageDocument{}, errors.New("首页背景不存在，请重新上传")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, ok := s.documents[id]; !ok || previous.Revision != revision {
		return homepageDocument{}, errors.New("桌面已被修改，请刷新后重试")
	}
	document := homepageDocument{Homepage: c, Revision: revision + 1}
	if err := writeJSON(filepath.Join(dir, "config.json"), document); err != nil {
		return homepageDocument{}, errors.New("首页配置保存失败")
	}
	s.documents[id] = document
	pruneRouteImages(dir, Config{Homepage: c})
	pruneHomepageBackgrounds(dir, c.Background)
	return document, nil
}
