#!/usr/bin/env bash
# 在 Linux / macOS 上交叉编译 Linux 可执行文件（用于 systemd 直接部署，不需要 Docker）
#
#   ./scripts/build-linux.sh            # 输出到 dist/（amd64 + arm64）
#   ./scripts/build-linux.sh amd64      # 只编 amd64
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p dist

CGO_ENABLED=0 GOOS=linux
export CGO_ENABLED GOOS

archs=("${1:-both}")
if [ "${archs[0]}" = "both" ]; then archs=(amd64 arm64); fi

for arch in "${archs[@]}"; do
  echo "==> 编译 $arch"
  GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "dist/course-hub-linux-$arch" .
  ls -lh "dist/course-hub-linux-$arch"
done

echo
echo "完成。上传到服务器后："
echo "  chmod +x course-hub-linux-amd64 && ./course-hub-linux-amd64"
