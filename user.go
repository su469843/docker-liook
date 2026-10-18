package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ---------------- 登录限流：固定窗口，IP+用户名 维度 ----------------

type loginLimiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

const maxFails = 10
const failWindow = 15 * time.Minute

func newLoginLimiter() *loginLimiter { return &loginLimiter{fails: map[string][]time.Time{}} }

func (l *loginLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	keep := l.fails[key][:0]
	for _, t := range l.fails[key] {
		if now.Sub(t) < failWindow {
			keep = append(keep, t)
		}
	}
	l.fails[key] = keep
	return len(keep) >= maxFails
}

func (l *loginLimiter) recordFail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[key] = append(l.fails[key], time.Now())
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

// ---------------- 认证接口 ----------------

func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) {
	st := a.db.GetSettings()
	switch st.RegisterMode {
	case "closed":
		writeErr(w, http.StatusForbidden, "本站已关闭注册，请联系机构管理员开通账号")
		return
	case "invite":
		// 需要机构邀请码
	default:
		st.RegisterMode = "open"
	}
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		InviteCode  string `json:"invite_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		!usernameRe.MatchString(req.Username) || len(req.Password) < 6 {
		writeErr(w, http.StatusBadRequest, "用户名需 3-32 位字母数字下划线，密码至少 6 位")
		return
	}
	if st.RegisterMode == "invite" {
		if st.OrgInviteCode == "" || !strings.EqualFold(strings.TrimSpace(req.InviteCode), st.OrgInviteCode) {
			writeErr(w, http.StatusForbidden, "邀请码错误，请联系机构管理员获取")
			return
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, 500, "内部错误")
		return
	}
	display := req.DisplayName
	if strings.TrimSpace(display) == "" {
		display = req.Username
	}
	id, err := a.db.CreateUser(&User{
		Username:     req.Username,
		PasswordHash: string(hash),
		DisplayName:  display,
		Role:         "student", // 自助注册一律是学员；老师账号由管理员开通
		Status:       "active",
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, "用户名已被占用")
		return
	}
	setSessionCookie(w, a.cfg.Secret, id, a.cfg.CookieSecure)
	u, _ := a.db.GetUser(id)
	writeJSON(w, http.StatusOK, u)
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "参数错误")
		return
	}
	key := clientIP(r) + "|" + req.Username
	if a.limiter.blocked(key) {
		writeErr(w, http.StatusTooManyRequests, "尝试次数过多，请 15 分钟后再试")
		return
	}
	u, err := a.db.GetUserByUsername(req.Username)
	if err != nil {
		a.limiter.recordFail(key)
		writeErr(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		a.limiter.recordFail(key)
		writeErr(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if u.Status != "active" {
		writeErr(w, http.StatusForbidden, "账号已被禁用，请联系管理员")
		return
	}
	a.limiter.reset(key)
	setSessionCookie(w, a.cfg.Secret, u.ID, a.cfg.CookieSecure)
	writeJSON(w, http.StatusOK, u)
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request, u *User) {
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) handleMe(w http.ResponseWriter, r *http.Request, u *User) {
	writeJSON(w, http.StatusOK, u)
}

// 公开配置（登录页用）
func (a *API) handleConfig(w http.ResponseWriter, r *http.Request) {
	st := a.db.GetSettings()
	writeJSON(w, http.StatusOK, map[string]any{
		"site_name":      st.SiteName,
		"register_mode":  st.RegisterMode,
		"allow_register": st.RegisterMode != "closed",
		"need_invite":    st.RegisterMode == "invite",
	})
}

// ---------------- 站点设置（管理员） ----------------

func (a *API) handleGetSettings(w http.ResponseWriter, r *http.Request, u *User) {
	writeJSON(w, http.StatusOK, a.db.GetSettings())
}

func (a *API) handleUpdateSettings(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		SiteName     string `json:"site_name"`
		RegisterMode string `json:"register_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	st := a.db.GetSettings()
	if s := strings.TrimSpace(req.SiteName); s != "" {
		if len([]rune(s)) > 30 {
			writeErr(w, 400, "站点名称过长（最多 30 字）")
			return
		}
		st.SiteName = s
	}
	if req.RegisterMode != "" {
		if req.RegisterMode != "open" && req.RegisterMode != "invite" && req.RegisterMode != "closed" {
			writeErr(w, 400, "注册模式无效")
			return
		}
		st.RegisterMode = req.RegisterMode
	}
	if st.RegisterMode == "invite" && st.OrgInviteCode == "" {
		st.OrgInviteCode = genInviteCode()
	}
	if err := a.db.SaveSettings(st); err != nil {
		writeErr(w, 500, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *API) handleNewOrgInvite(w http.ResponseWriter, r *http.Request, u *User) {
	st := a.db.GetSettings()
	st.OrgInviteCode = genInviteCode()
	if err := a.db.SaveSettings(st); err != nil {
		writeErr(w, 500, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// ---------------- 用户管理（管理员） ----------------

func (a *API) handleListUsers(w http.ResponseWriter, r *http.Request, u *User) {
	users, err := a.db.ListUsers()
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	if users == nil {
		users = []User{}
	}
	writeJSON(w, http.StatusOK, users)
}

func (a *API) handleCreateUser(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		!usernameRe.MatchString(req.Username) || len(req.Password) < 6 {
		writeErr(w, http.StatusBadRequest, "用户名需 3-32 位字母数字下划线，密码至少 6 位")
		return
	}
	if req.Role != "teacher" && req.Role != "student" && req.Role != "admin" {
		writeErr(w, http.StatusBadRequest, "角色无效")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	display := req.DisplayName
	if strings.TrimSpace(display) == "" {
		display = req.Username
	}
	id, err := a.db.CreateUser(&User{
		Username: req.Username, PasswordHash: string(hash),
		DisplayName: display, Role: req.Role, Status: "active",
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, "用户名已被占用")
		return
	}
	nu, _ := a.db.GetUser(id)
	writeJSON(w, http.StatusOK, nu)
}

func (a *API) handleSetUserStatus(w http.ResponseWriter, r *http.Request, admin *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效用户 ID")
		return
	}
	if id == admin.ID || id == 1 {
		writeErr(w, http.StatusBadRequest, "不能禁用自己或初始管理员")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		(req.Status != "active" && req.Status != "disabled") {
		writeErr(w, 400, "状态无效")
		return
	}
	if err := a.db.SetUserStatus(id, req.Status); err != nil {
		writeErr(w, 500, "操作失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) handleResetPassword(w http.ResponseWriter, r *http.Request, admin *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "无效用户 ID")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Password) < 6 {
		writeErr(w, 400, "密码至少 6 位")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err := a.db.SetUserPassword(id, string(hash)); err != nil {
		writeErr(w, 500, "操作失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
