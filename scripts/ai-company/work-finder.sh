#!/usr/bin/env bash
# Work-Finder（找活工）— 队列薄时为已有产品造 agent-safe 小票，再 sync 进 GitHub。
# 不新开产品线；不碰密钥 / merge-policy / 支付。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"
# shellcheck source=lib/notify.sh
source "$SCRIPT_DIR/lib/notify.sh"

REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
GITHUB_ORG="${GITHUB_ORG:-chenzh}"
STATE_DIR="${WORK_FINDER_STATE_DIR:-$HOME/.multica}"
STATE_FILE="${WORK_FINDER_STATE_FILE:-$STATE_DIR/work-finder-state.json}"
LOG_DIR="${WORK_FINDER_LOG_DIR:-$HOME/.multica/work-finder-logs}"
QUEUE_TARGET="${WORK_FINDER_QUEUE_TARGET:-3}"
MAX_NEW="${WORK_FINDER_MAX_NEW:-3}"
MAX_PER_PROJECT="${WORK_FINDER_MAX_PER_PROJECT:-2}"
MODE="${WORK_FINDER_MODE:-heuristic}" # heuristic | agent | auto（cron 默认 heuristic）
DRY_RUN=0
FORCE=0
SYNC=1
NOTIFY=1
QUIET_START="${WORK_FINDER_QUIET_START:-23}"
QUIET_END="${WORK_FINDER_QUIET_END:-6}"
CURSOR_AGENT_BIN="${CURSOR_AGENT_BIN:-cursor-agent}"

usage() {
  cat <<'EOF'
Usage: work-finder.sh [options]

When company QUEUE(agent-safe) < QUEUE_TARGET, invent small agent-safe tickets
into .ai-company/examples/<slug>/backlog.md, then sync-portfolio-backlogs.

Options:
  --dry-run              Propose / print only; no backlog write, no sync
  --force                Ignore quiet hours
  --mode auto|heuristic|agent
  --queue-target N       Find work only if TOTAL_QUEUE < N (default 3)
  --max-new N            Max tickets this run company-wide (default 3)
  --max-per-project N    Cap per project (default 2)
  --no-sync              Skip sync-portfolio-backlogs.sh
  --no-notify            Skip Feishu
  --registry PATH
  --org ORG
  -h, --help

Env: WORK_FINDER_* mirrors flags; quiet 23:00–06:00 Asia/Shanghai.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --force) FORCE=1; shift ;;
    --mode) MODE="${2:?}"; shift 2 ;;
    --queue-target) QUEUE_TARGET="${2:?}"; shift 2 ;;
    --max-new) MAX_NEW="${2:?}"; shift 2 ;;
    --max-per-project) MAX_PER_PROJECT="${2:?}"; shift 2 ;;
    --no-sync) SYNC=0; shift ;;
    --no-notify) NOTIFY=0; shift ;;
    --registry) REGISTRY="${2:?}"; shift 2 ;;
    --org) GITHUB_ORG="${2:?}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

mkdir -p "$STATE_DIR" "$LOG_DIR"
TS="$(date '+%Y%m%dT%H%M%S%z')"
LOG_FILE="$LOG_DIR/work-finder-$TS.log"
exec > >(tee -a "$LOG_FILE") 2>&1

echo "=== work-finder $(date -Iseconds) ==="
echo "mode=$MODE queue_target=$QUEUE_TARGET max_new=$MAX_NEW dry_run=$DRY_RUN force=$FORCE"

shanghai_hour() {
  TZ=Asia/Shanghai date '+%H' | sed 's/^0*//;s/^$/0/'
}

in_quiet_hours() {
  local h
  h="$(shanghai_hour)"
  if [ "$h" -ge "$QUIET_START" ] || [ "$h" -lt "$QUIET_END" ]; then
    return 0
  fi
  return 1
}

if [ "$FORCE" -eq 0 ] && in_quiet_hours; then
  echo "summary action=quiet hour=$(shanghai_hour)"
  echo '{"lastRunAt":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'","action":"quiet"}' >"$STATE_FILE"
  exit 0
fi

# Reuse dashboard-ish queue counting via gh labels (lightweight)
TOTAL_QUEUE=0
TOTAL_BLOCKED=0
count_repo_queue() {
  local repo="$1"
  local q b
  q="$(gh issue list -R "$repo" -l agent-safe -s open --limit 50 --json number,labels \
    -q '[.[] | select((.labels|map(.name)|index("agent-running")|not) and (.labels|map(.name)|index("agent-blocked")|not) and (.labels|map(.name)|index("agent-done")|not))] | length' 2>/dev/null || echo 0)"
  b="$(gh issue list -R "$repo" -l agent-blocked -s open --limit 20 --json number -q 'length' 2>/dev/null || echo 0)"
  echo "${q:-0} ${b:-0}"
}

# Parse active projects: priority\tid\trepo\tstack\tslug\tbacklog
PROJECTS="$(
  python3 - "$REGISTRY" "$GITHUB_ORG" "$MULTICA_ROOT" <<'PY'
import sys
from pathlib import Path
registry, org, root = Path(sys.argv[1]), sys.argv[2], Path(sys.argv[3])
current = {}
rows = []

def flush():
    global current
    if not current.get("id"):
        current = {}
        return
    if current.get("paused") == "true":
        current = {}
        return
    slug = current.get("delivery_slug") or current.get("id")
    repo = current.get("repo", "")
    repo = repo.replace("https://github.com/", "").replace("github.com/", "")
    if repo.startswith("your-org/"):
        repo = repo.replace("your-org/", f"{org}/", 1)
    backlog = root / ".ai-company/examples" / slug / "backlog.md"
    if not backlog.is_file():
        current = {}
        return
    pri = int(current.get("priority") or "50")
    stack = current.get("stack") or "cloudflare-pages"
    rows.append((pri, current["id"], repo, stack, slug, str(backlog)))
    current = {}

for line in registry.read_text(encoding="utf-8").splitlines():
    s = line.strip()
    if s.startswith("- id:"):
        flush()
        current = {"id": s.split(":", 1)[1].strip()}
        continue
    if not current:
        continue
    for key in ("paused", "delivery_slug", "repo", "priority", "stack"):
        if s.startswith(key + ":"):
            current[key] = s.split(":", 1)[1].strip()
flush()
rows.sort(key=lambda r: r[0])  # lower priority number = sooner? registry uses 10=landing high urgency — lower first
for pri, pid, repo, stack, slug, backlog in rows:
    print(f"{pri}\t{pid}\t{repo}\t{stack}\t{slug}\t{backlog}")
PY
)"

if [ -z "$PROJECTS" ]; then
  echo "summary action=no-projects"
  exit 0
fi

declare -a PROJ_LINES=()
while IFS=$'\t' read -r pri pid repo stack slug backlog; do
  [ -z "$pid" ] && continue
  read -r q b <<<"$(count_repo_queue "$repo")"
  TOTAL_QUEUE=$((TOTAL_QUEUE + q))
  TOTAL_BLOCKED=$((TOTAL_BLOCKED + b))
  echo "project $pid queue=$q blocked=$b stack=$stack"
  PROJ_LINES+=("$pri	$pid	$repo	$stack	$slug	$backlog	$q")
done <<<"$PROJECTS"

echo "TOTAL_QUEUE=$TOTAL_QUEUE TOTAL_BLOCKED=$TOTAL_BLOCKED target=$QUEUE_TARGET"

if [ "$TOTAL_QUEUE" -ge "$QUEUE_TARGET" ]; then
  echo "summary action=queue-ok queue=$TOTAL_QUEUE blocked=$TOTAL_BLOCKED added=0"
  printf '%s\n' "{\"lastRunAt\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"action\":\"queue-ok\",\"queue\":$TOTAL_QUEUE,\"added\":0}" >"$STATE_FILE"
  exit 0
fi

NEED=$((QUEUE_TARGET - TOTAL_QUEUE))
if [ "$NEED" -gt "$MAX_NEW" ]; then
  NEED=$MAX_NEW
fi
echo "need_tickets=$NEED"

resolve_mode() {
  case "$MODE" in
    heuristic|agent) echo "$MODE" ;;
    auto)
      if command -v "$CURSOR_AGENT_BIN" >/dev/null 2>&1 && "$CURSOR_AGENT_BIN" status &>/dev/null; then
        echo agent
      else
        echo heuristic
      fi
      ;;
    *) echo heuristic ;;
  esac
}

EFFECTIVE_MODE="$(resolve_mode)"
echo "effective_mode=$EFFECTIVE_MODE"

run_heuristic() {
  local backlog="$1" slug="$2" stack="$3" maxn="$4"
  local args=(--backlog "$backlog" --slug "$slug" --stack "$stack" --max "$maxn" --date "$(TZ=Asia/Shanghai date +%F)")
  if [ "$DRY_RUN" -eq 1 ]; then
    args+=(--dry-run)
  fi
  python3 "$SCRIPT_DIR/lib/work-finder-heuristic.py" "${args[@]}"
}

run_agent() {
  local backlog="$1" slug="$2" repo="$3" stack="$4" maxn="$5"
  local next_num prompt_file out_log
  next_num="$(
    python3 - "$backlog" <<'PY'
import re, sys
from pathlib import Path
text = Path(sys.argv[1]).read_text(encoding="utf-8")
best = 0
for m in re.finditer(r"^###\s+TICKET-[A-Z]*(\d+)", text, re.M):
    best = max(best, int(m.group(1)))
print(best + 1)
PY
  )"
  prompt_file="$LOG_DIR/prompt-$slug-$TS.txt"
  out_log="$LOG_DIR/agent-$slug-$TS.log"
  python3 - "$MULTICA_ROOT/.ai-company/templates/work-finder-prompt.md" "$prompt_file" \
    "$slug" "$repo" "$maxn" "$backlog" "$next_num" "$(TZ=Asia/Shanghai date +%F)" <<'PY'
from pathlib import Path
import sys
src, dst, slug, repo, maxn, backlog, next_num, date = sys.argv[1:9]
text = Path(src).read_text(encoding="utf-8")
repl = {
    "{{SLUG}}": slug,
    "{{REPO}}": repo,
    "{{MAX_TICKETS}}": maxn,
    "{{BACKLOG_PATH}}": backlog,
    "{{NEXT_NUM}}": next_num,
    "{{DATE}}": date,
}
for k, v in repl.items():
    text = text.replace(k, v)
Path(dst).write_text(text, encoding="utf-8")
PY

  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] agent would run for $slug (next=TICKET-$next_num max=$maxn)"
    echo "WORK_FINDER_OK added=0"
    return 0
  fi

  if ! (
    cd "$MULTICA_ROOT"
    "$CURSOR_AGENT_BIN" -p -f --model composer-2-fast "$(cat "$prompt_file")"
  ) >"$out_log" 2>&1; then
    echo "warn: agent failed for $slug — see $out_log"
    return 1
  fi
  if grep -q 'WORK_FINDER_OK added=' "$out_log" 2>/dev/null; then
    grep 'WORK_FINDER_OK added=' "$out_log" | tail -1
  else
    echo "WORK_FINDER_OK added=?"
  fi
  return 0
}

ADDED_TOTAL=0
TOUCHED_SLUGS=""

for line in "${PROJ_LINES[@]}"; do
  [ "$NEED" -le 0 ] && break
  IFS=$'\t' read -r pri pid repo stack slug backlog pq <<<"$line"
  if [ "${pq:-0}" -ge 2 ]; then
    echo "skip $pid (project queue=$pq)"
    continue
  fi
  take=$MAX_PER_PROJECT
  if [ "$take" -gt "$NEED" ]; then
    take=$NEED
  fi
  echo "→ find work: $pid ($repo) take≤$take mode=$EFFECTIVE_MODE"

  before_count="$(grep -cE '^### TICKET-' "$backlog" 2>/dev/null || true)"
  before_count="${before_count:-0}"
  if [ "$EFFECTIVE_MODE" = "agent" ]; then
    if ! run_agent "$backlog" "$slug" "$repo" "$stack" "$take"; then
      echo "fallback: heuristic for $pid"
      run_heuristic "$backlog" "$slug" "$stack" "$take" || true
    fi
  else
    run_heuristic "$backlog" "$slug" "$stack" "$take" || true
  fi

  after_count="$(grep -cE '^### TICKET-' "$backlog" 2>/dev/null || true)"
  after_count="${after_count:-0}"
  delta=$((after_count - before_count))
  if [ "$delta" -lt 0 ]; then delta=0; fi
  if [ "$DRY_RUN" -eq 1 ]; then
    # dry-run heuristic prints proposals; count from last WORK_FINDER_HEURISTIC line if present
    delta=0
  fi
  echo "  delta_tickets=$delta (before=$before_count after=$after_count)"
  ADDED_TOTAL=$((ADDED_TOTAL + delta))
  NEED=$((NEED - delta))
  if [ "$delta" -gt 0 ]; then
    TOUCHED_SLUGS="$TOUCHED_SLUGS $slug"
  fi
done

# dry-run should not overwrite success metrics confusingly
if [ "$DRY_RUN" -eq 1 ]; then
  echo "summary action=find-dry-run queue_was=$TOTAL_QUEUE blocked=$TOTAL_BLOCKED mode=$EFFECTIVE_MODE"
  exit 0
fi

if [ "$SYNC" -eq 1 ] && [ "$ADDED_TOTAL" -gt 0 ]; then
  echo ">> sync-portfolio-backlogs"
  bash "$SCRIPT_DIR/sync-portfolio-backlogs.sh" --registry "$REGISTRY" --org "$GITHUB_ORG" || {
    echo "warn: sync failed" >&2
  }
fi

summary_msg="找活工: added=$ADDED_TOTAL queue_was=$TOTAL_QUEUE blocked=$TOTAL_BLOCKED mode=$EFFECTIVE_MODE"
echo "summary action=find added=$ADDED_TOTAL queue_was=$TOTAL_QUEUE blocked=$TOTAL_BLOCKED mode=$EFFECTIVE_MODE"
printf '%s\n' "{\"lastRunAt\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"action\":\"find\",\"added\":$ADDED_TOTAL,\"queueWas\":$TOTAL_QUEUE,\"mode\":\"$EFFECTIVE_MODE\"}" >"$STATE_FILE"

if [ "$NOTIFY" -eq 1 ] && [ "$ADDED_TOTAL" -gt 0 ]; then
  notify_ceo_brief "🧭 $summary_msg
slugs:${TOUCHED_SLUGS:- none}
log: $LOG_FILE" || true
fi

exit 0
