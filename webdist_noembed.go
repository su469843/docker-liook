//go:build noembed

package main

import "embed"

// 用 -tags noembed 构建时不嵌入前端产物，方便在没跑过 pnpm build 的环境里
// 执行 go vet / go test；此时启动服务只会返回“前端资源未构建”的提示。
var webDist embed.FS
