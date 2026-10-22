package main

import "net/http"

// 版本信息：默认值只在 `go run .` 调试时生效，正式构建由
// scripts/version.sh 计算，再通过 -ldflags 注入，例如：
//
//	go build -ldflags "-s -w -X main.version=1.0.0+sha.6f179c0 \
//	  -X main.commit=6f179c0 -X main.buildTime=2025-10-22T13:06:00Z" .
//
// 主版本号来自 web/package.json 的 version 字段（唯一版本源，发版时改它）。
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

// handleHealthz 健康检查：Docker HEALTHCHECK / 负载均衡探针用，不需要鉴权。
// 顺带回传版本信息，方便确认线上跑的到底是哪次构建（见「登录页底部 / 用户菜单」）。
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "ok",
		"version":    version,
		"commit":     commit,
		"build_time": buildTime,
	})
}
