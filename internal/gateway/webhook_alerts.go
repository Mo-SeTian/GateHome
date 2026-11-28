package gateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const webhookQueueLimit = 512

type webhookEvent struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Status   string    `json:"status"`
	Resource string    `json:"resource"`
	Message  string    `json:"message"`
	Time     time.Time `json:"time"`
}

type webhookState struct {
	URL    string   `json:"url,omitempty"`
	Format string   `json:"format,omitempty"`
	ChatID string   `json:"chat_id,omitempty"`
	Secret string   `json:"secret,omitempty"`
	Events []string `json:"events"`
	alertQueue
}

var validWebhookFormats = map[string]bool{
	"":         true,
	"generic":  true,
	"wecom":    true,
	"feishu":   true,
	"dingtalk": true,
	"telegram": true,
	"bark":     true,
	"gotify":   true,
}

func (s webhookState) normalizedFormat() string {
	if s.Format == "" {
		return "generic"
	}
	return s.Format
}

func (s webhookState) validate() error {
	if err := validateWebhook(s.URL, s.Enabled); err != nil {
		return err
	}
	if !validWebhookFormats[s.Format] {
		return errors.New("不支持的 Webhook 推送模板")
	}
	if len(s.Secret) > 512 || containsCredentialNewline(s.Secret) {
		return errors.New("Webhook 签名密钥格式无效")
	}
	if len(s.ChatID) > 128 || containsCredentialNewline(s.ChatID) {
		return errors.New("Telegram Chat ID 格式无效")
	}
	if s.Enabled && s.normalizedFormat() == "telegram" && s.ChatID == "" {
		return errors.New("启用 Telegram 推送需填写 Chat ID")
	}
	return s.validateSelection()
}

func (s webhookState) eventSelection() []string {
	if s.Events == nil {
		return []string{"upstream", "backup", "certificate"}
	}
	return s.Events
}

func (s webhookState) accepts(kind string) bool {
	return s.Enabled && slices.Contains(s.eventSelection(), kind)
}

func (s webhookState) validateSelection() error {
	if s.Enabled && len(s.eventSelection()) == 0 {
		return errors.New("启用推送需至少选择一项通知事件")
	}
	return validateEventSelection(s.Events)
}

type alertHistory struct {
	Active activeAlerts   `json:"active,omitempty"`
	Events []webhookEvent `json:"events,omitempty"`
}

type activeAlerts map[string]bool

type alertQueue struct {
	Enabled   bool           `json:"enabled"`
	Active    activeAlerts   `json:"active,omitempty"`
	Pending   []webhookEvent `json:"pending,omitempty"`
	LastError string         `json:"last_error,omitempty"`
	Dropped   int            `json:"dropped,omitempty"`
}

func validateWebhook(raw string, enabled bool) error {
	if raw == "" && !enabled {
		return nil
	}
	u, err := url.Parse(raw)
	if err == nil && u.Hostname() != "" && u.User == nil && u.Fragment == "" && len(raw) <= 2048 {
		if port := u.Port(); port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return errors.New("Webhook 端口无效")
			}
		}
		ip, err := netip.ParseAddr(u.Hostname())
		if err == nil && (ip.Zone() != "" || !allowedServiceIP(ip.Unmap())) {
			return errors.New("Webhook 地址不允许访问")
		}
		if u.Scheme == "https" || (u.Scheme == "http" && err == nil && (ip.Unmap().IsPrivate() || ip.Unmap().IsLoopback())) {
			return nil
		}
	}
	return errors.New("请填写 HTTPS Webhook 地址，内网 IP 也可使用 HTTP；地址不能包含用户信息或片段")
}

// Snapshots share their maps until a transition is recorded.
func (active *activeAlerts) transition(key, kind, resource string, failing bool, failure, recovery string, now time.Time) (webhookEvent, bool) {
	if (*active)[key] == failing {
		return webhookEvent{}, false
	}
	next := maps.Clone(*active)
	status, message := "resolved", recovery
	if failing {
		if next == nil {
			next = make(activeAlerts)
		}
		next[key] = true
		status, message = "firing", failure
	} else {
		delete(next, key)
	}
	*active = next
	return webhookEvent{rand.Text(), kind, status, resource, message, now.UTC()}, true
}

// Copy on write: previous Store snapshots may still be in use by readers.
func (s *alertQueue) observe(key, kind, resource string, failing bool, failure, recovery string, now time.Time) bool {
	if !s.Enabled {
		return false
	}
	event, changed := s.Active.transition(key, kind, resource, failing, failure, recovery, now)
	if !changed {
		return false
	}
	s.enqueueEvent(event)
	return true
}

func (s *alertQueue) enqueueEvent(event webhookEvent) {
	if len(s.Pending) >= webhookQueueLimit {
		s.Pending = s.Pending[len(s.Pending)-webhookQueueLimit+1:]
		s.Dropped++
	}
	s.Pending = append(slices.Clip(s.Pending), event)
}

// Removing a monitored service ends tracking without announcing a recovery.
func (active *activeAlerts) pruneRemoved(c Config) {
	if len(*active) == 0 {
		return
	}
	retained := make(map[string]bool)
	if (*active)["webdav"] {
		retained["webdav"] = true
	}
	for _, route := range c.Routes {
		key := "route/" + route.ID
		if (*active)[key] {
			retained[key] = true
		}
	}
	for _, f := range c.Forwards {
		key := "forward/" + f.ID
		if (*active)[key] {
			retained[key] = true
		}
	}
	for _, cert := range c.ACME.Requests {
		key := "cert/" + cert.ID
		if (*active)[key] {
			retained[key] = true
		}
	}
	for _, g := range c.DDNS.Groups {
		key := "ddns/" + g.ID
		if (*active)[key] {
			retained[key] = true
		}
	}
	for key, val := range *active {
		if val && strings.HasPrefix(key, "mihomo") {
			retained[key] = true
		}
	}
	*active = retained
}

// Delivery bookkeeping does not change the revision of editable configuration.
func (s *Store) changeWebhook(change func(*State) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state
	if !change(&next) {
		return nil
	}
	if writeJSON(s.path, next) != nil {
		return errors.New("Webhook 记录保存失败，请检查配置目录权限")
	}
	s.state = next
	return nil
}

func (a *Admin) observeAlert(key, kind, resource string, failing bool, failure, recovery string, now time.Time) {
	if a == nil || a.store == nil {
		return
	}
	state := a.store.Snapshot()
	current, wx := state.Alerts, state.WxPusher
	if state.NoticeHistory.Active[key] == failing && (!current.accepts(kind) || current.Active[key] == failing) && (!wx.accepts(kind) || wx.Active[key] == failing) {
		return
	}
	err := a.store.changeWebhook(func(s *State) bool {
		return s.observeNotification(key, kind, resource, failing, failure, recovery, now)
	})
	if err != nil && a.logs != nil {
		a.logs.Add(LogEntry{Category: "admin", Action: "Webhook 通知", Message: err.Error()})
	}
}

func (a *Admin) clearAlerts(keys ...string) {
	if a == nil || a.store == nil || len(keys) == 0 {
		return
	}
	err := a.store.changeWebhook(func(s *State) bool {
		return s.clearAlertKeys(keys...)
	})
	if err != nil && a.logs != nil {
		a.logs.Add(LogEntry{Category: "admin", Action: "通知清理", Message: err.Error()})
	}
}

func (a *Admin) emitIncidentAlert(dedupeKey, kind, resource, message string, now time.Time) {
	if a == nil || a.store == nil {
		return
	}
	const cooldown = 5 * time.Minute
	a.mu.Lock()
	if a.incidentCooldown == nil {
		a.incidentCooldown = map[string]time.Time{}
	}
	if last, ok := a.incidentCooldown[dedupeKey]; ok && now.Sub(last) < cooldown {
		a.mu.Unlock()
		return
	}
	if len(a.incidentCooldown) > 512 {
		for k, ts := range a.incidentCooldown {
			if now.Sub(ts) >= cooldown {
				delete(a.incidentCooldown, k)
			}
		}
	}
	a.incidentCooldown[dedupeKey] = now
	a.mu.Unlock()

	event := webhookEvent{
		ID:       rand.Text(),
		Type:     kind,
		Status:   "firing",
		Resource: resource,
		Message:  message,
		Time:     now.UTC(),
	}
	err := a.store.changeWebhook(func(s *State) bool {
		return s.recordIncidentNotification(event)
	})
	if err != nil && a.logs != nil {
		a.logs.Add(LogEntry{Category: "admin", Action: "安全通知", Message: err.Error()})
	}
}

func (a *Admin) checkCertificateAlerts(rows []CertificateStatus, now time.Time) {
	for _, cert := range rows {
		if cert.Staging || cert.NotAfter.IsZero() {
			continue
		}
		failing := !cert.NotAfter.After(now.Add(7 * 24 * time.Hour))
		if !failing && !cert.Ready {
			continue
		}
		message := "证书将于 " + cert.NotAfter.UTC().Format(time.RFC3339) + " 到期"
		if !cert.NotAfter.After(now) {
			message = "证书已过期"
		}
		a.observeAlert("cert/"+cert.ID, "certificate", cert.Host, failing, message, "证书有效期已恢复", now)
	}
}

func alertTypeLabel(kind string) string {
	switch kind {
	case "upstream":
		return "反代上游"
	case "backup":
		return "远程备份"
	case "certificate":
		return "SSL 证书"
	case "ddns":
		return "动态域名"
	case "mihomo":
		return "代理分流"
	case "security":
		return "安全防护"
	case "test":
		return "测试通知"
	default:
		return kind
	}
}

func alertStatusLabel(status string) string {
	switch status {
	case "firing":
		return "告警触发"
	case "resolved":
		return "已恢复"
	case "test":
		return "测试"
	default:
		return status
	}
}

func formatAlertTitleBody(event webhookEvent) (title, body string) {
	title = fmt.Sprintf("GateHome [%s] %s", alertStatusLabel(event.Status), alertTypeLabel(event.Type))
	body = fmt.Sprintf("详情：%s\n资源：%s\n时间：%s\n事件：%s",
		event.Message, event.Resource, event.Time.Local().Format("2006-01-02 15:04:05"), event.ID)
	return title, body
}

func postWebhook(ctx context.Context, s State, event webhookEvent) error {
	client := serviceEndpointClient(s, 8*time.Second)
	defer closeClient(client)
	return sendWebhook(ctx, client, s.Alerts, event)
}

func sendWebhook(ctx context.Context, client *http.Client, settings webhookState, event webhookEvent) error {
	format := settings.normalizedFormat()
	targetURL := settings.URL
	title, bodyText := formatAlertTitleBody(event)

	var body []byte
	var err error
	extraHeaders := map[string]string{}

	switch format {
	case "generic":
		body, err = json.Marshal(event)
		if err == nil && settings.Secret != "" {
			mac := hmac.New(sha256.New, []byte(settings.Secret))
			mac.Write(body)
			extraHeaders["X-GateHome-Signature"] = "sha256=" + hex.EncodeToString(mac.Sum(nil))
		}
	case "wecom":
		content := fmt.Sprintf("**%s**\n> %s\n- 资源：`%s`\n- 时间：%s",
			title, event.Message, event.Resource, event.Time.Local().Format("2006-01-02 15:04:05"))
		body, err = json.Marshal(map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": content,
			},
		})
	case "feishu":
		payload := map[string]any{
			"msg_type": "post",
			"content": map[string]any{
				"post": map[string]any{
					"zh_cn": map[string]any{
						"title": title,
						"content": [][]map[string]string{
							{{"tag": "text", "text": bodyText}},
						},
					},
				},
			},
		}
		if settings.Secret != "" {
			ts := strconv.FormatInt(time.Now().Unix(), 10)
			mac := hmac.New(sha256.New, []byte(ts+"\n"+settings.Secret))
			payload["timestamp"] = ts
			payload["sign"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
		}
		body, err = json.Marshal(payload)
	case "dingtalk":
		text := fmt.Sprintf("### %s\n\n%s\n\n- 资源：`%s`\n- 时间：%s",
			title, event.Message, event.Resource, event.Time.Local().Format("2006-01-02 15:04:05"))
		if settings.Secret != "" {
			ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
			mac := hmac.New(sha256.New, []byte(settings.Secret))
			mac.Write([]byte(ts + "\n" + settings.Secret))
			sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
			u, uErr := url.Parse(targetURL)
			if uErr != nil {
				return errors.New("钉钉 Webhook 地址无效")
			}
			q := u.Query()
			q.Set("timestamp", ts)
			q.Set("sign", sig)
			u.RawQuery = q.Encode()
			targetURL = u.String()
		}
		body, err = json.Marshal(map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"title": title,
				"text":  text,
			},
		})
	case "telegram":
		if settings.ChatID == "" {
			return errors.New("Telegram 推送缺少 Chat ID")
		}
		body, err = json.Marshal(map[string]any{
			"chat_id": settings.ChatID,
			"text":    title + "\n" + bodyText,
		})
	case "bark":
		body, err = json.Marshal(map[string]any{
			"title": title,
			"body":  bodyText,
			"group": "GateHome",
		})
	case "gotify":
		priority := 5
		if event.Status == "firing" {
			priority = 8
		} else if event.Status == "resolved" {
			priority = 4
		}
		body, err = json.Marshal(map[string]any{
			"title":    title,
			"message":  bodyText,
			"priority": priority,
		})
		if settings.Secret != "" {
			extraHeaders["X-Gotify-Key"] = settings.Secret
		}
	default:
		return errors.New("不支持的 Webhook 推送模板")
	}

	if err != nil {
		return errors.New("Webhook 通知格式无效")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return errors.New("Webhook 请求地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GateHome-Webhook/1")
	req.Header.Set("Idempotency-Key", event.ID)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("Webhook 连接失败，请检查网络、证书和接收端")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Webhook 返回 HTTP %d", resp.StatusCode)
	}

	if format == "generic" || format == "gotify" {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var check struct {
		ErrCode    int   `json:"errcode"`
		Code       int   `json:"code"`
		StatusCode int   `json:"StatusCode"`
		OK         *bool `json:"ok"`
	}
	if json.Unmarshal(raw, &check) != nil {
		return nil
	}
	switch format {
	case "wecom", "dingtalk":
		if check.ErrCode != 0 {
			return fmt.Errorf("接收端拒绝请求（错误码 %d）", check.ErrCode)
		}
	case "feishu":
		if check.Code != 0 || check.StatusCode != 0 {
			code := check.Code
			if code == 0 {
				code = check.StatusCode
			}
			return fmt.Errorf("飞书拒绝请求（错误码 %d）", code)
		}
	case "telegram":
		if check.OK != nil && !*check.OK {
			return errors.New("Telegram 拒绝了通知请求，请检查 Bot Token 与 Chat ID")
		}
	case "bark":
		if check.Code != 0 && check.Code != 200 {
			return fmt.Errorf("Bark 拒绝请求（业务码 %d）", check.Code)
		}
	}
	return nil
}

func (a *Admin) deliverWebhook(ctx context.Context) bool {
	return a.deliverNotification(ctx, func(s *State) *alertQueue { return &s.Alerts.alertQueue }, postWebhook)
}

func (a *Admin) deliverNotification(ctx context.Context, queue func(*State) *alertQueue, send func(context.Context, State, webhookEvent) error) bool {
	s := a.store.Snapshot()
	settings := queue(&s)
	if !settings.Enabled || len(settings.Pending) == 0 {
		return false
	}
	event := settings.Pending[0]
	sendErr := send(ctx, s, event)
	if ctx.Err() != nil {
		return true
	}
	err := a.store.changeWebhook(func(next *State) bool {
		settings := queue(next)
		if len(settings.Pending) == 0 || settings.Pending[0].ID != event.ID {
			return false
		}
		if sendErr != nil {
			if settings.LastError == sendErr.Error() {
				return false
			}
			settings.LastError = sendErr.Error()
			return true
		}
		settings.LastError = ""
		settings.Pending = settings.Pending[1:]
		return true
	})
	if err != nil && a.logs != nil {
		a.logs.Add(LogEntry{Category: "admin", Action: "故障通知投递", Message: err.Error()})
	}
	return err != nil || sendErr != nil
}

func (a *Admin) StartWebhookAlerts(ctx context.Context) {
	go runAlertTicker(ctx, time.Minute, func() {
		now := time.Now()
		a.checkCertificateAlerts(a.certs.Status(), now)
		if a.mihomo != nil {
			a.mihomo.RunAutoUpdateTick(ctx, now)
			a.mihomo.EvaluateSubscriptionAlerts(now)
		}
	})
	go a.runAlertDelivery(ctx, a.deliverWebhook, func(s State) int { return len(s.Alerts.Pending) })
	go a.runAlertDelivery(ctx, a.deliverWxPusher, func(s State) int { return len(s.WxPusher.Pending) })
}

func runAlertTicker(ctx context.Context, interval time.Duration, check func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		check()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *Admin) runAlertDelivery(ctx context.Context, deliver func(context.Context) bool, pending func(State) int) {
	var retryAt time.Time
	runAlertTicker(ctx, 5*time.Second, func() {
		if time.Now().Before(retryAt) {
			return
		}
		for i := 0; i < 16 && ctx.Err() == nil && pending(a.store.Snapshot()) > 0; i++ {
			if deliver(ctx) {
				retryAt = time.Now().Add(time.Minute)
				break
			}
		}
	})
}

func webhookSummary(w http.ResponseWriter, s State) {
	h := s.Alerts
	jsonResponse(w, 200, map[string]any{
		"events":            h.eventSelection(),
		"enabled":           h.Enabled,
		"configured":        h.URL != "",
		"format":            h.normalizedFormat(),
		"chat_id":           h.ChatID,
		"secret_configured": h.Secret != "",
		"pending":           len(h.Pending),
		"last_error":        h.LastError,
		"dropped":           h.Dropped,
		"revision":          s.Revision,
	})
}

func (a *Admin) webhookRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/notifications/history", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, append([]webhookEvent{}, a.store.Snapshot().NoticeHistory.Events...))
	}))
	mux.HandleFunc("GET /api/notifications/webhook", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		webhookSummary(w, a.store.Snapshot())
	}))
	mux.HandleFunc("PUT /api/notifications/webhook", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Enabled  bool     `json:"enabled"`
			Events   []string `json:"events"`
			URL      *string  `json:"url"`
			Format   *string  `json:"format"`
			ChatID   *string  `json:"chat_id"`
			Secret   *string  `json:"secret"`
			Revision int      `json:"revision"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		code := http.StatusBadRequest
		var invalid error
		err := a.store.changeWebhook(func(next *State) bool {
			if input.Revision != next.Revision {
				code = http.StatusConflict
				invalid = errors.New("配置已变化，请重新打开推送设置")
				return false
			}
			before := next.Alerts
			raw := before.URL
			if input.URL != nil {
				raw = strings.TrimSpace(*input.URL)
			}
			format := before.normalizedFormat()
			if input.Format != nil {
				format = strings.TrimSpace(*input.Format)
			}
			chatID := before.ChatID
			if input.ChatID != nil {
				chatID = strings.TrimSpace(*input.ChatID)
			}
			secret := before.Secret
			if input.Secret != nil {
				secret = strings.TrimSpace(*input.Secret)
			}
			settings := webhookState{
				URL:        raw,
				Format:     format,
				ChatID:     chatID,
				Secret:     secret,
				Events:     before.Events,
				alertQueue: alertQueue{Enabled: input.Enabled},
			}
			if input.Events != nil {
				settings.Events = input.Events
			}
			if invalid = settings.validate(); invalid != nil {
				return false
			}
			sameDestination := raw == before.URL &&
				settings.normalizedFormat() == before.normalizedFormat() &&
				chatID == before.ChatID &&
				secret == before.Secret &&
				settings.Enabled == before.Enabled
			if sameDestination {
				if slices.Equal(settings.eventSelection(), before.eventSelection()) {
					return false
				}
				settings.alertQueue = before.alertQueue
				settings.filterEvents(settings.accepts)
			}
			if settings.accepts("backup") && next.WebDAVBackup.LastError != "" {
				settings.observe("webdav", "backup", "WebDAV", true, "远程备份失败", "远程备份已恢复", time.Now())
			}
			next.Alerts = settings
			next.Revision++
			return true
		})
		if invalid != nil {
			apiError(w, code, invalid.Error())
			return
		}
		if err != nil {
			apiError(w, 500, err.Error())
			return
		}
		webhookSummary(w, a.store.Snapshot())
	}))
	mux.HandleFunc("POST /api/notifications/webhook/test", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		s := a.store.Snapshot()
		if s.Alerts.URL == "" {
			apiError(w, 400, "请先保存 Webhook 地址")
			return
		}
		if s.Alerts.normalizedFormat() == "telegram" && s.Alerts.ChatID == "" {
			apiError(w, 400, "请先保存 Telegram Chat ID")
			return
		}
		if err := postWebhook(r.Context(), s, webhookEvent{rand.Text(), "test", "test", "GateHome", "Webhook 测试通知", time.Now().UTC()}); err != nil {
			apiError(w, 502, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"sent": true})
	}))
}
