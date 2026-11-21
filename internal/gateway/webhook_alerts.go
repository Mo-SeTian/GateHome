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
	Enabled   bool            `json:"enabled"`
	URL       string          `json:"url,omitempty"`
	Active    map[string]bool `json:"active,omitempty"`
	Pending   []webhookEvent  `json:"pending,omitempty"`
	LastError string          `json:"last_error,omitempty"`
	Dropped   int             `json:"dropped,omitempty"`
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

// Copy on write: previous Store snapshots may still be in use by readers.
func (s *webhookState) observe(key, kind, resource string, failing bool, failure, recovery string, now time.Time) bool {
	if !s.Enabled || s.Active[key] == failing {
		return false
	}
	s.Active = maps.Clone(s.Active)
	status, message := "resolved", recovery
	if failing {
		if s.Active == nil {
			s.Active = make(map[string]bool)
		}
		s.Active[key] = true
		status, message = "firing", failure
	} else {
		delete(s.Active, key)
	}
	if len(s.Pending) >= webhookQueueLimit {
		s.Pending = s.Pending[len(s.Pending)-webhookQueueLimit+1:]
		s.Dropped++
	}
	s.Pending = append(slices.Clip(s.Pending), webhookEvent{rand.Text(), kind, status, resource, message, now.UTC()})
	return true
}

// Removing a monitored service ends tracking without announcing a recovery.
func (s *webhookState) pruneRemoved(c Config) {
	if len(s.Active) == 0 {
		return
	}
	retained := make(map[string]bool)
	if s.Active["webdav"] {
		retained["webdav"] = true
	}
	for _, route := range c.Routes {
		key := "route/" + route.ID
		if s.Active[key] {
			retained[key] = true
		}
	}
	for _, cert := range c.ACME.Requests {
		key := "cert/" + cert.ID
		if s.Active[key] {
			retained[key] = true
		}
	}
	s.Active = retained
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
	current := a.store.Snapshot().Alerts
	if !current.Enabled || current.Active[key] == failing {
		return
	}
	err := a.store.changeWebhook(func(s *State) bool {
		return s.Alerts.observe(key, kind, resource, failing, failure, recovery, now)
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

func (a *Admin) deliverWebhook(ctx context.Context) (retry bool) {
	s := a.store.Snapshot()
	if !s.Alerts.Enabled || len(s.Alerts.Pending) == 0 {
		return false
	}
	event := s.Alerts.Pending[0]
	sendErr := postWebhook(ctx, s, event)
	if ctx.Err() != nil {
		return true
	}
	err := a.store.changeWebhook(func(next *State) bool {
		settings := &next.Alerts
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
		a.logs.Add(LogEntry{Category: "admin", Action: "Webhook 投递", Message: err.Error()})
	}
	return err != nil || sendErr != nil
}

func (a *Admin) StartWebhookAlerts(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		var nextCheck, nextAttempt time.Time
		for ctx.Err() == nil {
			now := time.Now()
			if a.store.Snapshot().Alerts.Enabled && !now.Before(nextCheck) {
				a.checkCertificateAlerts(a.certs.Status(), now)
				nextCheck = now.Add(time.Minute)
			}
			if !now.Before(nextAttempt) {
				for i := 0; i < 16 && ctx.Err() == nil && len(a.store.Snapshot().Alerts.Pending) > 0; i++ {
					if a.deliverWebhook(ctx) {
						nextAttempt = time.Now().Add(time.Minute)
						break
					}
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func webhookSummary(w http.ResponseWriter, s State) {
	h := s.Alerts
	jsonResponse(w, 200, map[string]any{"enabled": h.Enabled, "configured": h.URL != "", "pending": len(h.Pending), "last_error": h.LastError, "dropped": h.Dropped, "revision": s.Revision})
}

func (a *Admin) webhookRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/notifications/webhook", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		webhookSummary(w, a.store.Snapshot())
	}))
	mux.HandleFunc("PUT /api/notifications/webhook", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Enabled  bool    `json:"enabled"`
			URL      *string `json:"url"`
			Revision int     `json:"revision"`
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
			if raw != next.Alerts.URL || input.Enabled != next.Alerts.Enabled {
				next.Alerts = webhookState{Enabled: input.Enabled, URL: raw}
				if next.WebDAVBackup.LastError != "" {
					next.Alerts.observe("webdav", "backup", "WebDAV", true, "远程备份失败", "远程备份已恢复", time.Now())
				}
				next.Revision++
				return true
			}
			return false
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
