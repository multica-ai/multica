#!/usr/bin/env bash
# Append a harness learning candidate (routing queue). Does not PATCH norm docs.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MULTICA_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
QUEUE="${QUEUE:-$MULTICA_ROOT/.ai-company/docs/system-evolution/harness-candidates.md}"
CONTENT=""
SUGGEST=""
SOURCE="manual"
LAYER=""
DRY_RUN=0
DEPOSIT=0
VAULT="${VAULT:-$HOME/Documents/SecondBrain}"

usage() {
  cat <<'EOF'
Usage: record-harness-learning.sh --content "结论" [options]

Append one row to .ai-company/docs/system-evolution/harness-candidates.md.
Does NOT edit company-os / HARNESS / CLAUDE.md (CEO promotes weekly).

Options:
  --content TEXT      Required. One-line conclusion.
  --suggest PATH      Target doc hint (e.g. docs/18-definition-of-done.md)
  --layer LAYER       company | project | vault | task (auto if omitted)
  --source LABEL      manual | milestone | blocked | autopilot (default: manual)
  --deposit           Also run SecondBrain deposit-second-brain.sh if available
  --queue PATH        Override queue file
  --dry-run
  -h, --help

Examples:
  record-harness-learning.sh --content "INFRA: gh needs proxy in cron PATH" \
    --suggest docs/23-local-agent-environment.md --source blocked
EOF
}

infer_suggest() {
  local text="$1"
  case "$text" in
    *INFRA*|*proxy*|*cursor-agent*|*pnpm*)
      echo "docs/23-local-agent-environment.md" ;;
    *NEED_CLARIFY*|*agent-safe*|*分级*)
      echo "docs/06-task-grading.md" ;;
    *BLOCKED*|*VERIFY_EXHAUSTED*)
      echo "runbooks/blocked-triage.md" ;;
    *Verifier*|*DoD*|*exit\ 0*)
      echo "docs/18-definition-of-done.md" ;;
    *merge-policy*|*POLICY_DENY*)
      echo "docs/06-task-grading.md + .delivery/config/merge-policy.json" ;;
    *Tier-0*|*company-harness*|*省\ token*)
      echo "docs/27-norm-sync.md" ;;
    *选型*|*定稿*|*弃用*|*架构*)
      echo "Vault HARNESS/projects/{slug}.md" ;;
    *)
      echo "docs/31-harness-learnings-routing.md" ;;
  esac
}

infer_layer() {
  local text="$1"
  case "$text" in
    *Vault*|*HARNESS/projects*)
      echo "vault" ;;
    *brief*|*accept_cases*|*delivery*)
      echo "project" ;;
    *Issue*|*本票*)
      echo "task" ;;
    *)
      echo "company" ;;
  esac
}

while [ $# -gt 0 ]; do
  case "$1" in
    --content) CONTENT="${2:?}"; shift 2 ;;
    --suggest) SUGGEST="${2:?}"; shift 2 ;;
    --layer) LAYER="${2:?}"; shift 2 ;;
    --source) SOURCE="${2:?}"; shift 2 ;;
    --deposit) DEPOSIT=1; shift ;;
    --queue) QUEUE="${2:?}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 1 ;;
  esac
done

[ -n "$CONTENT" ] || { usage >&2; exit 1; }

if [ "${#CONTENT}" -gt 500 ]; then
  echo "error: --content exceeds 500 characters" >&2
  exit 1
fi

if printf '%s' "$CONTENT" | grep -qiE \
  '(ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9]{10,}|Bearer [A-Za-z0-9]|api[_-]?key[[:space:]]*[:=]|password[[:space:]]*[:=]|CURSOR_API_KEY[[:space:]]*[:=]|xox[baprs]-[A-Za-z0-9-]{10,})'; then
  echo "error: content may contain secrets — redact before recording" >&2
  exit 1
fi

if [ -z "$SUGGEST" ]; then
  SUGGEST="$(infer_suggest "$CONTENT")"
fi
if [ -z "$LAYER" ]; then
  LAYER="$(infer_layer "$CONTENT$SUGGEST")"
fi

date_utc="$(date -u +"%Y-%m-%d")"
# Escape pipes for markdown table
esc() { printf '%s' "$1" | tr '\n' ' ' | sed 's/|/\\|/g'; }
content_esc="$(esc "$CONTENT")"
suggest_esc="$(esc "$SUGGEST")"
source_esc="$(esc "$SOURCE")"
row="| open | ${date_utc} | ${suggest_esc} | ${content_esc} | ${source_esc} |"

if [ "$DRY_RUN" -eq 1 ]; then
  echo "[dry-run] append to $QUEUE"
  echo "$row"
  exit 0
fi

[ -f "$QUEUE" ] || { echo "error: queue missing: $QUEUE" >&2; exit 1; }

python3 - "$QUEUE" "$row" <<'PY'
import sys
from pathlib import Path

path = Path(sys.argv[1])
row = sys.argv[2]
text = path.read_text(encoding="utf-8")
placeholder = "_（尚无条目；里程碑或 `记录 harness 经验` 后会出现）_"
if placeholder in text:
    text = text.replace(
        f"| — | — | — | {placeholder} | — |",
        row,
        1,
    )
else:
    lines = text.splitlines()
    # insert after header separator (line starting with |------)
    for i, line in enumerate(lines):
        if line.startswith("|------"):
            lines.insert(i + 1, row)
            break
    else:
        lines.append(row)
    text = "\n".join(lines) + "\n"
path.write_text(text, encoding="utf-8")
print(f"[OK] harness candidate -> {path}")
PY

if [ "$DEPOSIT" -eq 1 ]; then
  dep="$VAULT/10-SYSTEM/scripts/deposit-second-brain.sh"
  if [ -x "$dep" ] || [ -f "$dep" ]; then
    bash "$dep" --content "$CONTENT" --project-root "$MULTICA_ROOT" \
      --target inbox --source milestone --title "harness-candidate: $LAYER" || true
  else
    echo "note: deposit script not found, skipped" >&2
  fi
fi

echo "layer=$LAYER suggest=$SUGGEST"
