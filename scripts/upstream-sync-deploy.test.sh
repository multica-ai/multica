#!/usr/bin/env bash
#
# Tests for scripts/upstream-sync-deploy.sh — the nightly auto-deploy pass.
#
# Mechanics that touch real git/prod (merge, rollback) are covered by Tine's
# manual QA on a testbranch, not here. This file unit-tests the pure decision
# logic — the five safety fences — by sourcing the script and calling its
# functions with stub `gh` / `multica` / `curl` binaries on PATH.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/upstream-sync-deploy.sh"

PASS=0
FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   - %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL - %s\n' "$1"; }
check(){ if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want [$3], got [$2])"; fi; }

# Each test builds a fresh stub-bin dir on PATH. A stub reads $STUB_* files so a
# test can script the exact JSON `gh`/`multica` should return.
new_sandbox() {
  TMP="$(mktemp -d)"
  STUB="$TMP/bin"
  mkdir -p "$STUB"
  export STUB_DIR="$TMP"
  export PATH="$STUB:$PATH"

  cat >"$STUB/gh" <<'STUB'
#!/usr/bin/env bash
# Dispatch on the gh subcommand using fixture files the test wrote.
case "$1 $2" in
  "pr list")   cat "$STUB_DIR/gh_pr_list.json" 2>/dev/null || echo '[]' ;;
  "pr checks") cat "$STUB_DIR/gh_pr_checks.json" 2>/dev/null || echo '[]' ;;
  "pr view")   cat "$STUB_DIR/gh_pr_view.json" 2>/dev/null || echo '{}' ;;
  *) echo '{}' ;;
esac
STUB
  # gh with --jq must actually apply the jq filter; emulate by piping through jq.
  cat >"$STUB/gh" <<'STUB'
#!/usr/bin/env bash
jqfilter=""
prev=""
for a in "$@"; do
  if [[ "$prev" == "--jq" ]]; then jqfilter="$a"; fi
  prev="$a"
done
emit() {
  if [[ -n "$jqfilter" ]]; then jq -r "$jqfilter"; else cat; fi
}
case "$1 $2" in
  "pr list")   { cat "$STUB_DIR/gh_pr_list.json" 2>/dev/null || echo '[]'; } | emit ;;
  "pr checks") { cat "$STUB_DIR/gh_pr_checks.json" 2>/dev/null || echo '[]'; } | emit ;;
  "pr view")   { cat "$STUB_DIR/gh_pr_view.json" 2>/dev/null || echo '{}'; } | emit ;;
  *) echo '{}' ;;
esac
STUB
  chmod +x "$STUB/gh"

  cat >"$STUB/multica" <<'STUB'
#!/usr/bin/env bash
case "$1 $2 $3" in
  "issue label list") cat "$STUB_DIR/mc_labels.json" 2>/dev/null || echo '[]' ;;
  "issue comment list") cat "$STUB_DIR/mc_comments.json" 2>/dev/null || echo '[]' ;;
  "issue create") echo "FIR-9999 created" ;;
  *) echo '[]' ;;
esac
STUB
  chmod +x "$STUB/multica"

  cat >"$STUB/curl" <<'STUB'
#!/usr/bin/env bash
# Emit the code the test scripted in STUB_HTTP_CODE (default 200).
printf '%s' "${STUB_HTTP_CODE:-200}"
STUB
  chmod +x "$STUB/curl"
}

teardown() { rm -rf "$TMP"; }

# Source the script under test (dispatch is guarded out when sourced).
new_sandbox
export DEPLOY_TINE_AGENT_ID="tine-id"
export DEPLOY_STATUS_ISSUE="status-issue"
# Smoke config must be set BEFORE sourcing — the script bakes SMOKE_* at load.
export DEPLOY_SMOKE_GRACE=0 DEPLOY_SMOKE_TIMEOUT=1 DEPLOY_SMOKE_INTERVAL=1
# shellcheck disable=SC1090
source "$SCRIPT"

echo "== tracking_issue_from_body =="
check "Tracking-Issue trailer (FIR key)" \
  "$(tracking_issue_from_body $'title\n\nTracking-Issue: FIR-2217\n')" "FIR-2217"
check "Tracking-Issue trailer (uuid)" \
  "$(tracking_issue_from_body 'Tracking-Issue:   3ba9546d-19dc-49bf-bea3-ac4ca84f4ca9')" "3ba9546d-19dc-49bf-bea3-ac4ca84f4ca9"
check "mention://issue link fallback" \
  "$(tracking_issue_from_body 'see [FIR-1](mention://issue/abc-123-def) for context')" "abc-123-def"
check "no reference ⇒ empty" \
  "$(tracking_issue_from_body 'just a normal PR body')" ""

echo "== is_frozen (fence d) =="
echo '[{"name":"deploy-freeze","color":"#f00"}]' >"$STUB_DIR/mc_labels.json"
if is_frozen; then ok "freeze label present ⇒ frozen"; else bad "freeze label present ⇒ frozen"; fi
echo '[{"name":"bug"}]' >"$STUB_DIR/mc_labels.json"
if is_frozen; then bad "no freeze label ⇒ not frozen"; else ok "no freeze label ⇒ not frozen"; fi
echo '[]' >"$STUB_DIR/mc_labels.json"
if is_frozen; then bad "empty labels ⇒ not frozen"; else ok "empty labels ⇒ not frozen"; fi

echo "== pr_ci_green (fence b) =="
echo '[{"bucket":"pass"},{"bucket":"skipping"}]' >"$STUB_DIR/gh_pr_checks.json"
if pr_ci_green 1; then ok "all pass/skipping ⇒ green"; else bad "all pass/skipping ⇒ green"; fi
echo '[{"bucket":"pass"},{"bucket":"fail"}]' >"$STUB_DIR/gh_pr_checks.json"
if pr_ci_green 1; then bad "one fail ⇒ not green"; else ok "one fail ⇒ not green"; fi
echo '[{"bucket":"pass"},{"bucket":"pending"}]' >"$STUB_DIR/gh_pr_checks.json"
if pr_ci_green 1; then bad "pending ⇒ not green"; else ok "pending ⇒ not green"; fi
echo '[]' >"$STUB_DIR/gh_pr_checks.json"
if pr_ci_green 1; then bad "no checks ⇒ not green"; else ok "no checks ⇒ not green"; fi

echo "== pr_approved_by_tine (fence a) =="
echo '{"body":"Tracking-Issue: FIR-1","url":"https://github.com/x/y/pull/42"}' >"$STUB_DIR/gh_pr_view.json"
echo '[{"author_id":"tine-id","content":"QA — done\nResultat: PASS for #42"}]' >"$STUB_DIR/mc_comments.json"
if pr_approved_by_tine 42; then ok "Tine PASS referencing #42 ⇒ approved"; else bad "Tine PASS referencing #42 ⇒ approved"; fi

echo '[{"author_id":"someone-else","content":"Resultat: PASS for #42"}]' >"$STUB_DIR/mc_comments.json"
if pr_approved_by_tine 42; then bad "PASS from non-Tine ⇒ not approved"; else ok "PASS from non-Tine ⇒ not approved"; fi

echo '[{"author_id":"tine-id","content":"Resultat: FAIL for #42"}]' >"$STUB_DIR/mc_comments.json"
if pr_approved_by_tine 42; then bad "Tine FAIL ⇒ not approved"; else ok "Tine FAIL ⇒ not approved"; fi

echo '[{"author_id":"tine-id","content":"Resultat: PASS for #7"}]' >"$STUB_DIR/mc_comments.json"
if pr_approved_by_tine 42; then bad "stale PASS for #7 ⇒ #42 not approved"; else ok "stale PASS for #7 ⇒ #42 not approved"; fi

echo '{"body":"no tracking issue here","url":"https://github.com/x/y/pull/42"}' >"$STUB_DIR/gh_pr_view.json"
echo '[{"author_id":"tine-id","content":"Resultat: PASS for #42"}]' >"$STUB_DIR/mc_comments.json"
if pr_approved_by_tine 42; then bad "no tracking issue ⇒ not approved"; else ok "no tracking issue ⇒ not approved"; fi

echo "== scan_candidates (fence b/e filtering) =="
cat >"$STUB_DIR/gh_pr_list.json" <<'JSON'
[
  {"number":1,"isDraft":false,"mergeable":"MERGEABLE","mergeStateStatus":"CLEAN","headRefName":"chore/upstream-rolling"},
  {"number":2,"isDraft":true,"mergeable":"MERGEABLE","mergeStateStatus":"CLEAN","headRefName":"feature/x"},
  {"number":3,"isDraft":false,"mergeable":"CONFLICTING","mergeStateStatus":"DIRTY","headRefName":"feature/y"},
  {"number":4,"isDraft":false,"mergeable":"MERGEABLE","mergeStateStatus":"CLEAN","headRefName":"revert/pr-1-x"}
]
JSON
selected="$(scan_candidates | jq -c '[.[].number]')"
check "scan keeps only mergeable non-draft non-revert" "$selected" "[1]"

echo "== run_smoke (fence c detection) =="
STUB_HTTP_CODE=200 run_smoke >/dev/null 2>&1 && ok "200 ⇒ smoke pass" || bad "200 ⇒ smoke pass"
STUB_HTTP_CODE=503 run_smoke >/dev/null 2>&1 && bad "503 ⇒ smoke fail" || ok "503 ⇒ smoke fail"

teardown
echo
echo "== results: $PASS passed, $FAIL failed =="
[[ "$FAIL" -eq 0 ]]
