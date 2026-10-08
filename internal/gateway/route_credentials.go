package gateway

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

func routeKey(r Route) string { return r.GroupID + "/" + r.Host }

func validateRouteAuth(auth RouteAuthConfig) error {
	if auth.FailureLimit < 0 || auth.FailureLimit > 100 || auth.FreezeSeconds < 0 || auth.FreezeSeconds > 604800 || (auth.FreezeSeconds != 0 && (auth.FreezeSeconds < 60 || auth.FreezeSeconds%60 != 0)) {
		return errors.New("验证失败次数须为 1–100 次，冻结时长须为 1–10080 分钟")
	}
	if len(auth.Username) > 64 || !utf8.ValidString(auth.Username) || strings.TrimSpace(auth.Username) != auth.Username || strings.ContainsFunc(auth.Username, unicode.IsControl) || (auth.Enabled && auth.Username == "") {
		return errors.New("反代访问账号须为 1–64 字节，不能包含控制字符或首尾空格")
	}
	return nil
}

func hashRoutePasswords(c Config, passwords map[string]*string) (map[string]string, error) {
	routes := map[string]bool{}
	for _, r := range c.Routes {
		routes[routeKey(r)] = true
	}
	hashes := map[string]string{}
	for key, value := range passwords {
		if !routes[key] {
			return nil, errors.New("访问密码引用了不存在的反代规则")
		}
		if value == nil {
			continue
		}
		if *value == "" {
			hashes[key] = ""
			continue
		}
		hash, err := HashPassword([]byte(*value))
		if err != nil {
			return nil, errors.New("反代访问密码须为 12–72 字节")
		}
		hashes[key] = hash
	}
	return hashes, nil
}

func validateRouteCredentials(s State) error {
	if err := validateIPBlocks(s.IPBlocks); err != nil {
		return err
	}
	for _, r := range s.Config.Routes {
		hash := s.RoutePasswordHashes[routeKey(r)]
		if r.Auth.Enabled && hash == "" {
			return errors.New("请为开启账号验证的反代规则设置独立访问密码")
		}
		if hash != "" {
			cost, err := bcrypt.Cost([]byte(hash))
			if err != nil || cost != bcrypt.DefaultCost {
				return errors.New("反代访问密码数据无效")
			}
		}
	}
	return nil
}
