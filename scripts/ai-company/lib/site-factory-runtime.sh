#!/usr/bin/env bash
# Multica self-host runtime checks for site-factory dispatch (运行服务提供的并发/健康).
# shellcheck shell=bash

site_factory_multica_api() {
  local config="${MULTICA_CONFIG:-$HOME/.multica/config.json}"
  if [ ! -f "$config" ]; then
    echo ""
    return 0
  fi
  python3 - "$config" <<'PY'
import json, sys
cfg = json.load(open(sys.argv[1]))
print((cfg.get("server_url") or "").rstrip("/"))
PY
}

site_factory_runtime_ready() {
  local api="${1:-$(site_factory_multica_api)}"
  api="${api:-${MULTICA_SERVER_URL:-http://localhost:8081}}"
  if curl -fsS --max-time 5 "${api}/readyz" >/dev/null 2>&1; then
    return 0
  fi
  return 1
}

site_factory_daemon_running() {
  multica daemon status 2>/dev/null | grep -qi '^Daemon:[[:space:]]*running'
}

site_factory_runtime_human() {
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  bash "$script_dir/multica-runtime-status.sh" --human 2>/dev/null || true
}

# Print one integer: safe local dispatch slots (respects daemon + CLI load).
site_factory_dispatch_slots() {
  local want="${1:-2}"
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  python3 - "$want" "$script_dir" <<'PY'
import json, subprocess, sys

want = int(sys.argv[1])
script_dir = sys.argv[2]
try:
    out = subprocess.check_output(
        ["bash", f"{script_dir}/multica-runtime-status.sh", "--json"],
        text=True,
        timeout=60,
    )
    data = json.loads(out)
except Exception:
    print(min(want, 1))
    sys.exit(0)

daemon_max = int((data.get("daemon") or {}).get("max_concurrent_tasks") or 20)
working = int(data.get("working_agents_total_running") or 0)
cli = data.get("local_cursor_cli") or {}
portfolio = int(cli.get("portfolio") or 0)
feishu = int(cli.get("feishu_claw") or 0)
other = int(cli.get("other") or 0)

# Reserve headroom for feishu claw + one interactive session
headroom = feishu + min(other, 1)
daemon_free = max(0, daemon_max - working)
cli_free = max(0, 3 - portfolio - headroom)
slots = min(want, daemon_free, cli_free)
if slots <= 0 and want > 0:
    slots = 1 if portfolio == 0 else 0
print(slots)
PY
}

site_factory_require_runtime() {
  local api
  api="$(site_factory_multica_api)"
  api="${api:-${MULTICA_SERVER_URL:-http://localhost:8081}}"
  if site_factory_runtime_ready "$api"; then
    echo "runtime: Multica API ready ($api)"
  else
    echo "warn: Multica self-host not ready ($api) — dispatch uses local cursor-agent only" >&2
  fi
  if site_factory_daemon_running; then
    echo "runtime: multica daemon running"
  else
    echo "warn: multica daemon not running — start with: multica daemon start" >&2
  fi
}
