package gateway

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const passkeyCookie = "gatehouse_passkey"
const passkeyPath = "/api/account/passkeys"

type adminPasskey struct {
	RPID       string              `json:"rp_id"`
	Created    time.Time           `json:"created"`
	LastUsed   *time.Time          `json:"last_used,omitempty"`
	Credential webauthn.Credential `json:"credential"`
}

func (p adminPasskey) id() string { return base64.RawURLEncoding.EncodeToString(p.Credential.ID) }

type passkeyUser struct {
	id          []byte
	name        string
	credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return u.id }
func (u passkeyUser) WebAuthnName() string                       { return u.name }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.name }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func passkeyAccount(state State, rp string) passkeyUser {
	user := passkeyUser{id: state.PasskeyUserID, name: state.AdminUsername}
	for _, key := range state.Passkeys {
		if key.RPID == rp {
			user.credentials = append(user.credentials, key.Credential)
		}
	}
	return user
}

type passkeyFlow struct {
	origin, operation string
	sessionKey        [32]byte
	session           webauthn.SessionData
	deadline          time.Time
}

func (a *Admin) passkeyOrigin(r *http.Request) (string, string) {
	origin, ok := adminOrigin(r.Header.Get("Origin"))
	if !ok || r.Header.Get("Sec-Fetch-Site") == "cross-site" || !a.requestOriginAllowed(r) {
		return "", ""
	}
	u, _ := url.Parse(origin)
	host := u.Hostname()
	if net.ParseIP(host) != nil || strings.HasSuffix(host, ".") || (u.Scheme != "https" && host != "localhost") {
		return "", ""
	}
	return origin, host
}

func (a *Admin) passkeyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST "+passkeyPath+"/login/{step}", a.passkeyRequest)
	for _, operation := range []string{"register", "verify"} {
		mux.HandleFunc("POST "+passkeyPath+"/"+operation+"/{step}", a.requireAuth(a.passkeyRequest))
	}
	mux.HandleFunc("GET "+passkeyPath, a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		type summary struct {
			ID       string     `json:"id"`
			RPID     string     `json:"rp_id"`
			Created  time.Time  `json:"created"`
			LastUsed *time.Time `json:"last_used,omitempty"`
		}
		keys := []summary{}
		for _, key := range a.store.Snapshot().Passkeys {
			keys = append(keys, summary{key.id(), key.RPID, key.Created, key.LastUsed})
		}
		jsonResponse(w, 200, keys)
	}))
	mux.HandleFunc("DELETE "+passkeyPath+"/{id}", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		a.authMu.Lock()
		defer a.authMu.Unlock()
		if !a.requireRecentAdmin(w, r) {
			return
		}
		state := a.store.Snapshot()
		keys := slices.Clone(state.Passkeys)
		keys = slices.DeleteFunc(keys, func(key adminPasskey) bool { return key.id() == r.PathValue("id") })
		if len(keys) != len(state.Passkeys) && !a.store.savePasskeys(state.PasskeyUserID, keys) {
			apiError(w, 500, "无法保存通行密钥")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

func (a *Admin) passkeyRequest(w http.ResponseWriter, r *http.Request) {
	operation := strings.Split(strings.TrimPrefix(r.URL.Path, passkeyPath+"/"), "/")[0]
	step := r.PathValue("step")
	if step != "begin" && step != "finish" {
		http.NotFound(w, r)
		return
	}
	origin, rp := a.passkeyOrigin(r)
	if rp == "" {
		apiError(w, 400, "通行密钥需要 HTTPS 管理域名或 localhost")
		return
	}
	var sessionKey [32]byte
	if operation != "login" {
		var valid bool
		sessionKey, _, valid = a.managementSession(r, false)
		if !valid {
			apiError(w, 401, "请重新登录")
			return
		}
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID: rp, RPDisplayName: "GateHome", RPOrigins: []string{origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		},
	})
	if err != nil {
		apiError(w, 400, "当前管理地址不能使用通行密钥")
		return
	}

	var flow passkeyFlow
	var input json.RawMessage
	if step == "begin" {
		if !a.beginCredentialAttempt(r) {
			apiError(w, 429, "验证过于频繁，请稍后重试")
			return
		}
		defer a.endCredentialAttempt()
	} else {
		var ok bool
		flow, ok = a.takePasskey(r)
		if !ok || flow.origin != origin || flow.operation != operation || flow.sessionKey != sessionKey {
			apiError(w, 400, "验证已失效，请重试")
			return
		}
		if !decodeBody(w, r, &input) {
			return
		}
	}

	a.authMu.Lock()
	defer a.authMu.Unlock()
	if operation != "login" {
		if _, _, valid := a.managementSession(r, false); !valid {
			apiError(w, 401, "请重新登录")
			return
		}
	}
	if operation == "register" && !a.requireRecentAdmin(w, r) {
		return
	}
	state := a.store.Snapshot()
	user := passkeyAccount(state, rp)
	if step == "begin" {
		var options any
		var session *webauthn.SessionData
		if operation == "register" {
			if len(state.Passkeys) >= 16 {
				apiError(w, 409, "最多保留 16 个通行密钥")
				return
			}
			if len(user.id) == 0 {
				user.id = make([]byte, 32)
				if _, err := rand.Read(user.id); err != nil {
					apiError(w, 500, "无法创建通行密钥")
					return
				}
				if !a.store.savePasskeys(user.id, state.Passkeys) {
					apiError(w, 500, "无法保存通行密钥")
					return
				}
			}
			options, session, err = wa.BeginRegistration(user, webauthn.WithExclusions(webauthn.Credentials(user.credentials).CredentialDescriptors()))
		} else {
			if len(user.credentials) == 0 {
				apiError(w, 404, "此域名尚未注册通行密钥")
				return
			}
			options, session, err = wa.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationRequired))
		}
		if err != nil {
			apiError(w, 400, "无法开始通行密钥验证")
			return
		}
		a.beginPasskey(w, r, options, passkeyFlow{origin, operation, sessionKey, *session, time.Now().Add(2 * time.Minute)})
		return
	}
	assertion := r.Clone(r.Context())
	assertion.Body = io.NopCloser(bytes.NewReader(input))
	keys := slices.Clone(state.Passkeys)
	if operation == "register" {
		if len(keys) >= 16 {
			apiError(w, 409, "最多保留 16 个通行密钥")
			return
		}
		key, err := wa.FinishRegistration(user, flow.session, assertion)
		if err != nil || slices.ContainsFunc(keys, func(p adminPasskey) bool { return bytes.Equal(p.Credential.ID, key.ID) }) {
			apiError(w, 400, "无法注册通行密钥，或该密钥已存在")
			return
		}
		keys = append(keys, adminPasskey{RPID: rp, Created: time.Now(), Credential: *key})
	} else {
		key, err := wa.FinishLogin(user, flow.session, assertion)
		if err != nil || key.Authenticator.CloneWarning {
			apiError(w, 403, "通行密钥验证失败")
			return
		}
		for i := range keys {
			if keys[i].RPID == rp && bytes.Equal(keys[i].Credential.ID, key.ID) {
				now := time.Now()
				keys[i].Credential, keys[i].LastUsed = *key, &now
				break
			}
		}
	}
	if !a.store.savePasskeys(user.id, keys) {
		apiError(w, 500, "无法保存通行密钥")
		return
	}
	switch operation {
	case "login":
		a.issueAdminSession(w, r, "passkey")
	case "verify":
		a.markAdminVerified(w, sessionKey)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *Admin) beginPasskey(w http.ResponseWriter, r *http.Request, options any, flow passkeyFlow) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		apiError(w, 500, "无法开始通行密钥验证")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(secret[:])
	a.mu.Lock()
	now := time.Now()
	for id, challenge := range a.passkeyFlows {
		if !now.Before(challenge.deadline) {
			delete(a.passkeyFlows, id)
		}
	}
	if previous, err := r.Cookie(passkeyCookie); err == nil {
		delete(a.passkeyFlows, previous.Value)
	}
	if len(a.passkeyFlows) >= 128 {
		a.mu.Unlock()
		apiError(w, 429, "验证过于频繁，请稍后重试")
		return
	}
	a.passkeyFlows[token] = flow
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: passkeyCookie, Value: token, Path: passkeyPath, HttpOnly: true, Secure: a.secureSession(r), SameSite: http.SameSiteStrictMode, MaxAge: 120})
	jsonResponse(w, 200, options)
}

func (a *Admin) takePasskey(r *http.Request) (passkeyFlow, bool) {
	cookie, err := r.Cookie(passkeyCookie)
	if err != nil {
		return passkeyFlow{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	flow := a.passkeyFlows[cookie.Value]
	delete(a.passkeyFlows, cookie.Value)
	return flow, time.Now().Before(flow.deadline)
}

func (s *Store) savePasskeys(userID []byte, keys []adminPasskey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state
	next.PasskeyUserID, next.Passkeys = userID, keys
	if writeJSON(s.path, next) != nil {
		return false
	}
	s.state = next
	return true
}
