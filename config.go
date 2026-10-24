package main

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Port          string
	AdminPassword string // 首次启动时创建 admin 账号的初始密码；留空则随机生成并打印到日志
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

	AllowRegister bool   // 兼容旧环境变量：仅用于首次初始化注册模式
	RegisterMode  string // open | invite | closed（首次启动写入数据库，之后以后台设置为准）
	SiteName      string // 站点名称（首次启动写入数据库）
	OrgInviteCode string // 机构注册邀请码（首次初始化 invite 模式时使用）
	CookieSecure  bool   // HTTPS 部署时设 true（Cookie 仅经 HTTPS 传输）
	TurnURL       string // 可选 TURN 服务器（视频通话 NAT 中继），逗号分隔多个 url
	TurnUser      string
	TurnPass      string
	TurnSecret    string // coturn use-auth-secret 的共享密钥：配置后下发短时效临时账号，避免 TURN 被白嫖

	TTSURL   string  // 自建 TTS（OpenAI 兼容 POST /v1/audio/speech）端点；配置后前端朗读走 /api/tts 代理，API key 不进前端
	TTSKey   string  // TTS 端点的 Bearer key（Worker 未设 API_KEY 时可留空）
	TTSVoice string  // 朗读音色（Edge 音色名，如 en-US-AriaNeural）
	TTSSpeed float64 // 朗读语速 0.5-2.0，默认 0.9（跟读稍慢更清楚）

	DisableCSP bool // 排障用：设 true 关闭 Content-Security-Policy（默认开启）

	FFmpegPath   string // 服务端转码 HLS 用的 ffmpeg 可执行文件（默认 ffmpeg）
	MaxUploadMB  int    // 单个视频上传上限（MB）
	SignTTLMin   int    // 播放/下载签名 URL 有效期（分钟）
	TranscodeMax int    // 同时转码的任务数
}

func LoadConfig() *Config {
	c := &Config{
		Port:          getenv("PORT", "8080"),
		AdminPassword: strings.TrimSpace(os.Getenv("ADMIN_PASSWORD")),
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
		SiteName:      getenv("SITE_NAME", "课程中心"),
		OrgInviteCode: strings.ToUpper(strings.TrimSpace(os.Getenv("ORG_INVITE_CODE"))),
		CookieSecure:  getenv("COOKIE_SECURE", "false") == "true",
		TurnURL:       getenv("TURN_URL", ""),
		TurnUser:      getenv("TURN_USER", ""),
		TurnPass:      getenv("TURN_PASS", ""),
		TurnSecret:    os.Getenv("TURN_SECRET"),
		TTSURL:        strings.TrimSpace(os.Getenv("TTS_URL")),
		TTSKey:        os.Getenv("TTS_KEY"),
		TTSVoice:      getenv("TTS_VOICE", "en-US-AriaNeural"),
		TTSSpeed:      getenvFloat("TTS_SPEED", 0.9),
		DisableCSP:    getenv("CSP_DISABLE", "false") == "true",
		FFmpegPath:    getenv("FFMPEG_PATH", "ffmpeg"),
		MaxUploadMB:   getenvInt("MAX_UPLOAD_MB", 4096),
		SignTTLMin:    getenvInt("SIGN_TTL_MINUTES", 120),
		TranscodeMax:  getenvInt("TRANSCODE_CONCURRENCY", 2),
	}
	// 注册模式优先级：REGISTER_MODE > ALLOW_REGISTER（旧变量）> open
	switch strings.ToLower(strings.TrimSpace(os.Getenv("REGISTER_MODE"))) {
	case "open", "invite", "closed":
		c.RegisterMode = strings.ToLower(strings.TrimSpace(os.Getenv("REGISTER_MODE")))
	default:
		if c.AllowRegister {
			c.RegisterMode = "open"
		} else {
			c.RegisterMode = "closed"
		}
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

func getenvInt(k string, def int) int {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func getenvFloat(k string, def float64) float64 {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 2 {
			return f
		}
	}
	return def
}
