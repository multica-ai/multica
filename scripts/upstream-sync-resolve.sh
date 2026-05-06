#!/usr/bin/env bash
#
# upstream-sync-resolve.sh
#
# Auto-resolves predictable categories of merge conflicts during a cerebro-fork
# upstream sync. Run this AFTER `git merge upstream/main` has produced a
# conflict state.
#
# The script never pushes, never merges to main, and refuses to run on a
# detached HEAD or outside an active merge.
#
# Categories handled (in order):
#
#   1. Purge docs/i18n stack — for every path under
#      apps/{web/docs, web/content/docs, web/app/docs, docs} and the
#      explicit single-file list (architecture-diagram.tsx,
#      docs-settings.tsx, editorial.tsx, hero.tsx, mermaid.tsx, lib/i18n.ts,
#      lib/site.ts, lib/translations.ts, middleware.ts), `git rm` the file
#      regardless of unmerged status. The fork has chosen to drop the
#      multica docs site entirely; cerebro docs land elsewhere.
#
#   2. Both-deleted catch-all — any other path where both sides deleted the
#      same file (status DD) and that wasn't already handled by category 1.
#
#   3. UU sqlc-generated Go files in server/pkg/db/generated/ — runs
#      `git checkout --ours` then regenerates with `make sqlc`. Skipped if any
#      .sql query file is still in conflict (resolve queries manually first).
#
#   4. pnpm-lock.yaml — runs `git checkout --theirs` then regenerates with
#      `pnpm install`. Skipped if any package.json file is still in conflict.
#
# See docs/upstream-sync/COMPLETION-REPORT.md for the analysis that motivated
# the four-category split.
#
# Exit codes:
#   0  Completed (dry-run or apply)
#   1  Safety check failed
#   2  Bad argument

set -euo pipefail

DRY_RUN=true
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

usage() {
  cat <<'EOF'
Usage: upstream-sync-resolve.sh [--dry-run | --apply | --help]

Auto-resolves predictable conflict categories during an upstream sync.
Default mode is --dry-run; --apply must be explicit.

Run AFTER `git merge upstream/main` has produced a conflict state.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=true; shift ;;
    --apply)   DRY_RUN=false; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

# ---------- Safety checks ----------

BRANCH="$(git symbolic-ref --short -q HEAD || true)"
if [[ -z "$BRANCH" ]]; then
  echo "FATAL: detached HEAD. Refusing to operate." >&2
  exit 1
fi
if [[ "$BRANCH" == "main" || "$BRANCH" == "master" ]]; then
  echo "FATAL: on branch '$BRANCH'. Refusing to auto-resolve on main/master." >&2
  exit 1
fi

MERGE_HEAD_PATH="$(git rev-parse --git-path MERGE_HEAD)"
if [[ ! -f "$MERGE_HEAD_PATH" ]]; then
  echo "FATAL: no merge in progress (MERGE_HEAD missing at $MERGE_HEAD_PATH)." >&2
  echo "       Run 'git merge upstream/main' first." >&2
  exit 1
fi

mode="DRY-RUN"
[[ "$DRY_RUN" == "false" ]] && mode="APPLY"
echo "==> Branch: $BRANCH    Mode: $mode"
echo

# ---------- Helpers ----------

# Run-or-echo wrapper for write operations.
do_cmd() {
  if [[ "$DRY_RUN" == "true" ]]; then
    printf '    [dry-run] %s\n' "$*"
  else
    printf '    >> %s\n' "$*"
    "$@"
  fi
}

count_conflicts() {
  git status --porcelain | grep -cE '^(DD|AU|UD|UA|DU|AA|UU) ' || true
}

# Read conflict paths matching a 2-char status code into the global array
# `READ_PATHS_OUT`. Bash 3.2 has no nameref, so we use a single shared global.
# Usage: read_paths_with_status DD; then iterate "${READ_PATHS_OUT[@]}"
READ_PATHS_OUT=()
read_paths_with_status() {
  local status="$1"
  READ_PATHS_OUT=()
  local line
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    READ_PATHS_OUT+=("${line:3}")
  done < <(git status --porcelain | grep -E "^${status} " || true)
}

# ---------- Snapshot ----------

TOTAL_BEFORE="$(count_conflicts)"
echo "==> Conflict files before: $TOTAL_BEFORE"
echo

# ---------- Category 1: Purge docs/i18n stack ----------
echo "==> Category 1: Purge docs/i18n stack (any conflict status)"

# Paths that the fork has chosen to drop entirely. For these, any unmerged
# state resolves to "delete it" — we don't follow upstream's docs evolution.
purge_re='^(apps/web/docs/|apps/web/content/docs/|apps/web/app/docs/|apps/docs/|apps/web/components/(architecture-diagram|docs-settings|editorial|hero|mermaid)\.tsx$|apps/web/lib/(i18n|site|translations)\.ts$|apps/web/middleware\.ts$)'

# Walk every unmerged path; pick the ones that match purge_re.
purge_paths=()
while IFS= read -r line; do
  [[ -z "$line" ]] && continue
  status="${line:0:2}"
  case "$status" in
    DD|AU|UD|UA|DU|AA|UU)
      path="${line:3}"
      if [[ "$path" =~ $purge_re ]]; then
        purge_paths+=("$path")
      fi
      ;;
  esac
done < <(git status --porcelain)

cat1_resolved="${#purge_paths[@]}"
if [[ $cat1_resolved -gt 0 ]]; then
  for path in "${purge_paths[@]}"; do
    do_cmd git rm -f -- "$path"
  done
fi
echo "  Purged: $cat1_resolved"
echo

# ---------- Category 2: Both-deleted catch-all ----------
echo "==> Category 2: Both-deleted catch-all (status DD, outside purge paths)"

read_paths_with_status "DD"

dd_remaining=()
if [[ ${#READ_PATHS_OUT[@]} -gt 0 ]]; then
  for path in "${READ_PATHS_OUT[@]}"; do
    if [[ ! "$path" =~ $purge_re ]]; then
      dd_remaining+=("$path")
    fi
  done
fi

cat2_resolved="${#dd_remaining[@]}"
if [[ $cat2_resolved -gt 0 ]]; then
  for path in "${dd_remaining[@]}"; do
    do_cmd git rm -f -- "$path"
  done
fi
echo "  Handled: $cat2_resolved"
echo

# ---------- Category 3: sqlc-generated Go files ----------
echo "==> Category 3: sqlc-generated Go files"

sql_uu_count="$(git status --porcelain \
  | awk '$1=="UU" && $2 ~ /^server\/pkg\/db\/queries\/.*\.sql$/' \
  | wc -l | tr -d ' ')"

cat3_resolved=0
cat3_skipped=false

if [[ "$sql_uu_count" -gt 0 ]]; then
  echo "  STOP: $sql_uu_count .sql query file(s) still in conflict (UU)."
  echo "        Resolve server/pkg/db/queries/*.sql manually first, then re-run."
  cat3_skipped=true
else
  gen_paths=()
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    gen_paths+=("${line:3}")
  done < <(git status --porcelain \
    | grep -E '^UU server/pkg/db/generated/.*\.sql\.go$' || true)

  cat3_resolved="${#gen_paths[@]}"
  if [[ $cat3_resolved -gt 0 ]]; then
    for path in "${gen_paths[@]}"; do
      do_cmd git checkout --ours -- "$path"
      do_cmd git add -- "$path"
    done
    do_cmd make sqlc
    if [[ "$DRY_RUN" == "false" ]]; then
      git add server/pkg/db/generated/ 2>/dev/null || true
    fi
  fi
  echo "  Handled: $cat3_resolved"
fi
echo

# ---------- Category 4: pnpm-lock.yaml ----------
echo "==> Category 4: pnpm-lock.yaml"

cat4_resolved=0

if [[ "$cat3_skipped" == "true" ]]; then
  echo "  Skipped (depends on Category 3 succeeding first)."
else
  pkg_uu_count="$(git status --porcelain \
    | awk '$1=="UU" && $2 ~ /^(apps\/(web|desktop)|packages\/(views|core|ui))\/package\.json$/' \
    | wc -l | tr -d ' ')"

  if [[ "$pkg_uu_count" -gt 0 ]]; then
    echo "  STOP: $pkg_uu_count package.json file(s) still in conflict (UU)."
    echo "        Resolve apps/{web,desktop}/package.json + packages/{views,core,ui}/package.json first."
  else
    lock_status="$(git status --porcelain pnpm-lock.yaml 2>/dev/null | awk '{print $1}' | head -n1 || true)"
    if [[ "$lock_status" == "UU" || "$lock_status" == "AA" ]]; then
      do_cmd git checkout --theirs -- pnpm-lock.yaml
      do_cmd git add -- pnpm-lock.yaml
      do_cmd rm -f pnpm-lock.yaml
      do_cmd pnpm install --no-frozen-lockfile
      do_cmd git add -- pnpm-lock.yaml
      cat4_resolved=1
      echo "  Handled: 1"
    else
      echo "  pnpm-lock.yaml not in conflict (status='${lock_status:-clean}'); skipping."
    fi
  fi
fi
echo

# ---------- Final report ----------

TOTAL_AFTER="$(count_conflicts)"
WOULD_RESOLVE=$((cat1_resolved + cat2_resolved + cat3_resolved + cat4_resolved))

echo "================================================================"
printf "  %-32s %s\n" "Mode:"                       "$mode"
printf "  %-32s %d\n" "Conflict files before:"      "$TOTAL_BEFORE"
printf "  %-32s %d\n" "Cat 1 (UA-deletable):"        "$cat1_resolved"
printf "  %-32s %d\n" "Cat 2 (DD):"                  "$cat2_resolved"
printf "  %-32s %d\n" "Cat 3 (sqlc-generated):"      "$cat3_resolved"
printf "  %-32s %d\n" "Cat 4 (pnpm-lock):"           "$cat4_resolved"
printf "  %-32s %d\n" "Auto-resolved (this run):"    "$WOULD_RESOLVE"
printf "  %-32s %d\n" "Conflict files after:"        "$TOTAL_AFTER"
echo "================================================================"

if [[ "$DRY_RUN" == "true" ]]; then
  echo
  echo "Dry-run only — no changes applied. Re-run with --apply to execute."
fi
