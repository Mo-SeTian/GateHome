package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type JobStatus struct {
	Running     bool      `json:"running"`
	LastRun     time.Time `json:"last_run"`
	LastSuccess time.Time `json:"last_success"`
	Message     string    `json:"message"`
}
type Event struct {
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"`
	OK      bool      `json:"ok"`
	Message string    `json:"message"`
}

type Jobs struct {
	mu        sync.Mutex
	store     *Store
	certs     *Certificates
	ddnsKick  chan string
	logs      *Logs
	provider  func(State, DDNSGroup, *Logs) (dnsProvider, error)
	discover  func(context.Context, DDNSGroup, string) (DetectedIP, error)
	ipContext context.Context
	ips       map[string]*ipCheck
	certKick  chan struct{}
	status    map[string]JobStatus
	events    []Event
}

func NewJobs(store *Store, certs *Certificates) *Jobs {
	return &Jobs{store: store, certs: certs, ddnsKick: make(chan string, 100), certKick: make(chan struct{}, 1), status: map[string]JobStatus{}, events: []Event{}, provider: newDNSProvider, discover: detectIP, ipContext: context.Background(), ips: map[string]*ipCheck{}}
}

func (j *Jobs) Trigger(kind string) {
	if kind == "acme" {
		select {
		case j.certKick <- struct{}{}:
		default:
		}
		return
	}
	j.TriggerDDNS("")
}

func (j *Jobs) TriggerDDNS(id string) bool {
	select {
	case j.ddnsKick <- id:
		return true
	default:
		return false
	}
}

func (j *Jobs) Snapshot() (map[string]JobStatus, []Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	status := make(map[string]JobStatus, len(j.status))
	for k, v := range j.status {
		status[k] = v
	}
	return status, append([]Event{}, j.events...)
}

func (j *Jobs) record(kind string, running, ok bool, message string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := j.status[kind]
	s.Running, s.Message = running, message
	if running {
		s.LastRun = time.Now()
	} else if ok {
		s.LastSuccess = time.Now()
	}
	j.status[kind] = s
	if !running {
		j.events = append([]Event{{Time: time.Now(), Kind: kind, OK: ok, Message: message}}, j.events...)
		if len(j.events) > 60 {
			j.events = j.events[:60]
		}
	}
}

func (j *Jobs) Start(ctx context.Context) {
	j.mu.Lock()
	j.ipContext = ctx
	j.mu.Unlock()
	go j.ddnsLoop(ctx)
	go j.certLoop(ctx)
	j.Trigger("ddns")
	j.Trigger("acme")
}

func (j *Jobs) ddnsLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	last := map[string]time.Time{}
	for {
		id := ""
		force := false
		select {
		case <-ctx.Done():
			return
		case id = <-j.ddnsKick:
			force = true
		case <-ticker.C:
		}
		s := j.store.Snapshot()
		for _, g := range s.Config.DDNS.Groups {
			if ctx.Err() != nil {
				return
			}
			if !g.Enabled || (id != "" && id != g.ID) {
				continue
			}
			if time.Since(last[g.ID]) < 5*time.Second || (!force && time.Since(last[g.ID]) < time.Duration(g.Interval)*time.Second) {
				continue
			}
			last[g.ID] = time.Now()
			j.runDDNS(ctx, s, g)
		}
		j.ddnsSummary(j.store.Snapshot().Config)
	}
}

func (j *Jobs) runDDNS(ctx context.Context, s State, g DDNSGroup) {
	started := time.Now()
	key := "ddns:" + g.ID
	j.record(key, true, false, "正在检查 DNS 记录")
	j.ddnsSummary(s.Config)
	message, err := j.syncDNS(ctx, s, g)
	if err != nil {
		message = err.Error()
	}
	j.record(key, false, err == nil, message)
	j.logs.Add(LogEntry{Category: "ddns", Action: "同步任务", Target: g.Name, OK: err == nil, DurationMS: time.Since(started).Milliseconds(), Message: message})
}

func (j *Jobs) ddnsSummary(c Config) {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := JobStatus{}
	count, failed, pending := 0, 0, 0
	active := map[string]bool{}
	for _, g := range c.DDNS.Groups {
		active["ddns:"+g.ID] = true
		if !g.Enabled {
			continue
		}
		count++
		v := j.status["ddns:"+g.ID]
		s.Running = s.Running || v.Running
		if v.LastRun.After(s.LastRun) {
			s.LastRun = v.LastRun
		}
		if v.LastRun.IsZero() {
			pending++
		} else if !v.Running && v.LastSuccess.Before(v.LastRun) {
			failed++
		}
	}
	for key := range j.status {
		if strings.HasPrefix(key, "ddns:") && !active[key] {
			delete(j.status, key)
		}
	}
	for key := range j.ips {
		if strings.HasPrefix(key, "group:") && !active["ddns:"+strings.TrimPrefix(key, "group:")] {
			if j.ips[key].Cancel != nil {
				j.ips[key].Cancel()
			}
			delete(j.ips, key)
		}
	}
	if count > 0 && failed == 0 && pending == 0 && !s.Running {
		s.LastSuccess = time.Now()
	}
	s.Message = fmt.Sprintf("%d 个组已启用，%d 个失败，%d 个等待首次运行", count, failed, pending)
	j.status["ddns"] = s
}

func (j *Jobs) syncDNS(ctx context.Context, s State, g DDNSGroup) (string, error) {
	provider, err := j.provider(s, g, j.logs)
	if err != nil {
		return "", err
	}
	defer provider.Close()
	zoneID, err := provider.ZoneID(ctx, g.Zone)
	if err != nil {
		return "", err
	}
	ips := map[string]netip.Addr{}
	errorsByType := map[string]error{}
	var failures []error
	changed, checked := 0, 0
	for _, r := range g.Records() {
		if _, ok := ips[r.Type]; !ok && errorsByType[r.Type] == nil {
			started := time.Now()
			sample, err := j.discover(ctx, g, r.Type)
			j.recordIP(g, r.Type, sample, err)
			message := "已获取 " + r.Type + " 公网地址"
			if err != nil {
				errorsByType[r.Type] = err
				message = err.Error()
			} else {
				ips[r.Type] = sample.Address
			}
			method := http.MethodGet
			endpoint := sample.Endpoint
			if ipSource(g, r.Type) == "interface" {
				endpoint, method = g.Interface, ""
			}
			j.logs.Add(LogEntry{Category: "ddns", Action: "公网 IP 获取", Target: endpoint, Method: method, OK: err == nil, DurationMS: time.Since(started).Milliseconds(), Message: message})
		}
		started := time.Now()
		updated := false
		err := errorsByType[r.Type]
		if err == nil {
			updated, err = provider.SyncRecord(ctx, zoneID, r, ips[r.Type])
		}
		message := "IP 未变化"
		if updated {
			message = "DNS 记录已更新"
		}
		if err != nil {
			message = err.Error()
			failures = append(failures, fmt.Errorf("%s %s：%w", r.Host, r.Type, err))
		} else {
			checked++
			if updated {
				changed++
			}
		}
		j.logs.Add(LogEntry{Category: "ddns", Action: r.Type + " 记录同步", Target: r.Host, OK: err == nil, DurationMS: time.Since(started).Milliseconds(), Message: message})
	}
	if len(failures) > 0 {
		return "", fmt.Errorf("已完成 %d 条，更新 %d 条；%w", checked, changed, errors.Join(failures...))
	}
	return fmt.Sprintf("已检查 %d 条记录，更新 %d 条", checked, changed), nil
}

func (j *Jobs) certLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	lastAttempt := map[string]time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-j.certKick:
		case <-ticker.C:
		}
		if ctx.Err() != nil {
			return
		}
		s := j.store.Snapshot()
		if !s.Config.ACME.Enabled {
			continue
		}
		j.record("acme", true, false, "正在检查证书有效期")
		var failures []error
		issued, pending := 0, 0
		for _, request := range s.Config.ACME.Requests {
			if !request.Enabled {
				continue
			}
			host := strings.Join(request.Domains, ", ")
			if ctx.Err() != nil {
				return
			}
			if !j.certs.Due(request.ID, s.Config.ACME.Staging) {
				continue
			}
			key := environment(s.Config.ACME.Staging) + "/" + request.ID + "/" + host
			if time.Since(lastAttempt[key]) < 5*time.Minute {
				pending++
				continue
			}
			lastAttempt[key] = time.Now()
			j.record("acme", true, false, "正在为 "+host+" 申请证书，DNS 验证可能需要数分钟")
			started := time.Now()
			err := j.certs.Issue(s, request)
			message := "证书申请或续期成功"
			if err != nil {
				message = err.Error()
			}
			j.logs.Add(LogEntry{Category: "certificates", Action: "证书申请 / 续期", Target: host, OK: err == nil, DurationMS: time.Since(started).Milliseconds(), Message: message})
			if err != nil {
				failures = append(failures, fmt.Errorf("%s：%w", host, err))
			} else {
				issued++
			}
		}
		if len(failures) > 0 {
			j.record("acme", false, false, errors.Join(failures...).Error())
		} else if pending > 0 {
			j.record("acme", false, false, fmt.Sprintf("%d 个域名正在重试冷却期，请 5 分钟后再试", pending))
		} else {
			j.record("acme", false, true, fmt.Sprintf("检查完成，申请或续期 %d 张证书；每小时自动检查", issued))
		}
	}
}

func (s *Jobs) SetLogs(logs *Logs) { s.logs = logs }
