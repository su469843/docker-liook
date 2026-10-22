# 📚 CourseHub — 自托管课程平台（对标小鹅通 / 知识店铺）

<p align="center">
  <a href="https://github.com/su469843/docker-liook/releases">
    <img src="https://img.shields.io/github/release/su469843/docker-liook" alt="Release version">
  </a>
  <a href="https://github.com/su469843/docker-liook/blob/master/LICENSE">
    <img src="https://img.shields.io/github/license/su469843/docker-liook" alt="License">
  </a>
  <a href="https://github.com/su469843/docker-liook/pkgs/container/docker-liook">
    <img src="https://img.shields.io/badge/ghcr.io-su469843%2Fdocker--liook-blue" alt="GHCR">
  </a>
  <a href="https://github.com/su469843/docker-liook/stargazers">
    <img src="https://img.shields.io/github/stars/su469843/docker-liook?style=flat&logo=github&color=yellow&label=Stars" alt="GitHub Stars">
  </a>
  <a href="https://github.com/su469843/docker-liook/network">
    <img src="https://img.shields.io/github/forks/su469843/docker-liook?style=flat&logo=github&color=purple&label=Forks" alt="GitHub Forks">
  </a>
</p>


用 **Vue 3 + Go** 编写的轻量在线课程站点，界面与流程参考小鹅通 / 知识店铺：**老师有独立的教学中心**（建课、上架、课时管理、学员管理），**学员只在课程市场选课、上课、和老师/同学群聊**。视频上传后**由服务器 ffmpeg 切成 HLS 并加密**，播放走带有效期的签名地址；整个站点**自托管**，数据、存储、访问策略都在自己手里，单容器跑得动。

## ✨ 特性

- 🧑‍🏫 **老师教学中心**：老师建课 → 上传课时（视频 / 图文 / 文档）→ 一键上架到课程市场，或只把邀请码发给自己的学员
- 🛒 **课程市场**：老师把课程「上架市场」后，学员可以自己在市场里选课加入；没上架的课程只能凭邀请码加入
- 🎬 **服务端转码 HLS**：源视频直传/分片上传到存储后端，服务器用 ffmpeg 切成 HLS 并 **AES-128 加密**；播放地址是 **带有效期的签名 URL**，过期/外链盗用直接失效（没装 ffmpeg 时自动退化为 MP4 + HTTP Range 渐进播放）
- 📝 **三种课时类型**：视频课时（HLS 播放）、图文课时（富文本编辑器，支持插图）、文档课时（PDF / Word / PPT / 文本，在线预览或下载）
- ✍️ **作业 / 练习 / 打卡**：老师按课程出「作业 / 考卷 / 单词 / 句子」练习，题型支持选择、填空、判断、简答；**客观题交卷即自动判分**，含简答题时老师批改后出总分；学员端有**单词/句子每日打卡**（连续天数、本月与累计统计），老师可查看课程学员打卡情况。本版本**不内置题库**，题目全部由老师自己录入
- 💬 **课程群聊 + 私聊**：加入课程后自动获得「任课老师私聊」与「课程群聊」，群里可以**引用某个课时的某个播放时间点**，点一下直接跳到那一段
- 🎨 **手绘矢量图标**：全站图标与聊天表情都是内置的手绘 SVG（不依赖 emoji 字体，任何系统上显示一致、不发虚）
- 🔐 **注册策略可选**：后台三档 —— 开放注册 / 必须填机构邀请码 / 完全关闭（只能由管理员建号）
- 💾 **存储后端可切换**：本地目录 / S3（MinIO、AWS 等 S3 兼容存储）/ WebDAV，后台「站点设置 → 文件存储」里填地址一键测试并切换，也可用环境变量配置
- 📦 **Docker 一键部署**：前端打包产物嵌入 Go 二进制，单容器启动，无外部依赖
- 🗃️ **元数据存 SQLite**（纯 Go 驱动，无 CGO），随数据目录自动建库，旧版 `videos` 数据自动迁移
- 👥 **多用户与角色**：管理员管用户 / 注册策略 / 全部课程；老师管自己的课；学员选课上课
- 🔐 **会话安全**：登录态用 HttpOnly + SameSite Cookie（HMAC 签名），非 GET 请求校验 Origin 防 CSRF，登录失败限流
- ▶️ hls.js 播放，经后端代理存储，天然免 CORS
- 📞 **1 对 1 语音/视频通话**：WebRTC + WebSocket 信令，可选 TURN 中继，通话忙线自动提示；摄像头不可用自动降级语音，信令短暂断线/多标签页不会掉线

## 👥 角色与权限

| 角色 | 能做什么 | 入口 |
|---|---|---|
| 管理员 admin | 管理用户（建老师/学员账号、改密、禁用）、设置注册模式与站点名、管全部课程（改任课老师、上架/下架、删除） | 「后台管理」 |
| 老师 teacher | 只在**教学中心**建课、管理课时（视频/图文/文档）、上传封面、上架市场、查看学员、发邀请码；布置并批改作业/练习、查看学员打卡 | 「教学中心」 |
| 学员 student | 在**课程市场**浏览已上架课程、凭邀请码加入课程、上课（看视频/图文/文档）、**做作业/练习（自动判分）**、**单词句子每日打卡**、和老师私聊、和同班同学群聊 | 「发现课程」/「我的课程」/「练习中心」 |

- 首次启动自动创建管理员 **admin**，密码取环境变量 `ADMIN_PASSWORD`；登录后到「后台管理 → 用户管理」创建老师账号。
- 老师把课程**上架市场**，学员即可自助加入；不上架时，老师把课程**邀请码**发给学员，学员在「我的课程」输入邀请码加入。
- 学员注册得到的账号一律是 student；**老师 / 管理员账号只能由管理员创建**，任何人无法自助注册成老师。

## 🎯 对标小鹅通：已经做到哪一步

定位是「**自托管版的小鹅通 / 知识店铺**」：把 **建课 → 上架 → 选课 → 上课 → 群聊互动** 这条主链路做通，其余按自托管场景取舍，不做支付相关的营销体系。

图例：✅ 已具备 · 🟡 部分具备 · ⬜ 没做（含明确不做的）

| 能力 | 小鹅通 | CourseHub（本项目） | 状态 |
| --- | --- | --- | --- |
| 老师独立后台 | 知识店铺 + 讲师/员工角色 | 老师「教学中心」建课、管课时、看学员、发邀请码；管理员另有后台 | ✅ |
| 课程形态 | 视频 / 音频 / 图文 / 直播 / 专栏 | 视频（HLS）、图文（富文本）、文档（PDF/Word/PPT/文本）；**没有音频课时、直播、专栏合集** | 🟡 |
| 上架与分发 | 知识商品、渠道分销、店铺装修 | 「上架到课程市场」让学员自助选课，或课程邀请码定向分发 | ✅ |
| 视频转码 | 云端多清晰度、自适应码率、秒开 | 服务端 ffmpeg 转**单码率** HLS（10s 分片）；**无多清晰度/自适应**，进度只显示「转码中」 | 🟡 |
| 大文件上传 | 断点续传、分片 | 默认直传（带进度）+ 可选 16MB 分片上传，单片可重传、服务端合并 | ✅ |
| 内容保护 | 防盗链、播放域名白名单、跑马灯水印、DRM | HLS **AES-128 加密** + **带有效期签名 URL**（分片/密钥都签名）+ 登录态与课程权限校验；**无水印、无 DRM、无并发设备数限制** | 🟡 |
| 存储 | 点播云 / 对象存储 | 本地目录 / S3(MinIO) / WebDAV，后台一键切换并测试连接 | ✅ |
| 学习路径 | 目录、学习进度、学习记录、考试、作业、证书 | 课时目录 + 播放 + 「引用视频时间点」发群聊；**作业/练习（选择题/填空/判断/简答，客观题自动判分 + 简答题老师批改）、单词/句子打卡**；**无学习进度/完成度记录，无正式考试/证书** | 🟡 |
| 互动 | 社群、圈子、问答、评论、弹幕 | 课程群聊 + 老师私聊 + **1 对 1 语音/视频通话**；**无评论/问答板块、无弹幕** | 🟡 |
| 通知 | 短信、服务号、App Push、邮件 | 站内未读红点（多标签页同步）；**无短信/邮件/微信推送** | 🟡 |
| 用户与组织 | 会员体系、多门店、员工细分权限 | 三角色（管理员/老师/学员）+ 三档注册策略（开放 / 机构邀请码 / 关闭）；**无多租户、无机构层级、无细粒度权限** | 🟡 |
| 营销 | 优惠券、拼团、分销、裂变海报 | 不做（本项目没有价格/订单概念） | ⬜ |
| 支付 | 微信 / 支付宝 / 苹果 IAP、退款、发票 | **明确不做**：课程免费开放，凭邀请码或课程市场自助加入 | ⬜ |
| 数据 | 数据看板、转化漏斗、留存分析 | 工作台只有基础计数（课程/学员/课时/上架数）；**无转化、留存、播放量分析** | 🟡 |
| 客户端 | 小程序、H5、App、播放器 SDK | 响应式 Web（手机浏览器可用）；**无小程序/App/原生 SDK** | 🟡 |
| 直播 | 大班直播、连麦、回放 | 只有 1 对 1 点对点通话（WebRTC）；**无直播推流、大班连麦、直播回放** | 🟡 |
| 部署形态 | SaaS 多租户云服务 | **自托管单实例**：一个 Docker 容器 + 一个数据目录 | ✅ |

## 🏠 自托管：我们刻意选择这条路

**好处（也是本项目的设计前提）**

- **数据完全是自己的**：课程、学员、聊天记录、视频切片都在自己的服务器/对象存储里。SQLite 一个文件 + 存储目录，备份 = 拷 `./data`（或对象存储桶）。
- **没有按人头/按流量收费**：不限学员数、课程数、播放量，也不存在账号被停用、店铺被下架的问题。
- **可以内网/离线部署**：不依赖云转码、云存储、短信通道；转码用本机 ffmpeg，播放只连自己的服务器。
- **不锁定厂商**：存储可换本地 / S3 / WebDAV，数据库是标准 SQLite，前端是纯静态产物，迁移成本低。
- **访问策略自己定**：注册模式（开放 / 邀请码 / 关闭）、课程是否上架、邀请码随时重置，都在自己后台。
- **代码可改**：Go + Vue 单仓，REST/WebSocket 协议都写在 README 里，想接自己的 SSO、存储、播放器都能改。

**自托管要自己承担的部分（不藏着）**

- **HTTPS 与反代**：浏览器只在 `https://` 或 `localhost` 下允许摄像头/麦克风；公网通话必须配 HTTPS 反代，并透传 `/ws`、`/hls`、`/files`。
- **TURN 服务器**：双方都在对称 NAT / 企业网时，纯 STUN 可能连不通，需要自建 coturn 并配 `TURN_URL`。
- **转码吃 CPU**：服务端 ffmpeg 转 HLS 是重活，1 核 2G 的机器转长视频会很慢；可调 `TRANSCODE_CONCURRENCY`，或先在本机压成 H.264 再传。
- **单实例**：SQLite 单写者 + 本地临时目录，**不支持多副本/负载均衡**；没有 CDN，切片默认从你的服务器直出（接 S3 后可挂对象存储/CDN）。
- **运维自助**：没有监控告警（只有 `/healthz`）、没有自动备份、没有灰度发布；升级 = 换镜像，数据目录会自动迁移。
- **安全责任转移**：管理员密码、`.env`、`JWT_SECRET`、存储密钥都由你保管；建议只在内网或加反代鉴权后暴露到公网。

## 🔐 安全模型（为什么 API 文档可以公开）

接口、参数、错误码都写在 README 里，但**能不能用是另一回事**——每个接口都要过下面几道关，攻击面取决于部署配置而不是文档是否公开：

- **每个接口都要求身份**：除 `/healthz`、登录/注册、`/api/config` 外全部要求 HttpOnly 会话 Cookie；媒体走**带有效期的 HMAC 签名**（默认 2 小时），分片和 AES 密钥也会被重签到 m3u8 里。
- **按角色 + 按课程授权**：老师只能管自己的课，学员只能看已加入或已上架课程，群聊/私聊都要校验关系；越权一律 403。
- **CSRF / XSS / 注入**：非 GET 请求校验同源 `Origin`（含登录/注册）；会话 Cookie 是 HttpOnly，JS 偷不到；富文本白名单清洗；SQL 全参数化；`script-src 'self'` 的 CSP 兜底。
- **限流与枚举防护**：登录（IP+用户名）、注册、登录 IP 频率、WS 聊天/信令都有令牌桶；登录失败统一提示 + bcrypt 时间对齐。
- **媒体不可直连**：HLS 用 AES-128 加密，切片只经由后端代理（存储桶**不要开公共读**），盗链地址过期即失效。
- **密钥不外泄**：后台返回 S3/WebDAV 密钥自动打码；TURN 支持 `TURN_SECRET` 下发短时效临时账号。

完整的威胁模型、逐条部署检查清单、已知限制与漏洞报告方式见 **[SECURITY.md](SECURITY.md)**。

## 🧭 尚未完善 / 后续计划

按优先级排的「还不够好」清单（欢迎按需提 PR）：

**视频与播放**
- [ ] 多清晰度（360p/720p/1080p）+ 自适应码率（现在单码率）
- [ ] 转码进度百分比、失败重试按钮（现在只有 uploading / processing / ready / failed 四态）
- [ ] 转码任务持久化：**服务重启时正在转码的课时会停在「转码中」，需要重新上传**（当前实现；启动时会自动标记为失败）
- [ ] 播放进度记忆/续播、课程学习完成度统计
- [ ] 字幕（VTT）、缩略图预览、音频课时
- [ ] 上传会话清理：异常中断的临时分片留在 `data/tmp-upload/`，目前需手动清理

**教学与互动**
- [ ] 课程合集/专栏、课时试看、章节权限
- [ ] 评论 / 问答 / 正式考试（计时防作弊）/ 证书
- [ ] 课程水印（跑马灯）、截屏防护、并发播放设备数限制

**管理与运营**
- [ ] 数据看板（播放量、完课率、活跃）、操作审计日志
- [ ] 学员分组/班级、批量导入（CSV）
- [ ] 站内通知中心 + 邮件/Webhook 通知
- [ ] 多租户 / 机构层级 / 细粒度角色权限
- [ ] 全文搜索、i18n

**工程**
- [ ] 支持 PostgreSQL / 多实例部署（当前 SQLite 单写者）
- [ ] 持久化转码队列 + 进度上报（现在是内存信号量 + 前端轮询状态）
- [ ] Prometheus 指标、结构化日志

**明确不做**
- 在线支付 / 订单 / 退款 / 发票（本项目定位就是「不接支付的课程平台」）
- 小程序 / 原生 App / 直播推流

> 版本：当前 `v1.0.0`，发布说明见 [Releases](https://github.com/su469843/docker-liook/releases)。

## 🚀 部署

三种方式任选，产物都是同一个 Go 二进制（前端已嵌入，无外部运行时依赖）。

**📄 文档**：部署手册 [中文](docs/DEPLOYMENT.md) · [English](docs/DEPLOYMENT_EN.md)｜二次开发指南 [中文](docs/DEVELOPMENT.md) · [English](docs/DEVELOPMENT_EN.md)

### 方式一：Docker Compose 在服务器上直接构建（最简单）

```bash
# 1. 把项目放到服务器上（git clone 或直接上传）
git clone https://github.com/su469843/docker-liook.git
cd docker-liook

# 2. 配置（至少改密码！）
cp .env.example .env
vi .env    # 修改 ADMIN_PASSWORD；需要 S3/WebDAV 就改 STORAGE_TYPE 和对应配置

# 3. 构建并启动
docker compose up -d --build

# 4. 看状态 / 日志
docker compose ps          # STATUS 里出现 (healthy) 说明启动正常
docker compose logs -f
```

打开 `http://服务器IP:8080`，用 `admin` + 你设置的密码登录。宿主机 `./data` 挂到容器 `/data`（数据库 + 视频 + 文档 + 聊天图片都在里面，备份这个目录即可）。

> ⚠️ 用 bind mount（`./data:/data`）时，宿主机目录要属于容器内的 uid `10001`：
> `sudo chown -R 10001:10001 ./data`

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
sudo cp dist/course-hub-linux-amd64 /opt/coursehub/course-hub
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

### 忘记管理员密码 / 登录不上

`ADMIN_PASSWORD` **只在数据库第一次初始化时**用于创建 admin 账号，之后再改这个环境变量不会改密码。如果忘记密码：

```bash
# 方式 A：删掉数据库重建（会丢数据，仅限全新环境）
docker compose down && sudo rm -f ./data/coursehub.db* && docker compose up -d

# 方式 B：让另一个管理员在「后台管理 → 用户管理」里重置密码
```

连续输错密码会触发登录限流（同一 IP + 用户名 15 分钟内最多 10 次），等几分钟再试。

### HTTPS 与反代注意事项

- **音视频通话必须在安全上下文可用**：浏览器只在 `https://` 或 `localhost` 下允许访问摄像头/麦克风。公网使用通话功能请配 HTTPS，并把 `COOKIE_SECURE=true`。
- 用 Nginx/Caddy 反代时，除了 `/api`、`/hls`、`/files`，还要透传 WebSocket `/ws`（Nginx 需要 `proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection "upgrade";`）。
- 跨网络通话建议自建 TURN（如 coturn），配置 `TURN_URL` / `TURN_USER` / `TURN_PASS`；否则双方都在对称 NAT 下可能连不通。

### 通话排障速查

| 现象 | 原因 / 处理 |
| --- | --- |
| 点「接听」后提示权限被拒 | 浏览器地址栏允许摄像头/麦克风；http 访问时根本拿不到设备（改用 https / localhost） |
| 接通后对方黑屏、没声音，但对方能看到自己 | 已修复（媒体流挂载时机）；升级前端后强制刷新一次（`Ctrl+F5`），入口页已改为 `no-cache` |
| 画面一直「连接中…」然后失败 | NAT 穿透失败，配 TURN；企业网络禁用 UDP 时也需 TURN（TURN over TCP 443） |
| 通话中突然挂断 | 信令断线有 5 秒宽限，短暂切网/刷新不会掉线；多标签页也不会互相踢掉 |
| 没有摄像头也能用 | 摄像头不可用时自动降级为语音通话，不会整通失败 |

## 💻 本地开发

> 完整的本地开发流程、代码地图与改造示例见 **[二次开发指南](docs/DEVELOPMENT.md)**（English: [docs/DEVELOPMENT_EN.md](docs/DEVELOPMENT_EN.md)）。

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

运行测试：`go test ./...`（端到端覆盖鉴权、RBAC、建课/课时/市场、邀请码、群聊引用、注册模式、聊天与通话信令）。如果还没构建前端，用 `go test -tags noembed ./...` 只测后端。

> 不想在本机装 Go / Node / Docker 也可以：推到 GitHub 后 `.github/workflows/ci.yml` 会自动跑 gofmt / go vet / go test / 前端构建，`.github/workflows/docker.yml` 会自动构建并推送 Docker 镜像，本机只需要 `git push`。

## ⚙️ 环境变量

| 变量 | 说明 | 默认 |
|---|---|---|
| `PORT` | 容器内监听端口（一般不用改） | `8080` |
| `HOST_PORT` | docker compose 映射到宿主机的端口 | `8080` |
| `IMAGE` | docker compose 使用的镜像名（从镜像仓库拉取时指定） | `course-hub:latest` |
| `ADMIN_PASSWORD` | **首次初始化**创建 admin 账号的初始密码 | `admin123` |
| `JWT_SECRET` | 会话签名密钥。留空会自动生成并持久化到数据目录 `.jwt_secret`（重启不会掉登录）；**多实例部署必须显式指定同一个值** | 自动生成 |
| `SITE_NAME` | 站点名称（首次初始化写入数据库，之后在后台改） | `课程中心` |
| `REGISTER_MODE` | 注册模式（首次初始化写入数据库，之后在后台改）：`open` 开放 / `invite` 需机构邀请码 / `closed` 关闭 | `open` |
| `ORG_INVITE_CODE` | 机构注册邀请码（`REGISTER_MODE=invite` 时用；留空自动生成，后台可查看/重置） | 自动生成 |
| `ALLOW_REGISTER` | 兼容旧配置：`REGISTER_MODE` 未设置时，`true` = open、`false` = closed | `true` |
| `COOKIE_SECURE` | 全站 HTTPS 时设为 `true`（Cookie 仅经 HTTPS 传输） | `false` |
| `STORAGE_TYPE` | 存储后端：`local` / `s3` / `webdav` | `local` |
| `STORAGE_LOCAL_DIR` | 本地存储目录（SQLite 也在这里） | `./data`（容器内 `/data`） |
| `S3_ENDPOINT` / `S3_BUCKET` / `S3_ACCESS_KEY` / `S3_SECRET_KEY` / `S3_USE_SSL` | S3(MinIO 兼容) 配置 | - |
| `WEBDAV_URL` / `WEBDAV_USERNAME` / `WEBDAV_PASSWORD` | WebDAV 配置（也可在后台「站点设置 → 文件存储」里改） | - |
| `FFMPEG_PATH` | 服务端转 HLS 用的 ffmpeg（Docker 镜像内置 `/usr/bin/ffmpeg`；未安装则自动退化为 MP4 播放） | `ffmpeg` |
| `MAX_UPLOAD_MB` | 单个视频上传上限（MB），超过请走分片上传 | `4096` |
| `SIGN_TTL_MINUTES` | 播放/下载签名 URL 有效期（分钟） | `120` |
| `TRANSCODE_CONCURRENCY` | 同时转码任务数（按 CPU 核数调整） | `2` |
| `TURN_URL` / `TURN_USER` / `TURN_PASS` | 可选 TURN 服务器（多个用逗号分隔） | - |
| `TURN_SECRET` | coturn `use-auth-secret` 共享密钥：配置后下发 2 小时临时账号，比固定账号更安全 | - |
| `CSP_DISABLE` | 排障用：设 `true` 关闭 Content-Security-Policy（默认开启） | `false` |

> `SITE_NAME` / `REGISTER_MODE` / `ORG_INVITE_CODE` 只在数据库**首次初始化**时生效，之后以「后台管理 → 注册设置」里的值为准（避免重启把后台设置覆盖掉）。

## 🎞️ 视频处理链路（服务端转码 + 加密 + 签名）

```
老师选择文件
  ├─ 直接上传（默认）：POST /api/lessons/:id/video   ← 一次请求，XHR 带进度
  └─ 分片上传（可选）：/upload/init → 每片 16MB 逐片 PUT → /upload/:id/done
        ↓ 源文件落到存储后端 src/{lessonId}/source.*
   服务端 ffmpeg → HLS（10s 分片，AES-128 加密，key 单独存放）
        ↓ 产物 hls/{lessonId}/index.m3u8 + seg*.ts + key.bin
   课时 status: uploading → processing → ready
        ↓ 前端拿到的播放地址是签名 URL
   GET /hls/:id/index.m3u8?e=<过期时间>&s=<HMAC>  →  下发时把每个分片/密钥 URI 也签进 m3u8
```

- **有效期**：签名默认 2 小时（`SIGN_TTL_MINUTES`），过期后分片/密钥请求 401，页面刷新会重新签发；地址被复制到站外也很快失效。
- **加密**：HLS 用 AES-128（`#EXT-X-KEY`），密钥文件只存在存储后端，由后端按签名地址下发，播放器（hls.js / Safari）自动解密。
- **没有 ffmpeg 时**：自动退化为「源文件 + HTTP Range」渐进播放，功能不受影响（只有加密/切片没有）。
- **大文件**：默认直传适合 ≤1GB；更大的文件在弹窗里选「分片上传」，单片失败可重传，服务端按片落盘再合并。
- **磁盘/带宽**：源文件与切片都会进存储后端；接 S3 / WebDAV 后本机只留数据库，省空间。
- 文档课时最大 200MB，聊天图片 ≤10MB。

## 📡 API 一览（供二次开发）

除公开接口外均通过 HttpOnly Cookie 鉴权（同源 `fetch` 自动携带）；非 GET 请求需同源 Origin。列表接口空结果统一返回 `[]`。

```
POST   /api/register                  {username,password,display_name,invite_code?} → 注册学员（受注册模式控制）
POST   /api/login                     {username,password} → 用户信息（Set-Cookie 会话）
POST   /api/logout                    退出登录
GET    /api/me                        当前用户
GET    /api/config                    公开配置 {site_name,register_mode,allow_register,need_invite}

GET    /api/courses                   我的课程（老师=自己的课，学员=已加入的课）
POST   /api/courses                   {title,description,category?} 老师/管理员建课
GET    /api/courses/:id               课程详情（学员可预览已上架课程目录）
PUT    /api/courses/:id               {title,description,category} 修改课程
DELETE /api/courses/:id               删除课程及其课时
POST   /api/courses/:id/publish       {published:true|false} 上架/下架市场
POST   /api/courses/:id/invite        重新生成课程邀请码
POST   /api/courses/:id/cover         multipart 上传封面
GET    /api/courses/:id/cover         封面图片
GET    /api/courses/:id/students      已加入学员（老师/管理员）
POST   /api/courses/:id/teacher       {teacher_id} 指定任课老师（管理员）

GET    /api/market                    课程市场（已上架课程）
POST   /api/market/:id/enroll         自助加入市场课程
POST   /api/join                      {code} 凭邀请码加入课程

GET    /api/courses/:id/lessons       课时列表（目录）
POST   /api/courses/:id/lessons       {title,chapter?,kind:video|rich|doc,content?} 新建课时
GET    /api/lessons/:id               课时详情（视频/图文/文档）
PUT    /api/lessons/:id               修改课时（标题/章节/图文内容）
DELETE /api/lessons/:id               删除课时
POST   /api/lessons/:id/complete      {size,duration} 兼容旧接口
POST   /api/lessons/:id/video?name=x.mp4  一次性直传源视频（body=文件字节流）
POST   /api/lessons/:id/upload/init     {file_name,size} → {upload_id,chunk_size,chunks}
PUT    /api/lessons/:id/upload/:uid/:i  上传第 i 片（16MB，可重传）
GET    /api/lessons/:id/upload/:uid     已收到的分片列表（断点续传）
POST   /api/lessons/:id/upload/:uid/done  合并分片 → 开始转码
GET    /api/lessons/:id/video/status    转码状态 {status,duration,video_mode,video_url}
GET    /api/lessons/:id/stream?e=&s=    源文件播放（Range 渐进，签名或 Cookie 鉴权）
GET    /hls/:id/index.m3u8?e=&s=        加密 HLS 播放列表（下发时给分片/密钥追加签名）
POST   /api/lessons/:id/doc           multipart 上传文档（≤200MB）
GET    /api/lessons/:id/file          下载/预览文档
GET    /hls/:id/:file                 HLS 播放代理（m3u8 与分片同路径）
GET    /files/                       聊天图片 / 图文插图
POST   /api/upload/image             multipart 上传图片（≤10MB）
POST   /api/chat/upload              聊天图片上传（同上，语义化别名）

# 作业 / 练习 / 打卡
GET    /api/practices?course_id=       练习列表（学员=已加入课程的已发布练习，含我的提交状态；老师=自己课程；管理员=全部）
POST   /api/courses/:id/practices      {title,kind:homework|quiz|word|sentence,subject,deadline?,duration?,published?,questions:[{type,stem,options?,answer,score,analysis?}]} 老师布置作业/练习
GET    /api/practices/:id              练习详情 {practice,questions,submission}（交卷前不返回标准答案）
PUT    /api/practices/:id              修改练习/题目（已有学员提交后不能再改题）
DELETE /api/practices/:id              删除练习及其提交记录
POST   /api/practices/:id/submit       {answers:[{question_id,answer}]} 学员交卷：选择题/填空/判断自动判分，简答题待批改
GET    /api/practices/:id/submissions  提交列表（老师/管理员）
POST   /api/practices/:id/submissions/:sid/grade  {score,comment?} 老师批改出总分
GET    /api/checkins?month=YYYY-MM     我的打卡汇总（连续天数/本月/累计 + 当月明细）
POST   /api/checkins                   {course_id?,subject,kind:word|sentence|reading,count,minutes,content?} 打卡（同天同类覆盖）
GET    /api/courses/:id/checkins?month=  课程学员打卡统计（老师/管理员）

GET    /api/contacts                  可聊天联系人（含未读数/在线状态）
GET    /api/chat/:peer?before=id      私聊记录（默认最近 50 条）
POST   /api/chat/:peer/read           {last_id?} 标记私聊已读
GET    /api/groups                    我加入的课程群（含最后一条消息与未读数）
GET    /api/groups/:id/messages       群聊记录（默认最近 50 条）
POST   /api/groups/:id/read           {last_id?} 标记群聊已读
GET    /api/ice                       WebRTC ICE 服务器配置
GET    /ws                            WebSocket：私聊 + 群聊 + 通话信令

GET    /api/settings                  站点设置（管理员）
PUT    /api/settings                  {site_name,register_mode} 修改站点设置（管理员）
POST   /api/settings/storage/test     存储连接测试（管理员，后台「文件存储」用）
POST   /api/settings/invite           重新生成机构邀请码（管理员）
GET    /api/users                     用户列表（管理员）
POST   /api/users                     创建账号（管理员）
POST   /api/users/:id/status          启用/禁用账号（管理员）
POST   /api/users/:id/password        重置密码（管理员）
```

### WebSocket 消息协议

客户端 → 服务端：
`chat.send {to,msg:{type,content}}`、`chat.read {to}`、`group.send {group,msg:{type,content},ref?:{lesson_id,time}}`、`group.read {group}`、`call.offer {to,sdp,video}`、`call.answer {to,sdp,video}`、`call.ice {to,candidate}`、`call.end {to}`

服务端 → 客户端：
`presence {ids}`、`chat.new {from,msg}`、`chat.sent {msg}`、`group.new {group,gmsg}`、`group.sent {group,gmsg}`、`call.offer {from,sdp,video}`、`call.answer {from,sdp}`、`call.ice {from_id,candidate}`、`call.end {from_id}`、`call.busy {message}`、`error {message}`

## 🗂️ 项目结构

```
main.go        入口：配置、存储、数据库、静态资源嵌入、健康检查、优雅停机
config.go      环境变量（JWT_SECRET 缺省时自动生成并持久化）
session.go     Cookie 会话 + CSRF + 鉴权中间件
user.go        登录/注册（三种注册模式）/用户管理 + 登录限流
store.go       SQLite 模型与查询（用户/课程/课时/选课/市场/消息/群聊）
practice_store.go     作业/练习/打卡：表结构、模型、自动判分与查询
practice_handlers.go  作业/练习/打卡 REST 接口（出题、交卷、批改、打卡统计）
storage.go     存储抽象：local / S3 / WebDAV
handlers.go    课程、市场、课时、HLS/文档代理等 REST 路由
chat.go        WebSocket Hub：私聊 + 课程群聊（引用时间点）+ 通话信令
sanitize.go    图文课时 HTML 白名单清洗（防 XSS）
ws_test.go     端到端测试
practice_test.go      作业/练习/打卡：判分单测 + 端到端接口测试
web/           Vue 3 前端（Vite）：市场 / 我的课程 / 学习页 / 教学中心 / 练习中心 / 后台管理
  src/style.css    小鹅通风格设计系统
  src/components/  VideoPlayer(课件播放) / RichEditor(图文) / GroupChat(课程群) / Modal / Toast…
  src/views/       Login / Market / MyCourses / CourseDetail / Learn / Messages / PracticeCenter / PracticeDetail / Studio / StudioCourse / StudioPractice / Admin

Dockerfile             多阶段构建（前端 → 静态二进制 → alpine，约 50MB）
docker-compose.yml     一键部署（支持 --build 或拉取镜像仓库镜像）
.dockerignore          构建上下文裁剪
.github/workflows/     ci.yml（测试）与 docker.yml（多架构镜像推送 GHCR）
deploy/                systemd unit 与环境变量模板（不用 Docker 时）
docs/                  文档：部署指南（DEPLOYMENT.md）与二次开发指南（DEVELOPMENT.md），均有中英文版
scripts/               交叉编译 Linux 二进制的脚本（.ps1 / .sh）
```

## 🖥️ 界面速览

| 页面 | 说明 |
|---|---|
| 登录 / 注册 | 左侧品牌区 + 右侧表单，按注册模式显示邀请码输入框 |
| 发现课程（首页） | 站内课程市场：搜索、分类筛选、课程卡片，学员一键加入 |
| 我的课程 | 学员已加入的课程 + 邀请码加入入口 |
| 课程详情 | 封面、简介、目录预览、加入按钮；老师视角显示上架/邀请码管理 |
| 学习页 | 左侧视频 / 图文 / 文档内容，右侧「目录 / 讨论」切换，讨论区即课程群聊，可引用当前播放时间点 |
| 消息中心 | 私聊列表 + 课程群列表，未读角标，WebRTC 通话入口 |
| 教学中心 | 老师数据概览、新建课程、课程管理（上架/邀请码/删除） |
| 课程管理 | 内容管理（视频/图文/文档课时）、学员列表、课程设置（封面/简介/上架） |
| 后台管理 | 用户管理、注册设置（三档注册策略 + 机构邀请码）、课程管理 |
