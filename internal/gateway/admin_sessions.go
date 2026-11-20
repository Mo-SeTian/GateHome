package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const adminSessionLifetime = 12 * time.Hour
const recentAdminWindow = 5 * time.Minute

type adminSession struct {
	ID       string    `json:"id"`
	Device   string    `json:"device"`
	Method   string    `json:"method"`
	Started  time.Time `json:"started"`
	LastSeen time.Time `json:"last_seen"`
	Expires  time.Time `json:"expires"`
	Verified time.Time `json:"-"`
}

func (a *Admin) managementSession(r *http.Request, touch bool) (key [32]byte, session adminSession, valid bool) {
	cookie, err := r.Cookie("gatehouse_session")
	if err != nil {
		return
	}
	key = sha256.Sum256([]byte(cookie.Value))
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	session = a.sessions[key]
	if !now.Before(session.Expires) {
		delete(a.sessions, key)
		return key, adminSession{}, false
	}
	if touch {
		session.LastSeen = now
		a.sessions[key] = session
	}
	return key, session, true
}

func (a *Admin) issueAdminSession(w http.ResponseWriter, r *http.Request, method string) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		apiError(w, 500, "无法创建会话")
		return
	}
	token := hex.EncodeToString(secret[:])
	key := sha256.Sum256([]byte(token))
	now := time.Now()
	a.mu.Lock()
	var oldest [32]byte
	var earliest time.Time
	for id, session := range a.sessions {
		if !now.Before(session.Expires) {
			delete(a.sessions, id)
		} else if earliest.IsZero() || session.Expires.Before(earliest) {
			oldest, earliest = id, session.Expires
		}
	}
	if len(a.sessions) >= 32 {
		delete(a.sessions, oldest)
	}
	device := strings.ToValidUTF8(r.UserAgent()[:min(len(r.UserAgent()), 300)], "�")
	a.sessions[key] = adminSession{ID: hex.EncodeToString(key[:16]), Device: device, Method: method, Started: now, LastSeen: now, Verified: now, Expires: now.Add(adminSessionLifetime)}
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "gatehouse_session", Value: token, Path: "/", HttpOnly: true, Secure: a.secureSession(r), SameSite: http.SameSiteStrictMode, MaxAge: int(adminSessionLifetime / time.Second)})
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *Admin) requireRecentAdmin(w http.ResponseWriter, r *http.Request) bool {
	_, session, valid := a.managementSession(r, false)
	if !valid {
		apiError(w, 401, "请重新登录")
		return false
	}
	if time.Since(session.Verified) >= recentAdminWindow {
		apiError(w, http.StatusPreconditionRequired, "请验证身份后继续")
		return false
	}
	return true
}

func (a *Admin) markAdminVerified(w http.ResponseWriter, key [32]byte) {
	a.mu.Lock()
	session := a.sessions[key]
	if time.Now().Before(session.Expires) {
		session.Verified = time.Now()
		a.sessions[key] = session
	}
	a.mu.Unlock()
	if session.ID == "" || !time.Now().Before(session.Expires) {
		apiError(w, 401, "请重新登录")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *Admin) adminSessionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/account/sessions", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		_, current, valid := a.managementSession(r, false)
		if !valid {
			apiError(w, 401, "请重新登录")
			return
		}
		sessions := []adminSession{}
		a.mu.Lock()
		for id, session := range a.sessions {
			if time.Now().Before(session.Expires) {
				sessions = append(sessions, session)
			} else {
				delete(a.sessions, id)
			}
		}
		a.mu.Unlock()
		slices.SortFunc(sessions, func(a, b adminSession) int { return b.LastSeen.Compare(a.LastSeen) })
		jsonResponse(w, 200, map[string]any{"current": current.ID, "sessions": sessions})
	}))
	mux.HandleFunc("DELETE /api/account/sessions/{id}", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		key, current, valid := a.managementSession(r, false)
		if !valid {
			apiError(w, 401, "请重新登录")
			return
		}
		if current.ID == r.PathValue("id") {
			a.logout(w, r)
			return
		}
		a.mu.Lock()
		if time.Now().Before(a.sessions[key].Expires) {
			for id, session := range a.sessions {
				if session.ID == r.PathValue("id") {
					delete(a.sessions, id)
					break
				}
			}
		}
		a.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("POST /api/account/verify", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Password string `json:"password"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		if !a.beginCredentialAttempt(r) {
			apiError(w, 429, "验证过于频繁，请稍后重试")
			return
		}
		defer a.endCredentialAttempt()
		a.authMu.RLock()
		defer a.authMu.RUnlock()
		key, _, valid := a.managementSession(r, false)
		if !valid {
			apiError(w, 401, "请重新登录")
			return
		}
		if len(input.Password) > 72 || bcrypt.CompareHashAndPassword([]byte(a.store.Snapshot().PasswordHash), []byte(input.Password)) != nil {
			apiError(w, 403, "管理密码不正确")
			return
		}
		a.markAdminVerified(w, key)
	}))
}
