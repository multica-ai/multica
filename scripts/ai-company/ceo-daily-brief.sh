#!/usr/bin/env bash
# Generate CEO daily brief (markdown) and optionally push to Slack/Feishu.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"
# shellcheck source=lib/notify.sh
source "$SCRIPT_DIR/lib/notify.sh"

REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
GITHUB_ORG="${GITHUB_ORG:-chenzh}"
SINCE="${SINCE:-@yesterday}"
BRIEF_DIR="${CEO_BRIEF_DIR:-$HOME/.multica/ceo-briefs}"
NOTIFY=1
PRINT=1
OUT_FILE=""

usage() {
  cat <<'EOF'
Usage: ceo-daily-brief.sh [options]

Nightly employee report for the CEO: portfolio totals, BLOCKED items,
merged PRs, and recommended actions. Designed for 21:00 Asia/Shanghai cron.

Options:
  --registry PATH     project-registry.yaml
  --org ORG           GitHub org (default: chenzh)
  --since DATE        gh merged search window (default: @yesterday)
  --brief-dir PATH    Save markdown here (default: ~/.multica/ceo-briefs)
  --out PATH          Also write to this file
  --no-notify         Skip Slack/Feishu webhooks
  --quiet             Do not print brief to stdout
  -h, --help

Webhooks (optional, in .ai-company/config/local.env):
  SLACK_WEBHOOK_URL
  FEISHU_WEBHOOK_URL
  Or: bash scripts/ai-company/setup-feishu-bot-notify.sh (Bot private DM)
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --registry) REGISTRY="${2:?}"; shift 2 ;;
    --org) GITHUB_ORG="${2:?}"; shift 2 ;;
    --since) SINCE="${2:?}"; shift 2 ;;
    --brief-dir) BRIEF_DIR="${2:?}"; shift 2 ;;
    --out) OUT_FILE="${2:?}"; shift 2 ;;
    --no-notify) NOTIFY=0; shift ;;
    --quiet) PRINT=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

if ! command -v gh &>/dev/null; then
  echo "error: gh CLI required" >&2
  exit 1
fi

mkdir -p "$BRIEF_DIR"
STAMP="$(date '+%Y-%m-%d-%H%M')"
brief_file="$BRIEF_DIR/brief-${STAMP}.md"

JSON_LINES="$(
  bash "$SCRIPT_DIR/ceo-dashboard.sh" \
    --registry "$REGISTRY" \
    --org "$GITHUB_ORG" \
    --since "$SINCE" \
    --json
)"

RUNTIME_JSON="$(
  bash "$SCRIPT_DIR/multica-runtime-status.sh" --json 2>/dev/null || echo '{"api_ok":false}'
)"

OVERVIEW_JSON="$(
  bash "$SCRIPT_DIR/company-overview-snapshot.sh" 2>/dev/null || echo '{}'
)"

WORKBENCH_URL="${CEO_WORKBENCH_URL:-http://127.0.0.1:9477}"

export BRIEF_FILE="$brief_file"
export BRIEF_SINCE="$SINCE"
export BRIEF_JSON_LINES="$JSON_LINES"
export BRIEF_RUNTIME_JSON="$RUNTIME_JSON"
export BRIEF_OVERVIEW_JSON="$OVERVIEW_JSON"
export BRIEF_WORKBENCH_URL="$WORKBENCH_URL"

python3 <<'PY'
import json
import os
import subprocess
from datetime import datetime
from pathlib import Path

brief_file = os.environ["BRIEF_FILE"]
since = os.environ["BRIEF_SINCE"]
runtime = json.loads(os.environ.get("BRIEF_RUNTIME_JSON", "{}"))
overview = json.loads(os.environ.get("BRIEF_OVERVIEW_JSON", "{}"))
workbench_url = os.environ.get("BRIEF_WORKBENCH_URL", "http://127.0.0.1:9477")
content_wb = overview.get("content", {}).get("workbench_url") or os.environ.get(
    "CONTENT_WORKBENCH_URL", "https://hq.revoices.app/#content/review"
)
rows = [
    json.loads(line)
    for line in os.environ["BRIEF_JSON_LINES"].splitlines()
    if line.strip()
]

total_blocked = total_running = total_safe = total_merged = 0
project_lines = []
blocked_section = []
merged_section = []
pending_merge_section = []

for row in rows:
    repo = row["repo"]
    paused = row.get("paused") in (True, "true")
    blocked = int(row.get("blocked") or 0)
    running = int(row.get("running") or 0)
    safe = int(row.get("agent_safe") or 0)
    merged = int(row.get("merged_prs") or 0)
    accessible = row.get("accessible") in (True, "true")

    if not paused:
        total_blocked += blocked
        total_running += running
        total_safe += safe
        total_merged += merged

    pause_note = " (paused)" if paused else ""
    project_lines.append(
        f"- **{row['id']}**{pause_note}: BLOCKED {blocked} | RUNNING {running} | "
        f"QUEUE {safe} | MERGED({since}) {merged}"
    )

    if not accessible:
        continue

    if blocked > 0:
        out = subprocess.run(
            [
                "gh", "issue", "list", "-R", repo,
                "-l", "agent-blocked", "-s", "open",
                "--json", "number,title,url",
                "-q", r'.[] | "[\(.number)] \(.title) — \(.url)"',
            ],
            capture_output=True,
            text=True,
            check=False,
        )
        for line in out.stdout.splitlines():
            if line.strip():
                blocked_section.append(f"- [ ] {line.strip()}")

    out = subprocess.run(
        [
            "gh", "pr", "list", "-R", repo,
            "-s", "merged", "--search", f"merged:>{since}", "-L", "10",
            "--json", "number,title,url",
            "-q", r'.[] | "[\(.number)] \(.title) — \(.url)"',
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    for line in out.stdout.splitlines():
        if line.strip():
            merged_section.append(f"- {line.strip()}")

    out = subprocess.run(
        [
            "gh", "pr", "list", "-R", repo,
            "-s", "open", "-L", "10",
            "--json", "number,title,url,isDraft,statusCheckRollup",
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    if out.returncode == 0 and out.stdout.strip():
        import json as _json

        for pr in _json.loads(out.stdout):
            if pr.get("isDraft"):
                continue
            checks = pr.get("statusCheckRollup") or []
            if not checks:
                continue
            if all(
                c.get("status") == "COMPLETED" and c.get("conclusion") == "SUCCESS"
                for c in checks
            ):
                pending_merge_section.append(
                    f"- [ ] [#{pr['number']}] {pr['title']} — {pr['url']}"
                )

if total_blocked > 0:
    verdict = f"⚠️ {total_blocked} BLOCKED — 需 CEO 拍板"
    action = "处理 BLOCKED（runbooks/blocked-triage.md），其余可睡"
elif pending_merge_section:
    verdict = f"🟢 {len(pending_merge_section)} 条绿 PR 待 merge"
    action = "回复「merge #xx」或开启 CEO_AUTO_MERGE=1 自动合并"
elif total_safe == 0 and total_blocked == 0:
    verdict = "✅ 无 BLOCKED、队列已空 — 可躺平"
    action = "从 backlog 补 agent-safe 票（sync-backlog-to-issues.sh）或 unpause 项目"
elif total_merged == 0 and total_safe > 0:
    verdict = "💤 队列有粮、昨夜零交付 — 建议派单"
    action = "运行: ceo-dashboard.sh --dispatch 或工作台「智能派单」"
else:
    verdict = "✅ 无 BLOCKED — 可躺平"
    action = "抽检 1 条 merge 即可；无 BLOCKED 无需操作"

cursor = subprocess.run(["cursor-agent", "status"], capture_output=True, text=True, check=False)
cursor_ready = (
    "yes (local CLI)"
    if cursor.returncode == 0 and "Logged in" in (cursor.stdout + cursor.stderr)
    else "no"
)

runtime_lines = []
if runtime.get("api_ok"):
    daemon = runtime.get("daemon") or {}
    cli = runtime.get("local_cursor_cli") or {}
    runtime_lines.append(
        f"- daemon 上限 **{daemon.get('max_concurrent_tasks', '-')}** · "
        f"运行时 {daemon.get('runtimes', '-')}"
    )
    runtime_lines.append(
        f"- Multica task 合计在跑: **{runtime.get('working_agents_total_running', 0)}** · "
        f"本机 CLI 进程: portfolio {cli.get('portfolio', 0)} / "
        f"daemon {cli.get('multica_daemon', 0)} / 飞书 {cli.get('feishu_claw', 0)}"
    )
    for agent in runtime.get("agents") or []:
        runtime_lines.append(
            f"  - {agent.get('name', agent.get('id'))}: "
            f"上限 {agent.get('max_concurrent_tasks', '-')} · "
            f"跑 task {agent.get('running_task_count', 0)}"
        )
else:
    runtime_lines.append(f"- _不可用_ ({runtime.get('api_error', 'unknown')})")

overview_lines = []
proc = overview.get("process") or {}
hq = overview.get("hq") or {}
overview_lines.append(f"- HQ multica `{hq.get('sha', '-')}` · manifest {proc.get('manifest_paths', 0)} 文件")
cron_ok = proc.get("cron_installed")
overview_lines.append(
    f"- 21:00 cron: **{'已安装' if cron_ok else '未安装'}**"
)
if proc.get("last_nightly_log_line"):
    overview_lines.append(f"- 最近 nightly 日志: `{proc['last_nightly_log_line'][:80]}`")

stale_norms = []
missing_path = []
for proj in overview.get("projects") or []:
    if not proj.get("local_path_resolved"):
        missing_path.append(proj.get("id", "?"))
        continue
    cos = proj.get("company_os") or {}
    if not cos.get("synced"):
        stale_norms.append(f"{proj.get('id')} (未 sync company-os)")
    elif not cos.get("hq_sha_current"):
        stale_norms.append(f"{proj.get('id')} (@ {cos.get('hq_sha', '?')})")

if missing_path:
    overview_lines.append(f"- ⚠️ 无本机 path: {', '.join(missing_path)}")
if stale_norms:
    overview_lines.append(f"- ⚠️ 规范副本需更新: {', '.join(stale_norms)}")
elif overview.get("projects"):
    overview_lines.append("- ✅ 各产品 company-os 与 HQ 同 sha（或已 sync）")

overview_lines.append(f"- **指挥舱**: {workbench_url}")
overview_lines.append(f"- **内容审稿 HQ**: {content_wb}")

ts = datetime.now().strftime("%Y-%m-%d %H:%M %Z")
body = f"""# AI 公司日报 — {ts}

## 结论

**{verdict}**

| BLOCKED | RUNNING | QUEUE | MERGED({since}) |
|--------:|--------:|------:|-----------------:|
| {total_blocked} | {total_running} | {total_safe} | {total_merged} |

派单模式: {cursor_ready}

## 指挥舱 / 资产

{chr(10).join(overview_lines)}

## Multica / 本机 CLI

{chr(10).join(runtime_lines)}

## 项目

{chr(10).join(project_lines)}

## 需 CEO 拍板（BLOCKED）

{chr(10).join(blocked_section) if blocked_section else "_无_"}

## 昨夜交付（merged PR）

{chr(10).join(merged_section) if merged_section else "_无_"}

## 待 merge（CI 全绿）

{chr(10).join(pending_merge_section) if pending_merge_section else "_无_"}

## 建议动作

{action}

---
指挥舱: {workbench_url}
内容 HQ: {content_wb}
本地: `bash scripts/ai-company/ceo-workbench.sh`
"""
Path(brief_file).write_text(body, encoding="utf-8")
PY

if [ -n "$OUT_FILE" ]; then
  cp "$brief_file" "$OUT_FILE"
fi

if [ "$PRINT" -eq 1 ]; then
  cat "$brief_file"
fi

if [ "$NOTIFY" -eq 1 ]; then
  if ! has_ceo_notify_channel; then
    echo "notify: skipped (set SLACK_WEBHOOK_URL, FEISHU_WEBHOOK_URL, or run setup-feishu-bot-notify.sh)" >&2
  else
    notify_text="$(python3 - "$brief_file" "$WORKBENCH_URL" <<'PY'
import sys
from pathlib import Path

text = Path(sys.argv[1]).read_text(encoding="utf-8")
workbench_url = sys.argv[2]
lines = text.splitlines()
head = [f"📊 指挥舱 {workbench_url}", ""]
for line in lines:
    head.append(line)
    if line.startswith("## Multica"):
        break
for i, line in enumerate(lines):
    if line.startswith("## 建议动作"):
        head.extend(lines[i : i + 8])
        break
out = "\n".join(head)
if len(out) > 12000:
    out = out[:11900] + "\n…(truncated)"
print(out)
PY
)"
    notify_ceo_brief "$notify_text" && echo "notify: sent" >&2 || echo "notify: failed" >&2
  fi
  if [ "${CEO_FEISHU_APPROVAL_PUSH:-0}" = "1" ]; then
    if bash "$SCRIPT_DIR/ceo-feishu-approval.sh" sync >/dev/null 2>&1 \
      && bash "$SCRIPT_DIR/ceo-feishu-approval.sh" push >/dev/null 2>&1; then
      echo "approval cards: sent" >&2
    else
      echo "approval cards: skipped or failed (see ceo-feishu-approval.sh list)" >&2
    fi
  fi
fi

echo "brief: $brief_file" >&2
