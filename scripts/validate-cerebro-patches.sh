#!/usr/bin/env bash
#
# validate-cerebro-patches.sh — enforce CEREBRO-PATCH markers on upstream-zone diffs.
#
# Algorithm (per docs/upstream-sync/preflight/P6.md):
#   1. Resolve BASE = ${BASE_REF:-origin/main}.
#   2. List files changed between BASE...HEAD with `git diff --name-only`.
#   3. For each file, check it against scripts/cerebro-zones.txt — keep only
#      files inside the upstream zone (includes minus excludes).
#   4. For each kept file, run `git diff BASE...HEAD -- <file>` and confirm
#      at least one added line (^+, not ^+++) contains "CEREBRO-PATCH(".
#   5. Allow opt-out via env CEREBRO_ALLOW_NO_PATCH=1, or a
#      "CEREBRO-ALLOW-NO-PATCH:" token anywhere in commit messages on the
#      range BASE..HEAD.
#   6. Exit 0 if clean, 1 otherwise (stdout lists offenders).
#
# Flags:
#   --help    Print usage, exit 0.
#   --test    Run a self-test in a temp git repo, exit 0/1 based on result.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ZONES_FILE="${ZONES_FILE:-$SCRIPT_DIR/cerebro-zones.txt}"

usage() {
  cat <<'EOF'
Usage: validate-cerebro-patches.sh [--help|--test]

Enforces CEREBRO-PATCH(<name>): markers on upstream-zone diffs.

Environment:
  BASE_REF                 Git ref to diff against (default: origin/main).
  CEREBRO_ALLOW_NO_PATCH   If set to "1", bypass the check (logs a warning).
  ZONES_FILE               Path to zone glob list (default: scripts/cerebro-zones.txt).

Exit codes:
  0  All upstream-zone changes are marked.
  1  One or more upstream-zone files lack a CEREBRO-PATCH marker.
EOF
}

# Translate a shell-style glob into an anchored ERE regex.
# Supports: ** (anything incl. /), * (no /), ? (single non-/ char), literal others.
glob_to_regex() {
  local glob="$1" out="" i ch next
  for ((i = 0; i < ${#glob}; i++)); do
    ch="${glob:i:1}"
    next="${glob:i+1:1}"
    case "$ch" in
      '*')
        if [[ "$next" == "*" ]]; then
          out+=".*"; ((i++))
        else
          out+="[^/]*"
        fi
        ;;
      '?') out+="[^/]" ;;
      '.'|'+'|'('|')'|'{'|'}'|'['|']'|'|'|'^'|'$'|'\\') out+="\\$ch" ;;
      *) out+="$ch" ;;
    esac
  done
  printf '^%s$' "$out"
}

# Decide if a file is inside the upstream zone per ZONES_FILE.
in_upstream_zone() {
  local file="$1" line glob is_exclude=0 included=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
    is_exclude=0
    if [[ "${line:0:1}" == "!" ]]; then
      is_exclude=1
      glob="${line:1}"
    else
      glob="$line"
    fi
    local re; re="$(glob_to_regex "$glob")"
    if [[ "$file" =~ $re ]]; then
      if (( is_exclude )); then
        return 1
      else
        included=1
      fi
    fi
  done < "$ZONES_FILE"
  (( included )) && return 0 || return 1
}

# Check if any commit in BASE..HEAD opts out via CEREBRO-ALLOW-NO-PATCH:.
# Use a temp variable instead of `git log | grep -q` because `set -o pipefail`
# combined with grep -q's early-exit can cause git log to receive SIGPIPE and
# the pipeline to report failure even when the match was found.
commit_range_opts_out() {
  local base="$1"
  local log
  log="$(git log --format='%B' "$base..HEAD" 2>/dev/null)"
  [[ "$log" == *'CEREBRO-ALLOW-NO-PATCH:'* ]]
}

run_validation() {
  local base="${BASE_REF:-origin/main}"

  if ! git rev-parse --verify "$base" >/dev/null 2>&1; then
    echo "validate-cerebro-patches: BASE_REF '$base' not found; skipping check." >&2
    return 0
  fi

  if [[ "${CEREBRO_ALLOW_NO_PATCH:-0}" == "1" ]]; then
    echo "validate-cerebro-patches: CEREBRO_ALLOW_NO_PATCH=1 set; skipping (review carefully)." >&2
    return 0
  fi

  if commit_range_opts_out "$base"; then
    echo "validate-cerebro-patches: CEREBRO-ALLOW-NO-PATCH: token found in commit range; skipping." >&2
    return 0
  fi

  local -a offenders=()
  local file diff_added
  while IFS= read -r file; do
    [[ -z "$file" ]] && continue
    in_upstream_zone "$file" || continue
    # Restrict to text diffs; binary/renames-without-content show as "Binary files ... differ".
    diff_added="$(git diff "$base...HEAD" -- "$file" 2>/dev/null \
      | grep -E '^\+' | grep -vE '^\+\+\+' || true)"
    if ! grep -q 'CEREBRO-PATCH(' <<<"$diff_added"; then
      offenders+=("$file")
    fi
  done < <(git diff --name-only "$base...HEAD")

  if (( ${#offenders[@]} == 0 )); then
    echo "validate-cerebro-patches: OK (no upstream-zone files missing CEREBRO-PATCH)."
    return 0
  fi

  echo "validate-cerebro-patches: FAIL — these upstream-zone files lack a CEREBRO-PATCH(<name>): marker on an added line:" >&2
  printf '  - %s\n' "${offenders[@]}" >&2
  echo "" >&2
  echo "Fix: add '// CEREBRO-PATCH(<name>):' (or language-appropriate comment) on the modified lines, OR opt out by adding 'CEREBRO-ALLOW-NO-PATCH:' to a commit message in this PR." >&2
  return 1
}

self_test() {
  local tmp; tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  (
    cd "$tmp"
    git init -q -b main
    git config user.email "test@example.com"
    git config user.name "Test"
    mkdir -p packages/views packages/cerebro-x server scripts
    cp "$ZONES_FILE" scripts/cerebro-zones.txt
    echo "ok" > packages/views/baseline.ts
    git add . && git commit -q -m "baseline"
    git branch base
    # Positive case: upstream-zone file with marker -> OK
    printf 'export const x = 1; // CEREBRO-PATCH(self-test): added\n' > packages/views/marked.ts
    # Cerebro-zone file without marker -> ignored (excluded)
    echo 'ignored' > packages/cerebro-x/anything.ts
    git add . && git commit -q -m "marked + ignored"
    if BASE_REF=base ZONES_FILE="$tmp/scripts/cerebro-zones.txt" \
        bash "$SCRIPT_DIR/validate-cerebro-patches.sh"; then
      echo "self-test step 1 (positive): OK"
    else
      echo "self-test step 1 (positive) FAILED — script flagged a marked file." >&2
      return 1
    fi
    # Negative case: upstream-zone file without marker -> FAIL
    echo 'export const y = 2;' > packages/views/unmarked.ts
    git add . && git commit -q -m "unmarked"
    if BASE_REF=base ZONES_FILE="$tmp/scripts/cerebro-zones.txt" \
        bash "$SCRIPT_DIR/validate-cerebro-patches.sh" 2>/dev/null; then
      echo "self-test step 2 (negative) FAILED — script accepted unmarked file." >&2
      return 1
    else
      echo "self-test step 2 (negative): OK (correctly rejected)"
    fi
    # Opt-out case: CEREBRO-ALLOW-NO-PATCH: token in commit msg -> OK
    git commit --allow-empty -q -m "ship anyway

CEREBRO-ALLOW-NO-PATCH: hot-fix"
    if BASE_REF=base ZONES_FILE="$tmp/scripts/cerebro-zones.txt" \
        bash "$SCRIPT_DIR/validate-cerebro-patches.sh"; then
      echo "self-test step 3 (opt-out): OK"
    else
      echo "self-test step 3 (opt-out) FAILED — script ignored opt-out token." >&2
      return 1
    fi
    echo "self-test: all cases passed"
  )
}

main() {
  case "${1:-}" in
    --help|-h) usage; exit 0 ;;
    --test) self_test; exit $? ;;
    "") run_validation; exit $? ;;
    *) echo "Unknown flag: $1" >&2; usage; exit 2 ;;
  esac
}

main "$@"
