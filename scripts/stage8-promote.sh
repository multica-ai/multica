#!/usr/bin/env bash
set -euo pipefail

# Stage-8 deterministic promotion helper for the agent-improvement-loop.
#
# Moves a candidate file from dettools/prospect into dettools, updates
# dettools/prospect/manifest.json, optionally imports via Multica, and appends
# immutable promotion diagnostics.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${MULTICA_REPO_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"
cd "$REPO_ROOT"

usage() {
  cat <<'USAGE'
Usage: scripts/stage8-promote.sh --tool <tool_name> [options]

Required:
  --tool <tool_name>         Candidate stem (e.g. agent_install_retry_candidate)

Optional:
  --candidate <path>          Explicit prospect file (dettools/prospect/*.go)
  --approve-ref <ref>         Human approval reference (issue/pr/ops ticket)
  --approved-by <who>         Identity performing promotion
  --commit <sha>              Git commit SHA/short SHA to record
  --manifest <path>           Manifest path (default: dettools/prospect/manifest.json)
  --diagnostics <path>        Diagnostics JSONL path (default: diagnostics/stage8-promotion.jsonl)
  --events-index <path>        Stage 2 index path for baseline comparison (default: diagnostics/stage2/stage2_index.jsonl)
  --candidate-decision <path>  Optional JSON decision payload to embed
  --comparison-window-hours N  Pre/post telemetry comparison window in hours (default: 720)
  --reevaluate-days N          Re-evaluation timer days (default: 30)
  --required-skill-file <path> Repeatable expected skill file paths
  --dry-run                   Print actions without changing files/importing
  --force                     Allow overwrite if destination tool file exists
  --skip-import               Skip multica dettool import step
  --help                      Show this message
USAGE
}

TOOL_NAME=""
CANDIDATE_PATH=""
APPROVE_REF=""
APPROVED_BY=""
COMMIT_SHA=""
MANIFEST_PATH="dettools/prospect/manifest.json"
DIAG_PATH="diagnostics/stage8-promotion.jsonl"
EVENTS_INDEX_PATH="diagnostics/stage2/stage2_index.jsonl"
CANDIDATE_DECISION_PATH=""
COMPARISON_WINDOW_HOURS=720
REEVALUATE_DAYS=30
FORCE=0
DRY_RUN=0
SKIP_IMPORT=0

REQUIRED_SKILLS=(
  "skills/agent-improvement-loop/analyzer.md"
  "skills/agent-improvement-loop/evaluator.md"
  "skills/agent-improvement-loop/SETUP.md"
)

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tool)
      TOOL_NAME="${2:-}"; shift 2;;
    --candidate)
      CANDIDATE_PATH="${2:-}"; shift 2;;
    --approve-ref)
      APPROVE_REF="${2:-}"; shift 2;;
    --approved-by)
      APPROVED_BY="${2:-}"; shift 2;;
    --commit)
      COMMIT_SHA="${2:-}"; shift 2;;
    --manifest)
      MANIFEST_PATH="${2:-}"; shift 2;;
    --diagnostics)
      DIAG_PATH="${2:-}"; shift 2;;
    --events-index)
      EVENTS_INDEX_PATH="${2:-}"; shift 2;;
    --candidate-decision)
      CANDIDATE_DECISION_PATH="${2:-}"; shift 2;;
    --comparison-window-hours)
      COMPARISON_WINDOW_HOURS="${2:-}"; shift 2;;
    --reevaluate-days)
      REEVALUATE_DAYS="${2:-}"; shift 2;;
    --required-skill-file)
      REQUIRED_SKILLS+=("${2:-}"); shift 2;;
    --dry-run)
      DRY_RUN=1; shift ;;
    --force)
      FORCE=1; shift ;;
    --skip-import)
      SKIP_IMPORT=1; shift ;;
    --help|-h)
      usage
      exit 0 ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1 ;;
  esac
done

if [[ -z "$TOOL_NAME" ]]; then
  echo "--tool is required" >&2
  usage
  exit 1
fi

if [[ -z "$COMMIT_SHA" ]] && command -v git >/dev/null 2>&1; then
  COMMIT_SHA="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || true)"
fi

if [[ -z "$APPROVED_BY" ]]; then
  APPROVED_BY="${USER:-unknown}"
fi

if [[ -z "$CANDIDATE_PATH" ]]; then
  for f in \
    "dettools/prospect/${TOOL_NAME}_candidate.go" \
    "dettools/prospect/${TOOL_NAME}_generated.go" \
    "dettools/prospect/${TOOL_NAME}.go"; do
    if [[ -f "$f" ]]; then
      CANDIDATE_PATH="$f"
      break
    fi
  done
fi

if [[ ! -f "$CANDIDATE_PATH" ]]; then
  echo "Candidate file not found for tool '$TOOL_NAME'." >&2
  echo "Try --candidate dettools/prospect/<file>.go" >&2
  exit 1
fi

if [[ "${CANDIDATE_PATH##*.}" != "go" ]]; then
  echo "Candidate file must be a Go file ending in .go: $CANDIDATE_PATH" >&2
  exit 1
fi

if [[ ! -f "$MANIFEST_PATH" ]]; then
  echo "Manifest file not found: $MANIFEST_PATH" >&2
  exit 1
fi

BASE_NAME="$(basename "$CANDIDATE_PATH")"
TOOL_STEM="${BASE_NAME%.go}"
TOOL_STEM="${TOOL_STEM%_candidate}"
TOOL_STEM="${TOOL_STEM%_generated}"
PROMOTED_PATH="dettools/${TOOL_STEM}.go"

if [[ "$TOOL_STEM" != "$TOOL_NAME" ]]; then
  echo "Tool stem inferred from candidate file is '$TOOL_STEM' (different from --tool='$TOOL_NAME')." >&2
  TOOL_NAME="$TOOL_STEM"
  PROMOTED_PATH="dettools/${TOOL_NAME}.go"
fi

for file in "${REQUIRED_SKILLS[@]}"; do
  if [[ ! -f "$file" ]]; then
    echo "Required skill file missing: $file" >&2
    exit 1
  fi
 done

if [[ "$SKIP_IMPORT" -eq 0 ]] && ! command -v multica >/dev/null 2>&1; then
  echo "multica CLI not found. Use --skip-import if you intentionally want to defer import." >&2
  exit 1
fi

if [[ -f "$PROMOTED_PATH" ]] && [[ "$FORCE" -eq 0 ]]; then
  echo "Destination already exists: $PROMOTED_PATH (use --force to overwrite)." >&2
  exit 1
fi

if [[ -f "$PROMOTED_PATH" ]] && [[ "$FORCE" -eq 1 ]]; then
  cp -f "$PROMOTED_PATH" "${PROMOTED_PATH}.backup.$(date +%s)"
fi

MANIFEST_UPDATE=$(python3 - "$MANIFEST_PATH" "$CANDIDATE_PATH" "$TOOL_NAME" "$APPROVE_REF" "$APPROVED_BY" "$COMMIT_SHA" "$DRY_RUN" <<'PY'
import json
import os
import sys
from datetime import datetime, timezone

manifest_path, candidate_path, tool_name, approve_ref, approved_by, commit_sha, dry_run = sys.argv[1:8]

with open(manifest_path, "r", encoding="utf-8") as f:
    manifest = json.load(f)

items = manifest.get("items")
if not isinstance(items, list):
    raise SystemExit("Manifest does not contain items array")

candidate_base = os.path.basename(candidate_path)


def now_iso():
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")

match = None
for item in items:
    if not isinstance(item, dict):
        continue
    if item.get("tool_file") == candidate_base and item.get("tool_name") == tool_name:
        match = item
        break
    if item.get("tool_name") == tool_name and item.get("status") == "promoted":
        # avoid duplicate promotions on same loop iteration
        match = item
        break
    if item.get("tool_name") == tool_name:
        match = item
        break
    if item.get("tool_file") == candidate_base:
        match = item
        break

payload = {
    "tool_name": tool_name,
    "tool_file": candidate_base,
    "status": "promoted",
    "promoted_at": now_iso(),
    "human_approve_ref": approve_ref,
    "approved_by": approved_by,
    "source_candidate_path": candidate_path,
}
if commit_sha:
    payload["git_commit"] = commit_sha

if match is None:
    payload["generated_at"] = now_iso()
    payload["notes"] = ["auto-added by scripts/stage8-promote.sh"]
    items.append(payload)
else:
    match.update(payload)

summary = {
    "tool_name": tool_name,
    "candidate_file": candidate_base,
    "manifest_path": manifest_path,
    "matched": match is not None,
    "status": "promoted",
    "dry_run": dry_run == "1",
}

if dry_run != "1":
    with open(manifest_path, "w", encoding="utf-8") as f:
        json.dump(manifest, f, indent=2)
        f.write("\n")

print(json.dumps(summary))
PY
)

if [[ -z "$MANIFEST_UPDATE" ]]; then
  echo "Manifest update failed" >&2
  exit 1
fi

echo "Manifest update: $MANIFEST_UPDATE"

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "--dry-run: skipping move/import/diagnostics append"
  echo "Candidate: $CANDIDATE_PATH"
  echo "Destination: $PROMOTED_PATH"
  exit 0
fi

mv -f "$CANDIDATE_PATH" "$PROMOTED_PATH"
echo "Moved candidate: $CANDIDATE_PATH -> $PROMOTED_PATH"

action_import=0
if [[ "$SKIP_IMPORT" -eq 0 ]]; then
  if command -v multica >/dev/null 2>&1; then
    multica dettool import-file "$PROMOTED_PATH" --output table
    action_import=1
  else
    echo "multica CLI not found. Use --skip-import if you intentionally want to defer import." >&2
    exit 1
  fi
fi

mkdir -p "$(dirname "$DIAG_PATH")"
PROMOTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
python3 - "$DIAG_PATH" "$TOOL_NAME" "$CANDIDATE_PATH" "$PROMOTED_PATH" "$COMMIT_SHA" "$APPROVE_REF" "$APPROVED_BY" "${MANIFEST_PATH}" "$PROMOTED_AT" "$action_import" <<'PY'
import json
import sys

path, tool_name, source, dest, commit_sha, approve_ref, approved_by, manifest_path, ts, imported = sys.argv[1:]

entry = {
  "ts": ts,
  "event": "stage8_promotion",
  "tool_name": tool_name,
  "candidate_source": source,
  "promoted_tool": dest,
  "manifest_path": manifest_path,
  "approved_by": approved_by,
  "approve_ref": approve_ref,
  "commit_sha": commit_sha,
  "imported": bool(int(imported)),
  "tool_step": "stage8_promote_script",
}

with open(path, "a", encoding="utf-8") as f:
    f.write(json.dumps(entry))
    f.write("\n")


print(f"Diagnostics appended: {path}")
PY

if [[ ! "$COMPARISON_WINDOW_HOURS" =~ ^[0-9]+$ ]] || [[ "$COMPARISON_WINDOW_HOURS" -le 0 ]]; then
  echo "--comparison-window-hours must be a positive integer" >&2
  exit 1
fi

if [[ ! "$REEVALUATE_DAYS" =~ ^[0-9]+$ ]] || [[ "$REEVALUATE_DAYS" -le 0 ]]; then
  echo "--reevaluate-days must be a positive integer" >&2
  exit 1
fi

STAGE8_ARGS=(
  ail stage8
  --promotion-log "$DIAG_PATH"
  --index-path "$EVENTS_INDEX_PATH"
  --diagnostics-dir "$(dirname "$DIAG_PATH")"
  --tool "$TOOL_NAME"
  --approve-ref "$APPROVE_REF"
  --promoted-at "$PROMOTED_AT"
  --comparison-window-hours "$COMPARISON_WINDOW_HOURS"
  --reevaluate-days "$REEVALUATE_DAYS"
  --output table
)
if [[ -n "$CANDIDATE_DECISION_PATH" ]]; then
  STAGE8_ARGS+=(--candidate-decision-input "$CANDIDATE_DECISION_PATH")
fi
multica "${STAGE8_ARGS[@]}"

echo "Stage-8 promotion complete for: $TOOL_NAME"
