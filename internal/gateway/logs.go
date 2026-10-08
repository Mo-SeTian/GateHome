package gateway

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxLogBytes = 8 << 20
const maxLogEntries = 5000

type LogEntry struct {
	ID            int64         `json:"id"`
	Time          time.Time     `json:"time"`
	Category      string        `json:"category"`
	Action        string        `json:"action"`
	Target        string        `json:"target"`
	Method        string        `json:"method"`
	Path          string        `json:"path"`
	Remote        string        `json:"remote"`
	RemoteRegion  string        `json:"remote_region,omitempty"`
	Status        int           `json:"status"`
	OK            bool          `json:"ok"`
	DurationMS    int64         `json:"duration_ms"`
	Message       string        `json:"message"`
	Rule          string        `json:"rule"`
	Outcome       string        `json:"outcome,omitempty"`
	AuthResult    string        `json:"auth_result,omitempty"`
	FreezeCreated bool          `json:"freeze_created,omitempty"`
	Firewall      *FirewallHit  `json:"firewall,omitempty"`
	Security      []SecurityHit `json:"security,omitempty"`
}

type Logs struct {
	mu           sync.Mutex
	store        *Store
	path         string
	entries      []LogEntry
	lastID       int64
	size         int64
	totalSize    int64
	lastCleanup  time.Time
	writeError   bool
	cleanupError bool
}

type LogFilter struct {
	Scope, Category, Result, Rule, Search, Method string
	Firewall, Engine, Decision, IP                string
	RuleID                                        int
	Status                                        int
	From, To                                      time.Time
}

func NewLogs(dir string, store *Store) (*Logs, error) {
	return NewLogsAt(filepath.Join(dir, "logs"), store)
}

func NewLogsAt(dir string, store *Store) (*Logs, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	l := &Logs{store: store, path: filepath.Join(dir, "calls.jsonl"), entries: []LogEntry{}}
	names, err := logFileNames(dir)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxLogBytes {
			return nil, errors.New("日志文件类型或大小无效")
		}
		l.totalSize += info.Size()
		file, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(io.LimitReader(file, maxLogBytes))
		scanner.Buffer(make([]byte, 4096), 64<<10)
		for scanner.Scan() {
			var e LogEntry
			if json.Unmarshal(scanner.Bytes(), &e) == nil {
				l.entries = append(l.entries, e)
				if e.ID > l.lastID {
					l.lastID = e.ID
				}
			}
			if len(l.entries) > maxLogEntries {
				l.entries = l.entries[1:]
			}
		}
		file.Close()
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	if info, err := os.Stat(l.path); err == nil {
		l.size = info.Size()
	}
	_ = l.Cleanup(time.Now())
	return l, nil
}

func safeLogPath(path string) string {
	u, err := url.Parse(path)
	if err != nil {
		return "<REDACTED>"
	}
	parts := strings.Split(u.Path, "/")
	hideNext := false
	for i, p := range parts {
		lower := strings.ToLower(p)
		if hideNext || len(p) > 80 {
			parts[i] = "<REDACTED>"
		}
		hideNext = strings.Contains(lower, "token") || strings.Contains(lower, "password") || lower == "secret" || lower == "key" || lower == "session"
	}
	return strings.Join(parts, "/")
}

func safeLogURL(target string) string {
	if !strings.Contains(target, "://") {
		return target
	}
	u, err := url.Parse(target)
	if err != nil {
		return "<REDACTED>"
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.Path = safeLogPath(u.Path)
	u.RawPath = ""
	return u.String()
}

func (l *Logs) Add(e LogEntry) {
	if l == nil {
		return
	}
	e.Path = safeLogPath(e.Path)
	e.Target = safeLogURL(e.Target)
	fields := []*string{&e.Target, &e.Path, &e.Message, &e.Remote, &e.Rule}
	if e.Firewall != nil {
		hit := *e.Firewall
		e.Firewall = &hit
		fields = append(fields, &hit.Name, &hit.Group, &hit.Reason)
	}
	if len(e.Security) > 0 {
		e.Security = append([]SecurityHit(nil), e.Security...)
		for i := range e.Security {
			fields = append(fields, &e.Security[i].Name)
		}
	}
	if l.store != nil {
		s := l.store.Snapshot()
		secrets := []string{s.CloudflareToken, s.ProxyPassword, s.PasswordHash}
		for _, hash := range s.RoutePasswordHashes {
			secrets = append(secrets, hash)
		}
		for _, credential := range s.DNSCredentials {
			secrets = append(secrets, credential.Token)
		}
		for _, credential := range s.CertificateCredentials {
			secrets = append(secrets, credential.Token)
		}
		for _, secret := range secrets {
			if secret == "" {
				continue
			}
			for _, field := range fields {
				*field = strings.ReplaceAll(*field, secret, "<REDACTED>")
			}
		}
	}
	for _, field := range fields {
		if len(*field) > 2000 {
			*field = (*field)[:2000]
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.retention()
	l.lastID++
	e.ID = l.lastID
	e.Time = time.Now()
	l.entries = append(l.entries, e)
	if len(l.entries) > maxLogEntries {
		l.entries = append([]LogEntry(nil), l.entries[len(l.entries)-maxLogEntries:]...)
	}
	data, err := json.Marshal(e)
	if err != nil {
		l.writeError = true
		return
	}
	data = append(data, '\n')
	limit := int64(c.MaxSizeMB) << 20
	if l.size > 0 && l.size+int64(len(data)) > min(int64(maxLogBytes), limit/4) {
		if err := l.rotateLocked(e.Time); err != nil {
			l.writeError = true
			return
		}
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		l.writeError = true
		return
	}
	n, err := file.Write(data)
	file.Close()
	l.size += int64(n)
	l.totalSize += int64(n)
	l.writeError = err != nil
	if err == nil && (l.totalSize > limit || e.Time.Sub(l.lastCleanup) >= time.Minute) {
		l.cleanupError = l.cleanupLocked(c, e.Time) != nil
	}
}

func (l *Logs) List(category, result string, before int64, limit int) ([]LogEntry, int64, bool) {
	return l.ListFiltered("", category, result, "", before, limit)
}

func (l *Logs) ListFiltered(scope, category, result, rule string, before int64, limit int) ([]LogEntry, int64, bool) {
	return l.Query(LogFilter{Scope: scope, Category: category, Result: result, Rule: rule}, before, limit)
}

func (l *Logs) Query(f LogFilter, before int64, limit int) ([]LogEntry, int64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rows := []LogEntry{}
	next := int64(0)
	for i := len(l.entries) - 1; i >= 0; i-- {
		e := l.entries[i]
		if (before > 0 && e.ID >= before) || !matchesLog(e, f) {
			continue
		}
		if len(rows) == limit {
			next = rows[len(rows)-1].ID
			break
		}
		rows = append(rows, e)
	}
	return rows, next, l.writeError || l.cleanupError
}

func matchesLog(e LogEntry, f LogFilter) bool {
	if (f.Scope == "firewall" && !firewallEvent(e)) || (f.Scope == "security" && !securityEvent(e)) ||
		(f.Firewall != "" && (e.Firewall == nil || e.Firewall.ID != f.Firewall)) ||
		(f.Decision != "" && eventDecision(e) != f.Decision) {
		return false
	}
	if f.IP != "" {
		ip, err := netip.ParseAddr(e.Remote)
		if err != nil || ip.Unmap().String() != f.IP {
			return false
		}
	}
	if f.Engine != "" || f.RuleID != 0 {
		matched := f.RuleID == 0 && ((f.Engine == "ip" && firewallBlocked(e) && len(e.Security) == 0) ||
			(f.Engine == "auth" && authSecurityEvent(e)) || (f.Engine == "freeze" && (e.Outcome == "ip_frozen" || e.FreezeCreated)))
		for _, h := range e.Security {
			matched = matched || ((f.Engine == "" || h.Engine == f.Engine) && (f.RuleID == 0 || h.RuleID == f.RuleID))
		}
		if !matched {
			return false
		}
	}
	security := ""
	if f.Search != "" {
		if e.Firewall != nil {
			security += " " + e.Firewall.Name + " " + e.Firewall.Group + " " + e.Firewall.Reason
		}
		for _, h := range e.Security {
			security += " " + h.Engine + " " + strconv.Itoa(h.RuleID) + " " + h.Name
		}
	}
	return !((f.Scope == "project" && e.Category == "access") || (f.Scope == "access" && e.Category != "access") || (f.Rule != "" && e.Rule != f.Rule) ||
		(f.Category != "" && e.Category != f.Category) || (f.Result == "success" && !e.OK) || (f.Result == "error" && e.OK) ||
		(!f.From.IsZero() && e.Time.Before(f.From)) || (!f.To.IsZero() && e.Time.After(f.To)) || (f.Status != 0 && e.Status != f.Status) || (f.Method != "" && e.Method != f.Method) ||
		(f.Search != "" && !strings.Contains(strings.ToLower(e.Target+" "+e.Path+" "+e.Remote+" "+e.Message+" "+e.Action+security), strings.ToLower(f.Search))))
}

func firewallEvent(e LogEntry) bool {
	return e.Category == "access" && (firewallBlocked(e) || len(e.Security) > 0 || e.Outcome == "ip_frozen")
}
func authSecurityEvent(e LogEntry) bool {
	return e.AuthResult == "auth_failed" || e.AuthResult == "auth_rate_limited" || e.AuthResult == "auth_rejected"
}
func securityEvent(e LogEntry) bool {
	return e.Category == "access" && (firewallEvent(e) || e.Outcome == "ip_frozen" || authSecurityEvent(e))
}
func eventDecision(e LogEntry) string {
	if firewallBlocked(e) || e.Outcome == "ip_frozen" || e.FreezeCreated || authSecurityEvent(e) {
		return "block"
	}
	return "detect"
}

type LogPage struct {
	Entries    []LogEntry `json:"entries"`
	Page       int        `json:"page"`
	Size       int        `json:"size"`
	Total      int        `json:"total"`
	Pages      int        `json:"pages"`
	Through    int64      `json:"through"`
	WriteError bool       `json:"write_error"`
}

func (l *Logs) Page(f LogFilter, page, size int, through int64) LogPage {
	l.mu.Lock()
	defer l.mu.Unlock()
	if through == 0 {
		through = l.lastID
	}
	rows := []LogEntry{}
	for i := len(l.entries) - 1; i >= 0; i-- {
		e := l.entries[i]
		if e.ID <= through && matchesLog(e, f) {
			rows = append(rows, e)
		}
	}
	pages := max(1, (len(rows)+size-1)/size)
	page = min(page, pages)
	start := (page - 1) * size
	return LogPage{Entries: rows[start:min(start+size, len(rows))], Page: page, Size: size, Total: len(rows), Pages: pages, Through: through, WriteError: l.writeError || l.cleanupError}
}
