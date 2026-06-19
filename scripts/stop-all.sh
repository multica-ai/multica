#!/usr/bin/env bash
#
# stop-all.sh — 停止 Multica 全套服务（前端 + 守护进程 + 后端）
#
# Usage:
#   bash scripts/stop-all.sh
#

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${PORT:-8081}"
FRONTEND_PORT="${FRONTEND_PORT:-3000}"
PID_FILE="$ROOT_DIR/.multica-pids"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${CYAN}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
err()   { echo -e "${RED}[ERR]${NC}   $*"; }

# resolve_multica  — 查找 multica CLI：优先用已安装的二进制，否则用 go run
resolve_multica() {
  if command -v multica >/dev/null 2>&1; then
    MULTICA_CMD=(multica)
    return 0
  fi

  if [ -f "$ROOT_DIR/server/cmd/multica/main.go" ]; then
    MULTICA_CMD=(go run "$ROOT_DIR/server/cmd/multica")
    return 0
  fi

  warn "multica CLI 和 server/cmd/multica 均不可用；跳过守护进程操作"
  return 1
}

# kill_pid <pid> <label>  — 优雅终止进程，失败则强制 kill
kill_pid() {
  local pid="$1"
  local label="$2"
  if [ -z "$pid" ] || [ "$pid" -le 0 ] 2>/dev/null; then
    return 1
  fi
  # 检查进程是否存在
  if ! tasklist 2>/dev/null | grep -q "$pid"; then
    return 1
  fi
  info "终止 $label (PID $pid)..."
  # 先尝试 taskkill（不带 /f），再强制
  taskkill -pid "$pid" >/dev/null 2>&1 || taskkill -f -pid "$pid" >/dev/null 2>&1 || true
  sleep 1
  ok "$label 已停止"
  return 0
}

kill_port() {
  local port="$1"
  local pid
  pid=$(netstat -ano 2>/dev/null | grep "LISTENING" | grep ":$port " | awk '{print $5}' | head -1)
  if [ -n "$pid" ] && [ "$pid" -gt 0 ] 2>/dev/null; then
    kill_pid "$pid" "端口 $port" || {
      warn "端口 $port 强制释放中..."
      taskkill -f -pid "$pid" >/dev/null 2>&1 || true
      sleep 1
    }
  else
    info "端口 $port 无占用"
  fi
}

echo ""
info "========================================"
info "  Multica 一键停止脚本"
info "========================================"
echo ""

# ── 优先通过 PID 文件精确停止 ─────────────────────────

if [ -f "$PID_FILE" ]; then
  info "从 PID 文件读取进程信息..."

  # 读取并停止各进程
  while IFS='=' read -r name pid; do
    case "$name" in
      backend)  kill_pid "$pid" "后端服务" || true ;;
      frontend) kill_pid "$pid" "前端服务" || true ;;
      *)        warn "未知进程: $name (PID $pid)" ;;
    esac
  done < "$PID_FILE"

  rm -f "$PID_FILE"
  ok "PID 文件已清理"
  echo ""
fi

# ── 兜底：通过端口强制释放（防止 PID 文件不存在或残留） ──

info "检查端口占用..."

# 停止守护进程
if resolve_multica && "${MULTICA_CMD[@]}" daemon status >/dev/null 2>&1; then
  info "停止守护进程..."
  "${MULTICA_CMD[@]}" daemon stop >/dev/null 2>&1 || true
  ok "守护进程已停止"
else
  info "守护进程未运行"
fi

# 停止后端
kill_port "$PORT"

# 停止前端
kill_port "$FRONTEND_PORT"

echo ""
ok "所有服务已停止"
echo ""
