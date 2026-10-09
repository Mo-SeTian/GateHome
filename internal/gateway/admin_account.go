package gateway

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const defaultAdminUsername = "admin"

func ValidateAdminUsername(username string) error {
	if username == "" || len(username) > 64 || !utf8.ValidString(username) || strings.TrimSpace(username) != username || strings.ContainsFunc(username, unicode.IsControl) {
		return errors.New("管理员账号须为 1–64 字节，不含控制字符或首尾空格")
	}
	return nil
}

// Used by initialization/recovery. Route access accounts are separate state.
func (s *Store) SetAdminAccount(username, hash string) error {
	if err := ValidateAdminUsername(username); err != nil {
		return err
	}
	if cost, err := bcrypt.Cost([]byte(hash)); err != nil || cost != bcrypt.DefaultCost {
		return errors.New("管理员密码数据无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state
	next.AdminUsername, next.PasswordHash = username, hash
	if err := writeJSON(s.path, next); err != nil {
		return errors.New("管理员账户保存失败，请检查配置目录权限")
	}
	s.state = next
	return nil
}

func (s *Store) changeAdminAccount(username, hash string, previous State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.AdminUsername != previous.AdminUsername || s.state.PasswordHash != previous.PasswordHash {
		return errors.New("管理员账户已变化，请重新登录后修改")
	}
	next := s.state
	next.AdminUsername, next.PasswordHash = username, hash
	next.Revision++
	if err := writeJSON(s.path, next); err != nil {
		return errors.New("管理员账户保存失败，请检查配置目录权限")
	}
	s.state = next
	return nil
}

func (a *Admin) beginCredentialAttempt(r *http.Request) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for ip, attempt := range a.attempts {
		if now.Sub(attempt.started) >= time.Minute {
			delete(a.attempts, ip)
		}
	}
	ip := remoteIP(r.RemoteAddr)
	attempt := a.attempts[ip]
	if a.activeLogins >= 2 || attempt.count >= 8 || (attempt.count == 0 && len(a.attempts) >= 1024) {
		return false
	}
	if attempt.count == 0 {
		attempt.started = now
	}
	attempt.count++
	a.attempts[ip] = attempt
	a.activeLogins++
	return true
}

func (a *Admin) endCredentialAttempt() { a.mu.Lock(); a.activeLogins--; a.mu.Unlock() }

func (a *Admin) accountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/account", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]string{"username": a.store.Snapshot().AdminUsername})
	}))
	mux.HandleFunc("PUT /api/account", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Username        string `json:"username"`
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
			ConfirmPassword string `json:"confirm_password"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		if err := ValidateAdminUsername(input.Username); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		if input.NewPassword != input.ConfirmPassword {
			apiError(w, 400, "两次新管理密码不一致")
			return
		}
		if input.NewPassword != "" && (len(input.NewPassword) < 12 || len(input.NewPassword) > 72) {
			apiError(w, 400, "新管理密码须为 12–72 字节")
			return
		}
		if !a.beginCredentialAttempt(r) {
			w.Header().Set("Retry-After", "60")
			apiError(w, 429, "账户验证过于频繁，请一分钟后再试")
			return
		}
		defer a.endCredentialAttempt()
		if !a.updateMu.TryLock() {
			apiError(w, 409, "正在保存配置或执行维护，请稍后修改管理员账户")
			return
		}
		defer a.updateMu.Unlock()
		if a.maintenance.Busy() {
			apiError(w, 409, "维护操作执行中，请完成后修改管理员账户")
			return
		}
		a.authMu.Lock()
		defer a.authMu.Unlock()
		cookie, _ := r.Cookie("gatehouse_session")
		a.mu.Lock()
		validSession := cookie != nil && time.Now().Before(a.sessions[sha256.Sum256([]byte(cookie.Value))])
		a.mu.Unlock()
		if !validSession {
			apiError(w, 401, "登录已过期，请重新登录")
			return
		}
		previous := a.store.Snapshot()
		if len(input.CurrentPassword) > 72 || bcrypt.CompareHashAndPassword([]byte(previous.PasswordHash), []byte(input.CurrentPassword)) != nil {
			apiError(w, 400, "当前管理密码不正确")
			return
		}
		hash := previous.PasswordHash
		if input.NewPassword != "" {
			var err error
			hash, err = HashPassword([]byte(input.NewPassword))
			if err != nil {
				apiError(w, 400, "新管理密码须为 12–72 字节")
				return
			}
		}
		if err := a.store.changeAdminAccount(input.Username, hash, previous); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		a.mu.Lock()
		clear(a.sessions)
		clear(a.homepageSessions)
		a.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "gatehouse_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secureSession(r), SameSite: http.SameSiteStrictMode})
		jsonResponse(w, 200, map[string]string{"username": input.Username, "message": "管理员账户已更新，请使用新账号和管理密码重新登录"})
	}))
}
