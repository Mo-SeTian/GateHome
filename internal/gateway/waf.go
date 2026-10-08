package gateway

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"strings"

	crs "github.com/corazawaf/coraza-coreruleset/v4"
	"github.com/corazawaf/coraza/v3"
	"github.com/corazawaf/coraza/v3/collection"
	"github.com/corazawaf/coraza/v3/experimental/plugins/plugintypes"
	"github.com/corazawaf/coraza/v3/types/variables"
)

// The wrapper stays alive while a transaction is inspecting a request. Once no
// configuration references it, Coraza's shared regexp cache can be released.
type compiledWAF struct{ coraza.WAF }

func compileWAF(p Protection) (*compiledWAF, error) {
	if !p.wafEnabled() {
		return nil, nil
	}
	level := p.WAF.Level
	if level == 0 {
		level = 1
	}
	mode := "DetectionOnly"
	if p.WAF.Mode == "block" {
		mode = "On"
	}
	directives := fmt.Sprintf(`SecRuleEngine %s
SecAuditEngine Off
SecDebugLogLevel 0
SecResponseBodyAccess Off
SecRequestBodyLimit %d
SecRequestBodyInMemoryLimit %d
SecAction "id:10000,phase:1,pass,nolog,setvar:tx.blocking_paranoia_level=%d,setvar:tx.detection_paranoia_level=%d,setvar:tx.inbound_anomaly_score_threshold=5"
`, mode, p.bodyLimit()+1, p.bodyLimit()+1, level, level)
	for i, e := range p.WAF.Exceptions {
		ctl := fmt.Sprintf("ctl:ruleRemoveById=%d", e.RuleID)
		if e.Parameter != "" {
			ctl = fmt.Sprintf("ctl:ruleRemoveTargetById=%d;ARGS:%s", e.RuleID, e.Parameter)
		}
		if e.Host != "" {
			directives += fmt.Sprintf("SecRule SERVER_NAME \"@streq %s\" \"id:%d,phase:1,pass,nolog,t:none,chain\"\nSecRule REQUEST_URI \"@beginsWith %s\" \"t:none,%s\"\n", e.Host, 11000+i, e.Path, ctl)
		} else {
			directives += fmt.Sprintf("SecRule REQUEST_URI \"@beginsWith %s\" \"id:%d,phase:1,pass,nolog,t:none,%s\"\n", e.Path, 11000+i, ctl)
		}
	}
	waf, err := coraza.NewWAF(coraza.NewWAFConfig().WithRootFS(crs.FS).
		WithDirectivesFromFile("@coraza.conf-recommended").
		WithDirectivesFromFile("@crs-setup.conf.example").
		WithDirectives(directives).WithDirectivesFromFile("@owasp_crs/*.conf"))
	if err != nil {
		return nil, err
	}
	engine := &compiledWAF{WAF: waf}
	if closer, ok := waf.(io.Closer); ok {
		runtime.AddCleanup(engine, func(c io.Closer) { _ = c.Close() }, closer)
	}
	return engine, nil
}

// Request-only inspection leaves streaming responses and upgraded connections untouched.
// No request values, expanded messages or raw Coraza audit data leave this function.
func inspectWAF(waf coraza.WAF, p Protection, r *http.Request, consumed func()) ([]SecurityHit, int) {
	tx := waf.NewTransaction()
	defer func() { _ = tx.Close(); runtime.KeepAlive(waf) }()
	host, port, _ := net.SplitHostPort(r.RemoteAddr)
	n, _ := strconv.Atoi(port)
	tx.ProcessConnection(host, n, "", 0)
	tx.ProcessURI(r.URL.RequestURI(), r.Method, r.Proto)
	server := r.Host
	if h, _, err := net.SplitHostPort(server); err == nil {
		server = h
	}
	tx.SetServerName(strings.TrimSuffix(strings.ToLower(server), "."))
	for k, vs := range r.Header {
		for _, v := range vs {
			tx.AddRequestHeader(k, v)
		}
	}
	tx.AddRequestHeader("Host", r.Host)
	for _, v := range r.TransferEncoding {
		tx.AddRequestHeader("Transfer-Encoding", v)
	}
	status := 0
	if tx.ProcessRequestHeaders() == nil {
		if r.Body != nil && r.Body != http.NoBody {
			original := r.Body
			body, err := io.ReadAll(io.LimitReader(original, int64(p.bodyLimit())+1))
			r.Body = structReadCloser{Reader: &replayBody{prefix: bytes.NewReader(body), rest: original, consumed: consumed}, Closer: original}
			if err != nil {
				return []SecurityHit{{Engine: "waf", RuleID: 1001, Name: "请求体读取失败", Action: "block"}}, http.StatusBadRequest
			}
			if len(body) > p.bodyLimit() {
				action := "detect"
				if p.WAF.Mode == "block" {
					action = "block"
					status = http.StatusRequestEntityTooLarge
				}
				return []SecurityHit{{Engine: "waf", RuleID: 1002, Name: "请求体超出检查上限，未完成内容检查", Action: action}}, status
			}
			if _, _, err := tx.WriteRequestBody(body); err != nil {
				return []SecurityHit{{Engine: "waf", RuleID: 1003, Name: "请求体检测失败", Action: "block"}}, http.StatusBadRequest
			}
		}
		if _, err := tx.ProcessRequestBody(); err != nil {
			return []SecurityHit{{Engine: "waf", RuleID: 1003, Name: "请求体检测失败", Action: "block"}}, http.StatusBadRequest
		}
	}
	if tx.IsInterrupted() {
		status = http.StatusForbidden
	}
	score := 0
	if state, ok := tx.(plugintypes.TransactionState); ok {
		if col, ok := state.Collection(variables.TX).(collection.Keyed); ok {
			values := col.Get("blocking_inbound_anomaly_score")
			if len(values) > 0 {
				score, _ = strconv.Atoi(values[0])
			}
		}
	}
	hits := []SecurityHit{}
	seen := map[int]bool{}
	for _, m := range tx.MatchedRules() {
		id := m.Rule().ID()
		if seen[id] || !(id == 200002 || id == 200003 || (id >= 911000 && id < 949000 && m.Rule().Severity() >= 0)) {
			continue
		}
		seen[id] = true
		name := "Web 请求规则"
		for _, tag := range m.Rule().Tags() {
			if strings.HasPrefix(tag, "attack-") {
				name = wafAttackName(tag)
				break
			}
		}
		action := "detect"
		if status != 0 {
			action = "block"
		}
		hits = append(hits, SecurityHit{Engine: "waf", RuleID: id, Name: name, Action: action, Score: score, Severity: m.Rule().Severity().String()})
		if len(hits) == 32 {
			break
		}
	}
	if status != 0 && len(hits) == 0 {
		hits = append(hits, SecurityHit{Engine: "waf", RuleID: 1004, Name: "Web 请求被拒绝", Action: "block"})
	}
	return hits, status
}

type structReadCloser struct {
	io.Reader
	io.Closer
}

// Release the buffer reservation as soon as the replayed prefix has been sent,
// so long-running responses and upgraded sockets do not occupy an upload slot.
type replayBody struct {
	prefix   *bytes.Reader
	rest     io.Reader
	consumed func()
}

func (b *replayBody) Read(p []byte) (int, error) {
	if b.prefix != nil && b.prefix.Len() > 0 {
		n, err := b.prefix.Read(p)
		if b.prefix.Len() == 0 {
			b.prefix = nil
			if b.consumed != nil {
				b.consumed()
			}
		}
		return n, err
	}
	if b.consumed != nil {
		b.consumed()
	}
	return b.rest.Read(p)
}

func wafAttackName(tag string) string {
	switch tag {
	case "attack-sqli":
		return "SQL 注入"
	case "attack-xss":
		return "跨站脚本 XSS"
	case "attack-lfi":
		return "本地文件包含 / 路径穿越"
	case "attack-rfi":
		return "远程文件包含"
	case "attack-rce":
		return "远程命令执行"
	case "attack-protocol":
		return "HTTP 协议异常"
	case "attack-reputation-scanner":
		return "已知扫描器"
	case "attack-session-fixation":
		return "会话固定攻击"
	default:
		return tag
	}
}
