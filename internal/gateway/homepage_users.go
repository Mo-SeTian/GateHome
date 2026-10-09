package gateway

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"sort"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type homepageSession struct {
	UserID  string
	Expires time.Time
}

func validateHomepageUsers(state State) error {
	if len(state.HomepageUsers) > 32 {
		return errors.New("首页最多支持 32 个用户")
	}
	names := map[string]bool{state.AdminUsername: true}
	for id, user := range state.HomepageUsers {
		if !homepageUserID.MatchString(id) || ValidateAdminUsername(user.Username) != nil || names[user.Username] {
			return errors.New("首页账号名称或 ID 无效，账号名称须唯一")
		}
		if cost, err := bcrypt.Cost([]byte(user.PasswordHash)); err != nil || cost != bcrypt.DefaultCost {
			return errors.New("首页账号密码数据无效")
		}
		names[user.Username] = true
	}
	return nil
}

func (s *Store) saveHomepageUser(id string, user HomepageUser, revision int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if revision != s.state.Revision {
		return errors.New("账号列表已被修改，请刷新后重试")
	}
	next := s.state
	next.HomepageUsers = map[string]HomepageUser{}
	for key, previous := range s.state.HomepageUsers {
		next.HomepageUsers[key] = previous
	}
	next.HomepageUsers[id] = user
	if err := validateHomepageUsers(next); err != nil {
		return err
	}
	next.Revision++
	if err := writeJSON(s.path, next); err != nil {
		return errors.New("首页账号保存失败")
	}
	s.state = next
	return nil
}

func (a *Admin) homepageUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	cookie, err := r.Cookie("gatehomepage_session")
	if err != nil {
		return "", false
	}
	key := sha256.Sum256([]byte(cookie.Value))
	a.mu.Lock()
	session := a.homepageSessions[key]
	if !time.Now().Before(session.Expires) {
		delete(a.homepageSessions, key)
	}
	a.mu.Unlock()
	if !time.Now().Before(session.Expires) {
		return "", false
	}
	if session.UserID == homepageAdminSpace {
		return session.UserID, true
	}
	user, exists := a.store.Snapshot().HomepageUsers[session.UserID]
	return session.UserID, exists && user.Enabled
}

func (a *Admin) requireHomepageEditor(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !homepageOriginAllowed(r) {
			apiError(w, 403, "首页请求来源未获允许")
			return
		}
		if _, ok := a.homepageUser(w, r); !ok {
			apiError(w, 401, "请先登录首页账号")
			return
		}
		next(w, r)
	}
}

func (a *Admin) homepageUserRoutes(mux *http.ServeMux) {
	list := func(w http.ResponseWriter, r *http.Request) {
		state := a.store.Snapshot()
		type account struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Enabled  bool   `json:"enabled"`
			Admin    bool   `json:"admin"`
		}
		users := []account{{ID: homepageAdminSpace, Username: state.AdminUsername, Enabled: true, Admin: true}}
		for id, user := range state.HomepageUsers {
			users = append(users, account{ID: id, Username: user.Username, Enabled: user.Enabled})
		}
		sort.Slice(users[1:], func(i, j int) bool { return users[i+1].Username < users[j+1].Username })
		jsonResponse(w, 200, map[string]any{"users": users, "revision": state.Revision})
	}
	mux.HandleFunc("GET /api/homepage/users", a.requireAuth(list))
	save := a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Enabled  bool   `json:"enabled"`
			Revision int    `json:"revision"`
		}
		if !decodeBody(w, r, &input) {
			return
		}
		if ValidateAdminUsername(input.Username) != nil {
			apiError(w, 400, "首页账号须为 1–64 字节，不含控制字符或首尾空格")
			return
		}
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		if a.maintenance.Busy() {
			apiError(w, 409, "正在执行维护操作")
			return
		}
		a.authMu.Lock()
		defer a.authMu.Unlock()
		id := r.PathValue("id")
		state := a.store.Snapshot()
		previous, exists := state.HomepageUsers[id]
		if r.Method == "PUT" && !exists {
			apiError(w, 404, "首页账号不存在")
			return
		}
		if r.Method == "POST" {
			var err error
			id, err = stageID()
			if err != nil || len(state.HomepageUsers) >= 32 {
				apiError(w, 400, "无法添加更多首页账号")
				return
			}
		}
		user := HomepageUser{Username: input.Username, PasswordHash: previous.PasswordHash, Enabled: input.Enabled}
		if input.Password != "" || !exists {
			hash, err := HashPassword([]byte(input.Password))
			if err != nil {
				apiError(w, 400, "首页密码须为 12–72 字节")
				return
			}
			user.PasswordHash = hash
		}
		candidate := state
		candidate.HomepageUsers = map[string]HomepageUser{}
		for key, value := range state.HomepageUsers {
			candidate.HomepageUsers[key] = value
		}
		candidate.HomepageUsers[id] = user
		if err := validateHomepageUsers(candidate); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		if input.Revision != state.Revision {
			apiError(w, 409, "账号列表已变化，请刷新后重试")
			return
		}
		if !exists {
			home := defaultHomepage()
			home.Public = false
			if err := a.store.pages.ensure(id, home); err != nil {
				apiError(w, 400, err.Error())
				return
			}
		}
		if err := a.store.saveHomepageUser(id, user, input.Revision); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		a.mu.Lock()
		for key, session := range a.homepageSessions {
			if session.UserID == id {
				delete(a.homepageSessions, key)
			}
		}
		a.mu.Unlock()
		list(w, r)
	})
	mux.HandleFunc("POST /api/homepage/users", save)
	mux.HandleFunc("PUT /api/homepage/users/{id}", save)
}
