package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

const wxPusherEndpoint = "https://wxpusher.zjiecode.com/api/send/message"

var wxPushIdentifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)
var alertKinds = map[string]string{"route": "upstream", "webdav": "backup", "cert": "certificate"}

type wxPusherState struct {
	alertQueue
	AppToken string   `json:"app_token,omitempty"`
	UID      string   `json:"uid,omitempty"`
	Events   []string `json:"events"`
}

func (s wxPusherState) validate() error {
	if s.AppToken != "" && (!strings.HasPrefix(s.AppToken, "AT_") || len(s.AppToken) < 4 || !wxPushIdentifier.MatchString(s.AppToken)) {
		return errors.New("请填写 WxPusher 的 AppToken（AT_ 开头）")
	}
	if s.UID != "" && (!strings.HasPrefix(s.UID, "UID_") || len(s.UID) < 5 || !wxPushIdentifier.MatchString(s.UID)) {
		return errors.New("请填写 WxPusher 接收用户 UID（UID_ 开头）")
	}
	if err := validateEventSelection(s.Events); err != nil {
		return err
	}
	if s.Enabled && (s.AppToken == "" || s.UID == "" || len(s.Events) == 0) {
		return errors.New("启用 WxPusher 需要 AppToken、接收用户 UID 和至少一项通知事件")
	}
	return nil
}

func validateEventSelection(events []string) error {
	seen := map[string]bool{}
	for _, kind := range events {
		if seen[kind] || (kind != "upstream" && kind != "backup" && kind != "certificate") {
			return errors.New("通知事件无效")
		}
		seen[kind] = true
	}
	return nil
}

func (s wxPusherState) accepts(kind string) bool {
	return s.Enabled && slices.Contains(s.Events, kind)
}

func (q *alertQueue) filterEvents(accepts func(string) bool) {
	beforeActive := q.Active
	q.Active = make(map[string]bool)
	for key, active := range beforeActive {
		prefix, _, _ := strings.Cut(key, "/")
		kind := alertKinds[prefix]
		if accepts(kind) {
			q.Active[key] = active
		}
	}
	q.Pending = slices.DeleteFunc(slices.Clone(q.Pending), func(event webhookEvent) bool {
		return !accepts(event.Type)
	})
	if len(q.Pending) == 0 {
		q.LastError = ""
	}
}

func (s *State) observeNotification(key, kind, resource string, failing bool, failure, recovery string, now time.Time) bool {
	history := &s.NoticeHistory
	event, changed := history.Active.transition(key, kind, resource, failing, failure, recovery, now)
	if changed {
		const historyLimit = 60
		history.Events = append(slices.Clip(history.Events), event)
		if len(history.Events) > historyLimit {
			history.Events = history.Events[len(history.Events)-historyLimit:]
		}
	}
	if s.Alerts.accepts(kind) {
		changed = s.Alerts.observe(key, kind, resource, failing, failure, recovery, now) || changed
	}
	if s.WxPusher.accepts(kind) {
		changedWx := s.WxPusher.observe(key, kind, resource, failing, failure, recovery, now)
		return changed || changedWx
	}
	return changed
}

func postWxPusher(ctx context.Context, s State, event webhookEvent) error {
	client := outboundClient(s, 8*time.Second)
	defer closeClient(client)
	return sendWxPusher(ctx, client, s.WxPusher, event)
}

func sendWxPusher(ctx context.Context, client *http.Client, settings wxPusherState, event webhookEvent) error {
	summary := "GateHome · " + event.Message
	content := fmt.Sprintf("%s\n服务：%s\n时间：%s\n事件：%s", summary, event.Resource, event.Time.Format(time.RFC3339), event.ID)
	payload, err := json.Marshal(struct {
		AppToken    string   `json:"appToken"`
		Content     string   `json:"content"`
		ContentType int      `json:"contentType"`
		UIDs        []string `json:"uids"`
	}{settings.AppToken, content, 1, []string{settings.UID}})
	if err != nil {
		return errors.New("WxPusher 消息格式无效")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wxPusherEndpoint, bytes.NewReader(payload))
	if err != nil {
		return errors.New("WxPusher 请求无效")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GateHome-WxPusher/1")
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("无法连接 WxPusher，请检查网络和出站代理")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("WxPusher 返回 HTTP %d", resp.StatusCode)
	}
	var result struct {
		Code int `json:"code"`
		Data []struct {
			Code int    `json:"code"`
			UID  string `json:"uid"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result) != nil {
		return errors.New("WxPusher 返回了无效响应")
	}
	if result.Code != 1000 {
		return fmt.Errorf("WxPusher 拒绝请求（业务码 %d），请检查 AppToken 和接收用户", result.Code)
	}
	for _, target := range result.Data {
		if target.UID == settings.UID && target.Code == 1000 {
			return nil
		}
	}
	return errors.New("WxPusher 未受理该用户的消息，请确认 UID 已订阅此应用")
}

func (a *Admin) deliverWxPusher(ctx context.Context) bool {
	return a.deliverNotification(ctx, func(s *State) *alertQueue { return &s.WxPusher.alertQueue }, postWxPusher)
}

func wxPusherSummary(w http.ResponseWriter, s State) {
	wx := s.WxPusher
	events := wx.Events
	if events == nil {
		events = []string{"upstream", "backup", "certificate"}
	}
	jsonResponse(w, 200, map[string]any{"enabled": wx.Enabled, "token_configured": wx.AppToken != "", "uid": wx.UID, "events": events, "pending": len(wx.Pending), "last_error": wx.LastError, "dropped": wx.Dropped, "revision": s.Revision})
}

func (a *Admin) wxPusherRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/notifications/wxpusher", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		wxPusherSummary(w, a.store.Snapshot())
	}))
	mux.HandleFunc("PUT /api/notifications/wxpusher", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Enabled  bool     `json:"enabled"`
			AppToken *string  `json:"app_token"`
			UID      string   `json:"uid"`
			Events   []string `json:"events"`
			Revision int      `json:"revision"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		var invalid error
		conflict := false
		err := a.store.changeWebhook(func(next *State) bool {
			if input.Revision != next.Revision {
				conflict = true
				return false
			}
			before := next.WxPusher
			token := before.AppToken
			if input.AppToken != nil {
				token = strings.TrimSpace(*input.AppToken)
			}
			settings := wxPusherState{alertQueue: alertQueue{Enabled: input.Enabled}, AppToken: token, UID: strings.TrimSpace(input.UID), Events: append([]string{}, input.Events...)}
			if invalid = settings.validate(); invalid != nil {
				return false
			}
			if before.Enabled == settings.Enabled && before.AppToken == token && before.UID == settings.UID && (before.Events != nil || len(settings.Events) > 0) && slices.Equal(before.Events, settings.Events) {
				return false
			}
			if before.Enabled && settings.Enabled && before.AppToken == token && before.UID == settings.UID {
				settings.alertQueue = before.alertQueue
				settings.filterEvents(settings.accepts)
			}
			if settings.accepts("backup") && next.WebDAVBackup.Config.URL != "" && next.WebDAVBackup.LastError != "" {
				settings.observe("webdav", "backup", "WebDAV", true, "远程备份失败", "远程备份已恢复", time.Now())
			}
			next.WxPusher = settings
			next.Revision++
			return true
		})
		switch {
		case conflict:
			apiError(w, 409, "配置已变化，请重新打开推送设置")
		case invalid != nil:
			apiError(w, 400, invalid.Error())
		case err != nil:
			apiError(w, 500, err.Error())
		default:
			wxPusherSummary(w, a.store.Snapshot())
		}
	}))
	mux.HandleFunc("POST /api/notifications/wxpusher/test", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		s := a.store.Snapshot()
		if s.WxPusher.AppToken == "" || s.WxPusher.UID == "" {
			apiError(w, 400, "请先保存 WxPusher AppToken 和接收用户 UID")
			return
		}
		if err := postWxPusher(r.Context(), s, webhookEvent{rand.Text(), "test", "test", "GateHome", "WxPusher 测试通知", time.Now().UTC()}); err != nil {
			apiError(w, 502, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"sent": true})
	}))
}
