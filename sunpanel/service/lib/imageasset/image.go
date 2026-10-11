package imageasset

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"path/filepath"
	"strings"
)

const Limit = 10 << 20

func Extension(data []byte) (string, error) {
	if len(data) == 0 || len(data) > Limit {
		return "", errors.New("图片为空或超过 10 MiB")
	}
	if config, format, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		if config.Width < 1 || config.Height < 1 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 40_000_000 {
			return "", errors.New("图片尺寸过大")
		}
		ext := map[string]string{"png": ".png", "jpeg": ".jpg", "gif": ".gif", "webp": ".webp"}[format]
		if ext != "" {
			return ext, nil
		}
	}
	if validICO(data) {
		return ".ico", nil
	}
	if validSVG(data) {
		return ".svg", nil
	}
	return "", errors.New("图片格式无效，或 SVG 包含脚本/外部资源")
}
func Validate(name string, data []byte) bool {
	ext, err := Extension(data)
	actual := strings.ToLower(filepath.Ext(name))
	if actual == ".jpeg" {
		actual = ".jpg"
	}
	return err == nil && actual == ext
}
func validICO(data []byte) bool {
	if len(data) < 22 || !bytes.Equal(data[:4], []byte{0, 0, 1, 0}) {
		return false
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count < 1 || count > 256 || len(data) < 6+16*count {
		return false
	}
	for i := 0; i < count; i++ {
		entry := data[6+16*i : 22+16*i]
		size := uint64(binary.LittleEndian.Uint32(entry[8:12]))
		offset := uint64(binary.LittleEndian.Uint32(entry[12:16]))
		if size < 8 || offset < uint64(6+16*count) || offset+size > uint64(len(data)) {
			return false
		}
		payload := data[offset : offset+size]
		if bytes.HasPrefix(payload, []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
			config, _, err := image.DecodeConfig(bytes.NewReader(payload))
			if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 256 || config.Height > 256 {
				return false
			}
		} else if len(payload) < 40 || binary.LittleEndian.Uint32(payload[:4]) != 40 {
			return false
		} else {
			w, h := binary.LittleEndian.Uint32(payload[4:8]), binary.LittleEndian.Uint32(payload[8:12])
			if w < 1 || w > 256 || h < 2 || h > 512 {
				return false
			}
		}
	}
	return true
}
func validSVG(data []byte) bool {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	seen := false
	allowed := " svg g path rect circle ellipse line polyline polygon text tspan defs use symbol clippath mask pattern lineargradient radialgradient stop title desc "
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return seen && depth == 0
		}
		if err != nil {
			return false
		}
		switch v := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if seen || v.Name.Local != "svg" {
					return false
				}
				seen = true
			}
			if (v.Name.Space != "" && v.Name.Space != "http://www.w3.org/2000/svg") || !strings.Contains(allowed, " "+strings.ToLower(v.Name.Local)+" ") {
				return false
			}
			for _, attr := range v.Attr {
				name, value := strings.ToLower(attr.Name.Local), strings.ToLower(strings.TrimSpace(attr.Value))
				if strings.HasPrefix(name, "on") || name == "src" || name == "base" || (name == "href" && !strings.HasPrefix(value, "#")) {
					return false
				}
				if name == "style" && (strings.Contains(value, "url") || strings.ContainsAny(value, "\\@")) {
					return false
				}
				// Only local fragment references are supported in SVG paint/filter attributes.
				if strings.Contains(value, "url(") && !strings.HasPrefix(value, "url(#") {
					return false
				}
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.Directive:
			return false
		case xml.ProcInst:
			if v.Target != "xml" {
				return false
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(v)) != "" {
				return false
			}
		}
	}
}
