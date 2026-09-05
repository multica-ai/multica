#!/usr/bin/env bash
# One-line site factory: research → MVP → Cloudflare scaffold → harness → dispatch.
# Trigger: Feishu "做一个XX网站" or: bash site-factory.sh --intake "做一个XX网站"
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
# shellcheck source=lib/source-local-env.sh
source "$SCRIPT_DIR/lib/source-local-env.sh"
# shellcheck source=lib/notify.sh
source "$SCRIPT_DIR/lib/notify.sh"
# shellcheck source=lib/site-factory-runtime.sh
source "$SCRIPT_DIR/lib/site-factory-runtime.sh"
# shellcheck source=lib/site-factory-multica.sh
source "$SCRIPT_DIR/lib/site-factory-multica.sh"
# shellcheck source=lib/site-factory-fallback.sh
source "$SCRIPT_DIR/lib/site-factory-fallback.sh"
# shellcheck source=lib/site-factory-activate.sh
source "$SCRIPT_DIR/lib/site-factory-activate.sh"

REGISTRY="${REGISTRY:-$MULTICA_ROOT/.ai-company/templates/project-registry.yaml}"
PROJECTS_BASE="${SITE_FACTORY_PROJECTS_BASE:-$HOME/Projects}"
GITHUB_ORG="${GITHUB_ORG:-chenzh}"
CURSOR_AGENT_BIN="${CURSOR_AGENT_BIN:-cursor-agent}"
RUN_LOGS="$MULTICA_ROOT/.ai-company/run-logs"

INTAKE=""
SLUG=""
IDEA=""
TARGET=""
DRY_RUN=0
SKIP_RESEARCH=0
SKIP_MVP=0
SKIP_SCAFFOLD=0
SKIP_BOOTSTRAP=0
SKIP_DISPATCH=0
SKIP_REGISTRY=0
NOTIFY=0
CREATE_REPO=0
PUSH=0
MAX_DISPATCH=2
USE_MULTICA="${SITE_FACTORY_USE_MULTICA:-auto}"
BACKGROUND=0
ACTIVATE_AUTOPILOT=-1
EXPLICIT_SKIP_REGISTRY=0
EXPLICIT_SKIP_BOOTSTRAP=0
EXPLICIT_SKIP_DISPATCH=0

usage() {
  cat <<'EOF'
Usage: site-factory.sh --intake "做一个XX网站" [options]

Phases (sequential, logged under .ai-company/run-logs/):
  1. parse      — slug + paths from natural language
  2. scaffold   — Cloudflare Pages repo + harness + default delivery files
  3. research   — cursor-agent → .delivery/<slug>/research.md (competitors)
  4. mvp        — cursor-agent → brief.md, accept_cases.md, backlog.md
  5. bootstrap  — GitHub labels/issues (with --create-repo)
  6. registry   — append project-registry.yaml
  7. dispatch   — local agent dispatch (Multica 运行服务并发门禁)

Options:
  --intake TEXT       CEO one-liner (required unless --slug + --idea)
  --slug SLUG         Force project slug
  --idea TEXT         Force idea text
  --target DIR        Project directory (default: ~/Projects/<slug>)
  --create-repo       gh repo create + push (bootstrap Issues)
  --activate-autopilot  接入自迭代：registry + 本机路径 + 首票派单（与 --create-repo 合用）
  --no-autopilot      不接入自迭代（非交互默认）
  --push              git push with --create-repo
  --max-dispatch N    Dispatch up to N agent-safe tickets (default: 2)
  --notify            Feishu/Slack summary via notify.sh
  --background        Detach after parse (for Feishu claw; writes active-pipelines)
  --skip-research     Skip research agent
  --skip-mvp          Skip MVP agent (use example templates only)
  --skip-scaffold     Assume repo exists
  --skip-bootstrap    Skip bootstrap-project.sh
  --skip-registry     Skip project-registry update
  --skip-dispatch     Stop after bootstrap
  --dry-run           Print plan only
  -h, --help

Feishu:
  私聊/群聊 @Bot: 「做一个 JSON 格式化网站」
  或: site-factory: 做一个 XX 网站
  或: multica: site-factory --intake "做一个 XX 网站"

Requires: cursor-agent logged in, gh (if --create-repo), pnpm (after scaffold).
EOF
}

slugify() {
  python3 - "$1" <<'PY'
import re, sys, unicodedata
raw = sys.argv[1].strip().lower()
# Keep common product tokens before ascii fold
tokens = re.findall(r"[a-z0-9]+", raw.lower())
if not tokens:
    raw_norm = unicodedata.normalize("NFKD", raw)
    raw_norm = raw_norm.encode("ascii", "ignore").decode("ascii")
    tokens = [t for t in re.sub(r"[^a-z0-9]+", " ", raw_norm).split() if t]
slug = "-".join(tokens[:6]) if tokens else "site"
slug = re.sub(r"-+", "-", slug).strip("-")[:48]
if len(slug) < 3:
    slug = (slug + "-site") if slug else "new-site"
if slug in {"site", "web", "app", "json", "tool"}:
    slug = slug + "-site"
print(slug)
PY
}

parse_intake() {
  local text="$1"
  python3 - "$text" <<'PY'
import re, sys
text = sys.argv[1].strip()
patterns = [
    r"^(?:请)?(?:帮我)?(?:做|建|开发|创建)(?:一个|个)?(.+?)(?:网站|站点|网页|landing)(?:吧|吗|？|\?|!|！|\.|…|$)",
    r"^site-factory[:\uff1a]\s*(.+)$",
    r"^建站[:\uff1a]\s*(.+)$",
]
idea = text
for p in patterns:
    m = re.search(p, text, re.I | re.S)
    if m:
        idea = m.group(1).strip()
        break
idea = re.sub(r"^(一个|个)\s*", "", idea).strip()
print(idea or text)
PY
}

run_agent_phase() {
  local phase="$1"
  local workspace="$2"
  local prompt_file="$3"
  local log_file="$4"
  local delivery_dir="$5"

  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] $CURSOR_AGENT_BIN phase=$phase workspace=$workspace"
    return 0
  fi

  if ! command -v "$CURSOR_AGENT_BIN" &>/dev/null; then
    echo "error: $CURSOR_AGENT_BIN not found" >&2
    return 1
  fi
  if ! "$CURSOR_AGENT_BIN" status &>/dev/null; then
    echo "error: cursor-agent not logged in" >&2
    return 1
  fi

  mkdir -p "$delivery_dir"

  echo ">> agent phase: $phase"
  set +e
  "$CURSOR_AGENT_BIN" -p --force --trust --approve-mcps \
    --workspace "$workspace" \
    --output-format text \
    -- "$(cat "$prompt_file")" 2>&1 | tee -a "$log_file"
  local agent_exit=${PIPESTATUS[0]}
  set -e
  if [ "$agent_exit" -ne 0 ]; then
    echo "warn: $phase agent exit $agent_exit" | tee -a "$log_file"
  fi

  case "$phase" in
    research)
      if [ ! -f "$delivery_dir/research.md" ]; then
        echo "warn: research.md missing after $phase — using template brief only" >&2
        return 1
      fi
      ;;
    mvp)
      for f in brief.md accept_cases.md backlog.md competitor_inventory.md wont_do.md; do
        if [ ! -f "$delivery_dir/$f" ]; then
          echo "warn: $f missing after $phase (visual replica gate)" >&2
          return 1
        fi
      done
      ;;
  esac
}

render_prompt() {
  local template="$1"
  local out="$2"
  python3 - "$template" "$out" "$SLUG" "$IDEA" <<'PY'
import sys
from pathlib import Path
template, out, slug, idea = sys.argv[1:5]
text = Path(template).read_text(encoding="utf-8")
text = text.replace("{{SLUG}}", slug).replace("{{IDEA}}", idea)
Path(out).write_text(text, encoding="utf-8")
PY
}

append_registry() {
  if [ "$SKIP_REGISTRY" -eq 1 ]; then
    echo "skip: registry"
    return 0
  fi
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] append registry id=$SLUG repo=$GITHUB_ORG/$SLUG"
    return 0
  fi
  if grep -q "id: $SLUG" "$REGISTRY" 2>/dev/null; then
    echo "registry: $SLUG already listed"
    return 0
  fi
  cat >>"$REGISTRY" <<EOF

  - id: $SLUG
    repo: github.com/$GITHUB_ORG/$SLUG
    tier: experiment
    priority: 12
    stack: cloudflare-pages
    autopilot_id: null
    max_nightly_tickets: 2
    openapi: false
    e2e: false
    paused: false
    delivery_slug: $SLUG
    notes: "Feishu site-factory $(date -u +%Y-%m-%d) — $IDEA"
EOF
  echo "registry: appended $SLUG"
}

dispatch_tickets() {
  local repo="$GITHUB_ORG/$SLUG"
  local count=0
  if [ "$SKIP_DISPATCH" -eq 1 ] || [ "$MAX_DISPATCH" -le 0 ]; then
    echo "skip: dispatch"
    return 0
  fi
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] dispatch up to $MAX_DISPATCH tickets on $repo"
    return 0
  fi
  if ! cursor-agent status &>/dev/null 2>&1; then
    echo "warn: cursor-agent not ready — skip local dispatch" >&2
    return 0
  fi

  site_factory_require_runtime | tee -a "$LOG_FILE"

  local slots
  slots="$(site_factory_dispatch_slots "$MAX_DISPATCH")"
  if [ "$slots" -le 0 ]; then
    echo "warn: no dispatch slots (Multica/daemon busy) — queue issues only" | tee -a "$LOG_FILE"
    return 0
  fi
  echo "dispatch: runtime slots=$slots (requested $MAX_DISPATCH)" | tee -a "$LOG_FILE"

  local use_multica="$USE_MULTICA"
  if [ "$use_multica" = "auto" ]; then
    if site_factory_daemon_running && site_factory_runtime_ready; then
      use_multica=1
    else
      use_multica=0
    fi
  fi

  if [ "$use_multica" = "1" ] || [ "$use_multica" = "true" ]; then
    if site_factory_dispatch_via_multica "$repo" "$TARGET" "$SLUG" "$slots" "$LOG_FILE"; then
      echo "dispatch: Multica daemon path OK" | tee -a "$LOG_FILE"
      return 0
    fi
    echo "warn: Multica dispatch failed — falling back to local cursor-agent-cli" | tee -a "$LOG_FILE"
  fi

  local issues_json
  issues_json="$(gh issue list -R "$repo" -l agent-safe -s open --json number 2>/dev/null || echo '[]')"
  local nums
  nums="$(python3 - "$issues_json" "$slots" <<'PY'
import json, sys
issues = json.loads(sys.argv[1])
limit = int(sys.argv[2])
issues.sort(key=lambda row: row["number"])
for row in issues[:limit]:
    print(row["number"])
PY
)"
  local default_branch
  default_branch="$(gh repo view "$repo" --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null || echo main)"

  for num in $nums; do
    [ -z "$num" ] && continue
    echo ">> dispatch issue #$num (base=$default_branch)" | tee -a "$LOG_FILE"
    local dispatch_log="$RUN_LOGS/dispatch-${SLUG}-${num}-${TS}.log"
    GITHUB_REPOSITORY="$repo" REPO_ROOT="$TARGET" WORKTREE_BASE="$default_branch" \
      bash "$MULTICA_ROOT/scripts/agent-delivery/dispatch-cursor-agent-cli.sh" "$num" \
      >>"$dispatch_log" 2>&1 || echo "warn: dispatch #$num failed (see $dispatch_log)" | tee -a "$LOG_FILE"
    count=$((count + 1))
  done
  if [ "$count" -eq 0 ]; then
    echo "dispatch: no open agent-safe issues (create with --create-repo)" | tee -a "$LOG_FILE"
  else
    echo "dispatch: started $count local agent job(s)" | tee -a "$LOG_FILE"
  fi
}

pipeline_main() {
  TS="$(date -u +%Y%m%dT%H%M%SZ)"
  LOG_FILE="$RUN_LOGS/site-factory-${SLUG}-${TS}.log"
  mkdir -p "$RUN_LOGS"
  echo "ts=$TS slug=$SLUG target=$TARGET" | tee "$LOG_FILE"
  echo "$TS $SLUG $TARGET" >>"$RUN_LOGS/active-pipelines.txt"

  site_factory_require_runtime | tee -a "$LOG_FILE"

  local delivery_dir="$TARGET/.delivery/$SLUG"
  local prompt_dir="$MULTICA_ROOT/.ai-company/run-logs/.prompt-$SLUG-$TS"
  mkdir -p "$prompt_dir" "$delivery_dir"

  # Phase 1: scaffold first (harness + CF stack + default templates)
  if [ "$SKIP_SCAFFOLD" -eq 0 ]; then
    if [ "$DRY_RUN" -eq 1 ]; then
      echo "[dry-run] scaffold-cloudflare.sh $TARGET $SLUG"
    else
      bash "$SCRIPT_DIR/scaffold-cloudflare.sh" "$TARGET" "$SLUG" "$IDEA"
    fi
  else
    mkdir -p "$delivery_dir"
  fi

  # Phase 2–3: research + MVP in product repo (agents see full harness)
  if [ "$SKIP_RESEARCH" -eq 0 ]; then
    local research_prompt="$prompt_dir/research-prompt.txt"
    render_prompt "$MULTICA_ROOT/.ai-company/templates/site-factory/research-prompt.md" "$research_prompt"
    run_agent_phase research "$TARGET" "$research_prompt" "$LOG_FILE" "$delivery_dir" || {
      site_factory_write_research_fallback "$delivery_dir" "$SLUG" "$IDEA"
      echo "fallback: wrote research.md" | tee -a "$LOG_FILE"
    }
  fi

  if [ "$SKIP_MVP" -eq 0 ]; then
    local mvp_prompt="$prompt_dir/mvp-prompt.txt"
    render_prompt "$MULTICA_ROOT/.ai-company/templates/site-factory/mvp-prompt.md" "$mvp_prompt"
    if [ -f "$delivery_dir/research.md" ]; then
      {
        echo ""
        echo "## research.md (input)"
        cat "$delivery_dir/research.md"
      } >>"$mvp_prompt"
    fi
    run_agent_phase mvp "$TARGET" "$mvp_prompt" "$LOG_FILE" "$delivery_dir" || {
      site_factory_write_mvp_fallback "$delivery_dir" "$SLUG" "$IDEA"
      echo "fallback: wrote brief/backlog from template" | tee -a "$LOG_FILE"
    }
  fi

  # Phase 4: bootstrap (GitHub labels + issues when repo exists)
  if [ "$SKIP_BOOTSTRAP" -eq 0 ]; then
    local boot_args=("$TARGET")
    [ "$CREATE_REPO" -eq 1 ] && boot_args+=(--create-repo)
    [ "$PUSH" -eq 1 ] && boot_args+=(--push)
    if [ "$CREATE_REPO" -eq 1 ]; then
      boot_args+=(--repo "$GITHUB_ORG/$SLUG")
      boot_args+=(--sync-backlog --from TICKET-001 --to TICKET-005)
    else
      echo "skip: backlog sync (use --create-repo for GitHub issues)" | tee -a "$LOG_FILE"
    fi
    if [ "$DRY_RUN" -eq 1 ]; then
      echo "[dry-run] bootstrap-project.sh ${boot_args[*]}"
    else
      bash "$SCRIPT_DIR/bootstrap-project.sh" "${boot_args[@]}" >>"$LOG_FILE" 2>&1 || true
    fi
  fi

  append_registry
  site_factory_ensure_local_repo_path "$SLUG" "$TARGET" "$MULTICA_ROOT" "$SCRIPT_DIR" "$DRY_RUN"
  dispatch_tickets

  local autopilot_note="自迭代: 已接入"
  if [ "${ACTIVATE_AUTOPILOT:-0}" -ne 1 ]; then
    autopilot_note="自迭代: 未接入（activate-project-autopilot.sh 可补开）"
  fi

  local summary="🏭 建站流水线完成
项目: $SLUG
目录: $TARGET
想法: $IDEA
$autopilot_note
日志: $LOG_FILE
运行服务: $(site_factory_multica_api || echo ${MULTICA_SERVER_URL:-http://localhost:8081})
下一步: CEO 工作台 → 处理 BLOCKED → 勾 AC"

  echo "$summary" | tee -a "$LOG_FILE"
  if [ "$NOTIFY" -eq 1 ]; then
    notify_ceo_brief "$summary" || true
  fi
  rm -rf "$prompt_dir"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --intake) INTAKE="${2:?}"; shift 2 ;;
    --slug) SLUG="${2:?}"; shift 2 ;;
    --idea) IDEA="${2:?}"; shift 2 ;;
    --target) TARGET="${2:?}"; shift 2 ;;
    --create-repo) CREATE_REPO=1; shift ;;
    --activate-autopilot) ACTIVATE_AUTOPILOT=1; shift ;;
    --no-autopilot) ACTIVATE_AUTOPILOT=0; shift ;;
    --push) PUSH=1; CREATE_REPO=1; shift ;;
    --max-dispatch) MAX_DISPATCH="${2:?}"; shift 2 ;;
    --notify) NOTIFY=1; shift ;;
    --background) BACKGROUND=1; shift ;;
    --skip-research) SKIP_RESEARCH=1; shift ;;
    --skip-mvp) SKIP_MVP=1; shift ;;
    --skip-scaffold) SKIP_SCAFFOLD=1; shift ;;
    --skip-bootstrap) SKIP_BOOTSTRAP=1; EXPLICIT_SKIP_BOOTSTRAP=1; shift ;;
    --skip-registry) SKIP_REGISTRY=1; EXPLICIT_SKIP_REGISTRY=1; shift ;;
    --skip-dispatch) SKIP_DISPATCH=1; EXPLICIT_SKIP_DISPATCH=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage; exit 1 ;;
  esac
done

if [ -z "$INTAKE" ] && { [ -z "$SLUG" ] || [ -z "$IDEA" ]; }; then
  usage >&2
  exit 1
fi

if [ -n "$INTAKE" ]; then
  IDEA="$(parse_intake "$INTAKE")"
fi
if [ -z "$SLUG" ]; then
  SLUG="$(slugify "$IDEA")"
fi
if [ -z "$TARGET" ]; then
  TARGET="$PROJECTS_BASE/$SLUG"
fi

site_factory_resolve_autopilot_choice "$CREATE_REPO"

echo "== Site factory =="
echo "  idea:   $IDEA"
echo "  slug:   $SLUG"
echo "  target: $TARGET"
if [ "$CREATE_REPO" -eq 1 ]; then
  echo "  autopilot: $([ "$ACTIVATE_AUTOPILOT" -eq 1 ] && echo yes || echo no)"
fi
echo ""

if [ "$BACKGROUND" -eq 1 ] && [ "$DRY_RUN" -eq 0 ]; then
  nohup bash "$0" \
    --slug "$SLUG" --idea "$IDEA" --target "$TARGET" \
    ${CREATE_REPO:+--create-repo} ${PUSH:+--push} \
    $( [ "$ACTIVATE_AUTOPILOT" -eq 1 ] && echo --activate-autopilot ) \
    $( [ "$ACTIVATE_AUTOPILOT" -eq 0 ] && echo --no-autopilot ) \
    --max-dispatch "$MAX_DISPATCH" \
    ${NOTIFY:+--notify} \
    >>"$RUN_LOGS/site-factory-${SLUG}-nohup.log" 2>&1 &
  echo "pipeline: background pid=$! slug=$SLUG"
  exit 0
fi

pipeline_main
