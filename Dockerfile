# syntax=docker/dockerfile:1
#
# CourseHub 一体镜像：前端静态资源 → 嵌入 Go 二进制 → 极简运行镜像
# 构建：docker build -t course-hub .
# 多架构：docker buildx build --platform linux/amd64,linux/arm64 -t course-hub .
# 体积：约 50MB（alpine + 静态二进制，无 CGO、无运行时依赖）

# ---------- 1. 前端构建 ----------
# 前端产物与 CPU 架构无关，固定用构建机架构跑，arm64 构建时不用 QEMU 模拟，快很多
FROM --platform=$BUILDPLATFORM node:20-alpine AS web
ARG PNPM_VERSION=10.28.2
WORKDIR /app
RUN npm i -g pnpm@${PNPM_VERSION}
# 先只拷贝依赖清单，充分利用层缓存
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# ---------- 2. 后端编译（纯 Go，CGO_ENABLED=0 → 静态二进制） ----------
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
# 国内网络可换成 https://goproxy.cn,https://proxy.golang.org,direct
ARG GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct
ARG TARGETARCH
WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /app/dist ./web/dist
# TARGETARCH 由 BuildKit 注入（amd64 / arm64），未指定时用构建机架构
RUN GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags="-s -w" -o /out/course-hub .

# ---------- 3. 运行镜像 ----------
FROM alpine:3.20

ARG VERSION=dev
LABEL org.opencontainers.image.title="CourseHub" \
      org.opencontainers.image.description="自托管课程视频平台（浏览器切片 HLS、多角色、站内聊天与音视频通话）" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="MIT"

# ca-certificates: 访问 S3/WebDAV 时校验 HTTPS 证书；tzdata: 日志时间用东八区
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -g 10001 -S app \
 && adduser -u 10001 -S -D -H -G app app \
 && mkdir -p /data \
 && chown -R app:app /data

COPY --from=build /out/course-hub /usr/local/bin/course-hub

ENV PORT=8080 \
    STORAGE_TYPE=local \
    STORAGE_LOCAL_DIR=/data \
    TZ=Asia/Shanghai

WORKDIR /app
USER app
VOLUME ["/data"]
EXPOSE 8080

# /healthz 为公开接口，不需要登录态
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null "http://127.0.0.1:${PORT}/healthz" || exit 1

STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/course-hub"]
