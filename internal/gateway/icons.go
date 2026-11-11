package gateway

import (
	"bytes"
	"embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"image"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/mozillazg/go-pinyin"
)

//go:embed icon_builtin/*.svg
var builtinIcons embed.FS

var iconSources = []string{"builtin", "imported", "uploads"}

const iconRefPrefix = "/icons/"
const iconFilePrefix = "/api/icons/file/"

type libraryIcon struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	CNName   string   `json:"cnName,omitempty"`
	Pinyin   []string `json:"pinyin,omitempty"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
	Style    string   `json:"style"`
	Format   string   `json:"format"`
	Source   string   `json:"source"`
	Pack     string   `json:"pack,omitempty"`
	RelPath  string   `json:"relPath"`
	URL      string   `json:"url"`
	Width    int      `json:"width,omitempty"`
	Height   int      `json:"height,omitempty"`
	Bytes    int64    `json:"bytes"`
}

type iconStats struct {
	Total      int            `json:"total"`
	Bytes      int64          `json:"bytes"`
	Path       string         `json:"path"`
	Sources    map[string]int `json:"sources"`
	Categories map[string]int `json:"categories"`
	Styles     map[string]int `json:"styles"`
}

type iconIndex struct {
	Items    []libraryIcon    `json:"items"`
	Postings map[string][]int `json:"postings"`
	Stats    iconStats        `json:"stats"`
	terms    [][]string
}

type iconLibrary struct {
	once  sync.Once
	mu    sync.RWMutex
	index *iconIndex
	jobMu sync.Mutex
	job   iconImportStatus
}

var iconMIMEs = map[string]string{
	"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg",
	"gif": "image/gif", "webp": "image/webp", "ico": "image/x-icon", "svg": "image/svg+xml",
}

func iconFormat(name string) string { return strings.ToLower(strings.TrimPrefix(path.Ext(name), ".")) }

func safeIconPath(rel string) bool {
	if len(rel) > 1024 || !fs.ValidPath(rel) || strings.ContainsAny(rel, "\\:%?#") || strings.ContainsFunc(rel, unicode.IsControl) {
		return false
	}
	source, _, ok := strings.Cut(rel, "/")
	return ok && slices.Contains(iconSources, source) && iconMIMEs[iconFormat(rel)] != "" &&
		!strings.Contains(rel, "/.") && strings.TrimSpace(path.Base(rel)) == path.Base(rel)
}

func iconFileURL(rel string) string {
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return iconFilePrefix + strings.Join(parts, "/")
}

// Check every component, including directories, and keep opens confined to the data root.
func plainIconPath(root *os.Root, rel string, dir, create bool) (fs.FileInfo, error) {
	parts := strings.Split(rel, "/")
	for i := range parts {
		name := strings.Join(parts[:i+1], "/")
		wantDir := i < len(parts)-1 || dir
		info, err := root.Lstat(name)
		if wantDir && create && errors.Is(err, fs.ErrNotExist) {
			if err := root.Mkdir(name, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
				return nil, err
			}
			info, err = root.Lstat(name)
		}
		if err != nil {
			return nil, err
		}
		if (wantDir && !info.IsDir()) || (!wantDir && !info.Mode().IsRegular()) {
			return nil, errors.New("图标路径不是普通目录或文件")
		}
		if i == len(parts)-1 {
			return info, nil
		}
	}
	return nil, fs.ErrInvalid
}

func openIconFile(data, rel string) (*os.File, fs.FileInfo, error) {
	if !safeIconPath(rel) {
		return nil, nil, fs.ErrInvalid
	}
	root, err := os.OpenRoot(data)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	name := "icons/" + rel
	if _, err := plainIconPath(root, name, false, false); err != nil {
		return nil, nil, err
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxImageUpload {
		file.Close()
		return nil, nil, fs.ErrInvalid
	}
	return file, info, nil
}

func stageIcons(data, prefix string) (string, error) {
	root, err := os.OpenRoot(data)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if _, err := plainIconPath(root, "icons", true, false); err != nil {
		return "", err
	}
	return os.MkdirTemp(filepath.Join(data, "icons"), prefix)
}

func prepareIconDirs(data string) error {
	root, err := os.OpenRoot(data)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, source := range iconSources {
		if _, err := plainIconPath(root, "icons/"+source, true, true); err != nil {
			return err
		}
	}
	entries, err := builtinIcons.ReadDir("icon_builtin")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := "icons/builtin/" + entry.Name()
		if _, err := root.Lstat(name); !errors.Is(err, fs.ErrNotExist) {
			if err != nil {
				return err
			}
			continue
		}
		content, err := builtinIcons.ReadFile("icon_builtin/" + entry.Name())
		if err != nil {
			return err
		}
		if err := atomicWrite(filepath.Join(data, name), content); err != nil {
			return err
		}
	}
	return nil
}

func iconDimensions(r io.Reader, format string) (int, int, error) {
	switch format {
	case "svg":
		d := xml.NewDecoder(r)
		var depth, width, height int
		seenRoot := false
		for {
			token, err := d.Token()
			if errors.Is(err, io.EOF) && seenRoot && depth == 0 {
				return width, height, nil
			}
			if err != nil {
				return 0, 0, fs.ErrInvalid
			}
			switch t := token.(type) {
			case xml.StartElement:
				if depth == 0 {
					if seenRoot || t.Name.Local != "svg" || (t.Name.Space != "" && t.Name.Space != "http://www.w3.org/2000/svg") {
						return 0, 0, fs.ErrInvalid
					}
					seenRoot = true
					width, height = svgIconSize(t.Attr)
				}
				depth++
			case xml.EndElement:
				depth--
			case xml.CharData:
				if depth == 0 && len(bytes.TrimSpace(bytes.TrimPrefix(t, []byte("\xef\xbb\xbf")))) > 0 {
					return 0, 0, fs.ErrInvalid
				}
			}
		}
	case "ico":
		data, err := io.ReadAll(r)
		if err != nil || !validDiscoveryICO(data) {
			return 0, 0, fs.ErrInvalid
		}
		w, h := 1+(int(data[6])+255)%256, 1+(int(data[7])+255)%256
		return w, h, nil
	default:
		config, detected, err := image.DecodeConfig(r)
		if format == "jpg" {
			format = "jpeg"
		}
		if err != nil || detected != format || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 {
			return 0, 0, fs.ErrInvalid
		}
		return config.Width, config.Height, nil
	}
}

func svgIconSize(attrs []xml.Attr) (int, int) {
	parse := func(value string) int {
		n, err := strconv.ParseFloat(strings.TrimSuffix(value, "px"), 64)
		if err != nil || !(n >= 1 && n <= 100_000) {
			return 0
		}
		return int(n)
	}
	var width, height, viewWidth, viewHeight int
	for _, attr := range attrs {
		switch attr.Name.Local {
		case "width":
			width = parse(attr.Value)
		case "height":
			height = parse(attr.Value)
		case "viewBox":
			parts := strings.Fields(strings.ReplaceAll(attr.Value, ",", " "))
			if len(parts) == 4 {
				viewWidth, viewHeight = parse(parts[2]), parse(parts[3])
			}
		}
	}
	if width > 0 && height > 0 {
		return width, height
	}
	return viewWidth, viewHeight
}

func (l *iconLibrary) reindex(data string) error {
	if err := prepareIconDirs(data); err != nil {
		return err
	}
	rootPath := filepath.Join(data, "icons")
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	defer root.Close()
	index := &iconIndex{
		Items: []libraryIcon{}, Postings: make(map[string][]int),
		Stats: iconStats{Path: iconRefPrefix, Sources: map[string]int{"builtin": 0, "imported": 0, "uploads": 0}, Categories: map[string]int{}, Styles: map[string]int{}},
	}
	packs := map[string]map[string]iconMetadata{}
	for _, source := range iconSources {
		err := fs.WalkDir(root.FS(), source, func(rel string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if strings.HasPrefix(entry.Name(), ".") {
					return fs.SkipDir
				}
				return nil
			}
			if !entry.Type().IsRegular() || !safeIconPath(rel) {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Size() == 0 || info.Size() > maxImageUpload {
				return nil
			}
			file, err := root.Open(rel)
			if err != nil {
				return err
			}
			format := iconFormat(rel)
			w, h, err := iconDimensions(file, format)
			file.Close()
			if err != nil {
				return nil
			}
			icon := libraryIcon{ID: rel, Name: strings.TrimSuffix(entry.Name(), path.Ext(rel)), RelPath: rel,
				Source: source, Format: format, Style: "original", URL: iconFileURL(rel), Bytes: info.Size(), Width: w, Height: h}
			if format == "svg" {
				icon.Style = "svg"
			}
			parts := strings.Split(rel, "/")
			if source == "imported" && len(parts) >= 3 {
				icon.Pack = parts[1]
				if len(parts) >= 4 && slices.Contains(importIconStyles, parts[2]) {
					icon.Style = parts[2]
				}
				if _, ok := packs[icon.Pack]; !ok {
					packs[icon.Pack] = readPackMetadata(root, icon.Pack)
				}
			}
			applyIconMetadata(&icon, packs[icon.Pack][rel])
			index.Items = append(index.Items, icon)
			index.Stats.Bytes += icon.Bytes
			index.Stats.Sources[source]++
			index.Stats.Categories[icon.Category]++
			index.Stats.Styles[icon.Style]++
			return nil
		})
		if err != nil {
			return err
		}
	}
	index.Stats.Total = len(index.Items)
	for i, icon := range index.Items {
		terms := []string{icon.Name, icon.CNName, icon.Category, icon.Style, icon.Format, icon.Pack}
		terms = append(terms, icon.Pinyin...)
		terms = append(terms, icon.Tags...)
		terms = append(terms, strings.Join(strings.FieldsFunc(icon.Name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }), ""))
		seen := map[string]bool{}
		for j, term := range terms {
			terms[j] = strings.ToLower(term)
			for _, gram := range iconGrams(terms[j]) {
				if !seen[gram] {
					index.Postings[gram] = append(index.Postings[gram], i)
					seen[gram] = true
				}
			}
		}
		index.terms = append(index.terms, terms)
	}
	encoded, err := json.Marshal(index)
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(rootPath, "index.json"), encoded); err != nil {
		return err
	}
	l.index = index
	return nil
}

func iconGrams(term string) []string {
	runes := []rune(term)
	grams := make([]string, 0, len(runes)*2)
	for i, r := range runes {
		grams = append(grams, string(r))
		if i+1 < len(runes) {
			grams = append(grams, string(runes[i:i+2]))
		}
	}
	return grams
}

func (index *iconIndex) search(keyword, category, source, style string, page, limit int) ([]libraryIcon, int) {
	words := strings.Fields(strings.ToLower(keyword))
	var candidates []int
	for _, word := range words {
		for _, gram := range iconGrams(word) {
			posting := index.Postings[gram]
			if len(posting) == 0 {
				return []libraryIcon{}, 0
			}
			if candidates == nil || len(posting) < len(candidates) {
				candidates = posting
			}
		}
	}
	items := make([]libraryIcon, 0, limit)
	total := 0
	match := func(i int) {
		icon := index.Items[i]
		if (category != "" && icon.Category != category && !slices.Contains(icon.Tags, category)) ||
			(source != "" && source != icon.Source && !strings.EqualFold(source, icon.Pack)) || (style != "" && icon.Style != style) {
			return
		}
		for _, word := range words {
			if !slices.ContainsFunc(index.terms[i], func(term string) bool { return strings.Contains(term, word) }) {
				return
			}
		}
		if total >= (page-1)*limit && len(items) < limit {
			items = append(items, icon)
		}
		total++
	}
	if candidates == nil {
		for i := range index.Items {
			match(i)
		}
	} else {
		for _, i := range candidates {
			match(i)
		}
	}
	return items, total
}

type iconMetadata struct {
	Name     string   `json:"name"`
	CNName   string   `json:"cnName"`
	Pinyin   []string `json:"pinyin"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
	URL      string   `json:"url"`
	RelPath  string   `json:"relPath"`
}

func applyIconMetadata(icon *libraryIcon, meta iconMetadata) {
	key := strings.TrimRight(strings.ToLower(icon.Name), "-_0123456789")
	defaults := iconAliases[key]
	if meta.Name != "" {
		icon.Name = meta.Name
	}
	icon.CNName = meta.CNName
	if icon.CNName == "" {
		icon.CNName = defaults.CNName
	}
	icon.Category = meta.Category
	if icon.Category == "" {
		icon.Category = defaults.Category
	}
	if icon.Category == "" {
		icon.Category = "other"
	}
	if icon.Source == "uploads" {
		icon.Category = "uploads"
	}
	icon.Tags = append([]string{icon.Category}, defaults.Tags...)
	icon.Tags = append(icon.Tags, meta.Tags...)
	icon.Pinyin = meta.Pinyin
	for _, label := range []string{icon.Name, icon.CNName} {
		addIconPinyin(icon, label)
		words := strings.Fields(label)
		if len(words) > 1 {
			for _, word := range words {
				addIconPinyin(icon, word)
			}
		}
	}
}

func addIconPinyin(icon *libraryIcon, label string) {
	syllables := pinyin.LazyPinyin(label, pinyin.NewArgs())
	if len(syllables) == 0 {
		return
	}
	var initials strings.Builder
	for _, syllable := range syllables {
		initials.WriteByte(syllable[0])
	}
	for _, value := range []string{strings.Join(syllables, ""), initials.String()} {
		if !slices.Contains(icon.Pinyin, value) {
			icon.Pinyin = append(icon.Pinyin, value)
		}
	}
}

var iconAliases = map[string]iconMetadata{
	"web":            {CNName: "网页 网站", Category: "tool", Tags: []string{"website", "http"}},
	"nas":            {CNName: "网络存储", Category: "nas", Tags: []string{"storage"}},
	"gateway":        {CNName: "网关 路由器", Category: "tool", Tags: []string{"router", "network"}},
	"linux":          {CNName: "终端 操作系统", Category: "nas", Tags: []string{"terminal", "system"}},
	"database":       {CNName: "数据库", Category: "tool", Tags: []string{"sql"}},
	"network":        {CNName: "网络", Category: "tool"},
	"cloud":          {CNName: "云服务", Category: "tool"},
	"media":          {CNName: "影音媒体", Category: "media", Tags: []string{"video"}},
	"download":       {CNName: "下载传输", Category: "download"},
	"shield":         {CNName: "防火墙", Category: "tool", Tags: []string{"security"}},
	"container":      {CNName: "容器", Category: "tool", Tags: []string{"docker"}},
	"monitor":        {CNName: "状态监控", Category: "tool"},
	"jellyfin":       {CNName: "果冻影音", Category: "media", Tags: []string{"媒体库", "docker"}},
	"plex":           {CNName: "影音媒体库", Category: "media", Tags: []string{"docker"}},
	"emby":           {CNName: "影音媒体库", Category: "media", Tags: []string{"docker"}},
	"qbittorrent":    {CNName: "下载器", Category: "download", Tags: []string{"qb", "torrent", "docker"}},
	"transmission":   {CNName: "种子下载", Category: "download", Tags: []string{"torrent"}},
	"alist":          {CNName: "网盘挂载", Category: "nas", Tags: []string{"docker"}},
	"synology":       {CNName: "群晖", Category: "nas"},
	"qnap":           {CNName: "威联通", Category: "nas"},
	"truenas":        {CNName: "网络存储", Category: "nas"},
	"openwrt":        {CNName: "软路由", Category: "tool", Tags: []string{"router"}},
	"1panel":         {CNName: "服务器面板", Category: "tool", Tags: []string{"docker"}},
	"adguard":        {CNName: "广告过滤", Category: "tool", Tags: []string{"dns", "docker"}},
	"home-assistant": {CNName: "智能家居", Category: "tool", Tags: []string{"ha", "docker"}},
}
