#!/bin/sh
# 版本号计算：唯一版本源是 web/package.json 的 "version" 字段，
# 再叠加 git 提交信息（没有 git 时用构建时间），保证「每次构建都有唯一版本号」。
#
#   ./scripts/version.sh            # 展示版本（含构建元数据）：1.0.0+sha.6f179c0
#   ./scripts/version.sh tag        # 镜像标签（Docker tag 不允许 + 号）：1.0.0-sha-6f179c0
#   ./scripts/version.sh commit     # 短提交号：6f179c0（无 git 时为 unknown）
#   ./scripts/version.sh base       # 仅 package.json 里的版本：1.0.0
#
# 环境变量（可选）：
#   VERSION      指定展示版本（CI / Docker 构建传进来时优先）
#   VERSION_TAG  指定镜像标签（一般不用；默认由展示版本推导）
#
# 输出示例：
#   干净仓库：1.0.0+sha.6f179c0        → tag: 1.0.0-sha-6f179c0
#   有未提交改动：1.0.0+sha.6f179c0.dirty → tag: 1.0.0-sha-6f179c0-dirty
#   没有 .git（docker build 上下文）：1.0.0+build.20251022T130600 → tag: 1.0.0-build.20251022T130600
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
PKG="$ROOT/web/package.json"

# ---- 主版本号：读 package.json ----
base=""
if [ -f "$PKG" ]; then
  base=$(sed -n 's/^[[:space:]]*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$PKG" | head -n 1)
fi
[ -n "$base" ] || base="0.0.0"

# ---- git 提交号（docker build 上下文里没有 .git，会走时间戳兜底） ----
sha=""
dirty=""
if command -v git >/dev/null 2>&1; then
  sha=$(git -C "$ROOT" rev-parse --short=7 HEAD 2>/dev/null || true)
  if [ -n "$sha" ]; then
    if [ -n "$(git -C "$ROOT" status --porcelain 2>/dev/null || true)" ]; then
      dirty="dirty"
    fi
  fi
fi

# ---- 完整版本号（展示用） ----
if [ -n "${VERSION:-}" ]; then
  full="$VERSION"
elif [ -n "$sha" ]; then
  full="$base+sha.$sha"
  [ -z "$dirty" ] || full="$full.$dirty"
else
  full="$base+build.$(date -u +%Y%m%dT%H%M%S)"
fi

mode="${1:-full}"
case "$mode" in
  base)
    printf '%s\n' "$base"
    ;;
  commit)
    printf '%s\n' "${sha:-unknown}"
    ;;
  full)
    printf '%s\n' "$full"
    ;;
  tag)
    if [ -n "${VERSION_TAG:-}" ]; then
      printf '%s\n' "$VERSION_TAG"
    else
      # 展示版本 → 镜像标签：+ 等 Docker 不允许的字符换成 -，其余保持一致
      printf '%s\n' "$full" | sed 's/[^A-Za-z0-9_.-]/-/g; s/-\{2,\}/-/g; s/^-*//; s/-*$//'
    fi
    ;;
  *)
    printf '未知参数: %s（可用：full | tag | commit | base）\n' "$mode" >&2
    exit 1
    ;;
esac
