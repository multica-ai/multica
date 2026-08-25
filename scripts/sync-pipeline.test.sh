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
expect_grep "any KEEP_OURS conflict keeps the fork-owned file" 'git checkout "\$\{FORK_REMOTE\}/\$\{FORK_BRANCH\}" -- "\$\{keep\}"' "${SYNC}"
expect_grep "upstream README is saved separately" 'git show "\$\{UPSTREAM_REF\}:README\.md" > "\$\{UPSTREAM_README_SNAPSHOT\}"' "${SYNC}"
expect_grep "any non-KEEP_OURS conflict still parks for a human" 'exit 2' "${SYNC}"
expect_grep "a merge failure with no conflicts is still fatal" 'Merge failed for a non-conflict reason' "${SYNC}"

# ── scripts/upstream-sync.sh — annotated tags are peeled to commits ─────────
# `git rev-parse <ref>` on a ref pointing at an ANNOTATED tag object returns the
# tag object's own SHA, not the commit it points at, unless peeled with
# `^{commit}`. An unpeeled SHA written into the cursor's `sha=` line later fails
# `git replace --graft`, which requires literal commit objects ("Not a valid
# commit name"). This is the exact ANK-117 / AIPLAT-232 failure on the
# v0.4.32 -> v0.4.33 hop. Both peel sites are plain script lines, not
# functions, so — like normalize_jira_ref above — they are lifted out by
# pattern match and eval'd for real, against a real annotated tag, rather than
# reimplemented here.
echo "==> upstream-sync.sh: annotated tag SHAs are peeled to commits"
PEEL_SCRATCH="$(mktemp -d)"
(
  cd "${PEEL_SCRATCH}"
  git init -q .
  git config user.email test@example.com
  git config user.name test
  echo hello > file.txt
  git add file.txt
  git commit -q -m "initial commit"
  git tag -a v9.9.9 -m "annotated release tag"
) >/dev/null

TAG_COMMIT="$(git -C "${PEEL_SCRATCH}" rev-parse v9.9.9^{commit})"
TAG_OBJECT="$(git -C "${PEEL_SCRATCH}" rev-parse v9.9.9)"
if [[ "$(git -C "${PEEL_SCRATCH}" cat-file -t "${TAG_OBJECT}")" == "tag" ]]; then
  pass "fixture tag is a real annotated tag object (reproduces the bug)"
else
  fail "fixture tag is not annotated — test would not reproduce the bug"
fi

# Write-time peel: UPSTREAM_HEAD=$(git rev-parse "${UPSTREAM_REF}^{commit}")
UPSTREAM_HEAD_LINE="$(grep -E '^UPSTREAM_HEAD=\$\(git rev-parse' "${SYNC}")"
if [[ -z "${UPSTREAM_HEAD_LINE}" ]]; then
  fail "could not find the UPSTREAM_HEAD assignment in ${SYNC}"
else
  WRITE_TIME_SHA="$(cd "${PEEL_SCRATCH}" && UPSTREAM_REF="refs/tags/v9.9.9" \
    bash -c "${UPSTREAM_HEAD_LINE}"'; printf "%s" "${UPSTREAM_HEAD}"')"
  WRITE_TIME_TYPE="$(git -C "${PEEL_SCRATCH}" cat-file -t "${WRITE_TIME_SHA}" 2>/dev/null || echo missing)"
  expect_eq "write-time UPSTREAM_HEAD resolves to the tagged commit" \
    "${TAG_COMMIT}" "${WRITE_TIME_SHA}"
  expect_eq "write-time UPSTREAM_HEAD is a commit object (cursor sha= is safe to graft)" \
    "commit" "${WRITE_TIME_TYPE}"
fi

# Cursor-read peel: the `if [ -f "${CURSOR_FILE}" ]; then ... fi` block that
# reads FORK_POINT back out of an on-disk cursor. Simulates the already-
# corrupted cursor left behind by a pre-fix run (sha= holding a bare tag
# object), which is exactly the AIPLAT-232 v0.4.32 cursor state.
FORK_POINT_BLOCK="$(sed -n '/^if \[ -f "\${CURSOR_FILE}" \]; then$/,/^fi$/p' "${SYNC}")"
if [[ -z "${FORK_POINT_BLOCK}" ]]; then
  fail "could not extract the cursor-read FORK_POINT block from ${SYNC}"
else
  CURSOR_SCRATCH_FILE="${PEEL_SCRATCH}/.upstream-sync-cursor"
  printf 'tag=v9.9.9\nsha=%s\n' "${TAG_OBJECT}" > "${CURSOR_SCRATCH_FILE}"
  CURSOR_READ_SHA="$(cd "${PEEL_SCRATCH}" && CURSOR_FILE="${CURSOR_SCRATCH_FILE}" \
    bash -c "${FORK_POINT_BLOCK}"'; printf "%s" "${FORK_POINT}"')"
  CURSOR_READ_TYPE="$(git -C "${PEEL_SCRATCH}" cat-file -t "${CURSOR_READ_SHA}" 2>/dev/null || echo missing)"
  expect_eq "cursor-read FORK_POINT peels an already-corrupted tag-object sha" \
    "${TAG_COMMIT}" "${CURSOR_READ_SHA}"
  expect_eq "cursor-read FORK_POINT is a commit object (safe for git replace --graft)" \
    "commit" "${CURSOR_READ_TYPE}"
fi
rm -rf "${PEEL_SCRATCH}"

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

# A hop parked at `syncing` has to be resumable. The interrupted-branch guard
# used to glob every `upstream-sync/*` branch on origin — and those branches are
# never deleted after their PR merges, so the guard fired on every single hop and
# no `syncing` block could ever clear itself.
expect_no_grep "no unscoped upstream-sync/* refname glob reaches ls-remote" \
  "ls-remote.*upstream-sync/\\*['\"]" "${TICK}"

# stale_sync_branch is the single answer to "has this hop already pushed?", so both
# the stage_idle pre-check and the stage_syncing guard inherit its scoping. It is
# exercised for real against a stub `git` — the branch list below is the shape that
# broke the old guard: many merged hops, none of them this hop.
echo "==> sync-tick.sh: stale branch detection"
eval "$(sed -n '/^stale_sync_branch() {$/,/^}$/p' "${TICK}")"
if ! declare -F stale_sync_branch >/dev/null; then
  fail "could not extract stale_sync_branch from ${TICK}"
else
  STUB_DIR="$(mktemp -d)"
  trap 'rm -rf "${STUB_DIR}"' EXIT
  # Emulates `git ls-remote --heads origin <pattern>`: the real command applies the
  # refname glob on the server and prints `<sha>\t<ref>`, so the stub matches
  # ${FAKE_BRANCHES} the same way and prints the survivors in that same format.
  cat > "${STUB_DIR}/git" <<'STUB'
#!/usr/bin/env bash
[[ "$1" == "ls-remote" ]] || exit 1
pattern="${!#}"
for b in ${FAKE_BRANCHES:-}; do
  # shellcheck disable=SC2053  # unquoted RHS is the glob match, on purpose
  [[ "${b}" == ${pattern} ]] \
    && printf '0000000000000000000000000000000000000000\trefs/heads/%s\n' "${b}"
done
exit 0
STUB
  chmod +x "${STUB_DIR}/git"
  stale_for() { ( PATH="${STUB_DIR}:${PATH}"; export FAKE_BRANCHES="$2"; stale_sync_branch "$1" ); }

  MERGED="upstream-sync/v0.4.12-to-v0.4.13 upstream-sync/v0.4.13-to-v0.4.14 upstream-sync/v0.4.14-to-v0.4.15"

  expect_eq "a fork with no sync branches at all is not stale" \
    "" "$(stale_for v0.4.16 "")"
  expect_eq "branches from hops that already merged are not stale" \
    "" "$(stale_for v0.4.16 "${MERGED}")"
  expect_eq "a branch for THIS target is stale, and is named without refs/heads/" \
    "upstream-sync/v0.4.15-to-v0.4.16" \
    "$(stale_for v0.4.16 "${MERGED} upstream-sync/v0.4.15-to-v0.4.16")"
  expect_eq "a from-side short SHA (no cursor tag) still matches" \
    "upstream-sync/a9a4a3d-to-v0.4.16" \
    "$(stale_for v0.4.16 "${MERGED} upstream-sync/a9a4a3d-to-v0.4.16")"
  expect_eq "a tag that is a prefix of another does not cross-match" \
    "" "$(stale_for v0.4.1 "upstream-sync/v0.4.0-to-v0.4.16")"
fi

# stage_idle must consult it BEFORE opening a ticket and running the sync:
# upstream-sync.sh rebuilds the merge commit with a fresh timestamp, so its push
# against a leftover branch is a non-fast-forward and the hop dies at the very last
# step with an error that names none of the cause.
expect_grep "stage_idle checks for a leftover branch before starting a hop" \
  'stale="\$\(stale_sync_branch "\$\{latest\}"\)"' "${TICK}"
expect_grep "a leftover branch is reported, not pushed over" \
  'block stale_sync_branch ' "${TICK}"
expect_grep "the report names the exact command that clears it" \
  'git push origin --delete \$\{stale\}' "${TICK}"
expect_grep "stale_sync_branch has no auto-clear case, so it parks for a human" \
  'waits for a human, no action this tick' "${TICK}"

# `git status --porcelain` prints nothing on stdout when it cannot read the
# repository at all, so testing only its output read an unusable checkout as a
# clean one and synced on regardless.
expect_grep "a git status that fails is fatal, not 'clean'" \
  'if ! DIRTY=\$\(git status --porcelain\)' "${SYNC}"
expect_grep "a dirty tree reports which paths are dirty" \
  "printf '%s\\\\n' \"\\\$\{DIRTY\}\" | head -20" "${SYNC}"

echo "==> git ownership trust"

# The runtime creates the checkout as uid 50012 while the agent runs as 1000, so
# git exits 128 with `detected dubious ownership` on the FIRST git command in
# either script — before any stage runs and before anything is reported. Both
# scripts are entry points (the tick on a schedule, upstream-sync.sh by hand), so
# both must declare the trust themselves.
for script in "${TICK}" "${SYNC}"; do
  expect_grep "${script} declares the trust helper" \
    '^trust_git_checkouts\(\) \{' "${script}"
  expect_grep "${script} calls it before any git command" \
    '^trust_git_checkouts$' "${script}"
  # A path-specific entry cannot work: every task gets a freshly named workdir, so
  # an entry naming one run's path never covers the next run.
  expect_grep "${script} trusts by wildcard, not by this run's path" \
    'GIT_CONFIG_VALUE_\$\{n\}=\*' "${script}"
done

# Exercise the real helper rather than trusting the greps above: what matters is
# the exact env it leaves behind, and an off-by-one in the index silently drops
# the entry (git reads keys 0..COUNT-1 and ignores anything above).
TRUST_FN="$(sed -n '/^trust_git_checkouts() {$/,/^}$/p' "${TICK}")"
if [[ -z "${TRUST_FN}" ]]; then
  fail "could not extract trust_git_checkouts from ${TICK}"
else
  # Run the REAL helper in a child shell with a controlled env. Reports
  # `COUNT|KEY_at_that_index|VALUE_at_that_index` for the index the helper claims to
  # have filled, so a count that disagrees with the pair it actually wrote shows up
  # here — git reads keys 0..COUNT-1 and silently ignores anything outside that.
  trust_env() {
    env -u GIT_CONFIG_COUNT "$@" bash -c "${TRUST_FN}"'
      trust_git_checkouts
      i=$(( GIT_CONFIG_COUNT - 1 ))
      k="GIT_CONFIG_KEY_${i}"; v="GIT_CONFIG_VALUE_${i}"
      printf "%s|%s|%s\n" "${GIT_CONFIG_COUNT}" "${!k-UNSET}" "${!v-UNSET}"'
  }

  expect_eq "a clean env gets safe.directory at index 0" \
    "1|safe.directory|*" "$(trust_env)"

  # The runtime may already place env-scoped pairs (an auth header, a URL rewrite).
  # Overwriting index 0 would silently disable one of them, so the helper appends.
  expect_eq "pre-existing pairs are appended to, never overwritten" \
    "3|safe.directory|*" \
    "$(trust_env GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=http.version GIT_CONFIG_VALUE_0=HTTP/1.1)"

  expect_eq "the pre-existing pair at index 0 survives untouched" \
    "http.version=HTTP/1.1" \
    "$(env -u GIT_CONFIG_COUNT GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=http.version \
         GIT_CONFIG_VALUE_0=HTTP/1.1 bash -c "${TRUST_FN}"'
         trust_git_checkouts
         printf "%s=%s\n" "${GIT_CONFIG_KEY_0}" "${GIT_CONFIG_VALUE_0}"')"

  # A non-numeric or negative count must fall back to 0. Without the guard, "abc"
  # makes `$(( n + 1 ))` yield 1 while the pair lands at KEY_abc — the count and the
  # pair disagree, so git looks up KEY_0, finds nothing, and the trust is lost.
  for junk in "" abc -1 2x; do
    expect_eq "a GIT_CONFIG_COUNT of '${junk}' falls back to index 0" \
      "1|safe.directory|*" "$(trust_env "GIT_CONFIG_COUNT=${junk}")"
  done

  # End-to-end, on this very checkout: the repository git refuses without the helper
  # must be readable with it. GIT_CONFIG_GLOBAL is masked because this pod's
  # ~/.gitconfig has accreted per-workdir safe.directory entries from earlier runs,
  # and those hide the bug the helper exists to fix.
  NO_CFG=(env -u GIT_CONFIG_COUNT GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null)
  if [[ "$(stat -c '%u' .git 2>/dev/null || id -u)" == "$(id -u)" ]]; then
    pass "checkout is owned by this uid — ownership refusal not reproducible here"
  elif "${NO_CFG[@]}" git rev-parse HEAD >/dev/null 2>&1; then
    fail "expected git to refuse this foreign-owned checkout without the helper"
  else
    pass "git refuses a foreign-owned checkout without the helper"
    if "${NO_CFG[@]}" bash -c "${TRUST_FN}"'
         trust_git_checkouts
         git rev-parse HEAD' >/dev/null 2>&1; then
      pass "the helper makes that same checkout readable"
    else
      fail "git still refuses the checkout after trust_git_checkouts"
    fi
  fi
fi

# ── CI auto-remediation on block (ANK-96) ─────────────────────────────────────
# The postmortem this fixes: a dev-deploy failure that later went green sat
# `blocked` for ~24h because nothing ever looked again, and a human's "remove
# block" comment on the ticket was never read. These tests exercise the parts
# that don't need live GitHub/Multica state; the GO_VERSION autofix's git
# plumbing and the try_*_deploy_autofix orchestration are exercised for real
# only against a live sync (covered by manual verification, same as the rest
# of the CI-facing stage handlers in this file).
echo "==> sync-tick.sh: CI auto-remediation classification"

expect_eq "dev_smoke is self-repolled" "0" \
  "$(is_self_repolled_reason dev_smoke; echo $?)"
expect_eq "tools_smoke is self-repolled" "0" \
  "$(is_self_repolled_reason tools_smoke; echo $?)"
expect_eq "dev_deploy is self-repolled" "0" \
  "$(is_self_repolled_reason dev_deploy; echo $?)"
expect_eq "tools_deploy is self-repolled" "0" \
  "$(is_self_repolled_reason tools_deploy; echo $?)"
expect_eq "rollout_stale is self-repolled" "0" \
  "$(is_self_repolled_reason rollout_stale; echo $?)"
expect_eq "sync_conflict is NOT self-repolled (needs a human)" "1" \
  "$(is_self_repolled_reason sync_conflict; echo $?)"
expect_eq "unknown_stage is NOT self-repolled (needs a human)" "1" \
  "$(is_self_repolled_reason unknown_stage; echo $?)"

# The exact ANK-96 comment that motivated this: it must be recognised as a
# resolution so handle_human_comment() does not repeat that miss.
expect_eq "the actual ANK-96 comment reads as a resolution" "0" \
  "$(printf '%s' 'The deployment to development is now successful. Remove block' \
     | grep -qiE "${RESOLUTION_KEYWORDS}"; echo $?)"
expect_eq "'please retry' reads as a resolution" "0" \
  "$(printf '%s' 'please retry' | grep -qiE "${RESOLUTION_KEYWORDS}"; echo $?)"
expect_eq "'unblock this' reads as a resolution" "0" \
  "$(printf '%s' 'unblock this' | grep -qiE "${RESOLUTION_KEYWORDS}"; echo $?)"
expect_eq "an unrelated status update does not read as a resolution" "1" \
  "$(printf '%s' 'looking into it, will update later' \
     | grep -qiE "${RESOLUTION_KEYWORDS}"; echo $?)"

# ── stub multica for the metadata/label/comment writes below ────────────────
# Fakes just enough of the CLI surface so block()/mset()/mdel()/set_sync_label()
# run their real logic against in-memory META_JSON without touching the actual
# workspace. `issue metadata list`/`label list` return empty so mget/label_id
# behave as if nothing exists remotely; every write subcommand exits 0.
AUTOFIX_STUB_DIR="${TMPD}/autofix-test-stub"
mkdir -p "${AUTOFIX_STUB_DIR}"
cat > "${AUTOFIX_STUB_DIR}/multica" <<'STUB'
#!/usr/bin/env bash
case "$1 $2" in
  "issue metadata"*) [[ "$3" == "list" ]] && echo '{}' ;;
  "label list")      echo '[]' ;;
esac
exit 0
STUB
chmod +x "${AUTOFIX_STUB_DIR}/multica"

echo "==> sync-tick.sh: block() dedups a re-block on the same reason"
# `advance()` is the only thing that logs "stage → blocked" to stderr, so
# counting that line is a direct proxy for "did block() re-post", independent
# of now_epoch()'s subshell-local counter not surviving back into this scope.
(
  PATH="${AUTOFIX_STUB_DIR}:${PATH}"
  TICKET="test-ticket-autofix"
  META_JSON='{}'
  DRY_RUN=""

  block dev_deploy "first failure" >/dev/null
  echo "REASON:$(mget blocked_reason)"
  log "---after-first---"
  block dev_deploy "second failure, same reason" >/dev/null
  log "---after-second---"
  block dev_deploy_never_started "a genuinely different reason" >/dev/null
  log "---after-third---"
) > "${AUTOFIX_STUB_DIR}/result" 2>"${AUTOFIX_STUB_DIR}/result.stderr"
BLOCK_RESULT="$(cat "${AUTOFIX_STUB_DIR}/result")"
br_reason="$(printf '%s\n' "${BLOCK_RESULT}" | sed -n 's/^REASON://p')"
# `grep -c` exits 1 on a zero count, which would kill this whole script under
# `set -e` inside a `$(...)` assignment — `|| true` keeps a legitimate "0" a
# passing read instead of an abort.
advances_after_first="$(sed -n '1,/---after-first---/p' "${AUTOFIX_STUB_DIR}/result.stderr" | grep -c 'stage → blocked' || true)"
advances_after_second="$(sed -n '/---after-first---/,/---after-second---/p' "${AUTOFIX_STUB_DIR}/result.stderr" | grep -c 'stage → blocked' || true)"
advances_after_third="$(sed -n '/---after-second---/,/---after-third---/p' "${AUTOFIX_STUB_DIR}/result.stderr" | grep -c 'stage → blocked' || true)"
expect_eq "block() records the reason" "dev_deploy" "${br_reason}"
expect_eq "the first block advances the stage once" "1" "${advances_after_first}"
expect_eq "re-blocking on the SAME reason does not re-advance the stage" "0" "${advances_after_second}"
expect_eq "blocking on a DIFFERENT reason still advances the stage" "1" "${advances_after_third}"

echo "==> sync-tick.sh: autofix attempt counter"
(
  PATH="${AUTOFIX_STUB_DIR}:${PATH}"
  TICKET="test-ticket-autofix"
  META_JSON='{}'
  DRY_RUN=""
  n0="$(autofix_attempts dev_deploy)"
  bump_autofix_attempts dev_deploy
  n1="$(autofix_attempts dev_deploy)"
  bump_autofix_attempts dev_deploy
  n2="$(autofix_attempts dev_deploy)"
  # A different reason's counter must not share state with dev_deploy's.
  n_other="$(autofix_attempts tools_deploy)"
  printf '%s|%s|%s|%s\n' "${n0:-0}" "${n1}" "${n2}" "${n_other:-0}"
) > "${AUTOFIX_STUB_DIR}/counter-result" 2>/dev/null
COUNTER_RESULT="$(cat "${AUTOFIX_STUB_DIR}/counter-result")"
IFS='|' read -r ac0 ac1 ac2 ac_other <<< "${COUNTER_RESULT}"
expect_eq "no attempts yet reads as unset" "0" "${ac0}"
expect_eq "first bump → 1" "1" "${ac1}"
expect_eq "second bump → 2" "2" "${ac2}"
expect_eq "a different reason's counter is independent" "0" "${ac_other}"

echo "==> sync-tick.sh: flake retry is attempted once per run id"
cat > "${AUTOFIX_STUB_DIR}/gh" <<'STUB'
#!/usr/bin/env bash
[[ "$1 $2" == "run rerun" ]] && { echo "$3" >> "${GH_RERUN_LOG}"; exit 0; }
exit 1
STUB
chmod +x "${AUTOFIX_STUB_DIR}/gh"
(
  PATH="${AUTOFIX_STUB_DIR}:${PATH}"
  TICKET="test-ticket-autofix"
  META_JSON='{}'
  DRY_RUN=""
  export GH_RERUN_LOG="${AUTOFIX_STUB_DIR}/rerun.log"
  : > "${GH_RERUN_LOG}"
  try_flake_retry dev_deploy 999 cancelled dev.yml >/dev/null
  try_flake_retry dev_deploy 999 cancelled dev.yml >/dev/null   # same run id — must not re-fire
  try_flake_retry dev_deploy 1000 cancelled dev.yml >/dev/null  # a new run id — fires again
  cat "${GH_RERUN_LOG}"
) > "${AUTOFIX_STUB_DIR}/rerun-result" 2>/dev/null
RERUN_CALLS="$(wc -l < "${AUTOFIX_STUB_DIR}/rerun-result" | tr -d ' ')"
RERUN_IDS="$(tr '\n' ',' < "${AUTOFIX_STUB_DIR}/rerun-result")"
expect_eq "the same run id is only rerun once" "2" "${RERUN_CALLS}"
expect_eq "rerun fires for 999 then 1000, not a repeated 999" "999,1000," "${RERUN_IDS}"
expect_eq "a non-retryable conclusion is left to the caller" \
  "1" "$(try_flake_retry dev_deploy 2000 failure dev.yml >/dev/null; echo $?)"

echo
if (( FAILURES > 0 )); then
  printf '✗ %s check(s) failed.\n' "${FAILURES}"
  exit 1
fi
echo "✓ sync pipeline scripts verified."
