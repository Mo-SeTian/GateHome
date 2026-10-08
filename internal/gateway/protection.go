package gateway

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Protection extends an IP firewall. Zero values preserve legacy behavior.
type Protection struct {
	WAF    WAFPolicy    `json:"waf"`
	Rate   RatePolicy   `json:"rate"`
	Rules  []HTTPRule   `json:"rules"`
	Freeze FreezePolicy `json:"freeze"`
}
type WAFPolicy struct {
	Mode       string         `json:"mode"`
	Level      int            `json:"level"`
	BodyLimit  int            `json:"body_limit"`
	Exceptions []WAFException `json:"exceptions"`
}
type WAFException struct {
	RuleID    int    `json:"rule_id"`
	Host      string `json:"host"`
	Path      string `json:"path"`
	Parameter string `json:"parameter"`
}
type RatePolicy struct {
	Enabled       bool   `json:"enabled"`
	Requests      int    `json:"requests"`
	WindowSeconds int    `json:"window_seconds"`
	Path          string `json:"path"`
}
type FreezePolicy struct {
	Enabled       bool `json:"enabled"`
	Failures      int  `json:"failures"`
	WindowSeconds int  `json:"window_seconds"`
	Seconds       int  `json:"seconds"`
}
type HTTPRule struct {
	Name    string `json:"name"`
	Target  string `json:"target"`
	Pattern string `json:"pattern"`
	Action  string `json:"action"`
}
type SecurityHit struct {
	Severity string `json:"severity,omitempty"`
	Engine   string `json:"engine"`
	RuleID   int    `json:"rule_id"`
	Name     string `json:"name"`
	Action   string `json:"action"`
	Score    int    `json:"score,omitempty"`
}

func (p Protection) wafEnabled() bool { return p.WAF.Mode == "detect" || p.WAF.Mode == "block" }
func (p Protection) active() bool     { return p.wafEnabled() || p.Rate.Enabled || len(p.Rules) > 0 }
func (p Protection) bodyLimit() int {
	if p.WAF.BodyLimit == 0 {
		return 1 << 20
	}
	return p.WAF.BodyLimit
}

var safeExceptionPath = regexp.MustCompile(`^/[a-zA-Z0-9/_.~-]*$`)
var safeParameter = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,100}$`)

func validateProtection(p Protection) error {
	if p.WAF.Mode != "" && p.WAF.Mode != "off" && p.WAF.Mode != "detect" && p.WAF.Mode != "block" {
		return errors.New("WAF 模式无效")
	}
	if p.WAF.Level < 0 || p.WAF.Level > 4 || (p.WAF.BodyLimit != 0 && (p.WAF.BodyLimit < 65536 || p.WAF.BodyLimit > 32<<20)) {
		return errors.New("WAF 等级须为 1–4，请求体上限须为 64 KiB–32 MiB")
	}
	if len(p.WAF.Exceptions) > 50 || len(p.Rules) > 50 {
		return errors.New("规则例外和自定义规则各最多 50 条")
	}
	for _, e := range p.WAF.Exceptions {
		if e.RuleID < 911000 || e.RuleID >= 949000 || (e.Host != "" && !validDomain(e.Host)) || len(e.Path) > 256 || !safeExceptionPath.MatchString(e.Path) || (e.Parameter != "" && !safeParameter.MatchString(e.Parameter)) {
			return errors.New("WAF 例外：规则编号须为 911000–948999，路径须以 / 开头且只含字母、数字、/、_、.、~、-；参数仅支持字母、数字、_、.、-")
		}
	}
	for _, r := range p.Rules {
		if strings.TrimSpace(r.Name) == "" || len(r.Name) > 100 || strings.ContainsAny(r.Name, "\r\n\x00") || (r.Target != "user_agent" && r.Target != "path" && r.Target != "method") || (r.Action != "block" && r.Action != "detect") || r.Pattern == "" || len(r.Pattern) > 256 {
			return errors.New("自定义规则的名称、目标、动作或表达式无效")
		}
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return errors.New("自定义规则「" + r.Name + "」正则表达式无效")
		}
	}
	if p.Rate.Enabled && (p.Rate.Requests < 1 || p.Rate.Requests > 100000 || p.Rate.WindowSeconds < 1 || p.Rate.WindowSeconds > 3600 || len(p.Rate.Path) > 256 || !strings.HasPrefix(p.Rate.Path, "/") || strings.ContainsAny(p.Rate.Path, "\r\n\x00")) {
		return errors.New("访问限速：次数须为 1–100000，时间窗口为 1–3600 秒，路径须以 / 开头")
	}
	if p.Freeze.Enabled && (p.Freeze.Failures < 2 || p.Freeze.Failures > 10000 || p.Freeze.WindowSeconds < 1 || p.Freeze.WindowSeconds > 86400 || p.Freeze.Seconds < 60 || p.Freeze.Seconds > 604800) {
		return errors.New("自动冻结：触发次数须为 2–10000，累计窗口为 1–86400 秒，冻结时长为 60–604800 秒")
	}
	return nil
}

type protectionWindow struct {
	start   time.Time
	expires time.Time
	count   int
}
type protectionCounters struct {
	mu        sync.Mutex
	windows   map[string]protectionWindow
	lastSweep time.Time
	now       func() time.Time
}

func newProtectionCounters() *protectionCounters {
	return &protectionCounters{windows: map[string]protectionWindow{}, now: time.Now}
}

// Counts are isolated by route, IP and cause. State is bounded and expires.
func (c *protectionCounters) increment(key string, seconds, limit int) (bool, int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if now.Sub(c.lastSweep) >= time.Minute {
		for k, v := range c.windows {
			if !now.Before(v.expires) {
				delete(c.windows, k)
			}
		}
		c.lastSweep = now
	}
	v, exists := c.windows[key]
	if !exists && len(c.windows) >= 32768 {
		return false, seconds, false
	}
	if !exists || now.Sub(v.start) >= time.Duration(seconds)*time.Second {
		v = protectionWindow{start: now, expires: now.Add(time.Duration(seconds) * time.Second)}
	}
	if v.count <= limit {
		v.count++
	}
	c.windows[key] = v
	return v.count > limit, max(1, int(v.expires.Sub(now).Seconds())+1), true
}
func (c *protectionCounters) clear(route, ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := route + "|" + ip + "|"
	for k := range c.windows {
		if strings.HasPrefix(k, prefix) {
			delete(c.windows, k)
		}
	}
}
func securityReason(h SecurityHit) string {
	return fmt.Sprintf("%s · 规则 %d · %s", h.Engine, h.RuleID, h.Name)
}
