#!/usr/bin/env bash
# Verifies the upstream-sync pipeline scripts: scripts/upstream-sync.sh,
# scripts/sync-tick.sh and scripts/smoke-test-agentrunner.sh.
#
# Runs anywhere bash, git and jq exist — no agentfarm server, no Multica
# credential, no `gh`, no `acli`, no network. It covers the two things a
# reviewer cannot check by eye:
#
#   * the pure helpers (title refs, markdown flattening, throwaway
#     classification, acli key parsing), exercised by calling them; and
#   * the cross-file invariants that keep the pipeline honest — the JIRA mirror
#     never bypassing its timeout wrapper, the smoke script never cancelling the
#     inner issue inline again, and the smoke-result marker shape the dev-side
#     autopilot parses staying byte-identical.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

TICK="scripts/sync-tick.sh"
SYNC="scripts/upstream-sync.sh"
SMOKE="scripts/smoke-test-agentrunner.sh"

FAILURES=0
pass() { printf '  ok   %s\n' "$*"; }
fail() { printf '  FAIL %s\n' "$*"; FAILURES=$(( FAILURES + 1 )); }

expect_eq() {
  local label="$1" want="$2" got="$3"
  if [[ "${want}" == "${got}" ]]; then
    pass "${label}"
  else
    fail "${label}"
    printf '       want: %q\n       got:  %q\n' "${want}" "${got}"
  fi
}

expect_grep() {
  local label="$1" pattern="$2" file="$3"
  if grep -qE -- "${pattern}" "${file}"; then
    pass "${label}"
  else
    fail "${label} (no match for /${pattern}/ in ${file})"
  fi
}

expect_no_grep() {
  local label="$1" pattern="$2" file="$3"
  if grep -qE -- "${pattern}" "${file}"; then
    fail "${label} (unexpected match for /${pattern}/ in ${file})"
    grep -nE -- "${pattern}" "${file}" | sed 's/^/       /'
  else
    pass "${label}"
  fi
}

echo "==> syntax"
for f in "${SYNC}" "${TICK}" "${SMOKE}"; do
  if bash -n "${f}"; then pass "bash -n ${f}"; else fail "bash -n ${f}"; fi
done
if command -v shellcheck >/dev/null 2>&1; then
  for f in "${SYNC}" "${TICK}" "${SMOKE}"; do
    if shellcheck -S error "${f}"; then pass "shellcheck ${f}"; else fail "shellcheck ${f}"; fi
  done
else
  echo "  --   shellcheck not installed, skipped"
fi

# ── scripts/upstream-sync.sh — the PR-title JIRA ref ─────────────────────────
# normalize_jira_ref is lifted out of the script rather than reimplemented here:
# the script cannot be sourced (it syncs a repo on sight), and a copy of the
# logic in the test would pass while the real one rotted.
echo "==> upstream-sync.sh: JIRA ref normalisation"
eval "$(sed -n '/^normalize_jira_ref() {$/,/^}$/p' "${SYNC}")"
if ! declare -F normalize_jira_ref >/dev/null; then
  fail "could not extract normalize_jira_ref from ${SYNC}"
else
  expect_eq "unset JIRA_REF falls back to NO JIRA"  "NO JIRA"     "$(normalize_jira_ref "")"
  expect_eq "whitespace-only falls back"            "NO JIRA"     "$(normalize_jira_ref "   ")"
  expect_eq "a plain key passes through"            "AIPLAT-166"  "$(normalize_jira_ref "AIPLAT-166")"
  expect_eq "brackets are not doubled"              "AIPLAT-166"  "$(normalize_jira_ref "[AIPLAT-166]")"
  expect_eq "surrounding whitespace is trimmed"     "AIPLAT-166"  "$(normalize_jira_ref $'\n AIPLAT-166 \n')"
fi
expect_grep "PR title is built from PR_TITLE" '^  -f title="\$\{PR_TITLE\}" \\$' "${SYNC}"
expect_grep "PR_TITLE carries a bracketed ref" 'PR_TITLE="\$\{SYNC_SUBJECT\} \[\$\(normalize_jira_ref' "${SYNC}"

# ── scripts/sync-tick.sh — pure helpers ──────────────────────────────────────
# Sourcing runs the file's top-level setup (repo root, scratch dir) but not
# main(), and the dependency checks live in require_deps precisely so this works
# on a host with no agent runtime.
echo "==> sync-tick.sh: pure helpers"
# shellcheck disable=SC1090
source "${TICK}"

expect_eq "throwaway: inner agent-claim check → cancelled" \
  "cancelled" "$(sweep_want_status "Tools smoke agent-claim check 1a2d7969 (20260730T074036Z)")"
expect_eq "throwaway: legacy Smoke <ts> → cancelled" \
  "cancelled" "$(sweep_want_status "Smoke 20260718T122105Z")"
expect_eq "throwaway: dispatch ticket → done" \
  "done" "$(sweep_want_status "Tools smoke for v0.4.14 (1a2d7969)")"
expect_eq "not a throwaway: the sync ticket itself" \
  "" "$(sweep_want_status "Upstream sync v0.4.12 → v0.4.14")"
expect_eq "not a throwaway: unrelated work" \
  "" "$(sweep_want_status "dev.yml: serialize the three gitops-deploy writers")"

expect_eq "flatten: markdown link → text (url)" \
  "PR #248 (https://github.com/g2crowd/agentfarm/pull/248) merged" \
  "$(jira_flatten '[PR #248](https://github.com/g2crowd/agentfarm/pull/248) merged')"
expect_eq "flatten: backticks and bold are dropped" \
  "Dev smoke PASS for 1a2d7969" \
  "$(jira_flatten '**Dev smoke PASS** for `1a2d7969`')"
expect_eq "flatten: table rows become readable text" \
  "stage — awaiting_merge" \
  "$(jira_flatten '| stage | `awaiting_merge` |')"

expect_eq "acli key parsing: object response" \
  "AIPLAT-201" "$(jira_key_from '{"key":"AIPLAT-201","id":"1"}')"
expect_eq "acli key parsing: array response" \
  "AIPLAT-202" "$(jira_key_from '[{"key":"AIPLAT-202"}]')"
expect_eq "acli key parsing: non-JSON output still yields the key" \
  "AIPLAT-203" "$(jira_key_from 'Work item AIPLAT-203 created')"
expect_eq "acli key parsing: nothing usable → empty" \
  "" "$(jira_key_from 'error: could not create work item')"

# ── scripts/sync-tick.sh — JIRA failure isolation ────────────────────────────
# The hard requirement from ANK-43 scope 3: with acli deliberately broken, a hop
# still completes. Proven here against stub acli binaries rather than by breaking
# the real credentials. The stub lives under the tick's own scratch dir so its
# existing EXIT trap cleans it up — installing a second trap here would replace
# that one and leak the scratch dir.
echo "==> sync-tick.sh: JIRA failure isolation"
STUB_DIR="${TMPD}/test-stub"
mkdir -p "${STUB_DIR}"
JIRA_FAIL_FILE="${STUB_DIR}/failures"
JIRA_TIMEOUT=2
PATH="${STUB_DIR}:${PATH}"

printf '#!/usr/bin/env bash\nprintf "unauthenticated\\n" >&2\nexit 7\n' > "${STUB_DIR}/acli"
chmod +x "${STUB_DIR}/acli"

isolation_rc=0
jira_run jira workitem create --project NOPE >/dev/null || isolation_rc=$?
expect_eq "a failing acli returns non-zero instead of aborting the tick" "1" "${isolation_rc}"
if [[ -s "${JIRA_FAIL_FILE}" ]]; then
  pass "the failure is recorded for the once-per-hop degradation note"
else
  fail "the failure was not recorded, so no degradation note would ever be posted"
fi

# A key set but every call failing: comment and transition must still be no-ops
# that report success, since neither may change a stage or block a hop.
META_JSON='{"jira_key":"AIPLAT-9999"}'
isolation_rc=0
jira_comment "**transition** to \`awaiting_merge\`" || isolation_rc=$?
expect_eq "jira_comment swallows a failing acli" "0" "${isolation_rc}"
isolation_rc=0
jira_transition "A Status This Workflow Does Not Have" || isolation_rc=$?
expect_eq "an unrecognised transition degrades to a warning" "0" "${isolation_rc}"

# Atlassian hanging is the failure mode that would actually hurt: a stalled tick
# holds its task slot for 15 minutes.
printf '#!/usr/bin/env bash\nsleep 30\n' > "${STUB_DIR}/acli"
isolation_started="$(date -u +%s)"
isolation_rc=0
jira_run jira workitem search --jql "project = NOPE" >/dev/null || isolation_rc=$?
isolation_elapsed=$(( $(date -u +%s) - isolation_started ))
if (( isolation_rc != 0 )) && (( isolation_elapsed <= JIRA_TIMEOUT + 3 )); then
  pass "a hanging acli is bounded by the timeout (${isolation_elapsed}s)"
else
  fail "a hanging acli was not bounded (rc=${isolation_rc}, ${isolation_elapsed}s)"
fi

# And with no key at all, nothing is attempted.
META_JSON='{}'
isolation_rc=0
jira_comment "no key, no mirror" || isolation_rc=$?
expect_eq "no jira_key means no acli call at all" "0" "${isolation_rc}"

# ── Cross-file invariants ────────────────────────────────────────────────────
echo "==> invariants"

# A JIRA outage must never block a sync, which holds only while every acli call
# goes through jira_run (timeout + swallowed non-zero). A direct call anywhere
# else would inherit `set -e` and abort the tick. Comments, the `command -v acli`
# probe and the log/marker strings that merely name it are not calls.
acli_calls="$(grep -nE '(^|[^_[:alnum:]-])acli ' "${TICK}" \
  | grep -vE '^[0-9]+:[[:space:]]*#' \
  | grep -vE 'command -v acli' \
  | grep -vE "(log|printf) " || true)"
if [[ "$(printf '%s' "${acli_calls}" | grep -c . || true)" == "1" ]] \
  && printf '%s' "${acli_calls}" | grep -q 'timeout "\${JIRA_TIMEOUT}" acli'; then
  pass "every acli call in ${TICK} goes through jira_run"
else
  fail "an acli call in ${TICK} bypasses jira_run"
  printf '%s\n' "${acli_calls}" | sed 's/^/       /'
fi
expect_grep "acli calls are bounded by a timeout" \
  'timeout "\$\{JIRA_TIMEOUT\}" acli' "${TICK}"
expect_grep "the sweep runs at the tools_smoke PASS terminal transition" \
  'sweep_throwaways' "${TICK}"
expect_grep "the quiet path sweeps opportunistically" \
  'sweep_stale_throwaways$' "${TICK}"

# The race this whole sweep exists to avoid: the claiming agent's runtime writes
# `in_review` after its final comment, so an inline cancel here always loses.
expect_no_grep "the smoke script never cancels the inner issue inline" \
  'multica issue status "\$\{SMOKE_ISSUE_ID\}"' "${SMOKE}"
expect_grep "the smoke script records the throwaway for the sweeper" \
  'record_throwaway_issue "\$\{SMOKE_ISSUE_ID\}"' "${SMOKE}"
expect_grep "the sweeper's work list key is the one the smoke script writes" \
  'smoke_throwaway_issue_ids' "${TICK}"

# Live contract with the dev-workspace autopilot: it is in another namespace
# behind another token and cannot be updated from this repo, so the marker shape
# is frozen on both sides.
expect_grep "smoke-result marker shape is unchanged (dev autopilot parses it)" \
  "smoke-result %s=%s; status=%s" "${SMOKE}"
expect_grep "sync-tick reads that same marker shape" \
  'smoke-result \$\{key\}=\$\{sha\}' "${TICK}"

# The two classifiers must agree, or a throwaway swept by one side would be
# ignored by the other.
classifier_body() {
  sed -n "/^$2() {\$/,/^}\$/p" "$1" | sed '1d;$d' | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//'
}
expect_eq "both throwaway classifiers agree" \
  "$(classifier_body "${TICK}" sweep_want_status)" \
  "$(classifier_body "${SMOKE}" smoke_sweep_want_status)"

echo
if (( FAILURES > 0 )); then
  printf '✗ %s check(s) failed.\n' "${FAILURES}"
  exit 1
fi
echo "✓ sync pipeline scripts verified."
