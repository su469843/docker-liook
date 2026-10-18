package main

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Port          string
	AdminPassword string // 首次启动时创建 admin 账号的初始密码
	Secret        string
	DataDir       string
	StorageType   string // local | s3 | webdav

	S3Endpoint  string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string
	S3UseSSL    bool

	WebDAVURL  string
	WebDAVUser string
	WebDAVPass string

	AllowRegister bool   // 是否允许学员自主注册
	CookieSecure  bool   // HTTPS 部署时设 true（Cookie 仅经 HTTPS 传输）
	TurnURL       string // 可选 TURN 服务器（视频通话 NAT 中继），逗号分隔多个 url
	TurnUser      string
	TurnPass      string
}

func LoadConfig() *Config {
	c := &Config{
		Port:          getenv("PORT", "8080"),
		AdminPassword: getenv("ADMIN_PASSWORD", "admin123"),
		DataDir:       getenv("STORAGE_LOCAL_DIR", "./data"),
		StorageType:   getenv("STORAGE_TYPE", "local"),
		S3Endpoint:    getenv("S3_ENDPOINT", ""),
		S3Bucket:      getenv("S3_BUCKET", ""),
		S3AccessKey:   getenv("S3_ACCESS_KEY", ""),
		S3SecretKey:   getenv("S3_SECRET_KEY", ""),
		S3UseSSL:      getenv("S3_USE_SSL", "false") == "true",
		WebDAVURL:     getenv("WEBDAV_URL", ""),
		WebDAVUser:    getenv("WEBDAV_USERNAME", ""),
		WebDAVPass:    getenv("WEBDAV_PASSWORD", ""),
		AllowRegister: getenv("ALLOW_REGISTER", "true") == "true",
		CookieSecure:  getenv("COOKIE_SECURE", "false") == "true",
		TurnURL:       getenv("TURN_URL", ""),
		TurnUser:      getenv("TURN_USER", ""),
		TurnPass:      getenv("TURN_PASS", ""),
	}
	c.Secret = os.Getenv("JWT_SECRET")
	if c.Secret == "" {
		c.Secret = loadOrCreateSecret(c.DataDir)
	}
	return c
}

// loadOrCreateSecret 在没有显式配置 JWT_SECRET 时，把随机生成的会话密钥持久化到
// 数据目录，这样容器重启后已登录用户不会被强制退出。
// 多实例部署时请显式设置同一个 JWT_SECRET。
func loadOrCreateSecret(dataDir string) string {
	p := filepath.Join(dataDir, ".jwt_secret")
	if b, err := os.ReadFile(p); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}

	secret := ""
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Printf("警告：生成会话密钥失败（%v），重启后需要重新登录", err)
		return hex.EncodeToString(b)
	}
	secret = hex.EncodeToString(b)

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Printf("警告：无法创建数据目录 %s（%v），会话密钥仅在本次运行有效", dataDir, err)
		return secret
	}
	if err := os.WriteFile(p, []byte(secret+"\n"), 0o600); err != nil {
		log.Printf("警告：无法保存会话密钥到 %s（%v），重启后需要重新登录", p, err)
		return secret
	}
	log.Printf("已生成会话密钥并保存到 %s（多实例部署请显式设置 JWT_SECRET）", p)
	return secret
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
