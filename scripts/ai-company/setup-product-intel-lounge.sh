#!/usr/bin/env bash
# Idempotent setup for 产品情报站 (docs/35-product-intel-lounge.md).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TEMPLATE_DIR="$MULTICA_ROOT/.ai-company/templates/intel-lounge"

RUNTIME_ID="${MULTICA_INTEL_RUNTIME_ID:-${MULTICA_DEV_AGENT_ID:-}}"
DRY_RUN=0
SKIP_AGENTS=0
SKIP_AUTOPILOTS=0

usage() {
  cat <<'EOF'
Usage: setup-product-intel-lounge.sh [options]

Creates Multica agents, labels, project, and three autopilots for the intel lounge.
Feishu group + Bot wiring remains manual (printed at end).

Options:
  --dry-run           Print actions only
  --skip-agents       Skip agent create
  --skip-autopilots   Skip autopilot create
  -h, --help          Show help

Env:
  MULTICA_INTEL_RUNTIME_ID   Cursor/runtime UUID (default: first online local runtime)
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    --skip-agents) SKIP_AGENTS=1 ;;
    --skip-autopilots) SKIP_AUTOPILOTS=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage; exit 1 ;;
  esac
  shift
done

log() { echo "intel-setup: $*" >&2; }
run() {
  if [ "$DRY_RUN" -eq 1 ]; then
    log "[dry-run] $*"
  else
    log "$*"
    "$@"
  fi
}

require_multica() {
  if ! command -v multica >/dev/null 2>&1; then
    echo "error: multica CLI not found" >&2
    exit 1
  fi
  if ! multica daemon status 2>/dev/null | grep -qi '^Daemon:[[:space:]]*running'; then
    echo "error: multica daemon not running (multica daemon start)" >&2
    exit 1
  fi
}

resolve_runtime_id() {
  if [ -n "$RUNTIME_ID" ]; then
    echo "$RUNTIME_ID"
    return
  fi
  RUNTIME_ID="$(multica runtime list --output json 2>/dev/null | python3 -c "
import json, sys
rows = json.load(sys.stdin)
online = [r for r in rows if r.get('status') == 'online']
prefer = [r for r in online if r.get('provider') == 'cursor'] or online
if not prefer:
    sys.exit(1)
print(prefer[0]['id'])
" 2>/dev/null || true)"
  if [ -z "$RUNTIME_ID" ]; then
    echo "error: set MULTICA_INTEL_RUNTIME_ID or ensure an online runtime" >&2
    exit 1
  fi
  echo "$RUNTIME_ID"
}

agent_id_by_name() {
  local name="$1"
  multica agent list --output json 2>/dev/null | python3 -c "
import json, sys
name = sys.argv[1]
for row in json.load(sys.stdin):
    if row.get('name') == name and not row.get('archived_at'):
        print(row['id'])
        break
" "$name"
}

ensure_label() {
  local name="$1" color="$2"
  if multica label list --output json 2>/dev/null | python3 -c "
import json, sys
name = sys.argv[1]
print('yes' if any(r.get('name') == name for r in json.load(sys.stdin)) else 'no')
" "$name" | grep -q yes; then
    log "label exists: $name"
    return
  fi
  run multica label create --name "$name" --color "$color"
}

ensure_agent() {
  local name="$1" desc="$2" instructions_file="$3" max_tasks="${4:-2}"
  local existing
  existing="$(agent_id_by_name "$name" || true)"
  if [ -n "$existing" ]; then
    log "agent exists: $name ($existing)"
    echo "$existing"
    return
  fi
  local instructions
  instructions="$(cat "$instructions_file")"
  if [ "$DRY_RUN" -eq 1 ]; then
    log "[dry-run] create agent $name"
    echo "dry-run-$name"
    return
  fi
  multica agent create \
    --name "$name" \
    --description "$desc" \
    --instructions "$instructions" \
    --runtime-id "$RUNTIME_ID" \
    --visibility workspace \
    --max-concurrent-tasks "$max_tasks" \
    --output json | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])"
}

ensure_project() {
  local title="产品情报站"
  local pid
  pid="$(multica project list --output json 2>/dev/null | python3 -c "
import json, sys
for row in json.load(sys.stdin):
    if row.get('title') == sys.argv[1]:
        print(row['id']); break
" "$title" || true)"
  if [ -n "$pid" ]; then
    log "project exists: $title ($pid)"
    echo "$pid"
    return
  fi
  local lead_id="$1"
  if [ "$DRY_RUN" -eq 1 ]; then
    log "[dry-run] create project $title"
    echo "dry-run-project"
    return
  fi
  multica project create \
    --title "$title" \
    --description "Product intel lounge — daily scans, Feishu cards, command-based ticketing." \
    --icon "📡" \
    --lead "$lead_id" \
    --status in_progress \
    --repo "https://github.com/multica-ai/multica" \
    --output json | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])"
}

autopilot_id_by_title() {
  local title="$1"
  multica autopilot list --output json 2>/dev/null | python3 -c "
import json, sys
title = sys.argv[1]
data = json.load(sys.stdin)
rows = data.get('autopilots', data) if isinstance(data, dict) else data
for row in rows:
    if row.get('title') == title:
        print(row['id']); break
" "$title"
}

ensure_autopilot() {
  local title="$1" agent_name="$2" mode="$3" prompt_file="$4"
  local issue_template="${5:-}"
  local cron="${6:-}"
  local tz="${7:-Asia/Shanghai}"

  local existing
  existing="$(autopilot_id_by_title "$title" || true)"
  if [ -n "$existing" ]; then
    log "autopilot exists: $title ($existing)"
    echo "$existing"
    return
  fi

  local prompt
  prompt="$(cat "$prompt_file")"
  local ap_id=""

  if [ "$DRY_RUN" -eq 1 ]; then
    log "[dry-run] autopilot create: $title mode=$mode agent=$agent_name"
    ap_id="dry-run-$title"
  else
    local -a create_args=(
      multica autopilot create
      --title "$title"
      --description "$prompt"
      --agent "$agent_name"
      --mode "$mode"
    )
    if [ -n "$issue_template" ]; then
      create_args+=(--issue-title-template "$issue_template")
    fi
    ap_id="$("${create_args[@]}" --output json | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")"
    log "created autopilot: $title ($ap_id)"
  fi

  if [ -n "$cron" ] && [ -n "$ap_id" ]; then
    if [ "$DRY_RUN" -eq 1 ]; then
      log "[dry-run] trigger-add $ap_id cron=$cron tz=$tz"
    else
      run multica autopilot trigger-add "$ap_id" \
        --kind schedule \
        --cron "$cron" \
        --timezone "$tz" \
        --label "$title"
    fi
  fi
  echo "$ap_id"
}

print_feishu_manual() {
  cat <<'EOF'

── 飞书（需 CEO 手动）──
1. 建群「产品情报站」，置顶群规：
   cat .ai-company/templates/intel-lounge/feishu-group-pin.txt
2. Multica → Agents → intel-scout / product-analyst / intel-moderator
   → Capabilities → Integrations → 连接飞书 Bot（各 1 个）
3. 将 3 个 Bot 拉进群；CEO + 可选 1 真人搭档
4. 试跑每日扫描：
   multica autopilot trigger 0844ec2d-46c6-4f61-ab30-83193da48936
   # 或读 ~/.multica/intel-lounge.json

── 验收 ──
bash scripts/ai-company/verify-hands-off.sh
bash scripts/ai-company/setup-product-intel-lounge.sh --dry-run  # 应全 exists

EOF
}

main() {
  require_multica
  RUNTIME_ID="$(resolve_runtime_id)"
  log "runtime: $RUNTIME_ID"

  ensure_label "intel" "#6366f1"
  ensure_label "intel-watch" "#a855f7"

  if [ "$SKIP_AGENTS" -eq 0 ]; then
    SCOUT_ID="$(ensure_agent "intel-scout" "产品情报站 — 每日热点扫描" \
      "$TEMPLATE_DIR/agents/intel-scout.md" 2)"
    ANALYST_ID="$(ensure_agent "product-analyst" "产品情报站 — 产品解读" \
      "$TEMPLATE_DIR/agents/product-analyst.md" 2)"
    MOD_ID="$(ensure_agent "intel-moderator" "产品情报站 — 口令主持" \
      "$TEMPLATE_DIR/agents/intel-moderator.md" 2)"
    ensure_agent "content-picker" "产品情报站 — 内容选题" \
      "$TEMPLATE_DIR/agents/content-picker.md" 1 >/dev/null
    PROJECT_ID="$(ensure_project "$MOD_ID")"
    log "project: $PROJECT_ID (lead: intel-moderator)"
  fi

  if [ "$SKIP_AUTOPILOTS" -eq 0 ]; then
    ensure_autopilot "每日产品热点扫描" "intel-scout" "create_issue" \
      "$TEMPLATE_DIR/autopilots/daily-scan.md" \
      "intel/{{date}}-daily" \
      "0 9 * * 1-5" >/dev/null

    ensure_autopilot "热点产品解读" "product-analyst" "run_only" \
      "$TEMPLATE_DIR/autopilots/product-brief.md" \
      "" \
      "0 14 * * 1-5" >/dev/null

    ensure_autopilot "本周情报周报" "intel-moderator" "run_only" \
      "$TEMPLATE_DIR/autopilots/weekly-recap.md" \
      "" \
      "0 10 * * 6" >/dev/null
  fi

  log "done"
  if [ "$DRY_RUN" -eq 0 ]; then
    mkdir -p "$HOME/.multica"
    if multica agent list --output json 2>/dev/null | python3 -c "
import json, sys
names = {'intel-scout','product-analyst','intel-moderator','content-picker'}
out = {'agents': {}, 'autopilots': {}, 'project_id': None}
for row in json.load(sys.stdin):
    if row.get('name') in names:
        out['agents'][row['name']] = row['id']
for row in json.loads(__import__('subprocess').check_output(['multica','project','list','--output','json'])):
    if row.get('title') == '产品情报站':
        out['project_id'] = row['id']
        break
for row in json.loads(__import__('subprocess').check_output(['multica','autopilot','list','--output','json'])).get('autopilots', []):
    out['autopilots'][row['title']] = row['id']
path = sys.argv[1]
json.dump(out, open(path, 'w'), ensure_ascii=False, indent=2)
print(path)
" "$HOME/.multica/intel-lounge.json" 2>/dev/null; then
      log "wrote $HOME/.multica/intel-lounge.json"
    fi
  fi
  print_feishu_manual
}

main "$@"
