#!/usr/bin/env bash
#
# start-all.sh — 一键启动 Multica 全套服务（后端 + 守护进程 + 前端），后台运行
#
# Usage:
#   bash scripts/start-all.sh
#
# 环境变量（均为可选）：
#   PORT            后端端口，默认 8081
#   FRONTEND_PORT   前端端口，默认 3000
#

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${PORT:-8081}"
FRONTEND_PORT="${FRONTEND_PORT:-3000}"
PID_FILE="$ROOT_DIR/.multica-pids"

# ── 颜色 ──────────────────────────────────────────────
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${CYAN}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
err()   { echo -e "${RED}[ERR]${NC}   $*"; }

# ── 辅助函数 ──────────────────────────────────────────

# resolve_multica  — 查找 multica CLI：优先用已安装的二进制，否则用 go run
# 输出为 MULTICA_CMD 变量（数组），调用时用 "${MULTICA_CMD[@]}" args...
resolve_multica() {
  if command -v multica >/dev/null 2>&1; then
    MULTICA_CMD=(multica)
    return 0
  fi

  # Fallback: use go run from the source tree (mirrors `make multica`)
  if [ -f "$ROOT_DIR/server/cmd/multica/main.go" ]; then
    MULTICA_CMD=(go run "$ROOT_DIR/server/cmd/multica")
    return 0
  fi

  warn "multica CLI 和 server/cmd/multica 均不可用；跳过守护进程操作"
  return 1
}

# kill_port <port>  — 强制杀掉占用指定端口的进程
kill_port() {
  local port="$1"
  local pid
  # 在 Git Bash 中用 grep 提取 PID（netstat 第 5 列）
  pid=$(netstat -ano 2>/dev/null | grep "LISTENING" | grep ":$port " | awk '{print $5}' | head -1)
  if [ -n "$pid" ] && [ "$pid" -gt 0 ] 2>/dev/null; then
    warn "端口 $port 被 PID $pid 占用，正在释放..."
    taskkill -f -pid "$pid" >/dev/null 2>&1 || true
    sleep 2
  fi
}

# wait_health <port> <timeout_seconds>  — 等待 /health 返回成功
wait_health() {
  local port="$1"
  local timeout="$2"
  local elapsed=0
  while [ $elapsed -lt "$timeout" ]; do
    if curl -sf "http://localhost:$port/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  return 1
}

# save_pid <name> <pid>  — 将 PID 写入 PID 文件
save_pid() {
  local name="$1"
  local pid="$2"
  echo "$name=$pid" >> "$PID_FILE"
}

# ── 主流程 ────────────────────────────────────────────

echo ""
info "========================================"
info "  Multica 一键启动脚本（后台模式）"
info "========================================"
echo ""

# 0. 清理 PID 文件
rm -f "$PID_FILE"
touch "$PID_FILE"

# 1. 清理旧进程
info "清理旧进程..."

kill_port "$PORT"

if resolve_multica && "${MULTICA_CMD[@]}" daemon status >/dev/null 2>&1; then
  warn "检测到运行中的守护进程，正在停止..."
  "${MULTICA_CMD[@]}" daemon stop >/dev/null 2>&1 || true
  sleep 2
fi

kill_port "$FRONTEND_PORT"
ok "旧进程已清理"
echo ""

# 2. 启动 Go 后端（后台运行，输出重定向到日志）
info "启动后端服务 (端口 $PORT)..."
BACKEND_LOG="$ROOT_DIR/.multica-backend.log"
cd "$ROOT_DIR/server"
nohup go run ./cmd/server > "$BACKEND_LOG" 2>&1 &
BACKEND_PID=$!
save_pid "backend" "$BACKEND_PID"
cd "$ROOT_DIR"

info "等待后端编译启动（约 10-15 秒）..."
if wait_health "$PORT" 45; then
  ok "后端服务就绪 — http://localhost:$PORT/health"
else
  err "后端服务启动超时，请检查日志: $BACKEND_LOG"
  exit 1
fi
echo ""

# 3. 启动守护进程
info "启动 Multica 守护进程..."
if resolve_multica; then
  DAEMON_OUTPUT=$("${MULTICA_CMD[@]}" daemon start 2>&1) && ok "守护进程已启动" || warn "守护进程启动失败（$DAEMON_OUTPUT）"
fi
echo ""

# 4. 启动前端（后台运行，输出重定向到日志）
info "启动前端 (端口 $FRONTEND_PORT)..."
FRONTEND_LOG="$ROOT_DIR/.multica-frontend.log"
nohup pnpm dev:web > "$FRONTEND_LOG" 2>&1 &
FRONTEND_PID=$!
save_pid "frontend" "$FRONTEND_PID"

info "等待前端编译（约 10-20 秒）..."
FRONTEND_READY=false
for i in $(seq 1 20); do
  sleep 1
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:$FRONTEND_PORT" 2>/dev/null || true)
  # Next.js 根路由返回 200（有页面）或 404（无根路由，通过中间件跳转），都算启动成功
  if [ "$STATUS" = "200" ] || [ "$STATUS" = "404" ]; then
    FRONTEND_READY=true
    break
  fi
done

if [ "$FRONTEND_READY" = true ]; then
  ok "前端就绪 — http://localhost:$FRONTEND_PORT"
else
  warn "前端尚未就绪，可能仍在编译（后台继续运行中，查看日志: $FRONTEND_LOG）"
fi

# 收集守护进程状态（如果可用）
DAEMON_STATUS="（守护进程未启动）"
if resolve_multica; then
  DAEMON_STATUS=$("${MULTICA_CMD[@]}" daemon status 2>/dev/null | head -1)
fi

echo ""
info "========================================"
ok "  全套服务已启动（后台运行）！"
info ""
info "  后端 API:    http://localhost:$PORT"
info "  前端界面:    http://localhost:$FRONTEND_PORT"
info "  守护进程:    $DAEMON_STATUS"
info ""
info "  后端日志:    $BACKEND_LOG"
info "  前端日志:    $FRONTEND_LOG"
info "  PID 文件:    $PID_FILE"
info ""
info "  停止服务:    bash scripts/stop-all.sh"
info "========================================"
echo ""

# 不 wait — 脚本退出，服务在后台继续运行
