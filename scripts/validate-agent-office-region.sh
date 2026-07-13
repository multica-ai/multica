#!/usr/bin/env bash
#
# validate-agent-office-region.sh — CI write-gate for the Agent Office managed
# region in repo CLAUDE.md/AGENTS.md files (FIR-1775,
# docs/agents/agent-office.md §10).
#
# The managed region is delimited by markers, and the start marker seals the
# body with a checksum:
#
#   <!-- agent-office:start vN sha256:<64-hex> -->
#   ...governed body lines...
#   <!-- agent-office:end -->
#
# Canonical form (must stay byte-identical to
# server/internal/cerebro/agentoffice/managed_region.go): the body is the
# lines strictly between the marker lines, each stripped of a trailing CR and
# terminated by LF; the seal is the SHA-256 hex of that string.
#
# Algorithm:
#   1. Resolve BASE = ${BASE_REF:-origin/main}.
#   2. List CLAUDE.md/AGENTS.md files changed between BASE...HEAD.
#   3. For each file, validate the HEAD version:
#        - marker structure (one start, one end, start before end)
#        - the seal matches the body (a hand-edit breaks it)
#      and compare against the BASE version:
#        - a managed region may not be removed
#        - a body change requires a version bump (vN strictly greater)
#        - the version may never decrease
#      Files without a managed region are never touched — the human-owned
#      part of the file stays free.
#   4. Opt-out for humans: env AGENT_OFFICE_ALLOW_REGION_EDIT=1, or an
#      "AGENT-OFFICE-ALLOW-REGION:" token in a commit message on BASE..HEAD.
#   5. Exit 0 if clean, 1 otherwise (stdout lists offenders).
#
# The legitimate write path is `multica agent context region-sync`, which
# re-renders the seal and bumps the version.
#
# Flags:
#   --help    Print usage, exit 0.
#   --test    Run a self-test in a temp git repo, exit 0/1 based on result.

set -euo pipefail

START_RE='^<!-- agent-office:start v[0-9]+ sha256:[0-9a-f]{64} -->$'
END_RE='^<!-- agent-office:end -->$'
LOOSE_START_RE='agent-office:start'

usage() {
  cat <<'EOF'
Usage: validate-agent-office-region.sh [--help|--test]

CI write-gate for the Agent Office managed region in CLAUDE.md/AGENTS.md.

Environment:
  BASE_REF                        Git ref to diff against (default: origin/main).
  AGENT_OFFICE_ALLOW_REGION_EDIT  If set to "1", bypass the check (logs a warning).

Exit codes:
  0  All managed-region changes are properly sealed and version-bumped.
  1  One or more violations (hand-edit, removal, missing version bump).
EOF
}

sha256_cmd() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum | awk '{print $1}'
  else
    shasum -a 256 | awk '{print $1}'
  fi
}

# parse_region <file-with-content>
# Sets: R_STATE (none|ok|malformed), R_VERSION, R_SEAL, R_BODY_SHA, R_ERROR.
parse_region() {
  local f="$1" norm
  norm="$(mktemp)"
  sed 's/\r$//' "$f" > "$norm"

  local starts ends
  starts=$(grep -cE "$START_RE" "$norm" || true)
  ends=$(grep -cE "$END_RE" "$norm" || true)
  local loose
  loose=$(grep -c "$LOOSE_START_RE" "$norm" || true)

  R_STATE="none" R_VERSION="" R_SEAL="" R_BODY_SHA="" R_ERROR=""
  if [[ "$starts" -eq 0 && "$ends" -eq 0 ]]; then
    if [[ "$loose" -gt 0 ]] && grep -qE '^<!--.*agent-office:start' "$norm"; then
      R_STATE="malformed"; R_ERROR="malformed agent-office:start marker (expected '<!-- agent-office:start vN sha256:<64-hex> -->')"
    fi
    rm -f "$norm"; return 0
  fi
  if [[ "$starts" -ne 1 || "$ends" -ne 1 ]]; then
    R_STATE="malformed"; R_ERROR="expected exactly one start and one end marker (found $starts start, $ends end)"
    rm -f "$norm"; return 0
  fi
  local start_line end_line
  start_line=$(grep -nE "$START_RE" "$norm" | head -1 | cut -d: -f1)
  end_line=$(grep -nE "$END_RE" "$norm" | head -1 | cut -d: -f1)
  if [[ "$start_line" -gt "$end_line" ]]; then
    R_STATE="malformed"; R_ERROR="agent-office:end appears before agent-office:start"
    rm -f "$norm"; return 0
  fi
  R_VERSION=$(sed -nE 's/^<!-- agent-office:start v([0-9]+) sha256:[0-9a-f]{64} -->$/\1/p' "$norm" | head -1)
  R_SEAL=$(sed -nE 's/^<!-- agent-office:start v[0-9]+ sha256:([0-9a-f]{64}) -->$/\1/p' "$norm" | head -1)
  R_BODY_SHA=$(awk -v s="$start_line" -v e="$end_line" 'NR > s && NR < e' "$norm" | sha256_cmd)
  R_STATE="ok"
  rm -f "$norm"
}

# region_at_rev <rev> <path> — parse_region on the file as of a git rev.
# Sets R_EXISTS=0 when the file does not exist at that rev.
region_at_rev() {
  local rev="$1" path="$2" tmp
  tmp="$(mktemp)"
  if git show "$rev:$path" > "$tmp" 2>/dev/null; then
    R_EXISTS=1
    parse_region "$tmp"
  else
    R_EXISTS=0
    R_STATE="none" R_VERSION="" R_SEAL="" R_BODY_SHA="" R_ERROR=""
  fi
  rm -f "$tmp"
}

run_check() {
  local base="${BASE_REF:-origin/main}"

  if [[ "${AGENT_OFFICE_ALLOW_REGION_EDIT:-}" == "1" ]]; then
    echo "WARNING: AGENT_OFFICE_ALLOW_REGION_EDIT=1 — skipping managed-region gate." >&2
    return 0
  fi
  if git log --format=%B "$base..HEAD" 2>/dev/null | grep -q "AGENT-OFFICE-ALLOW-REGION:"; then
    echo "WARNING: AGENT-OFFICE-ALLOW-REGION: token found in commit messages — skipping managed-region gate." >&2
    return 0
  fi

  local violations=0 f
  while IFS= read -r f; do
    case "$(basename "$f")" in
      CLAUDE.md|AGENTS.md) ;;
      *) continue ;;
    esac

    # BASE side first — the gate only owns files that carry (or carried) a region.
    region_at_rev "$base" "$f"
    local base_exists="$R_EXISTS" base_state="$R_STATE" base_version="$R_VERSION" base_body_sha="$R_BODY_SHA"

    region_at_rev "HEAD" "$f"
    local head_exists="$R_EXISTS" head_state="$R_STATE" head_version="$R_VERSION" head_seal="$R_SEAL" head_body_sha="$R_BODY_SHA" head_error="$R_ERROR"

    if [[ "$head_exists" -eq 0 ]]; then
      if [[ "$base_exists" -eq 1 && "$base_state" == "ok" ]]; then
        echo "VIOLATION: $f — file with an Agent Office managed region was deleted. Route the change through 'multica agent context region-sync' or add an AGENT-OFFICE-ALLOW-REGION: token."
        violations=$((violations + 1))
      fi
      continue
    fi

    if [[ "$head_state" == "malformed" ]]; then
      echo "VIOLATION: $f — $head_error."
      violations=$((violations + 1))
      continue
    fi

    if [[ "$head_state" == "none" ]]; then
      if [[ "$base_state" == "ok" ]]; then
        echo "VIOLATION: $f — the Agent Office managed region was removed. It is governed by Agent Office; restore it or use 'multica agent context region-sync' (override: AGENT-OFFICE-ALLOW-REGION: token)."
        violations=$((violations + 1))
      fi
      continue
    fi

    # head_state == ok — verify the seal.
    if [[ "$head_seal" != "$head_body_sha" ]]; then
      echo "VIOLATION: $f — managed-region seal is broken (body was hand-edited without re-sealing). Use 'multica agent context region-sync' to edit the region."
      violations=$((violations + 1))
      continue
    fi

    if [[ "$base_state" == "ok" ]]; then
      if [[ "$head_body_sha" != "$base_body_sha" && "$head_version" -le "$base_version" ]]; then
        echo "VIOLATION: $f — managed-region body changed but the version did not bump (v$base_version → v$head_version). 'multica agent context region-sync' bumps it for you."
        violations=$((violations + 1))
        continue
      fi
      if [[ "$head_version" -lt "$base_version" ]]; then
        echo "VIOLATION: $f — managed-region version decreased (v$base_version → v$head_version)."
        violations=$((violations + 1))
        continue
      fi
    fi
  done < <(git diff --name-only "$base...HEAD" 2>/dev/null || true)

  if [[ "$violations" -gt 0 ]]; then
    echo ""
    echo "$violations managed-region violation(s). The Agent Office managed region in CLAUDE.md/AGENTS.md is edited via 'multica agent context region-sync', never by hand (docs/agents/agent-office.md §10)."
    return 1
  fi
  echo "OK: no managed-region violations."
  return 0
}

seal_line() {
  # seal_line <version> <body-file> — print a valid start marker for the body.
  local version="$1" body_file="$2" sha
  sha=$(sed 's/\r$//' "$body_file" | sha256_cmd)
  printf '<!-- agent-office:start v%s sha256:%s -->\n' "$version" "$sha"
}

self_test() {
  local tmp rc=0
  tmp="$(mktemp -d)"
  trap "rm -rf '$tmp'" EXIT
  local script
  script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"

  cd "$tmp"
  git init -q -b main .
  git config user.email test@test && git config user.name test

  # Base: CLAUDE.md with a sealed v1 region.
  printf 'governed rule one\n' > body.txt
  {
    printf '# CLAUDE.md\n\nBuild: make check\n\n'
    seal_line 1 body.txt
    cat body.txt
    printf '%s\n' '<!-- agent-office:end -->'
  } > CLAUDE.md
  git add CLAUDE.md && git commit -qm base
  git branch -f base-marker

  check() { # check <name> <expected-rc>
    local name="$1" expected="$2" got=0
    BASE_REF=base-marker bash "$script" >/dev/null 2>&1 || got=$?
    if [[ "$got" -ne "$expected" ]]; then
      echo "SELF-TEST FAIL: $name (expected rc=$expected, got rc=$got)"
      rc=1
    else
      echo "self-test ok: $name"
    fi
  }

  # 1. Human edit outside the region → pass.
  git checkout -qb t1 base-marker
  printf '\nMore build notes.\n' >> CLAUDE.md
  # appending after the end marker is outside the region
  git commit -qam t1
  check "human edit outside region passes" 0

  # 2. Hand-edit inside the region without re-sealing → fail.
  git checkout -qb t2 base-marker
  sed -i.bak 's/governed rule one/governed rule tampered/' CLAUDE.md && rm -f CLAUDE.md.bak
  git commit -qam t2
  check "hand-edit without re-seal fails" 1

  # 3. Legitimate sync: new body, new seal, bumped version → pass.
  git checkout -qb t3 base-marker
  printf 'governed rule two\n' > body.txt
  {
    printf '# CLAUDE.md\n\nBuild: make check\n\n'
    seal_line 2 body.txt
    cat body.txt
    printf '%s\n' '<!-- agent-office:end -->'
  } > CLAUDE.md
  git commit -qam t3
  check "sealed + bumped sync passes" 0

  # 4. Body change with valid seal but no version bump → fail.
  git checkout -qb t4 base-marker
  printf 'governed rule two\n' > body.txt
  {
    printf '# CLAUDE.md\n\nBuild: make check\n\n'
    seal_line 1 body.txt
    cat body.txt
    printf '%s\n' '<!-- agent-office:end -->'
  } > CLAUDE.md
  git commit -qam t4
  check "sealed but unbumped change fails" 1

  # 5. Region removed → fail.
  git checkout -qb t5 base-marker
  printf '# CLAUDE.md\n\nBuild: make check\n' > CLAUDE.md
  git commit -qam t5
  check "region removal fails" 1

  # 6. Region removed with override token → pass.
  git checkout -qb t6 base-marker
  printf '# CLAUDE.md\n\nBuild: make check\n' > CLAUDE.md
  git commit -qam "t6

AGENT-OFFICE-ALLOW-REGION: intentional restructure"
  check "override token passes" 0

  # 7. File without any region stays free → pass.
  git checkout -qb t7 base-marker
  printf '# AGENTS.md\n\nCode facts only.\n' > AGENTS.md
  git add AGENTS.md && git commit -qm t7
  check "region-less file stays free" 0

  return "$rc"
}

case "${1:-}" in
  --help) usage; exit 0 ;;
  --test) self_test; exit $? ;;
  "") run_check; exit $? ;;
  *) echo "unknown flag: $1" >&2; usage; exit 2 ;;
esac
