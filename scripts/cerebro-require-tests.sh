#!/usr/bin/env bash
#
# cerebro-require-tests.sh — enforce "a logic change ships with a test".
#
# Policy (FIR-2610, verification-before-completion skill): the proof that code
# works is an automated test, not a screenshot. A pull request that changes
# production source (logic) but adds/changes NO test is blocked. Docs, config,
# generated files, migrations and styling are exempt. A logic change that
# genuinely needs no test opts out explicitly (label / env / commit token) so
# the exemption is visible and justified — this is NOT "every PR needs a test".
#
# Algorithm:
#   1. Determine the changed file set:
#        - If CHANGED_FILES is set (newline-separated), use it verbatim
#          (used by the self-test and for local runs).
#        - Otherwise list files changed between BASE...HEAD with git, where
#          BASE = ${BASE_REF:-origin/main}.
#   2. Classify each file: test / exempt / production-source.
#   3. PASS if there are no production-source changes, OR at least one test
#      file was added/changed. FAIL if production source changed with no test.
#   4. Allow opt-out (PASS with a warning) via:
#        - env CEREBRO_ALLOW_NO_TEST=1 (the CI job sets this from a
#          `no-test-needed` PR label), or
#        - a "CEREBRO-ALLOW-NO-TEST:" token in any commit message on BASE..HEAD.
#   5. Exit 0 if clean, 1 otherwise (stdout explains why).
#
# Flags:
#   --help    Print usage, exit 0.
#   --test    Run the hermetic self-test, exit 0/1 based on result.

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: cerebro-require-tests.sh [--help|--test]

Blocks a PR that changes production source (logic) without adding/changing a test.

Environment:
  BASE_REF                Git ref to diff against (default: origin/main).
  CHANGED_FILES           Newline-separated file list to classify instead of
                          computing a git diff (used by --test and local runs).
  CEREBRO_ALLOW_NO_TEST   If "1", bypass the gate (logs a warning). The CI job
                          sets this when the PR carries the `no-test-needed` label.

Exit codes:
  0  No production-source change, or a test accompanies the change, or opted out.
  1  Production source changed with no accompanying test and no opt-out.
EOF
}

# --- classifiers -------------------------------------------------------------

# A test file: Go *_test.go, JS/TS *.test.* / *.spec.*, or anything under e2e/.
is_test_file() {
  local f="$1"
  case "$f" in
    *_test.go) return 0 ;;
    *.test.ts | *.test.tsx | *.test.js | *.test.jsx | *.test.mjs) return 0 ;;
    *.spec.ts | *.spec.tsx | *.spec.js | *.spec.jsx | *.spec.mjs) return 0 ;;
    e2e/* | */e2e/*) return 0 ;;
    *) return 1 ;;
  esac
}

# Exempt: never requires a test on its own (docs, config, generated, styling,
# migrations, tooling). Checked AFTER is_test_file.
is_exempt() {
  local f="$1"
  # Directory-based exemptions (tooling, docs, generated, fixtures).
  case "$f" in
    .github/* | scripts/* | docs/* | graphify-out/*) return 0 ;;
    */generated/* | */__generated__/* | */testdata/* | */node_modules/*) return 0 ;;
  esac
  # Generated / declaration / config source files that carry no hand-written logic.
  case "$f" in
    *.gen.ts | *.gen.tsx | *_gen.go | *.pb.go | *.d.ts) return 0 ;;
    *.config.ts | *.config.js | *.config.mjs | *.config.cjs) return 0 ;;
    *.stories.ts | *.stories.tsx) return 0 ;;
    */reserved-slugs.ts) return 0 ;;
  esac
  # Extension-based exemptions.
  case "$f" in
    *.md | *.mdx | *.txt) return 0 ;;
    *.json | *.yml | *.yaml | *.toml | *.lock | *.sum | *.mod) return 0 ;;
    *.sql) return 0 ;;
    *.css | *.scss | *.sass | *.less) return 0 ;;
    *.svg | *.png | *.jpg | *.jpeg | *.gif | *.ico | *.webp | *.woff | *.woff2 | *.ttf) return 0 ;;
    *.sh | *.bash) return 0 ;;
    *.example | .env* | Dockerfile* | docker-compose*) return 0 ;;
  esac
  return 1
}

# Production source: a hand-written logic file that should be covered by a test.
# Only reached after test + exempt filters, so this is "a real source ext".
is_source() {
  local f="$1"
  case "$f" in
    *.go | *.ts | *.tsx | *.js | *.jsx | *.mjs) return 0 ;;
    *) return 1 ;;
  esac
}

# --- gate --------------------------------------------------------------------

# classify_changes <file-list-on-stdin> -> sets globals: SRC_FILES, TEST_COUNT
classify_changes() {
  SRC_FILES=()
  TEST_COUNT=0
  local f
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    if is_test_file "$f"; then
      TEST_COUNT=$((TEST_COUNT + 1))
      continue
    fi
    if is_exempt "$f"; then
      continue
    fi
    if is_source "$f"; then
      SRC_FILES+=("$f")
    fi
  done
}

opted_out() {
  if [ "${CEREBRO_ALLOW_NO_TEST:-}" = "1" ]; then
    echo "opt-out: CEREBRO_ALLOW_NO_TEST=1 (no-test-needed label or explicit env)"
    return 0
  fi
  # Commit-message token on the range.
  local base="${BASE_REF:-origin/main}"
  if git rev-parse --verify -q "$base" >/dev/null 2>&1; then
    if git log --format='%B' "$base"..HEAD 2>/dev/null | grep -q 'CEREBRO-ALLOW-NO-TEST:'; then
      echo "opt-out: CEREBRO-ALLOW-NO-TEST: token found in a commit message"
      return 0
    fi
  fi
  return 1
}

collect_changed_files() {
  if [ -n "${CHANGED_FILES:-}" ]; then
    printf '%s\n' "$CHANGED_FILES"
    return
  fi
  local base="${BASE_REF:-origin/main}"
  git diff --name-only --diff-filter=ACM "$base"...HEAD
}

run_gate() {
  classify_changes < <(collect_changed_files)

  if [ "${#SRC_FILES[@]}" -eq 0 ]; then
    echo "✓ require-tests: no production-source changes — nothing to gate."
    return 0
  fi
  if [ "$TEST_COUNT" -gt 0 ]; then
    echo "✓ require-tests: ${#SRC_FILES[@]} source file(s) changed, accompanied by $TEST_COUNT test change(s)."
    return 0
  fi

  # Production source changed with no test.
  local reason
  if reason="$(opted_out)"; then
    echo "⚠ require-tests: ${#SRC_FILES[@]} source file(s) changed with NO test, but $reason. Allowed."
    return 0
  fi

  echo "✗ require-tests: these production-source files changed with NO accompanying test:"
  local f
  for f in "${SRC_FILES[@]}"; do
    echo "    - $f"
  done
  cat <<'EOF'

A logic change must ship with a test that fails before the change and passes
after (see the verification-before-completion skill). Do ONE of:
  1. Add or update a test (Go *_test.go, or *.test.ts / *.spec.ts / e2e/*.spec.ts).
  2. If this change genuinely needs no test (pure refactor, config-like code),
     add the `no-test-needed` label to the PR, OR put a line
     "CEREBRO-ALLOW-NO-TEST: <why>" in a commit message.
EOF
  return 1
}

# --- self-test ---------------------------------------------------------------

self_test() {
  local fails=0
  # expect_gate <expected-exit> <label> <env-assignments> <<changed files>>
  _case() {
    local expected="$1" label="$2"; shift 2
    local out rc
    # Run the gate in a subshell with the provided CHANGED_FILES / env.
    out="$(CEREBRO_ALLOW_NO_TEST="${CEREBRO_ALLOW_NO_TEST_CASE:-}" CHANGED_FILES="$FILES_CASE" run_gate 2>&1)" && rc=0 || rc=$?
    if [ "$rc" -ne "$expected" ]; then
      echo "FAIL [$label]: expected exit $expected, got $rc"
      echo "$out" | sed 's/^/    /'
      fails=$((fails + 1))
    else
      echo "ok   [$label] (exit $rc)"
    fi
  }

  FILES_CASE=$'server/internal/handler/comment.go'; CEREBRO_ALLOW_NO_TEST_CASE=''
  _case 1 "go logic, no test -> FAIL"

  FILES_CASE=$'server/internal/handler/comment.go\nserver/internal/handler/comment_test.go'; CEREBRO_ALLOW_NO_TEST_CASE=''
  _case 0 "go logic + go test -> PASS"

  FILES_CASE=$'apps/web/src/notifications.ts'; CEREBRO_ALLOW_NO_TEST_CASE=''
  _case 1 "ts logic, no test -> FAIL"

  FILES_CASE=$'apps/web/src/notifications.ts\ne2e/notifications.spec.ts'; CEREBRO_ALLOW_NO_TEST_CASE=''
  _case 0 "ts logic + e2e spec -> PASS"

  FILES_CASE=$'apps/web/src/foo.ts\napps/web/src/foo.test.ts'; CEREBRO_ALLOW_NO_TEST_CASE=''
  _case 0 "ts logic + unit test -> PASS"

  FILES_CASE=$'README.md\ndocs/guide.md'; CEREBRO_ALLOW_NO_TEST_CASE=''
  _case 0 "docs only -> PASS"

  FILES_CASE=$'.github/workflows/ci.yml\npackage.json'; CEREBRO_ALLOW_NO_TEST_CASE=''
  _case 0 "config only -> PASS"

  FILES_CASE=$'server/migrations/9001_cerebro_x.sql'; CEREBRO_ALLOW_NO_TEST_CASE=''
  _case 0 "migration only -> PASS"

  FILES_CASE=$'server/pkg/db/generated/queries.go'; CEREBRO_ALLOW_NO_TEST_CASE=''
  _case 0 "generated go only -> PASS"

  FILES_CASE=$'scripts/deploy.sh'; CEREBRO_ALLOW_NO_TEST_CASE=''
  _case 0 "shell script only -> PASS (tooling exempt)"

  FILES_CASE=$'packages/cerebro-feature-flags/registry.ts'; CEREBRO_ALLOW_NO_TEST_CASE='1'
  _case 0 "ts logic, no test, opt-out env -> PASS"

  FILES_CASE=$'apps/web/src/x.stories.tsx\napps/web/src/x.config.ts'; CEREBRO_ALLOW_NO_TEST_CASE=''
  _case 0 "stories + config source -> PASS (exempt)"

  FILES_CASE=$'server/internal/handler/comment_test.go'; CEREBRO_ALLOW_NO_TEST_CASE=''
  _case 0 "test-only change -> PASS"

  if [ "$fails" -gt 0 ]; then
    echo "self-test: $fails case(s) failed"
    return 1
  fi
  echo "self-test: all cases passed"
  return 0
}

main() {
  case "${1:-}" in
    --help | -h) usage; exit 0 ;;
    --test) self_test; exit $? ;;
    "") run_gate; exit $? ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
}

main "$@"
