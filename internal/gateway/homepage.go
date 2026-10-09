package gateway

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

type HomepageLink struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	LAN         string `json:"lan"`
	WAN         string `json:"wan"`
	Image       string `json:"image"`
	Favorite    bool   `json:"favorite"`
}

type HomepagePage struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Rows          int            `json:"rows"`
	Columns       int            `json:"columns"`
	MobileColumns int            `json:"mobile_columns"`
	Links         []HomepageLink `json:"links"`
}

type HomepageGroup struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Pages []HomepagePage `json:"pages"`
}

type HomepageSearchEngine struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	Image string `json:"image"`
}

type HomepageConfig struct {
	Enabled       bool                   `json:"enabled,omitempty"`
	Port          int                    `json:"port,omitempty"`
	Public        bool                   `json:"public"`
	Title         string                 `json:"title"`
	Tone          string                 `json:"tone"`
	Background    string                 `json:"background"`
	Shade         int                    `json:"shade"`
	Compact       bool                   `json:"compact"`
	ShowAddresses bool                   `json:"show_addresses"`
	CustomCSS     string                 `json:"custom_css"`
	SearchEngines []HomepageSearchEngine `json:"search_engines"`
	Groups        []HomepageGroup        `json:"groups"`
	Widgets       *HomepageWidgets       `json:"widgets,omitempty"`
}

func defaultHomepage() HomepageConfig {
	return HomepageConfig{Port: 16680, Public: true, Title: "我的数字空间", Tone: "forest", Shade: 50, Groups: []HomepageGroup{}, SearchEngines: []HomepageSearchEngine{
		{ID: "baidu", Name: "百度", URL: "https://www.baidu.com/s?wd={query}"},
		{ID: "google", Name: "Google", URL: "https://www.google.com/search?q={query}"},
	}}
}

func homepageImages(c HomepageConfig) map[string]bool {
	images := map[string]bool{}
	for _, e := range c.SearchEngines {
		if e.Image != "" {
			images[e.Image] = true
		}
	}
	for _, g := range c.Groups {
		for _, p := range g.Pages {
			for _, l := range p.Links {
				if l.Image != "" {
					images[l.Image] = true
				}
			}
		}
	}
	return images
}

// Older launchers snapshot only proxy-referenced icons. Their rollback would
// remove homepage-only icons, and they cannot restore the new background path.
func (m *Maintenance) checkHomepageFiles(incoming *HomepageConfig) error {
	if _, err := os.Lstat(m.paths.directory("page")); err == nil && !m.pageFiles {
		return errors.New("当前启动器不支持 data/page 的完整恢复与回滚，请使用最新安装脚本更新启动器并保留 config、log、data；Docker 请保留挂载目录后重建镜像")
	}
	if m.homepageFiles {
		return nil
	}
	var current struct {
		Config struct {
			Homepage HomepageConfig `json:"homepage"`
		} `json:"config"`
	}
	data, err := os.ReadFile(m.paths.file("state.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("当前首页配置无法检查")
	}
	if err == nil && json.Unmarshal(data, &current) != nil {
		return errors.New("当前首页配置无效")
	}
	needsFiles := func(c HomepageConfig) bool { return c.Background != "" || len(homepageImages(c)) > 0 }
	if needsFiles(current.Config.Homepage) || (incoming != nil && needsFiles(*incoming)) {
		return errors.New("旧启动器不支持首页图片的完整恢复与回滚，请使用最新安装脚本更新启动器（保留 config、log、data），Docker 请保留目录后重建镜像")
	}
	return nil
}

func validateHomepage(c HomepageConfig, groups []ProxyGroup) error {
	// Zero-valued configurations from older backups remain compatible.
	if c.Port == 0 && !c.Enabled && len(c.Groups) == 0 && c.CustomCSS == "" && c.Background == "" && c.SearchEngines == nil {
		return nil
	}
	if c.Port < 1024 || c.Port > 65535 {
		return errors.New("首页端口须为 1024–65535")
	}
	for _, g := range groups {
		if c.Enabled && (c.Port == g.HTTPPort || c.Port == g.HTTPSPort) {
			return errors.New("首页端口不能与反代监听端口相同")
		}
	}
	if strings.TrimSpace(c.Title) == "" || len(c.Title) > 160 || c.Shade < 0 || c.Shade > 90 || (c.Tone != "forest" && c.Tone != "dusk" && c.Tone != "midnight") || len(c.CustomCSS) > 32768 {
		return errors.New("首页标题、背景设置或 CSS 无效（CSS 最多 32 KiB）")
	}
	if c.Background != "" && !routeImageID.MatchString(c.Background) {
		return errors.New("首页背景图片引用无效")
	}
	if len(c.SearchEngines) < 1 || len(c.SearchEngines) > 12 {
		return errors.New("首页须设置 1–12 个搜索引擎")
	}
	engineIDs := map[string]bool{}
	for _, e := range c.SearchEngines {
		u, err := imageURL(e.URL)
		if !idPattern.MatchString(e.ID) || engineIDs[e.ID] || strings.TrimSpace(e.Name) == "" || len(e.Name) > 64 || err != nil || strings.ContainsAny(e.URL, "\r\n\x00") || strings.Count(e.URL, "{query}") != 1 || (!strings.Contains(u.Path, "{query}") && !strings.Contains(u.RawQuery, "{query}")) {
			return errors.New("搜索引擎名称或地址无效：须使用不含账号密码的 HTTP / HTTPS 地址，并在路径或查询参数中填写一次 {query}")
		}
		engineIDs[e.ID] = true
		if e.Image != "" && !routeImageID.MatchString(e.Image) {
			return errors.New("搜索引擎图标引用无效")
		}
	}
	if len(c.Groups) > 30 {
		return errors.New("首页最多支持 30 个分组")
	}
	ids, count := map[string]bool{}, 0
	validID := func(id string) bool {
		if !idPattern.MatchString(id) || ids[id] {
			return false
		}
		ids[id] = true
		return true
	}
	for _, g := range c.Groups {
		if !validID(g.ID) || strings.TrimSpace(g.Name) == "" || len(g.Name) > 100 || len(g.Pages) < 1 || len(g.Pages) > 30 {
			return errors.New("首页分组名称、ID 或页面数量无效，每组须有 1–30 个页面")
		}
		for _, p := range g.Pages {
			if !validID(p.ID) || strings.TrimSpace(p.Name) == "" || len(p.Name) > 100 || p.Rows < 1 || p.Rows > 8 || p.Columns < 1 || p.Columns > 8 || p.MobileColumns < 1 || p.MobileColumns > 3 {
				return errors.New("首页页面须设置 1–8 行、1–8 列，手机端 1–3 列")
			}
			for _, l := range p.Links {
				count++
				if count > 200 || !validID(l.ID) || strings.TrimSpace(l.Name) == "" || len(l.Name) > 100 || len(l.Description) > 240 || (l.LAN == "" && l.WAN == "") {
					return errors.New("首页链接名称、ID 或地址无效，最多 200 个链接")
				}
				for _, raw := range []string{l.LAN, l.WAN} {
					if raw != "" {
						if _, err := imageURL(raw); err != nil || strings.ContainsAny(raw, "\r\n\x00") {
							return errors.New("首页链接须为不含账号密码的 HTTP / HTTPS 地址")
						}
					}
				}
				if l.Image != "" && !routeImageID.MatchString(l.Image) {
					return errors.New("首页链接图片引用无效")
				}
			}
		}
	}
	return nil
}

func validateHomepageListener(c HomepageConfig, groups []ProxyGroup) error {
	if c.Port < 1024 || c.Port > 65535 {
		return errors.New("首页端口须为 1024–65535")
	}
	for _, group := range groups {
		if c.Enabled && (c.Port == group.HTTPPort || c.Port == group.HTTPSPort) {
			return errors.New("首页端口不能与反代监听端口相同")
		}
	}
	return nil
}
