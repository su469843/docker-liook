# 🚀 CourseHub 部署指南

> 🌐 本篇为简体中文版 · English edition: [docs/DEPLOYMENT_EN.md](DEPLOYMENT_EN.md)

本文是 **docker-liook（CourseHub）** 的完整部署手册：从准备一台服务器，到通过 IP / 域名访问站点、配置 HTTPS 反向代理、音视频通话 TURN 中继、数据备份与故障排查，一步步带你跑起来。

> 适用版本：`main` 分支（单容器单实例架构，SQLite 单写者）。
> 安全清单与威胁模型见 [SECURITY.md](../SECURITY.md)，功能与特性总览见 [README](../README.md)。

## 目录

- [1. 部署前准备](#1-部署前准备)
- [2. IP 与访问地址怎么定](#2-ip-与访问地址怎么定)
- [3. 方式一：Docker Compose 构建（推荐）](#3-方式一docker-compose-构建推荐)
- [4. 方式二：拉取 CI 构建好的镜像](#4-方式二用-ci-构建好的镜像服务器不用装-gonode)
- [5. 方式三：二进制 + systemd（不用 Docker）](#5-方式三二进制--systemd不用-docker)
- [6. 反向代理与 HTTPS](#6-反向代理与-https)
- [7. 防火墙 / 云安全组](#7-防火墙--云安全组)
- [8. 音视频通话与 TURN 中继](#8-音视频通话与-turn-中继)
- [9. 对象存储（可选）：S3 / MinIO / WebDAV](#9-对象存储可选s3--minio--webdav)
- [10. 日常运维](#10-日常运维)
- [11. FAQ](#11-faq)

---

## 1. 部署前准备

### 1.1 服务器要求

| 项目 | 最低建议 | 说明 |
|---|---|---|
| CPU | 2 核 | 转码 HLS 吃 CPU，核越多转码越快 |
| 内存 | 2 GB | 镜像常驻内存几十 MB，转码时有富余即可；小机器建议加 1G swap |
| 磁盘 | 视频体积 × 2 以上 | 源文件和 HLS 切片都存在 `data/` 目录 |
| 系统 | 任意能跑 Docker 的 Linux（amd64 / arm64 均可） | Ubuntu 22.04 / Debian 12 / CentOS Stream 等都行 |

### 1.2 需要安装的软件

- **方式一 / 方式二**：只需要 Docker（建议 20.10+，含 `docker compose` 插件）：

  ```bash
  curl -fsSL https://get.docker.com | sh   # 一键安装 Docker（含 compose 插件）
  docker compose version                    # 验证
  ```

- **方式三（裸二进制）**：不需要 Docker，但服务器上要有 Go 1.27+（或在别的机器上交叉编译好再拷过去）。

### 1.3 一个域名（可选但强烈建议）

公网部署**强烈建议**准备一个域名并解析到服务器公网 IP，原因：

1. 浏览器只允许在 `https://` 或 `localhost` 下使用摄像头 / 麦克风 —— **不配 HTTPS，音视频通话在公网上用不了**；
2. HTTPS 也避免运营商劫持、运营商缓存导致的「升级后跑旧前端」问题。

没有域名也可以用 IP 跑起来（见第 2 节），只是没有通话能力且要接受 `http://` 明文。

---

## 2. IP 与访问地址怎么定

这是部署时最容易迷糊的部分，先理清「哪台机器、哪个 IP、哪个端口」：

```text
学员浏览器
   │  http://192.168.1.10:8080        ← 内网直连（局域网内用）
   │  http://服务器公网IP:8080          ← 公网直连（仅临时测试）
   │  https://course.example.com       ← 域名 + HTTPS 反代（公网正式推荐）
   ▼
防火墙 / 云安全组（放行 80/443，按需放行 8080）
   ▼
Nginx / Caddy 反向代理（80/443，可选）
   ▼
CourseHub 容器（监听容器内 8080，宿主机映射 ${HOST_PORT:-8080}）
   ▼
./data/   ← SQLite 数据库 + 视频/文档/聊天图片，全在这一目录
```

### 2.1 按访问方式选地址

| 场景 | 访问地址 | 需要配置 |
|---|---|---|
| 在服务器本机试一下 | `http://localhost:8080` | 无 |
| 内网/局域网给同事用 | `http://<服务器内网IP>:8080`，如 `http://192.168.1.10:8080` | 放行 8080/TCP；`COOKIE_SECURE` 保持 `false` |
| 公网临时测试 | `http://<公网IP>:8080` | 云安全组放行 8080；仅建议临时测试用 |
| 公网正式使用 | `https://你的域名` | 域名 A 记录指到公网 IP + 反代 HTTPS（见第 6 节）+ `COOKIE_SECURE=true` |

> 查服务器内网 IP：`ip addr` 或 `hostname -I`；公网 IP 以云控制台为准（云主机的公网 IP 多为 NAT 映射，网卡上只能看到内网地址）。

### 2.2 端口与 IP 相关配置速查

| 配置项 | 在哪配 | 填什么 |
|---|---|---|
| 宿主机端口 | `.env` 的 `HOST_PORT` | 默认 `8080`；想走 80 就改 `80`（需 root 权限，且别和反代冲突） |
| 容器内端口 | 一般不改 | 固定 `8080`（compose 已固定 `PORT=8080`） |
| 访问地址 | 浏览器 | `http://<服务器IP>:8080` 或 `https://域名` |
| `COOKIE_SECURE` | `.env` | 全站 HTTPS 时设 `true`；用 IP + http 访问时**必须保持 `false`**，否则登录不上 |
| S3 / WebDAV 地址 | `.env` 或后台「站点设置 → 文件存储」 | 对象存储的内网/公网地址，如 `http://<minio内网IP>:9000` |
| `TURN_URL` | `.env` | 如 `turn:<服务器公网IP>:3478`，多个用英文逗号分隔 |

> ⚠️ **改端口后**：访问地址是 `http://服务器IP:HOST_PORT`，容器内仍是 `8080`，不用改 `PORT`。
>
> ⚠️ **改 IP 或换域名后**：视频通话 / WebSocket 走的是页面当前的源，无需在应用里改地址；但换了域名记得把 `COOKIE_SECURE` 与反代证书对应上。

### 2.3 常见「配错 IP」的现象

| 现象 | 多半是 |
|---|---|
| 本机能开，别的机器打不开 | 云安全组 / `ufw` / `firewalld` 没放行 8080，或服务只绑了 `127.0.0.1`（本项目默认监听 `0.0.0.0`，无此问题） |
| 能开页面但登录后马上掉 | 用 HTTPS 域名访问但 `COOKIE_SECURE=false` → 设 `true`；或反过来 http 访问却设了 `true` |
| 视频通话「连接中…」一直转 | 没用 https（浏览器拿不到摄像头）或没配 TURN（双方都在对称 NAT 后） |
| 内网用 IP 访问通话不可用 | 正常现象：浏览器只允许 `https` 或 `localhost` 使用摄像头/麦克风 |

---

## 3. 方式一：Docker Compose 构建（推荐）

最简单的方式，在服务器上直接构建并启动：

```bash
# 1. 安装 Docker（已有可跳过）
curl -fsSL https://get.docker.com | sh

# 2. 拉代码
git clone https://github.com/su469843/docker-liook.git
cd docker-liook

# 3. 配置环境变量（至少改管理员密码！）
cp .env.example .env
vi .env
```

`.env` 里**至少**改这两项：

```ini
# 管理员初始密码，务必改掉默认的 admin123
ADMIN_PASSWORD=你的强密码

# 会话签名密钥：填 32 位以上随机串（也可以留空自动生成）
# 生成随机串：openssl rand -hex 32
JWT_SECRET=
```

常用可选项（都有默认值，按需改）：

```ini
# 宿主机端口：想让用户直接访问 80 就改成 80（之后访问 http://服务器IP）
HOST_PORT=8080

# 站点名称（之后在后台也能改）
SITE_NAME=我的课程站

# 注册模式：open 开放注册 / invite 凭机构邀请码注册 / closed 关闭注册
REGISTER_MODE=invite

# 全站 HTTPS 后打开（Cookie 仅经 HTTPS 传输）
COOKIE_SECURE=false
```

然后启动：

```bash
# 4. 构建并启动（首次构建要拉依赖，几分钟）
docker compose up -d --build

# 5. 验证
docker compose ps        # STATUS 出现 (healthy) 即正常
curl http://127.0.0.1:8080/healthz   # → {"status":"ok"}
docker compose logs -f   # 看日志，Ctrl+C 退出
```

### 3.1 首次登录

浏览器打开 **`http://服务器IP:8080`**（端口按你改的 `HOST_PORT`）：

1. 用户名 `admin`，密码 = 你在 `.env` 里设的 `ADMIN_PASSWORD`；
2. 立刻到 **「后台管理 → 用户管理」** 创建老师账号（老师账号只能由管理员创建）；
3. 到 **「后台管理 → 注册设置」** 确认注册模式（这里的设置之后优先于 `.env`）。

> 数据都在宿主机 `./data/` 目录（SQLite + 视频 + 文档 + 聊天图片 + `.jwt_secret`），**备份 = 拷这个目录**。

### 3.2 数据目录权限

用 bind mount（`./data:/data`）时，宿主机目录要属于容器内用户 uid `10001`，否则启动报「数据库只读 / 无法写入」：

```bash
sudo chown -R 10001:10001 ./data
```

---

## 4. 方式二：用 CI 构建好的镜像（服务器不用装 Go/Node）

推送到 GitHub 后，`.github/workflows/docker.yml` 会自动构建 **linux/amd64 + arm64** 双架构镜像并推到 GHCR，本仓库对应的镜像地址是：

```
ghcr.io/su469843/docker-liook:latest
```

```bash
git clone https://github.com/su469843/docker-liook.git
cd docker-liook

cp .env.example .env && vi .env      # 同上，至少改 ADMIN_PASSWORD

# 拉镜像并启动（IMAGE 变量决定 compose 用哪个镜像）
IMAGE=ghcr.io/su469843/docker-liook:latest docker compose pull
IMAGE=ghcr.io/su469843/docker-liook:latest docker compose up -d
```

- 镜像若是私有的，先登录（密码用 GitHub PAT，勾 `read:packages`）：
  `echo <PAT> | docker login ghcr.io -u <GitHub用户名> --password-stdin`
- 推到其它仓库（阿里云 ACR / Docker Hub / 私有 Harbor 同理）：

  ```bash
  docker build -t registry.cn-hangzhou.aliyuncs.com/<命名空间>/course-hub:latest .
  docker login registry.cn-hangzhou.aliyuncs.com
  docker push registry.cn-hangzhou.aliyuncs.com/<命名空间>/course-hub:latest
  # 服务器上：IMAGE=registry.cn-hangzhou.aliyuncs.com/<命名空间>/course-hub:latest docker compose up -d
  ```

> 本机不想装 Docker：代码推到 GitHub 让 Actions 构建，服务器只 `docker pull`。

---

## 5. 方式三：二进制 + systemd（不用 Docker）

适合 512M 小机器或已有 systemd 的环境。产物是纯静态 Go 二进制（前端已嵌入，无外部运行时依赖）。

```bash
# 1. 编译（有 Go 1.27+ 的机器上，项目根目录执行）
./scripts/build-linux.sh amd64        # 产出 dist/course-hub-linux-amd64
# Windows 上交叉编译：.\scripts\build-linux.ps1

# 2. 安装到服务器
sudo useradd -r -s /usr/sbin/nologin -d /opt/coursehub coursehub
sudo mkdir -p /opt/coursehub /var/lib/coursehub
sudo cp dist/course-hub-linux-amd64 /opt/coursehub/course-hub
sudo chmod +x /opt/coursehub/course-hub
sudo chown -R coursehub:coursehub /opt/coursehub /var/lib/coursehub

# 3. 配置 + systemd
sudo cp deploy/coursehub.env.example /etc/coursehub.env
sudo vi /etc/coursehub.env             # 改 ADMIN_PASSWORD 等
sudo cp deploy/course-hub.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now course-hub

# 4. 验证
systemctl status course-hub
journalctl -u course-hub -f
curl http://127.0.0.1:8080/healthz
```

注意 systemd 版的数据目录是 `/var/lib/coursehub`（unit 文件里已通过 `ReadWritePaths` 放行），不是 Docker 的 `./data`。

---

## 6. 反向代理与 HTTPS

公网正式使用建议：**Nginx 或 Caddy 443 收口 → 转发到本机 8080**，这样：

- 浏览器拿到 HTTPS，摄像头/麦克风（音视频通话）可用；
- 配合 `.env` 里 `COOKIE_SECURE=true`，会话 Cookie 只走 HTTPS；
- 服务端**只在直连方是内网/回环地址时才信任 `X-Forwarded-For`**（即只信自己的反代），所以登录限流不会被伪造头绕过 —— 反代时请务必带 `X-Forwarded-For` 或 `X-Real-IP` 头。

### Nginx 示例（含 WebSocket / 大文件 / HLS）

```nginx
server {
    listen 80;
    server_name course.example.com;
    return 301 https://$host$request_uri;   # 强制跳 HTTPS
}

server {
    listen 443 ssl http2;
    server_name course.example.com;

    ssl_certificate     /etc/nginx/ssl/fullchain.pem;
    ssl_certificate_key /etc/nginx/ssl/privkey.pem;

    # 上传大视频必须放开；≥ .env 里的 MAX_UPLOAD_MB
    client_max_body_size 4096m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # WebSocket（聊天 + 通话信令）必须透传 Upgrade
    location /ws {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

### Caddy 示例（自动 HTTPS，最省事）

```caddy
course.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Caddy 原生支持 WebSocket 与 `X-Forwarded-For`，无需额外配置。

### HTTPS 上线检查单

- [ ] 反代把 `/`、`/api`、`/hls`、`/files`、`/ws` 都转发到 8080（上面配置已覆盖）
- [ ] `.env` 改 `COOKIE_SECURE=true`，然后 `docker compose up -d` 重建
- [ ] Nginx `client_max_body_size` ≥ 上传上限；Caddy 默认不限
- [ ] 用 `https://` 访问确认锁图标；`curl https://域名/healthz` 返回 `{"status":"ok"}`

---

## 7. 防火墙 / 云安全组

按部署形态放行端口（以 Ubuntu `ufw` 为例，云服务器同时在控制台安全组里放行）：

| 部署形态 | 需要放行 | 命令示例 |
|---|---|---|
| 内网 http 直连 | 8080/tcp | `sudo ufw allow 8080/tcp` |
| 公网 http 临时测试 | 8080/tcp | 同上（不推荐长期暴露） |
| HTTPS 反代 | 80/tcp + 443/tcp | `sudo ufw allow 80,443/tcp` |
| 自建 TURN | 3478/tcp+udp，49160-49200/udp | `sudo ufw allow 3478` `sudo ufw allow 49160:49200/udp` |

> SSH 22 端口记得保留放行。**8080 可以不对公网开放**（只绑反代时），更安全。

---

## 8. 音视频通话与 TURN 中继

WebRTC 通话默认走 STUN 打洞，双方只要有一方在普通家庭/办公网就能直连。以下情况需要自建 TURN（如 coturn）：

- 双方都在对称 NAT / 企业网内，画面一直「连接中…」；
- 企业网络禁 UDP，需要 TURN over TCP 443。

`.env` 里配：

```ini
TURN_URL=turn:turn.example.com:3478    # 或 turn:<公网IP>:3478，多个用逗号分隔
TURN_SECRET=换成coturn的static-auth-secret   # 推荐 use-auth-secret 模式，下发 2 小时临时账号
```

coturn 侧关键配置（`/etc/turnserver.conf`）：

```
listening-port=3478
fingerprint
use-auth-secret
static-auth-secret=和 TURN_SECRET 一致的值
realm=course.example.com
min-port=49160
max-port=49200
external-ip=<公网IP>/<内网IP>   # 云主机 NAT 后必须配
```

并在防火墙放行 `3478/tcp+udp` 与 `49160-49200/udp`。更多排障见 [README → 通话排障速查](../README.md#通话排障速查)。

---

## 9. 对象存储（可选）：S3 / MinIO / WebDAV

默认 `STORAGE_TYPE=local` 时视频切片、文档、图片都在 `./data`。想省服务器磁盘，可以在后台 **「站点设置 → 文件存储」** 里一键切换到 S3/WebDAV（可先「测试连接」），或直接用环境变量：

```ini
STORAGE_TYPE=s3
S3_ENDPOINT=<minio内网IP>:9000        # ⚠️ 只填 host:port，不要带 http:// —— 是否用 HTTPS 由 S3_USE_SSL 控制
S3_BUCKET=coursehub
S3_ACCESS_KEY=xxx
S3_SECRET_KEY=xxxx
S3_USE_SSL=false
```

- compose 文件里附带了注释掉的 **MinIO** 服务，取消注释并设 `STORAGE_TYPE=s3` 即可一起跑起来；
- **存储桶不要开公共读**，切片是加密的、地址是签名的，公共读等于绕过保护；
- 接对象存储后，本机只剩 SQLite 数据库，备份更轻。

---

## 10. 日常运维

### 备份与恢复

```bash
# 备份（建议先 docker compose down 或低峰期再打包，SQLite 运行中直接拷可能不一致）
sudo tar czf coursehub-backup-$(date +%F).tar.gz ./data
# 用了 S3/WebDAV 的，另存对象存储桶

# 恢复：把 data 目录放回原位（注意 chown 10001:10001），再 up -d
```

### 升级

```bash
git pull
docker compose up -d --build      # 或 docker compose pull && docker compose up -d
```

数据目录会自动迁移，升级前建议先备份 `./data`。

### 忘记 admin 密码

`ADMIN_PASSWORD` **只在数据库第一次初始化时**用于创建 admin，之后再改环境变量不会改密码：

```bash
# 全新环境（会丢数据，慎用）
docker compose down && sudo rm -f ./data/coursehub.db* && docker compose up -d

# 有其它管理员：在「后台管理 → 用户管理」里重置密码
```

### 健康检查与优雅停机

- `GET /healthz` 不需要登录，Docker `HEALTHCHECK` 已内置；`docker compose ps` 出现 `(healthy)` 即正常。
- `docker stop` / `systemctl stop` 会先等请求处理完再关库，不会丢正在写的数据。

### 常用命令

```bash
docker compose ps                     # 状态（healthy）
docker compose logs -f --tail 200     # 日志
docker compose restart                 # 重启
docker compose down                    # 停止（数据仍在 ./data）
docker compose down && sudo rm -f ./data/coursehub.db* && docker compose up -d  # 清库重建（会丢数据！）
```

---

## 11. FAQ

| 问题 | 处理 |
|---|---|
| 打不开页面 | `docker compose ps` 看是否 healthy；`curl http://127.0.0.1:8080/healthz` 区分「服务没起」还是「防火墙没放行」；安全组/`ufw` 放行端口 |
| 登录提示失败但密码没错 | 连续错 10 次触发登录限流（同 IP+用户名 15 分钟），等几分钟；或确认没用 `COOKIE_SECURE=true` 走 http |
| 上传视频报「文件超过上限」 | 直传受 `MAX_UPLOAD_MB`（默认 4096MB）限制；更大文件用上传弹窗里的「分片上传」；走 Nginx 时记得 `client_max_body_size` ≥ 该值 |
| 视频一直「转码中」 | 服务端 ffmpeg 转码是重活，小机器慢属正常；重启后正在转的课时会标记为失败，需重新上传（已知限制） |
| 通话「连接中…」失败 | 没 https 或没 TURN，见上文第 6/8 节 |
| 想让学员只能看不能注册 | 后台「注册设置」选「关闭注册」，账号全部由管理员创建 |
| 前端白屏 | 先 `Ctrl+F5` 强刷；仍异常可在 `.env` 临时 `CSP_DISABLE=true` 重启排查，解决后记得关掉 |

---

## 部署检查清单（一行版）

```text
改 ADMIN_PASSWORD → 定 JWT_SECRET → 选访问形态（内网 IP:8080 / 域名 HTTPS）→
配反代（WS + 大文件）→ COOKIE_SECURE=true → 放行安全组端口 → chown 10001 ./data →
（可选）S3/MinIO、TURN → 备份 ./data
```

有问题先看 `docker compose logs -f` 与 `GET /healthz`，其余参见 [README 的排障小节](../README.md)。
