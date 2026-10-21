# 🛠️ CourseHub 二次开发指南

> 🌐 本篇为简体中文版 · English edition: [docs/DEVELOPMENT_EN.md](DEVELOPMENT_EN.md)

本篇面向想把 **CourseHub（docker-liook）** 改造成自己系统的开发者：讲清技术栈、代码地图、核心机制，并手把手带你完成三个最常见的改造——**新增 REST 接口、新增存储后端、扩展 WebSocket 消息**。

> 部署相关请看 [部署指南（中文）](DEPLOYMENT.md) / [Deployment Guide (English)](DEPLOYMENT_EN.md)；API 全表见 [README](../README.md#-api-一览供二次开发)。

## 目录

- [1. 技术栈与环境准备](#1-技术栈与环境准备)
- [2. 本地开发环境](#2-本地开发环境)
- [3. 代码地图](#3-代码地图)
- [4. 核心机制](#4-核心机制)
- [5. 实战一：新增一个 REST 接口](#5-实战一新增一个-rest-接口)
- [6. 实战二：新增存储后端](#6-实战二新增存储后端)
- [7. 实战三：扩展 WebSocket 消息](#7-实战三websocket-新消息类型)
- [8. 前端二次开发](#8-前端二次开发)
- [9. 测试](#9-测试)
- [10. 构建与发布](#10-构建与发布)
- [11. 改代码的安全红线](#11-安全红线改代码必须遵守)
- [12. 常见二次开发场景速查](#12-常见二次开发场景速查)

---

## 1. 技术栈与环境准备

| 层 | 技术 | 说明 |
|---|---|---|
| 后端 | Go 1.27+（`go.mod` 要求 1.27.2） | 标准库 `net/http`（Go 1.22+ 路由模式匹配）+ `gorilla/websocket` |
| 数据库 | SQLite（`modernc.org/sqlite`，纯 Go 无 CGO） | 单写者，`SetMaxOpenConns(1)`，默认 journal 模式（非 WAL） |
| 存储 | 本地目录 / S3（minio-go v7）/ WebDAV | 统一 `Storage` 接口，可自己实现接入 |
| 转码 | ffmpeg（可选） | 服务端 HLS + AES-128；没有时退化 MP4 + Range |
| 前端 | Vue 3 + Vite 5 + vue-router 4 + hls.js | 无 UI 框架依赖，手绘 SVG 图标 |

安装：

```bash
# Go 1.27+
# Node 20 + pnpm 10（前端）
npm i -g pnpm@10
# ffmpeg（只有本地想调试转码才需要；跳过也能跑，视频退化为 MP4）
```

## 2. 本地开发环境

### 2.1 前后端分离跑（推荐，改代码即时生效）

```bash
# 终端 1：后端（默认监听 :8080，数据目录 ./data）
go run .

# 终端 2：前端 dev server（端口 5173，代理 /api /hls /files /ws 到 8080）
cd web
pnpm install
pnpm dev
```

浏览器打开 `http://localhost:5173`（vite 代理配置见 `web/vite.config.js`，API/HLS/文件/WebSocket 都转发到 8080）。

> 后端可以直接跑是因为构建时用了 build tag：`webdist.go`（默认嵌入 `web/dist`）与 `webdist_noembed.go`（`-tags noembed` 时不嵌入，启动只返回「前端资源未构建」提示）。

### 2.2 生产构建（前端嵌入二进制）

```bash
cd web && pnpm build     # 生成 web/dist（必须先构建，否则 go:embed 直接报错）
cd .. && go build .      # 单个静态二进制，前端已嵌入
```

### 2.3 快速验证

```bash
go test -tags noembed ./...   # 不构建前端也能跑后端测试
curl http://localhost:8080/healthz   # {"status":"ok"}
```

## 3. 代码地图

```
main.go        入口：装配 Config/Storage/Store/Hub、CSP 安全头、/healthz、优雅停机
config.go      环境变量 → Config 结构体（JWT_SECRET 缺省自动生成到 <DataDir>/.jwt_secret）
session.go     会话 Cookie（HMAC token，7 天）+ CSRF（sameOrigin）+ requireUser/requireRole
user.go        登录/注册/三档注册模式/用户管理 + 登录限流 + 存储配置测试接口
store.go       SQLite：表结构、模型（User/Course/Lesson/Message...）、全部查询
storage.go     Storage 接口 + local/S3/WebDAV 三个实现 + 运行时切换（stg()）
handlers.go    全部 REST 路由注册 + 课程/课时/市场/媒体代理 handler + 授权助手
media.go       视频链路：直传/分片上传、ffmpeg 转 HLS(AES-128)、签名 URL、HLS 代理
chat.go        WebSocket Hub：私聊 + 课程群聊 + 通话信令 + 限流 + 通话忙线
sanitize.go    图文 HTML 白名单清洗（防存储型 XSS）
security.go    clientIP（反代感知）、令牌桶限流、请求体上限、bcrypt 时序对齐、密钥打码
config.go      环境变量加载（getenv/getenvInt，JWT_SECRET 持久化）
webdist.go     go:embed all:web/dist（-tags noembed 时不嵌入）
ws_test.go     端到端测试（真实 HTTP + WebSocket）
web/           Vue 3 前端（Vite）
  src/api.js        fetch 封装：same-origin Cookie、401 自动跳 /login
  src/ws.js         全局单例 WebSocket + 3s 自动重连 + onWS 订阅
  src/store.js     前端状态、src/router.js 路由、src/style.css 设计系统
  src/views/       Login/Market/MyCourses/CourseDetail/Learn/Messages/Studio/StudioCourse/Admin
  src/components/  VideoPlayer/RichEditor/GroupChat/Modal/ToastHost/Icon/Sticker…
deploy/        systemd unit + 环境变量模板
scripts/       build-linux.sh / .ps1 交叉编译
```

## 4. 核心机制

### 4.1 路由与鉴权

路由用 Go 1.22+ 的 `ServeMux` 方法模式注册（`handlers.go` 的 `routes()`）：

```go
mux.HandleFunc("POST /api/courses", a.requireRole(a.handleCreateCourse, "teacher", "admin"))
```

- `requireUser(h)`：校验 Origin（非 GET，防 CSRF）→ 读 Cookie → 校验 HMAC token → 加载用户（必须 `active`）→ 注入 `*User` 调用 handler。
- `requireRole(h, roles...)`：在 `requireUser` 上追加角色白名单（`admin`/`teacher`/`student`）。
- 课程级授权：`canAccessCourse(u, courseID)`（管理员全部 / 老师限自己的课 / 学员限已加入或已上架）与 `canManageCourse(u, courseID)`（管理员或任课老师）。**任何课程资源操作都要先过这两个函数**。
- 会话 token：`u:<uid>:<exp>` 的 HMAC-SHA256，HttpOnly + SameSite=Lax Cookie（`coursehub_session`），有效期 7 天；签名密钥是 `JWT_SECRET`（留空自动生成持久化到 `data/.jwt_secret`）。
- CSRF：所有非 GET 请求校验 `Origin` 与 `Host` 同源（curl 等无 Origin 的客户端放行）。
- `clientIP()`：只有直连方是内网/回环地址时才信任 `X-Forwarded-For`/`X-Real-IP`（公网直连伪造无效）。

### 4.2 响应与错误约定

```go
writeJSON(w, http.StatusOK, v)        // 任意 JSON
writeErr(w, 400, "参数错误")           // → {"error":"参数错误"}
```

列表接口空结果统一返回 `[]`（不是 `null`）；未匹配的 `/api/*` 返回 JSON 404 而不是前端页面（见 `main.go`）。

### 4.3 存储抽象

```go
type Storage interface {
    Put(ctx, key, data) error
    PutStream(ctx, key, r io.Reader, size int64) error  // 大文件分片写
    Get(ctx, key) (io.ReadCloser, error)
    GetRange(ctx, key, off int64, length int64) (io.ReadCloser, error) // length<=0 读到结尾
    Stat(ctx, key) (int64, error)
    Delete(ctx, key) error
    DeletePrefix(ctx, prefix) error
}
```

- 三个实现：`newLocal`（本地目录）/ `newS3`（minio-go，endpoint **不带协议**，HTTPS 由 `S3_USE_SSL` 控制）/ `newWebDAV`。
- 取存储一律用 `a.stg()`：后台「站点设置 → 文件存储」保存后自动重建连接（`storageSig` 对比配置签名，失败沿用原后端）。
- 后台「测试连接」走 `testStorage(cfg)`：写一个 probe 文件再读回删除。

### 4.4 视频处理链路

```
直传 POST /api/lessons/:id/video        （body = 文件字节流，上限 MAX_UPLOAD_MB）
分片 POST /api/lessons/:id/upload/init  → 16MB/片 PUT → /done（断点续传，uploadID 白名单 ^[a-f0-9]{16,32}$）
   ↓ 源文件 → 存储后端 src/{lessonID}/source.*
transcodeLesson（media.go）：
   临时目录下载源 → 生成 AES-128 key(16B)+IV(16B)+keyinfo → ffmpeg：
   -c:v libx264 -preset veryfast -crf 23 -pix_fmt yuv420p -c:a aac -b:a 128k -ac 2
   -hls_time 10 -hls_playlist_type vod -hls_key_info_file ... -hls_segment_filename seg%05d.ts
   → 上传 index.m3u8 + seg*.ts + key.bin 到 hls/{lessonID}/
   ↓ decorateLesson 给前端签发播放地址
   video_mode=hls → 签名 /hls/{id}/index.m3u8；无 ffmpeg → video_mode=mp4 → /api/lessons/{id}/stream
```

- 签名：`signPath = HMAC-SHA256(secret, "media|<path>|<exp>")` hex，`?e=<exp>&s=<sig>`，TTL `SIGN_TTL_MINUTES`（默认 120 分钟）；`checkSign` 用 `subtle.ConstantTimeCompare` 防时序；`handleHLS` 下发 m3u8 时给每个分片/密钥 URI **重签**。
- 并发转码数 `TRANSCODE_CONCURRENCY`（默认 2，信号量）；**重启时 `ResetStuckLessons()` 把「转码中」标记为失败**（转码是进程内任务，不持久化）。
- 上限：JSON 请求体全局 1MB；视频 `MAX_UPLOAD_MB`（默认 4096）；文档 200MB；图片 10MB；WS 单条 1MB。

### 4.5 WebSocket Hub（chat.go）

- `Hub.clients` 是 `map[userID]map[*client]struct{}`：**一个账号允许多条连接**（多标签页），`busy` 记录通话占用。
- 客户端→服务端：`chat.send` / `chat.read` / `group.send` / `group.read` / `call.offer` / `call.answer` / `call.ice` / `call.end`。
- 服务端→客户端：`presence` / `chat.new` / `chat.sent` / `group.new` / `group.sent` / `call.*` / `error`。
- 每类消息进入 `handleMsg` 前都有限流：聊天 2/s（burst 15）、信令 2/s（burst 20）、ICE 15/s（burst 120）；单条消息 ≤1MB；私聊/群聊发送前都校验 `CanChat` / `CanGroupChat` 关系。
- 前端 `web/src/ws.js` 是全局单例（一条连接共用），断线 3 秒自动重连，组件用 `onWS(fn)` 订阅。

### 4.6 内容安全

- 富文本（图文课时）：`sanitize.go` 白名单清洗——`script/iframe/object` 等连子树删除、非白名单标签展开成子节点、事件属性（`on*`）与 `srcset/formaction` 全剔除、`a.href`/`img.src` 走协议白名单、`style` 过正则白名单。
- 所有 SQL 参数化（store.go）；存储 key/文件名走 `nameRe` 白名单（`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`，防路径穿越）。
- 富文本保存前必须过 `sanitizeHTML()`，不要绕过。

## 5. 实战一：新增一个 REST 接口

以「管理员看全站统计」为例，4 步走通。

**① 定义 DB 查询（store.go）**——直接组合已有查询即可：

```go
// store.go
func (s *Store) SiteStats() (users, courses, lessons int64, err error) {
	err = s.db.QueryRow(
		`SELECT (SELECT COUNT(*) FROM users), (SELECT COUNT(*) FROM courses), (SELECT COUNT(*) FROM lessons)`,
	).Scan(&users, &courses, &lessons)
	return
}
```

> 表结构都在 `store.go` 顶部的 `schema` 字符串里；**改表结构**时，新增列用 `ALTER TABLE ... ADD COLUMN` 并放在迁移区（项目用 `CREATE TABLE IF NOT EXISTS` + 启动时补列的方式做自动迁移，参考 store.go 中已有的迁移写法）。

**② 写 handler（handlers.go）**——遵循统一模式：解析参数 → 鉴权 → 查询 → `writeJSON`/`writeErr`：

```go
func (a *API) handleSiteStats(w http.ResponseWriter, r *http.Request, u *User) {
	users, courses, lessons, err := a.db.SiteStats()
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{
		"users": users, "courses": courses, "lessons": lessons,
	})
}
```

**③ 注册路由（handlers.go 的 `routes()`）**：

```go
mux.HandleFunc("GET /api/stats/site", a.requireRole(a.handleSiteStats, "admin"))
```

**④ 写测试（ws_test.go）**：

```go
func TestSiteStats(t *testing.T) {
	api, h := newTestAPI(t)
	ts := httptest.NewServer(h)
	testBase = ts.URL
	defer ts.Close()

	admin := newClient()
	if resp := postJSON(t, admin, "/api/login", map[string]string{"username": "admin", "password": "admin-pass-123"}); resp.StatusCode != 200 {
		t.Fatal("admin login failed")
	}
	resp := getJSON(t, admin, "/api/stats/site")   // getJSON 是 ws_test.go 里现成的 helper
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}
```

**⑤ 前端调用**（`web/src/api.js` 的 `api()` 已带 401 跳登录）：

```js
import { api } from './api'
const stats = await api('/api/stats/site')
```

## 6. 实战二：新增存储后端

存储层只有一个接口（`storage.go`），实现它即可接入任何后端（比如 Azure Blob、OSS 原生 SDK）：

```go
type Storage interface {
	Put(ctx context.Context, key string, data []byte) error
	PutStream(ctx context.Context, key string, r io.Reader, size int64) error // 大文件
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	GetRange(ctx context.Context, key string, off, length int64) (io.ReadCloser, error) // length<=0 读到结尾，HLS/文档 Range 播放靠它
	Stat(ctx context.Context, key string) (int64, error)
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) error // 删课时/课程时清理
}
```

接入步骤：

1. 新建 `storage_azure.go`，实现上述 7 个方法（参考 `s3Store`：`GetRange` 直接映射到 HTTP Range；`PutStream` 分片上传）。
2. 在 `NewStorage` 的 switch 里注册：

```go
case "azure":
    return newAzure(cfg)
```

3. `Config` 加字段（`config.go` 读环境变量，如 `AZURE_CONTAINER`），compose/`.env.example` 同步补注释。
4. 后台「站点设置 → 文件存储」的表单在 `web/src/views/Admin.vue`，后端接收在 `handleStorageTest`/`storageReq.applyTo`（user.go）——如果要支持后台切换，`Settings` 模型（store.go）与 `storageSig` 也要同步加字段。
5. 用后台「测试连接」验证（走 `testStorage`，写 probe 文件再读校验）。

注意：S3 的 `minio.New()` endpoint **不能带 `http://`**（是否 HTTPS 由 `S3_USE_SSL` 决定）；key 全部走白名单 `nameRe`，新后端不需要自己再做路径校验。

## 7. 实战三：WebSocket 新消息类型

WS 全部逻辑在 `chat.go`：连接进 `Hub.clients`（一个账号多标签页多条连接），消息分发在 `Hub.handleMsg`。

新增一种消息类型（例如课程内广播 `lesson.notify`）：

```go
// 1. wsMsg 加字段（chat.go 顶部）
type wsMsg struct {
    // ...已有字段
    LessonID int64 `json:"lesson_id,omitempty"`
}

// 2. handleMsg 的 switch 里加分发
case "lesson.notify":
    if !h.api.db.CanManageLesson(c.user, m.Group) { /* 权限校验，参照 CanGroupChat */ }
    h.sendTo(m.To, serverMsg{Type: "lesson.notify", Message: m.Msg.Content})

// 3. 前端订阅（任意组件）
import { onWS } from '../ws.js'
onWS((m) => { if (m.type === 'lesson.notify') showToast(m.message) })
```

规则：所有消息进 `handleMsg` 都会先过限流（按 `m.Type` 分组），未知类型统一回 `error`；**消息内容长度已限 8000 字符、单条 1MB**，新的消息类型沿用 `serverMsg` 结构体扩展字段即可，前端 `ws.js` 是全局单例 + `onWS` 订阅 + 3 秒自动重连。

## 8. 前端二次开发

- **技术栈**：Vue 3 + Vite 5 + vue-router 4（`createWebHistory`），无 UI 组件库，样式在 `src/style.css`（小鹅通风格设计系统）。
- **API 封装**：`src/api.js`——`api(path, {json|body})` 自动带 Cookie、非 2xx 抛 `data.error`、401 跳 `/login`；`rawPut`/`postForm` 是便捷封装。
- **WebSocket**：`src/ws.js` 全局单例，`onWS(fn)` 订阅、断线 3s 重连；聊天/通话组件都挂在 `Messages.vue` / `GroupChat.vue`。
- **加一个页面**：`src/router.js` 加路由 → `src/views/` 新建 `Xxx.vue`（参考 `Market.vue` 的结构）→ 导航入口在 `App.vue`。
- **手绘图标**：全站图标是 `components/Icon.vue` 内置 SVG，新增图标加一个 `<path>` 即可；表情在 `src/stickers.js`。
- 生产构建后 `web/dist` 会被 embed，本地调试用 `pnpm dev`（代理已配好 `/api`、`/hls`、`/files`、`/ws`）。

## 9. 测试

测试在 `ws_test.go`，模式是「真实 HTTP + WebSocket」：`newTestAPI` 起临时存储 + SQLite（`t.TempDir()`）+ `httptest.Server`，再用带 cookiejar 的 `http.Client` 当浏览器跑完整流程（登录 → 建课 → 上传 → 聊天 → 信令）。

```bash
go test ./...                    # 全量（需先 cd web && pnpm build，因为 go:embed）
go test -tags noembed ./...      # 没构建前端也能跑
go test -run TestRBAC -v ./...   # 单跑某个
```

新功能请照 `newTestAPI` + `postJSON/getJSON` 的既有模式补端到端测试；CI（`.github/workflows/ci.yml`）会跑 gofmt 检查 → go vet → go test → 完整构建。

## 10. 构建与发布

```bash
# 本地生产构建（Linux 二进制，可交叉编译）
./scripts/build-linux.sh amd64          # → dist/course-hub-linux-amd64（Windows 用 build-linux.ps1）

# Docker（多阶段：前端 → Go 静态编译 → alpine 运行镜像，内置 ffmpeg）
docker build -t course-hub .

# 发布新版本：打 tag 后 CI 自动构建镜像并推 GHCR（semver 标签 + latest）
git tag v1.1.0 && git push origin v1.1.0
```

- CI（`.github/workflows/ci.yml`）：**先构建前端**（embed 依赖它）→ gofmt 格式检查 → go vet → go test → go build。提交前本地跑 `gofmt -w .` 可避免 CI 挂。
- `docker.yml` 在 push main / `v*` tag 时构建 amd64+arm64 镜像推 GHCR（`latest` + semver 标签）。

## 11. 安全红线（改代码必须遵守）

1. **新接口必须挂 `requireUser` / `requireRole`**，课程资源必须先过 `canAccessCourse` / `canManageCourse`；
2. 非 GET 请求自动有 Origin 校验（在 `requireUser` 里），**别用 `mux.HandleFunc` 裸注册绕过它**；
3. 所有 SQL 走参数化（`?` 占位），禁止拼字符串；表结构改动放 `store.go` 的 schema/迁移；
4. 用户输入的文件名/存储 key 必须过 `nameRe` 白名单，路径一律 `path.Join`；
5. 富文本入库前必须过 `sanitizeHTML()`；图片/文档上传必须用 `MaxBytesReader` 限大小；
6. 媒体地址一律 `signedURL()` 签名，不要暴露存储直链；存储桶不要开公共读；
7. 新增 WS 消息类型要在 `handleMsg` 加限流分支，发送内容 ≤8000 字符并做关系校验（`CanChat`/`CanGroupChat`）；
8. 不要在响应里返回 `PasswordHash` / 存储密钥（模型已有 `json:"-"`，`maskSecret` 打码）；
9. 改了限流/鉴权逻辑要同步更新 `SECURITY.md` 与对应测试；
10. 提交前：`gofmt -w .` + `go vet ./...` + `go test -tags noembed ./...`（CI 全会查）。

## 12. 常见二次开发场景速查

| 想做什么 | 主要改动点 |
|---|---|
| 加课程字段（如简介标签） | `store.go`（schema + 模型 + 迁移）→ `handlers.go` handler → `web/src` 展示 |
| 换 PostgreSQL | `store.go` 换驱动；注意**单写者假设**（`MaxOpenConns(1)`、内存限流/转码队列都是单实例假设，README 工程路线图里有说明） |
| 多清晰度转码 | `media.go` 的 ffmpeg 参数（现在单码率 libx264 veryfast crf23），加多档输出 + master m3u8 |
| 接 SSO / 企业登录 | `user.go` 注册/登录 handler + `store.go` 用户表加字段；会话直接用 `setSessionCookie` |
| 加支付 | 需要订单模型 + 回调接口 + 课程价格字段；注意 CORS/CSRF 与签名验单 |
| 接自己的对象存储 | 实现第 6 节接口即可，前端/转码/签名逻辑全部无感 |
| 加消息推送 | `chat.go` Hub 的 `sendTo` 是唯一出口，挂一个 webhook 即可拿到全部消息事件 |
| 新增课时类型 | `Lesson.Kind` 扩展 → `decorateLesson`（media.go）加播放地址逻辑 → 前端 `Learn.vue` / `StudioCourse.vue` 分支渲染 |

> 更完整的「还没做」清单（多清晰度、学习进度、水印、PostgreSQL、审计日志…）见 [README → 尚未完善](../README.md#-尚未完善--后续计划)，欢迎按需提 PR。
