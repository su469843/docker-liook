# 安全说明 / Security Policy

本项目是**自托管**的课程平台，代码、API 文档、Dockerfile 全部公开。**公开文档不等于可被攻击**：所有接口都要求已登录会话或带有效期的签名，授权按角色和课程校验，攻击面取决于部署配置与运维，而不是「接口有没有被写进 README」。

## 威胁模型

| 攻击者 | 能做什么 | 我们怎么防 |
| --- | --- | --- |
| 未登录的互联网用户 | 访问登录页、注册页、`/healthz` | 除公开接口外全部 401；媒体地址需签名或登录；登录/注册限流 |
| 已登录的学员 | 看别人的课程、下载未报名课件、刷消息、盗链视频 | 课程访问控制（`canAccessCourse`）；媒体走签名 URL + 课程权限；聊天/信令限流 |
| 已登录的老师 | 管理别人的课程 | `canManageCourse` 校验课程归属；创建老师账号只能由管理员 |
| 拿到播放地址的外部人员 | 转存/盗链视频 | HLS AES-128 加密 + 分片/密钥同签名；签名 2 小时过期；分片 URL 不可跨课时复用 |
| 拿到管理员密码的人 | 后台全部权限 | 只能靠强密码 + 不暴露后台入口；建议放内网或反代加一层鉴权 |
| 恶意脚本（XSS） | 窃取会话 | 会话在 HttpOnly Cookie 里 JS 读不到；富文本白名单过滤；CSP 限制脚本来源 |

## 已实现的安全措施

- **认证**：HttpOnly + SameSite=Lax Cookie（HMAC 签名，默认 7 天），`COOKIE_SECURE=true` 时仅 HTTPS 传输；登出清 Cookie。
- **授权**：三角色 RBAC；课程级校验（老师只能管自己的课，学员只能看已加入或已上架课程）；群聊/私聊关系校验。
- **CSRF**：所有非 GET 请求校验 `Origin` 同源（含登录/注册），跨站请求直接 403。
- **限流**：登录（IP + 用户名，15 分钟 10 次）、按 IP 的登录频率、注册（IP 10 分钟 10 个）、WS 聊天（2 条/秒，突发 15）、通话信令（2/秒）与 ICE（15/秒）。
- **枚举防护**：登录失败统一提示；用户不存在时也跑一次 bcrypt 对齐响应时间。
- **媒体保护**：HLS 分片 AES-128 加密；播放地址 HMAC 签名 + 有效期（`SIGN_TTL_MINUTES`）；m3u8 下发时给分片/密钥重签；`/hls`、`/stream` 先鉴权再查库，避免探测课时是否存在。
- **文件**：上传按类型/大小限制；存储 key 与文件名走白名单（禁止路径穿越）；文档下载可签名；S3/WebDAV 只由服务端代理，**不要给 bucket 开公共读**。
- **注入**：SQL 全部参数化；富文本走 `x/net/html` 白名单清洗（去掉脚本、事件属性、`javascript:`）；输出在 Vue 中默认转义。
- **请求限制**：JSON 请求体上限 1MB；上传有独立上限（视频 `MAX_UPLOAD_MB`、文档 200MB、图片 10MB）；WS 单条消息 1MB。
- **响应头**：`X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy`、`Permissions-Policy`（仅允许本站摄像头/麦克风）、`Cross-Origin-Opener-Policy`、`Content-Security-Policy`（`script-src 'self'`、禁止被内嵌）。
- **密钥**：后台接口返回 S3/WebDAV 密钥时打码（`********`），不改就不会被覆盖；TURN 支持 coturn `use-auth-secret` 下发 2 小时临时账号，避免 TURN 被白嫖。
- **最小镜像**：Alpine + 静态二进制，非 root 用户（uid 10001），无 CGO。

## 部署安全清单（务必逐条确认）

- [ ] 改掉 `ADMIN_PASSWORD`，用强密码；`JWT_SECRET` 用 32 位以上随机串并保密。
- [ ] 公网必须上 HTTPS（反代 Caddy/Nginx），设置 `COOKIE_SECURE=true`。
- [ ] 反代透传 `/ws`、`/hls`、`/files`，并限制 `client_max_body_size`（要 ≥ 你允许的上传大小）。
- [ ] **不要把 S3 / MinIO 的 bucket 设为公共读**，否则可绕过签名直接下载切片。
- [ ] 配 TURN 时用 `TURN_SECRET`（`use-auth-secret`），不要用长期固定的账号密码。
- [ ] `./data` 目录权限属于 10001，且只给运维账号访问；定期备份（含对象存储桶）。
- [ ] 内网/家庭部署也建议给后台加一层访问限制（IP 白名单或 Basic Auth）。
- [ ] 关注 Release 与依赖更新（Go/Vue/ffmpeg）；升级前先备份 `data`。

## 已知限制

- 单实例、进程内限流与转码队列：**重启后限流计数清零**，转码任务不会续跑（会被标记为失败）。
- 没有设备数限制/水印/DRM：签名地址在有效期内可以被分享，只是下一个人同样需要能访问到该地址，且 2 小时后失效。
- 没有内置 2FA、审计日志、IP 封禁；需要的话放在反代/网关层做。
- 静态口令登录（无 SSO/LDAP/微信登录）。

## 漏洞报告

请**不要**公开提交 issue。可以：
1. 用 GitHub 的 [Security advisory](https://github.com/su469843/docker-liook/security/advisories/new) 私密报告；
2. 或发邮件到仓库所有者的公开邮箱（见 GitHub 个人主页），标题注明 `[SECURITY]`。

我会在确认后尽快修复并在 Release 里说明。请附上复现步骤、影响范围与版本（`docker image inspect` 的镜像标签或 commit SHA）。
