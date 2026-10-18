//go:build !noembed

package main

import "embed"

// 默认构建把前端产物打进二进制（web/dist 由 cd web && pnpm build 生成）。
// 只想跑后端测试、还没构建前端时，用 -tags noembed：
//
//	go test -tags noembed ./...
//
//go:embed all:web/dist
var webDist embed.FS
