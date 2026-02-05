#!/usr/bin/env bash
#
# 本地构建脚本 - 构建未签名、未公证的 macOS .app
# 仅用于个人使用，不适合分发
#
# 用法:
#   ./scripts/build-local.sh           # 完整构建（含 typecheck）
#   ./scripts/build-local.sh --fast    # 跳过 typecheck，加速构建
#   ./scripts/build-local.sh --open    # 构建完成后自动打开输出目录
#   ./scripts/build-local.sh --fast --open
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

SKIP_TYPECHECK=false
OPEN_DIST=false

for arg in "$@"; do
  case "$arg" in
    --fast) SKIP_TYPECHECK=true ;;
    --open) OPEN_DIST=true ;;
    *)
      echo "未知参数: $arg"
      echo "用法: $0 [--fast] [--open]"
      exit 1
      ;;
  esac
done

cd "$PROJECT_DIR"

# 0. 清空 dist 目录
if [ -d "$PROJECT_DIR/dist" ]; then
  echo "==> 清空 dist 目录..."
  rm -rf "$PROJECT_DIR/dist"
fi

# 1. TypeCheck（可选）
if [ "$SKIP_TYPECHECK" = false ]; then
  echo "==> 运行 TypeScript 类型检查..."
  pnpm typecheck
else
  echo "==> 跳过类型检查 (--fast)"
fi

# 2. Vite 构建
echo "==> 构建 Electron + Vite 项目..."
npx electron-vite build

# 3. 打包 .app（跳过签名和公证）
echo "==> 打包 macOS .app（跳过签名和公证）..."
CSC_IDENTITY_AUTO_DISCOVERY=false \
npx electron-builder --mac \
  --config electron-builder.yml \
  -c.mac.identity=null \
  -c.mac.notarize=false

DIST_DIR="$PROJECT_DIR/dist"

echo ""
echo "============================================"
echo "  构建完成！"
echo "============================================"
echo ""
echo "输出目录: $DIST_DIR"
echo ""

# 列出 .dmg 文件
if ls "$DIST_DIR"/*.dmg 1>/dev/null 2>&1; then
  echo "DMG 文件:"
  ls -lh "$DIST_DIR"/*.dmg
  echo ""
fi

# 列出 .app 文件
echo ".app 位置:"
find "$DIST_DIR" -name "*.app" -maxdepth 3 2>/dev/null | while read -r app; do
  echo "  $app"
done
echo ""
echo "提示: 首次打开未签名的 app 时，右键点击 -> 打开，即可绕过 Gatekeeper。"
echo "      或者运行: xattr -cr <app路径>"

# 自动打开输出目录
if [ "$OPEN_DIST" = true ]; then
  echo ""
  echo "==> 打开输出目录..."
  open "$DIST_DIR"
fi
