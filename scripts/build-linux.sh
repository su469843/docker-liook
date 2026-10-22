#!/usr/bin/env bash
# 在 Linux / macOS 上交叉编译 Linux 可执行文件（用于 systemd 直接部署，不需要 Docker）
#
#   ./scripts/build-linux.sh            # 输出到 dist/（amd64 + arm64）
#   ./scripts/build-linux.sh amd64      # 只编 amd64
#
# 版本号自动读取 web/package.json 并叠加 git 提交（scripts/version.sh），
# 通过 -ldflags 注入二进制：启动日志、「登录页底部 / 用户菜单」、GET /healthz 都能看到。
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p dist

CGO_ENABLED=0 GOOS=linux
export CGO_ENABLED GOOS

VERSION=$(sh scripts/version.sh full)
COMMIT=$(sh scripts/version.sh commit)
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}"

archs=("${1:-both}")
if [ "${archs[0]}" = "both" ]; then archs=(amd64 arm64); fi

for arch in "${archs[@]}"; do
  echo "==> 编译 $arch"
  GOARCH="$arch" go build -trimpath -ldflags="$LDFLAGS" -o "dist/course-hub-linux-$arch" .
  ls -lh "dist/course-hub-linux-$arch"
done

echo
echo "版本：$VERSION（提交 $COMMIT，构建于 $BUILD_TIME）"
echo "完成。上传到服务器后："
echo "  chmod +x course-hub-linux-amd64 && ./course-hub-linux-amd64"
