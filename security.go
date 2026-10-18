package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ================= 安全基础设施：客户端 IP / 限流 / 请求体上限 =================
//
// 说明：本项目的 API 文档是公开的，安全性不依赖「接口保密」，而依赖：
//   - 每个接口都要求会话 Cookie（HttpOnly）或签名 URL，授权按角色/课程校验
//   - 登录、注册、聊天、通话信令都有限流；失败次数过多会被锁
//   - 媒体（视频/密钥/文档）走 HMAC 签名地址 + 有效期，过期即失效
//   - 请求体大小受限，路径/文件名走白名单，SQL 全部参数化

// ---------------- 客户端 IP（支持反向代理） ----------------

var privateNets []*net.IPNet

func init() {
	for _, c := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10"} {
		if _, n, err := net.ParseCIDR(c); err == nil {
			privateNets = append(privateNets, n)
		}
	}
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, n := range privateNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP 取真实客户端 IP。
// 只有当直连方本身是内网/回环地址（也就是我们自己的反代）时才信任 X-Forwarded-For，
// 因此公网直连的请求无法靠伪造头绕过登录限流。
func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = strings.Trim(host[:i], "[]")
	}
	ip := net.ParseIP(host)
	if ip != nil && isPrivateIP(ip) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := strings.TrimSpace(strings.Split(xff, ",")[0])
			if v := net.ParseIP(first); v != nil {
				return v.String()
			}
		}
		if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
			if v := net.ParseIP(xr); v != nil {
				return v.String()
			}
		}
	}
	if ip != nil {
		return ip.String()
	}
	return host
}

// ---------------- 令牌桶限流（进程内，够单实例用） ----------------

type bucket struct {
	tokens float64
	last   time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	ops     int
}

func newRateLimiter() *rateLimiter { return &rateLimiter{buckets: map[string]*bucket{}} }

// allow key 是否还能拿到一个令牌；rate=每秒补充，burst=最大突发
func (l *rateLimiter) allow(key string, rate, burst float64) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ops++
	// 简单的容量控制，避免被刷爆内存
	if l.ops%2048 == 0 && len(l.buckets) > 4096 {
		for k, b := range l.buckets {
			if now.Sub(b.last) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
	}
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: burst, last: now}
		l.buckets[key] = b
	}
	if dt := now.Sub(b.last).Seconds(); dt > 0 {
		b.tokens += dt * rate
		if b.tokens > burst {
			b.tokens = burst
		}
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ---------------- 请求体大小限制 ----------------
//
// 只限制 JSON 请求体（1MB）：上传类请求是 multipart / 原始字节流，
// 由各自的 handler 用 MaxBytesReader 精确限制，不在这里拦。

func withBodyLimit(next http.Handler, max int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			r.Body = http.MaxBytesReader(w, r.Body, max)
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------- 登录时序对齐 ----------------
//
// 用户不存在时也跑一次 bcrypt，避免通过响应时间枚举用户名。

var dummyBcryptHash []byte

func init() {
	h, _ := bcrypt.GenerateFromPassword([]byte("this-is-not-a-real-password"), bcrypt.DefaultCost)
	dummyBcryptHash = h
}

func bcryptDummyCompare(password string) {
	_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(password))
}

// ---------------- 敏感字段脱敏 ----------------

const maskedSecret = "********"

// maskSecret 后台接口返回配置时把密钥打码，避免截图/录屏泄露
func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	return maskedSecret
}
