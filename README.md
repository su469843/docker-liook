# 📚 CourseHub — 自托管课程视频平台

用 **Vue 3 + Go** 编写的轻量课程视频站点：视频在**浏览器内**用 ffmpeg.wasm 切成 HLS，**服务器零转码压力**，2GB 内存的小服务器也能跑。支持**管理员 / 老师 / 学员**三种角色、邀请码加入课程、站内私聊与 1 对 1 音视频通话。

## ✨ 特性

- 🎬 **浏览器端切片**：上传时在浏览器里用 ffmpeg.wasm 把视频切成 HLS（m3u8 + ts 分片），服务器只负责存文件和播放
- 💾 **存储后端可配置**：本地目录 / S3（MinIO、AWS 等 S3 兼容存储）/ WebDAV，通过环境变量一键切换
- 📦 **Docker 一键部署**：前端打包产物嵌入 Go 二进制，单容器启动，无外部依赖
- 🗃️ **元数据存 SQLite**（纯 Go 驱动，无 CGO），随数据目录自动建库
- 👥 **多用户与角色**：管理员管理账号与课程；老师建课、上传视频、查看学员；学员凭邀请码加入课程
- 🔐 **会话安全**：登录态用 HttpOnly + SameSite Cookie（HMAC 签名），非 GET 请求校验 Origin 防 CSRF，登录失败限流
- ▶️ hls.js 播放，经后端代理存储，天然免 CORS
- 💬 **站内私聊**：WebSocket 实时消息，支持文字、表情、图片，离线消息存库、未读计数
- 📞 **1 对 1 语音/视频通话**：WebRTC + WebSocket 信令，可选 TURN 中继，通话忙线自动提示

## 👥 角色权限

| 角色 | 课程 | 视频 | 聊天 | 后台 |
|---|---|---|---|---|
| 管理员 admin | 全部课程，可指定任课老师 | 全部可管理 | 可与所有人私聊 | 用户管理 |
| 老师 teacher | 自己任教的课程 | 自己课程可上传/删除 | 与课程内学员私聊 | - |
| 学员 student | 凭邀请码加入的课程 | 观看已加入课程 | 与任教老师私聊 | 可自助注册（`ALLOW_REGISTER=true`） |

首次启动会自动创建管理员账号 **admin**，密码取环境变量 `ADMIN_PASSWORD`，登录后可以在「用户管理」里创建老师/学员账号、改密、禁用账号。

老师把课程卡片上的**邀请码**发给学员，学员在「我的课程」页输入即可加入。

## 🚀 部署

三种方式任选，产物都是同一个 Go 二进制（前端已嵌入，无外部运行时依赖）。

### 方式一：Docker Compose 在服务器上直接构建（最简单）

```bash
# 1. 把项目放到服务器上（git clone 或直接上传）
cd course-hub

# 2. 配置（至少改密码！）
cp .env.example .env
vi .env    # 修改 ADMIN_PASSWORD；需要 S3/WebDAV 就改 STORAGE_TYPE 和对应配置

# 3. 构建并启动
docker compose up -d --build

# 4. 看状态 / 日志
docker compose ps          # STATUS 里出现 (healthy) 说明启动正常
docker compose logs -f
```

打开 `http://服务器IP:8080`，用 `admin` + 你设置的密码登录。宿主机 `./data` 挂到容器 `/data`（数据库 + 视频 + 聊天图片都在里面，备份这个目录即可）。

镜像约 **50MB**（alpine + 静态二进制），常驻内存几十 MB；2G 内存的小机器绰绰有余（建议加 1G swap：`fallocate -l 1G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile`）。

### 方式二：用 CI 构建好的镜像（GHCR / 自己的镜像仓库）

推送到 GitHub 后，`.github/workflows/docker.yml` 会自动构建 **linux/amd64 + linux/arm64** 双架构镜像并推到 GitHub 容器仓库：

```
ghcr.io/<你的GitHub用户名>/<仓库名>:latest
```

服务器上不需要装 Go / Node，只要 Docker：

```bash
# 公共镜像可直接拉；私有镜像先登录（密码用 GitHub PAT，勾选 read:packages）
echo <你的PAT> | docker login ghcr.io -u <GitHub用户名> --password-stdin

IMAGE=ghcr.io/<你的GitHub用户名>/<仓库名>:latest docker compose pull
IMAGE=ghcr.io/<你的GitHub用户名>/<仓库名>:latest docker compose up -d
```

推送到别的镜像仓库（阿里云 ACR、Docker Hub、私有 Harbor 都一样）：

```bash
# 在有 Docker 的机器上构建（也可在服务器上构建）
docker build -t registry.cn-hangzhou.aliyuncs.com/<命名空间>/course-hub:latest .
docker login registry.cn-hangzhou.aliyuncs.com
docker push registry.cn-hangzhou.aliyuncs.com/<命名空间>/course-hub:latest
```

> 本机不想装 Docker：直接把代码推到 GitHub，让 Actions 构建推送即可，服务器只负责 `docker pull`。

### 方式三：不用 Docker，直接跑二进制（systemd）

Go 静态编译（`CGO_ENABLED=0`），单文件即可运行，适合 512M 内存的小机器或已有 systemd 服务的环境。

```powershell
# Windows 上交叉编译出 Linux 版本（在项目根目录执行）
.\scripts\build-linux.ps1              # 生成 dist\course-hub-linux-amd64 与 -arm64
```

```bash
# Linux 服务器上有 Go 的话也可以直接：
./scripts/build-linux.sh amd64

# 服务器上安装（以 amd64 为例）
sudo useradd -r -s /usr/sbin/nologin -d /opt/coursehub coursehub
sudo mkdir -p /opt/coursehub /var/lib/coursehub
sudo cp course-hub-linux-amd64 /opt/coursehub/course-hub
sudo chmod +x /opt/coursehub/course-hub
sudo chown -R coursehub:coursehub /opt/coursehub /var/lib/coursehub
sudo cp deploy/coursehub.env.example /etc/coursehub.env
sudo vi /etc/coursehub.env             # 改 ADMIN_PASSWORD、端口等
sudo cp deploy/course-hub.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now course-hub
sudo systemctl status course-hub
sudo journalctl -u course-hub -f
```

### 健康检查与优雅停机

- `GET /healthz` → `200 {"status":"ok"}`（不需要登录），Docker 镜像已内置 `HEALTHCHECK`。
- 收到 `SIGTERM`（`docker stop` / `systemctl stop`）时会先停止接收新请求、等待进行中的请求结束再关闭 SQLite，不会丢正在写入的数据。

### HTTPS 与反代注意事项

- **音视频通话必须在安全上下文可用**：浏览器只在 `https://` 或 `localhost` 下允许访问摄像头/麦克风。公网使用通话功能请配 HTTPS，并把 `COOKIE_SECURE=true`。
- 用 Nginx/Caddy 反代时，除了 `/api`、`/hls`、`/files`，还要透传 WebSocket `/ws`（Nginx 需要 `proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection "upgrade";`）。
- 跨网络通话建议自建 TURN（如 coturn），配置 `TURN_URL` / `TURN_USER` / `TURN_PASS`；否则双方都在对称 NAT 下可能连不通。

## 💻 本地开发

```bash
# 前端（端口 5173，已代理 API / HLS / 文件 / WebSocket 到 8080）
cd web
pnpm install
pnpm dev

# 后端（另开终端，Go 1.27+）
go mod tidy
go run .
```

生产构建：`cd web && pnpm build && go build .`（`web/dist` 会被 `go:embed` 打进二进制，所以**必须先构建前端**）

运行测试：`go test ./...`（端到端覆盖鉴权、RBAC、邀请码、聊天与通话信令）。如果还没构建前端，用 `go test -tags noembed ./...` 只测后端。

> 不想在本机装 Go / Node / Docker 也可以：推到 GitHub 后 `.github/workflows/ci.yml` 会自动跑 gofmt / go vet / go test / 前端构建，`.github/workflows/docker.yml` 会自动构建并推送 Docker 镜像，本机只需要 `git push`。

## ⚙️ 环境变量

| 变量 | 说明 | 默认 |
|---|---|---|
| `PORT` | 容器内监听端口（一般不用改） | `8080` |
| `HOST_PORT` | docker compose 映射到宿主机的端口 | `8080` |
| `IMAGE` | docker compose 使用的镜像名（从镜像仓库拉取时指定） | `course-hub:latest` |
| `ADMIN_PASSWORD` | 首次启动创建 admin 账号的初始密码 | `admin123` |
| `JWT_SECRET` | 会话签名密钥。留空会自动生成并持久化到数据目录 `.jwt_secret`（重启不会掉登录）；**多实例部署必须显式指定同一个值** | 自动生成 |
| `ALLOW_REGISTER` | 是否允许学员自助注册 | `true` |
| `COOKIE_SECURE` | 全站 HTTPS 时设为 `true`（Cookie 仅经 HTTPS 传输） | `false` |
| `STORAGE_TYPE` | 存储后端：`local` / `s3` / `webdav` | `local` |
| `STORAGE_LOCAL_DIR` | 本地存储目录（SQLite 也在这里） | `./data`（容器内 `/data`） |
| `S3_ENDPOINT` / `S3_BUCKET` / `S3_ACCESS_KEY` / `S3_SECRET_KEY` / `S3_USE_SSL` | S3(MinIO 兼容) 配置 | - |
| `WEBDAV_URL` / `WEBDAV_USERNAME` / `WEBDAV_PASSWORD` | WebDAV 配置 | - |
| `TURN_URL` / `TURN_USER` / `TURN_PASS` | 可选 TURN 服务器（多个用逗号分隔） | - |

## 🎞️ 切片模式说明

| 模式 | 说明 | 适用 |
|---|---|---|
| ⚡ 快速切片（默认） | `-c copy` 不转码直接切，几秒到几十秒 | 源视频是 H.264/AAC（绝大多数 mp4） |
| 🔄 兼容转码 | 转成 H.264 再切，速度慢 | 源编码特殊、快速切片失败时 |

⚠️ **浏览器内存限制**：ffmpeg.wasm 运行在浏览器内存里，单个视频文件建议 **不超过 2GB**。超大的视频请先在本地压缩，或用快速切片模式。

## 📡 API 一览（供二次开发）

除公开接口外均通过 HttpOnly Cookie 鉴权（同源 `fetch` 自动携带）；非 GET 请求需同源 Origin。

```
POST   /api/register                  {username,password,display_name} → 注册学员（受 ALLOW_REGISTER 控制）
POST   /api/login                     {username,password} → 用户信息（Set-Cookie 会话）
POST   /api/logout                    退出登录
GET    /api/me                        当前用户
GET    /api/config                    公开配置（是否允许注册）

GET    /api/courses                   课程列表（按角色过滤）
POST   /api/courses                   {title,description,teacher_id?} 老师/管理员建课
DELETE /api/courses/:id               删除课程及其视频
GET    /api/courses/:id/videos        课程视频列表
POST   /api/join                      {code} 凭邀请码加入课程
POST   /api/courses/:id/join          {code} 同上（指定课程）
POST   /api/courses/:id/invite        重新生成邀请码（老师/管理员）
GET    /api/courses/:id/students      已加入学员（老师/管理员）
POST   /api/courses/:id/teacher       {teacher_id} 指定任课老师（管理员）

POST   /api/videos                    {course_id,title} → {id}
GET    /api/videos/:id                视频详情
POST   /api/videos/:id/complete       {size,duration} → 状态置为 ready
PUT    /api/videos/:id/m3u8           原始 m3u8 内容
PUT    /api/videos/:id/segments/:ts   分片内容
DELETE /api/videos/:id                删除视频
GET    /hls/:id/:file                 HLS 播放代理（m3u8 与分片同路径）
GET    /files/chat/:name              聊天图片

GET    /api/contacts                  可聊天联系人（含未读数/在线状态）
GET    /api/chat/:peer?before=id      聊天记录（默认最近 50 条）
POST   /api/chat/upload               multipart 上传聊天图片（≤10MB）
GET    /api/ice                       WebRTC ICE 服务器配置
GET    /ws                            WebSocket：聊天 + 通话信令

GET    /api/users                     用户列表（管理员）
POST   /api/users                     创建账号（管理员）
POST   /api/users/:id/status          启用/禁用账号（管理员）
POST   /api/users/:id/password        重置密码（管理员）
```

### WebSocket 消息协议

客户端 → 服务端：`chat.send {to,msg:{type,content}}`、`chat.read {to}`、`call.offer {to,sdp,video}`、`call.answer {to,sdp,video}`、`call.ice {to,candidate}`、`call.end {to}`

服务端 → 客户端：`presence {ids}`、`chat.new {from,msg}`、`chat.sent {msg}`、`call.offer {from,sdp,video}`、`call.answer {from,sdp}`、`call.ice {from_id,candidate}`、`call.end {from_id}`、`call.busy {message}`、`error {message}`

## 🗂️ 项目结构

```
main.go        入口：配置、存储、数据库、静态资源嵌入、健康检查、优雅停机
config.go      环境变量（JWT_SECRET 缺省时自动生成并持久化）
session.go     Cookie 会话 + CSRF + 鉴权中间件
user.go        登录/注册/用户管理 + 登录限流
store.go       SQLite 模型与查询（用户/课程/选课/消息/视频）
storage.go     存储抽象：local / S3 / WebDAV
handlers.go    课程、视频、HLS 代理等 REST 路由
chat.go        WebSocket Hub：私聊 + 通话信令 + 聊天上传
ws_test.go     端到端测试
web/           Vue 3 前端（Vite）

Dockerfile             多阶段构建（前端 → 静态二进制 → alpine，约 50MB）
docker-compose.yml     一键部署（支持 --build 或拉取镜像仓库镜像）
.dockerignore          构建上下文裁剪
.github/workflows/     ci.yml（测试）与 docker.yml（多架构镜像推送 GHCR）
deploy/                systemd unit 与环境变量模板（不用 Docker 时）
scripts/               交叉编译 Linux 二进制的脚本（.ps1 / .sh）
```
