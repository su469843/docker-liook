package main

import (
	"context"
	"crypto/rand"
	"errors"
	"io/fs"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// securityHeaders 全局安全响应头（CSP 与资源类型限制）
const cspPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; media-src 'self' blob:; font-src 'self' data:; " +
	"connect-src 'self' ws: wss:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'"

func withSecureHeaders(next http.Handler, disableCSP bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		// 只允许本站使用摄像头/麦克风（通话用），其余敏感能力全部关闭
		h.Set("Permissions-Policy", "camera=(self), microphone=(self), geolocation=(), payment=(), usb=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if !disableCSP {
			h.Set("Content-Security-Policy", cspPolicy)
		}
		next.ServeHTTP(w, r)
	})
}

// wrapHandler 统一出口：安全响应头 + JSON 请求体上限（1MB，上传类请求另有精确限制）
func wrapHandler(h http.Handler, disableCSP bool) http.Handler {
	return withBodyLimit(withSecureHeaders(h, disableCSP), 1<<20)
}

// genAdminPassword 随机管理员初始密码：16 位无易混字符。
// 仅在未设置 ADMIN_PASSWORD 且首次启动建号时使用，打印到日志后即不再保存明文。
func genAdminPassword() string {
	b := make([]byte, 16)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(inviteAlphabet))))
		if err != nil {
			// 系统熵源失败极少见；与其静默落成弱口令，不如拒绝启动
			log.Fatalf("生成随机密码失败: %v", err)
		}
		b[i] = inviteAlphabet[n.Int64()]
	}
	return string(b)
}

func main() {
	cfg := LoadConfig()
	st, err := NewStorage(cfg)
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}
	db, err := OpenStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	// 转码是进程内任务，重启后不会自动续：把「转码中」的课时标为失败，免得一直转圈
	db.ResetStuckLessons()

	// 首次启动：自动创建管理员账号（用户名 admin，密码取 ADMIN_PASSWORD；
	// 未设置则随机生成并打印到日志，不再使用固定默认口令）
	if n, _ := db.CountUsers(); n == 0 {
		pw := cfg.AdminPassword
		if pw == "" {
			pw = genAdminPassword()
			log.Printf("未设置 ADMIN_PASSWORD，已随机生成管理员初始密码（下一行仅显示这一次，请登录后台修改）")
			log.Printf("[初始密码] admin / %s", pw)
		}
		hash, _ := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if _, err := db.CreateUser(&User{
			Username:     "admin",
			PasswordHash: string(hash),
			DisplayName:  "管理员",
			Role:         "admin",
			Status:       "active",
		}); err != nil {
			log.Fatalf("创建管理员失败: %v", err)
		}
		if cfg.AdminPassword != "" {
			log.Printf("已创建管理员账号：admin（密码来自环境变量 ADMIN_PASSWORD，请尽快登录修改）")
		}
	}

	// 首次启动：写入站点设置（注册模式在后台可改，环境变量只影响首次初始化）
	if err := db.InitSettings(cfg.SiteName, cfg.RegisterMode, cfg.OrgInviteCode); err != nil {
		log.Printf("初始化站点设置失败: %v", err)
	}
	settings := db.GetSettings()
	if settings.RegisterMode == "invite" && settings.OrgInviteCode == "" {
		settings.OrgInviteCode = genInviteCode()
		if err := db.SaveSettings(settings); err != nil {
			log.Printf("生成机构邀请码失败: %v", err)
		}
	}
	switch settings.RegisterMode {
	case "invite":
		log.Printf("注册模式：邀请码注册，机构邀请码：%s（后台 → 站点设置 可查看/重置）", settings.OrgInviteCode)
	case "closed":
		log.Printf("注册模式：关闭（只能由管理员在后台创建账号）")
	default:
		log.Printf("注册模式：开放注册")
	}

	api := &API{cfg: cfg, st: st, db: db, limiter: newLoginLimiter(), rl: newRateLimiter()}
	api.hub = newHub(api)

	mux := http.NewServeMux()
	api.routes(mux)

	// 健康检查（Docker HEALTHCHECK / 负载均衡探针用，不需要鉴权，响应里带构建版本）
	mux.HandleFunc("GET /healthz", handleHealthz)

	sub, err := fs.Sub(webDist, "web/dist")
	if err != nil {
		log.Fatal(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		// 未匹配的 API 路径不应回退到前端页面，直接返回 404 JSON
		if p == "api" || strings.HasPrefix(p, "api/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"接口不存在"}`))
			return
		}
		if strings.HasPrefix(p, "assets/") {
			// 构建产物文件名带哈希，可长期缓存
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		if p == "index.html" {
			// 入口页必须每次校验，否则升级后浏览器可能跑旧前端
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		}
		if _, err := fs.Stat(sub, p); err != nil {
			// SPA 回退到 index.html
			b, err2 := fs.ReadFile(sub, "index.html")
			if err2 != nil {
				http.Error(w, "前端资源未构建，请先在 web 目录运行 pnpm build", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
			w.Write(b)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: wrapHandler(mux, cfg.DisableCSP),
		// 防止慢速请求拖住连接（HLS 分片与 WebSocket 不受影响）
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 收到 Ctrl+C / docker stop（SIGTERM）时优雅退出：先停止接收新请求，
	// 再等待进行中的请求结束，最后关闭数据库。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Printf("收到退出信号，正在关闭服务…")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("优雅关闭超时: %v", err)
		}
	}()

	log.Printf("CourseHub %s 启动: http://localhost:%s （站点: %s，存储: %s，提交: %s）",
		version, cfg.Port, settings.SiteName, cfg.StorageType, commit)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	if err := db.Close(); err != nil {
		log.Printf("关闭数据库失败: %v", err)
	}
	log.Printf("已安全退出")
}
