#!/usr/bin/env bash
# 本地构建带版本号的 Docker 镜像（推荐用它代替裸 `docker build`）：
# 版本号由 scripts/version.sh 从 web/package.json + git 提交自动生成。
#
#   ./scripts/docker-build.sh                 # 构建 <版本> 和 latest 两个标签
#   ./scripts/docker-build.sh --no-latest     # 只打 <版本> 标签
#   IMAGE_NAME=ghcr.io/你的用户名/course-hub ./scripts/docker-build.sh
set -euo pipefail

cd "$(dirname "$0")/.."

name="${IMAGE_NAME:-course-hub}"
with_latest=1
for arg in "$@"; do
  case "$arg" in
    --no-latest) with_latest=0 ;;
    -h|--help) sed -n '2,10p' "$0"; exit 0 ;;
    *) echo "未知参数: $arg（可用：--no-latest）" >&2; exit 1 ;;
  esac
done

full=$(sh scripts/version.sh full)
tag=$(sh scripts/version.sh tag)
commit=$(sh scripts/version.sh commit)
build_time=$(date -u +%Y-%m-%dT%H:%M:%SZ)

tags=(-t "$name:$tag")
if [ "$with_latest" = 1 ]; then
  tags+=(-t "$name:latest")
fi

echo "==> 构建 $name:$tag（版本 $full，提交 $commit）"
docker build \
  --build-arg "VERSION=$full" \
  --build-arg "COMMIT=$commit" \
  --build-arg "BUILD_DATE=$build_time" \
  "${tags[@]}" \
  .

echo
echo "完成，镜像标签："
printf '  %s:%s\n' "$name" "$tag"
if [ "$with_latest" = 1 ]; then
  printf '  %s:latest\n' "$name"
fi
echo "部署到服务器：docker save $name:$tag | gzip | ssh 服务器 'docker load'，或推到镜像仓库后 docker compose pull"
