package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
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
	Events []string `json:"events"`
	alertQueue
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
	if len(s.Pending) >= webhookQueueLimit {
		s.Pending = s.Pending[len(s.Pending)-webhookQueueLimit+1:]
		s.Dropped++
	}
	s.Pending = append(slices.Clip(s.Pending), event)
	return true
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
	for _, cert := range c.ACME.Requests {
		key := "cert/" + cert.ID
		if (*active)[key] {
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

func postWebhook(ctx context.Context, s State, event webhookEvent) error {
	client := serviceEndpointClient(s, 8*time.Second)
	defer closeClient(client)
	body, err := json.Marshal(event)
	if err != nil {
		return errors.New("Webhook 通知格式无效")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Alerts.URL, bytes.NewReader(body))
	if err != nil {
		return errors.New("Webhook 请求地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GateHome-Webhook/1")
	req.Header.Set("Idempotency-Key", event.ID)
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("Webhook 连接失败，请检查网络、证书和接收端")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Webhook 返回 HTTP %d", resp.StatusCode)
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
		a.checkCertificateAlerts(a.certs.Status(), time.Now())
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
	jsonResponse(w, 200, map[string]any{"events": h.eventSelection(), "enabled": h.Enabled, "configured": h.URL != "", "pending": len(h.Pending), "last_error": h.LastError, "dropped": h.Dropped, "revision": s.Revision})
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
			raw := next.Alerts.URL
			if input.URL != nil {
				raw = strings.TrimSpace(*input.URL)
			}
			if invalid = validateWebhook(raw, input.Enabled); invalid != nil {
				return false
			}
			before := next.Alerts
			settings := webhookState{URL: raw, Events: before.Events, alertQueue: alertQueue{Enabled: input.Enabled}}
			if input.Events != nil {
				settings.Events = input.Events
			}
			if invalid = settings.validateSelection(); invalid != nil {
				return false
			}
			if raw == before.URL && settings.Enabled == before.Enabled {
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
		if err := postWebhook(r.Context(), s, webhookEvent{rand.Text(), "test", "test", "GateHome", "Webhook 测试通知", time.Now().UTC()}); err != nil {
			apiError(w, 502, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"sent": true})
	}))
}
