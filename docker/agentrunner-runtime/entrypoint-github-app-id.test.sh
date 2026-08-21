#!/bin/bash
# Tests the runtime-start GITHUB_APP_ID materialization in entrypoint.sh (PRO-68).
#
# gh-platform-bot-wrapper.sh's file fallback only helps if entrypoint.sh
# actually writes ${HOME}/.secrets/GITHUB_APP_ID at pod boot, and only when
# the same condition that wires up the git credential helper is true — the
# file must not appear (or must not exist) in a pod that never configured
# GitHub App auth in the first place. This exercises exactly that startup
# path, not the whole entrypoint (which requires MULTICA_PAT, a running
# daemon, etc.). Run: ./entrypoint-github-app-id.test.sh
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENTRYPOINT="${SCRIPT_DIR}/entrypoint.sh"

failures=0
pass() { echo "  ok   — $1"; }
fail() { echo "  FAIL — $1" >&2; failures=$((failures + 1)); }

# ── Extract the region under test ─────────────────────────────────────────────
# From the top of the file (mandatory-env guards) through the end of the
# GitHub credential helper block, i.e. everything before "── Git identity ──".
# This is exactly entrypoint.sh's configured startup path for GITHUB_APP_ID,
# run as real bash (not re-implemented), so the test tracks the shipped code.
extract_startup_region() {
  sed -n '1,/^# ── Git identity/p' "${ENTRYPOINT}" | sed '$d'
}

startup_src="$(extract_startup_region)"
if ! grep -q 'SECRETS_DIR}/GITHUB_APP_ID' <<<"${startup_src}"; then
  echo "FATAL: could not extract the GITHUB_APP_ID startup region from ${ENTRYPOINT}" >&2
  echo "       (did the section markers or the file name change?)" >&2
  exit 1
fi

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

# git config must not fail the run (set -e is in force in the extracted
# region) even though HOME is a scratch dir with no real git identity needs.
FAKE_BIN="${WORK}/bin"
mkdir -p "${FAKE_BIN}"
cat > "${FAKE_BIN}/git" <<'EOF'
#!/bin/bash
exit 0
EOF
chmod +x "${FAKE_BIN}/git"

run_startup_region() {
  local test_home="$1"; shift
  env -i \
    HOME="${test_home}" \
    PATH="${FAKE_BIN}:/usr/bin:/bin" \
    MULTICA_PAT=x MULTICA_WORKSPACE_ID=x ANTHROPIC_API_KEY=x OPENAI_API_KEY=x WORKSPACE_SLUG=x \
    "$@" \
    bash -c "${startup_src}"
}

# ── Tests ─────────────────────────────────────────────────────────────────────

echo "both App creds present: file is written with the App ID, mode 600"
TEST_HOME="$(mktemp -d)"
run_startup_region "${TEST_HOME}" GITHUB_APP_ID=987654 GITHUB_APP_PRIVATE_KEY="-----BEGIN FAKE-----"
app_id_file="${TEST_HOME}/.secrets/GITHUB_APP_ID"
if [ -f "${app_id_file}" ]; then
  pass "GITHUB_APP_ID file created"
  [ "$(cat "${app_id_file}")" = "987654" ] && pass "file contains the App ID verbatim" \
    || fail "file contents wrong: $(cat "${app_id_file}")"
  mode=$(stat -c '%a' "${app_id_file}" 2>/dev/null || stat -f '%Lp' "${app_id_file}" 2>/dev/null)
  [ "${mode}" = "600" ] && pass "file mode is 600" || fail "expected mode 600, got ${mode}"
else
  fail "GITHUB_APP_ID file was not created when both creds were present"
fi

echo "GITHUB_APP_PRIVATE_KEY missing: file must not exist"
TEST_HOME="$(mktemp -d)"
run_startup_region "${TEST_HOME}" GITHUB_APP_ID=987654
[ -f "${TEST_HOME}/.secrets/GITHUB_APP_ID" ] \
  && fail "file was created despite a missing private key" \
  || pass "no file created without a private key"

echo "GITHUB_APP_ID missing: file must not exist"
TEST_HOME="$(mktemp -d)"
run_startup_region "${TEST_HOME}" GITHUB_APP_PRIVATE_KEY="-----BEGIN FAKE-----"
[ -f "${TEST_HOME}/.secrets/GITHUB_APP_ID" ] \
  && fail "file was created despite a missing App ID" \
  || pass "no file created without an App ID"

echo "neither cred present: no file, and the run still boots cleanly"
TEST_HOME="$(mktemp -d)"
run_startup_region "${TEST_HOME}"
rc=$?
[ "${rc}" -eq 0 ] && pass "startup region exits 0 with no App creds at all" \
  || fail "startup region exited ${rc} with no App creds"
[ -f "${TEST_HOME}/.secrets/GITHUB_APP_ID" ] \
  && fail "file was created despite no App creds at all" \
  || pass "no file created without any App creds"

echo
if (( failures > 0 )); then
  echo "${failures} failure(s)" >&2
  exit 1
fi
echo "all entrypoint GITHUB_APP_ID tests passed"
