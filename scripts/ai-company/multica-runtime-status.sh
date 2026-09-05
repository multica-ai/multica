#!/usr/bin/env bash
# Snapshot Multica agent concurrency + local cursor-agent processes (portfolio / daemon / feishu).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"

JSON=0
HUMAN=0

usage() {
  cat <<'EOF'
Usage: multica-runtime-status.sh [--json | --human]

Reports:
  - Multica daemon status and machine-wide max concurrent tasks
  - Per-agent max_concurrent_tasks and running task counts (working-agents API)
  - Local cursor-agent process breakdown (portfolio dispatch / multica daemon / feishu-claw)
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --json) JSON=1; shift ;;
    --human) HUMAN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

if [ "$JSON" -eq 0 ] && [ "$HUMAN" -eq 0 ]; then
  HUMAN=1
fi

CONFIG="${MULTICA_CONFIG:-$HOME/.multica/config.json}"
API=""
TOKEN=""
WSID=""
if [ -f "$CONFIG" ]; then
  API="$(python3 - "$CONFIG" <<'PY'
import json, sys
print(json.load(open(sys.argv[1])).get("server_url", "").rstrip("/"))
PY
)"
  TOKEN="$(python3 - "$CONFIG" <<'PY'
import json, sys
print(json.load(open(sys.argv[1])).get("token", ""))
PY
)"
  WSID="$(python3 - "$CONFIG" <<'PY'
import json, sys
print(json.load(open(sys.argv[1])).get("workspace_id", ""))
PY
)"
fi

DAEMON_MAX="$(multica config get max_concurrent_tasks 2>/dev/null | sed -n 's/^max_concurrent_tasks:[[:space:]]*//p' | tr -d '[:space:]')"
if [ -z "$DAEMON_MAX" ] || [ "$DAEMON_MAX" = "(notset)" ] || [ "$DAEMON_MAX" = "(not set)" ]; then
  DAEMON_MAX="20"
fi

DAEMON_LINE="$(multica daemon status 2>/dev/null | sed -n '1p' || true)"
DAEMON_AGENTS="$(multica daemon status 2>/dev/null | sed -n '/^Agents:/p' | sed 's/^Agents:[[:space:]]*//' || true)"

if [ "$JSON" -eq 1 ]; then
  export MULTICA_RUNTIME_JSON=1
fi
export API TOKEN WSID DAEMON_MAX DAEMON_LINE DAEMON_AGENTS
python3 <<'PY'
import json
import os
import re
import subprocess
import sys
import urllib.request

api = os.environ.get("API", "")
token = os.environ.get("TOKEN", "")
wsid = os.environ.get("WSID", "")
daemon_max = int(os.environ.get("DAEMON_MAX", "20") or "20")
daemon_line = os.environ.get("DAEMON_LINE", "")
daemon_agents = os.environ.get("DAEMON_AGENTS", "")

agents = []
working = []
api_ok = False

if api and token and wsid:
    headers = {
        "Authorization": f"Bearer {token}",
        "X-Workspace-ID": wsid,
    }
    try:
        req = urllib.request.Request(f"{api}/api/agents", headers=headers)
        with urllib.request.urlopen(req, timeout=15) as resp:
            agents = json.load(resp)
        req = urllib.request.Request(f"{api}/api/working-agents?type=issue", headers=headers)
        with urllib.request.urlopen(req, timeout=15) as resp:
            working = json.load(resp)
        api_ok = True
    except Exception as exc:
        api_error = str(exc)
    else:
        api_error = ""
else:
    api_error = "no ~/.multica/config.json or missing token/workspace"

working_by_id = {w["id"]: w for w in working if isinstance(w, dict)}

agent_rows = []
for a in agents if isinstance(agents, list) else []:
    aid = a.get("id", "")
    w = working_by_id.get(aid, {})
    agent_rows.append(
        {
            "id": aid,
            "name": a.get("name", ""),
            "max_concurrent_tasks": a.get("max_concurrent_tasks"),
            "runtime_id": a.get("runtime_id"),
            "running_task_count": w.get("running_task_count", 0),
            "issue_ids": w.get("issue_ids", []),
        }
    )

def count_procs(pattern: str) -> int:
    try:
        out = subprocess.check_output(["pgrep", "-fl", pattern], text=True)
    except subprocess.CalledProcessError:
        return 0
    return len([ln for ln in out.splitlines() if ln.strip()])


def classify_proc(line: str) -> str:
    if "feishu-cursor-workspace" in line or "feishu-cursor-claw" in line:
        return "feishu_claw"
    if "multica_workspaces" in line or "/agent-delivery/" in line:
        return "multica_daemon"
    if "dispatch-cursor-agent-cli" in line or "agent-delivery" in line:
        return "portfolio"
    if "landing-tool" in line or "saas-stripe" in line or "MusicSaas" in line:
        return "portfolio"
    return "other"


proc_lines = []
try:
    raw = subprocess.check_output(["ps", "aux"], text=True)
    for line in raw.splitlines():
        if "cursor-agent" in line or "/.local/bin/agent " in line:
            if "grep" in line:
                continue
            proc_lines.append(line)
except subprocess.CalledProcessError:
    pass

breakdown = {"portfolio": 0, "multica_daemon": 0, "feishu_claw": 0, "other": 0}
for line in proc_lines:
    breakdown[classify_proc(line)] += 1

out = {
    "api_ok": api_ok,
    "api_error": api_error,
    "daemon": {
        "status_line": daemon_line,
        "runtimes": daemon_agents,
        "max_concurrent_tasks": daemon_max,
    },
    "agents": agent_rows,
    "working_agents_total_running": sum(int(a.get("running_task_count", 0) or 0) for a in agent_rows),
    "local_cursor_cli": {
        "total": len(proc_lines),
        "portfolio": breakdown["portfolio"],
        "multica_daemon": breakdown["multica_daemon"],
        "feishu_claw": breakdown["feishu_claw"],
        "other": breakdown["other"],
    },
}

if os.environ.get("MULTICA_RUNTIME_JSON") == "1":
    print(json.dumps(out, ensure_ascii=False, indent=2))
    sys.exit(0)

print("── Multica / 本机 Cursor CLI ──")
if api_ok:
    print(f"daemon: {daemon_line or 'unknown'} | 整机并发上限: {daemon_max} | 运行时: {daemon_agents or '-'}")
    print(f"Multica 智能体 task 合计在跑: {out['working_agents_total_running']}")
    for row in agent_rows:
        print(
            f"  · {row['name']}: 上限 {row['max_concurrent_tasks']} | "
            f"跑 task {row['running_task_count']}"
        )
else:
    print(f"Multica API: 不可用 ({api_error})")

cli = out["local_cursor_cli"]
print(
    f"本机 cursor-agent/agent 进程: {cli['total']} "
    f"(portfolio {cli['portfolio']}, multica daemon {cli['multica_daemon']}, "
    f"飞书桥接 {cli['feishu_claw']}, 其他 {cli['other']})"
)
PY
