package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 会话采用 HttpOnly Cookie：前端 JS 无法读取，XSS 无法窃取会话

const sessionCookie = "coursehub_session"
const sessionMaxAge = 7 * 24 * 3600

func makeSessionToken(secret string, userID int64) string {
	exp := time.Now().Add(time.Duration(sessionMaxAge) * time.Second).Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "u:%d:%d", userID, exp)
	payload := fmt.Sprintf("%d.%d", userID, exp)
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func parseSessionToken(secret, tok string) (int64, bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return 0, false
	}
	uid, err1 := strconv.ParseInt(parts[0], 10, 64)
	exp, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil || uid <= 0 || time.Now().Unix() > exp {
		return 0, false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "u:%d:%d", uid, exp)
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return 0, false
	}
	return uid, true
}

func setSessionCookie(w http.ResponseWriter, secret string, userID int64, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    makeSessionToken(secret, userID),
		Path:     "/",
		MaxAge:   sessionMaxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// sameOrigin 校验非 GET 请求的 Origin 头（浏览器总会带；防 CSRF）
func sameOrigin(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return true
	}
	o := r.Header.Get("Origin")
	if o == "" {
		return true // 非浏览器客户端（curl 等）无 Origin；攻击者浏览器端一定会带
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

// requireUser 鉴权中间件：加载当前用户，注入 handler
func (a *API) requireUser(h func(http.ResponseWriter, *http.Request, *User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "跨站请求被拒绝")
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "未登录")
			return
		}
		uid, ok := parseSessionToken(a.cfg.Secret, c.Value)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "登录已过期")
			return
		}
		u, err := a.db.GetUser(uid)
		if err != nil || u.Status != "active" {
			writeErr(w, http.StatusUnauthorized, "账号不存在或已被禁用")
			return
		}
		h(w, r, u)
	}
}

// requireRole 在 requireUser 基础上追加角色校验
func (a *API) requireRole(h func(http.ResponseWriter, *http.Request, *User), roles ...string) http.HandlerFunc {
	return a.requireUser(func(w http.ResponseWriter, r *http.Request, u *User) {
		for _, role := range roles {
			if u.Role == role {
				h(w, r, u)
				return
			}
		}
		writeErr(w, http.StatusForbidden, "权限不足")
	})
}
