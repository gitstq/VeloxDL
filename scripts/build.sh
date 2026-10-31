#!/usr/bin/env bash
#
# VeloxDL 一键跨平台构建脚本
#
# 用法:
#   ./scripts/build.sh              # 构建全部目标平台
#   ./scripts/build.sh linux/amd64  # 只构建指定平台
#
# 产物统一输出到 dist/ 目录，命名格式为 veloxdl_<os>_<arch>[.exe]
#
set -euo pipefail

# ---------- 基本信息 ----------
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

APP_NAME="${APP_NAME:-veloxdl}"
DIST_DIR="${DIST_DIR:-dist}"
PKG_PATH="${PKG_PATH:-./cmd/veloxdl}"
VERSION="${VERSION:-$(grep -oE 'Version[[:space:]]*=[[:space:]]*\"[^\"]+\"' pkg/veloxdl/api.go | sed -E 's/.*\"(.*)\".*/\1/' || echo dev)}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

LDFLAGS="-s -w \
  -X main.version=${VERSION} \
  -X main.commit=${COMMIT} \
  -X main.buildTime=${BUILD_TIME}"

# ---------- 目标平台矩阵 ----------
# 格式: "os/arch"
ALL_TARGETS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
  "windows/arm64"
)

if [ "$#" -ge 2 ]; then
  TARGETS=("$1/$2")
elif [ "$#" -eq 1 ]; then
  TARGETS=("$1")
else
  TARGETS=("${ALL_TARGETS[@]}")
fi

echo "🔨 构建 VeloxDL ${VERSION} (commit ${COMMIT})"
echo "📦 输出目录: ${DIST_DIR}"
echo

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

build_one() {
  local goos="$1"
  local goarch="$2"
  local out="${DIST_DIR}/${APP_NAME}_${goos}_${goarch}"

  if [ "$goos" = "windows" ]; then
    out="${out}.exe"
  fi

  echo "   ⚙️  ${goos}/${goarch}"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -trimpath -ldflags "$LDFLAGS" -o "$out" "$PKG_PATH"
}

for target in "${TARGETS[@]}"; do
  goos="${target%%/*}"
  goarch="${target##*/}"
  build_one "$goos" "$goarch"
done

# ---------- 生成校验和清单 ----------
echo
echo "🔐 生成 SHA256 校验和清单..."
(
  cd "$DIST_DIR"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum ./* >SHA256SUMS
  else
    # macOS 等没有 sha256sum 的环境
    for f in *; do
      [ -f "$f" ] || continue
      shasum -a 256 "$f" >>SHA256SUMS
    done
  fi
)

echo
echo "✅ 构建完成，产物列表:"
ls -lh "$DIST_DIR"
