package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// withSecureHeaders 全局安全响应头
func withSecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
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

	// 首次启动：自动创建管理员账号（用户名 admin，密码取 ADMIN_PASSWORD）
	if n, _ := db.CountUsers(); n == 0 {
		hash, _ := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
		if _, err := db.CreateUser(&User{
			Username:     "admin",
			PasswordHash: string(hash),
			DisplayName:  "管理员",
			Role:         "admin",
			Status:       "active",
		}); err != nil {
			log.Fatalf("创建管理员失败: %v", err)
		}
		log.Printf("已创建管理员账号：admin（密码来自环境变量 ADMIN_PASSWORD，请尽快登录修改）")
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

	api := &API{cfg: cfg, st: st, db: db, limiter: newLoginLimiter()}
	api.hub = newHub(api)

	mux := http.NewServeMux()
	api.routes(mux)

	// 健康检查（Docker HEALTHCHECK / 负载均衡探针用，不需要鉴权）
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte(`{"status":"ok"}`))
	})

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
		if _, err := fs.Stat(sub, p); err != nil {
			// SPA 回退到 index.html
			b, err2 := fs.ReadFile(sub, "index.html")
			if err2 != nil {
				http.Error(w, "前端资源未构建，请先在 web 目录运行 pnpm build", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(b)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: withSecureHeaders(mux),
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

	log.Printf("CourseHub 启动: http://localhost:%s （站点: %s，存储: %s）",
		cfg.Port, settings.SiteName, cfg.StorageType)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	if err := db.Close(); err != nil {
		log.Printf("关闭数据库失败: %v", err)
	}
	log.Printf("已安全退出")
}
