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

// ---------------- 认证接口 ----------------

func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeErr(w, http.StatusForbidden, "跨站请求被拒绝")
		return
	}
	// 注册限流：同一 IP 10 分钟内最多 10 个账号，防止批量刷号
	if !a.rate().allow("reg:"+clientIP(r), 10.0/600.0, 10) {
		writeErr(w, http.StatusTooManyRequests, "注册过于频繁，请稍后再试")
		return
	}
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
	if !sameOrigin(r) {
		writeErr(w, http.StatusForbidden, "跨站请求被拒绝")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "参数错误")
		return
	}
	key := clientIP(r) + "|" + req.Username
	if !a.rate().allow("login-ip:"+clientIP(r), 0.2, 30) {
		writeErr(w, http.StatusTooManyRequests, "登录尝试过于频繁，请稍后再试")
		return
	}
	if a.limiter.blocked(key) {
		writeErr(w, http.StatusTooManyRequests, "尝试次数过多，请 15 分钟后再试")
		return
	}
	u, err := a.db.GetUserByUsername(req.Username)
	if err != nil {
		bcryptDummyCompare(req.Password) // 对齐响应时间，避免枚举用户名
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

// 公开配置（登录页用；version 用于在页面上显示当前部署的构建版本）
func (a *API) handleConfig(w http.ResponseWriter, r *http.Request) {
	st := a.db.GetSettings()
	writeJSON(w, http.StatusOK, map[string]any{
		"site_name":      st.SiteName,
		"register_mode":  st.RegisterMode,
		"allow_register": st.RegisterMode != "closed",
		"need_invite":    st.RegisterMode == "invite",
		"version":        version,
		"commit":         commit,
		"build_time":     buildTime,
	})
}

// ---------------- 站点设置（管理员） ----------------

func (a *API) handleGetSettings(w http.ResponseWriter, r *http.Request, u *User) {
	st := a.db.GetSettings()
	out := *st
	// 密钥类字段脱敏返回（前端不改就不会覆盖，见 applyTo）
	out.S3SecretKey = maskSecret(out.S3SecretKey)
	out.WebDAVPass = maskSecret(out.WebDAVPass)
	writeJSON(w, http.StatusOK, &out)
}

// storageReq 后台存储设置（指针：未传的字段保持原值）
type storageReq struct {
	StorageType *string `json:"storage_type"`
	S3Endpoint  *string `json:"s3_endpoint"`
	S3Bucket    *string `json:"s3_bucket"`
	S3AccessKey *string `json:"s3_access_key"`
	S3SecretKey *string `json:"s3_secret_key"`
	S3UseSSL    *bool   `json:"s3_use_ssl"`
	WebDAVURL   *string `json:"webdav_url"`
	WebDAVUser  *string `json:"webdav_username"`
	WebDAVPass  *string `json:"webdav_password"`
}

func (s *storageReq) applyTo(st *Settings) bool {
	if s.StorageType != nil {
		v := strings.ToLower(strings.TrimSpace(*s.StorageType))
		if v != "" && v != "local" && v != "s3" && v != "webdav" {
			return false
		}
		st.StorageType = v
	}
	if s.S3Endpoint != nil {
		st.S3Endpoint = strings.TrimSpace(*s.S3Endpoint)
	}
	if s.S3Bucket != nil {
		st.S3Bucket = strings.TrimSpace(*s.S3Bucket)
	}
	if s.S3AccessKey != nil {
		st.S3AccessKey = strings.TrimSpace(*s.S3AccessKey)
	}
	if s.S3SecretKey != nil && *s.S3SecretKey != maskedSecret {
		st.S3SecretKey = strings.TrimSpace(*s.S3SecretKey)
	}
	if s.S3UseSSL != nil {
		st.S3UseSSL = *s.S3UseSSL
	}
	if s.WebDAVURL != nil {
		st.WebDAVURL = strings.TrimSpace(*s.WebDAVURL)
	}
	if s.WebDAVUser != nil {
		st.WebDAVUser = strings.TrimSpace(*s.WebDAVUser)
	}
	if s.WebDAVPass != nil && *s.WebDAVPass != maskedSecret {
		st.WebDAVPass = *s.WebDAVPass
	}
	return true
}

func (a *API) handleUpdateSettings(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		SiteName     *string `json:"site_name"`
		RegisterMode *string `json:"register_mode"`
		storageReq
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	st := a.db.GetSettings()
	if req.SiteName != nil {
		if s := strings.TrimSpace(*req.SiteName); s != "" {
			if len([]rune(s)) > 30 {
				writeErr(w, 400, "站点名称过长（最多 30 字）")
				return
			}
			st.SiteName = s
		}
	}
	if req.RegisterMode != nil && *req.RegisterMode != "" {
		m := *req.RegisterMode
		if m != "open" && m != "invite" && m != "closed" {
			writeErr(w, 400, "注册模式无效")
			return
		}
		st.RegisterMode = m
	}
	if st.RegisterMode == "invite" && st.OrgInviteCode == "" {
		st.OrgInviteCode = genInviteCode()
	}
	if !req.storageReq.applyTo(st) {
		writeErr(w, 400, "存储类型无效")
		return
	}
	if err := a.db.SaveSettings(st); err != nil {
		writeErr(w, 500, "保存失败")
		return
	}
	// 存储配置变了：下次访问时重建后端
	a.stMu.Lock()
	a.stSig = ""
	a.stMu.Unlock()
	writeJSON(w, http.StatusOK, st)
}

// handleStorageTest 测试后台填写的存储配置（不保存）
func (a *API) handleStorageTest(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		storageReq
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	st := a.db.GetSettings()
	if !req.storageReq.applyTo(st) {
		writeErr(w, 400, "存储类型无效")
		return
	}
	cfg := configFromSettings(a.cfg, st)
	if err := testStorage(cfg); err != nil {
		writeErr(w, 400, "连接失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "storage_type": cfg.StorageType, "message": "连接正常，可读可写",
	})
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
