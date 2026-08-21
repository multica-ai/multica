#!/bin/bash
# Tests the fallback/precedence logic in gh-platform-bot-wrapper.sh (PRO-68).
#
# Hermes tool-command subprocesses inherit GITHUB_APP_PRIVATE_KEY but not
# GITHUB_APP_ID, so the wrapper must resolve GITHUB_APP_ID from a runtime-
# written file when the env var is absent, while still preferring an explicit
# env var, still deferring to a caller-provided GH_TOKEN/GITHUB_TOKEN, and
# still falling through to the real `gh` unchanged when no App creds are
# available at all. Run: ./gh-platform-bot-wrapper.test.sh
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WRAPPER="${SCRIPT_DIR}/gh-platform-bot-wrapper.sh"

failures=0
pass() { echo "  ok   — $1"; }
fail() { echo "  FAIL — $1" >&2; failures=$((failures + 1)); }

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

# ── Fake HOME per test, with a fake /usr/bin/gh and a fake credential helper
# on PATH so the real network-hitting binaries are never invoked. ────────────
FAKE_BIN="${WORK}/bin"
mkdir -p "${FAKE_BIN}"

# Records every invocation (argv + relevant env) so assertions can inspect it.
CALL_LOG="${WORK}/calls.log"

cat > "${FAKE_BIN}/gh" <<EOF
#!/bin/bash
{
  echo "REAL_GH_INVOKED GH_TOKEN=\${GH_TOKEN:-<unset>} args=\$*"
} >> "${CALL_LOG}"
EOF
chmod +x "${FAKE_BIN}/gh"

# Fake credential helper: emits a token iff GITHUB_APP_ID is present in its
# own environment — mirrors git-credential-platform-bot.sh's real
# precondition, without any network calls or key material.
cat > "${FAKE_BIN}/git-credential-platform-bot" <<EOF
#!/bin/bash
{
  echo "CRED_HELPER_INVOKED GITHUB_APP_ID=\${GITHUB_APP_ID:-<unset>}"
} >> "${CALL_LOG}"
cat >/dev/null  # drain stdin (git's key=value protocol input)
if [ -n "\${GITHUB_APP_ID:-}" ]; then
  printf 'username=x-access-token\npassword=minted-token-for-%s\n' "\${GITHUB_APP_ID}"
fi
EOF
chmod +x "${FAKE_BIN}/git-credential-platform-bot"

# The wrapper script hardcodes /usr/bin/gh and /usr/local/bin/git-credential-platform-bot
# rather than resolving them on PATH, so exercising it without root (to
# overwrite those real paths) requires running it through a copy with those
# two paths substituted for our fakes. Substituting is simpler and safer than
# requiring root in CI.
PATCHED_WRAPPER="${WORK}/wrapper-under-test.sh"
sed \
  -e "s#/usr/bin/gh#${FAKE_BIN}/gh#g" \
  -e "s#/usr/local/bin/git-credential-platform-bot#${FAKE_BIN}/git-credential-platform-bot#g" \
  "${WRAPPER}" > "${PATCHED_WRAPPER}"
chmod +x "${PATCHED_WRAPPER}"

# run VAR=val... -- args...
# Runs the patched wrapper in a clean env plus the given assignments.
run() {
  : > "${CALL_LOG}"
  local env_assigns=()
  while [ "$1" != "--" ]; do
    env_assigns+=("$1")
    shift
  done
  shift
  env -i HOME="${TEST_HOME}" PATH="/usr/bin:/bin" "${env_assigns[@]}" "${PATCHED_WRAPPER}" "$@"
}

new_home() {
  TEST_HOME="$(mktemp -d)"
}

# ── Tests ─────────────────────────────────────────────────────────────────────

echo "explicit GH_TOKEN is never overridden"
new_home
mkdir -p "${TEST_HOME}/.secrets"
echo "12345" > "${TEST_HOME}/.secrets/GITHUB_APP_ID"
run GH_TOKEN=caller-token GITHUB_APP_PRIVATE_KEY="-----BEGIN FAKE-----" -- pr list --repo g2crowd/ue --state open
grep -q "REAL_GH_INVOKED GH_TOKEN=caller-token" "${CALL_LOG}" && pass "caller's GH_TOKEN reached the real gh" \
  || fail "caller's GH_TOKEN was not preserved: $(cat "${CALL_LOG}")"
grep -q "CRED_HELPER_INVOKED" "${CALL_LOG}" && fail "credential helper was invoked despite explicit GH_TOKEN" \
  || pass "credential helper skipped when GH_TOKEN is already set"

echo "explicit GITHUB_TOKEN is never overridden"
new_home
mkdir -p "${TEST_HOME}/.secrets"
echo "12345" > "${TEST_HOME}/.secrets/GITHUB_APP_ID"
run GITHUB_TOKEN=caller-token GITHUB_APP_PRIVATE_KEY="-----BEGIN FAKE-----" -- pr list
grep -q "REAL_GH_INVOKED GH_TOKEN=<unset>" "${CALL_LOG}" && pass "GITHUB_TOKEN left alone, no GH_TOKEN injected" \
  || fail "wrapper injected GH_TOKEN despite caller's GITHUB_TOKEN: $(cat "${CALL_LOG}")"
grep -q "CRED_HELPER_INVOKED" "${CALL_LOG}" && fail "credential helper was invoked despite explicit GITHUB_TOKEN" \
  || pass "credential helper skipped when GITHUB_TOKEN is already set"

echo "GITHUB_APP_ID present only in env (no file) is used directly"
new_home
run GITHUB_APP_ID=env-id GITHUB_APP_PRIVATE_KEY="-----BEGIN FAKE-----" -- pr list --repo g2crowd/ue --state open
grep -q "CRED_HELPER_INVOKED GITHUB_APP_ID=env-id" "${CALL_LOG}" && pass "env GITHUB_APP_ID forwarded to credential helper" \
  || fail "env GITHUB_APP_ID not forwarded: $(cat "${CALL_LOG}")"
grep -q "REAL_GH_INVOKED GH_TOKEN=minted-token-for-env-id" "${CALL_LOG}" && pass "minted token reached the real gh" \
  || fail "minted token did not reach gh: $(cat "${CALL_LOG}")"

echo "GITHUB_APP_ID absent from env falls back to the runtime-written file"
new_home
mkdir -p "${TEST_HOME}/.secrets"
printf '%s' "file-id" > "${TEST_HOME}/.secrets/GITHUB_APP_ID"
run GITHUB_APP_PRIVATE_KEY="-----BEGIN FAKE-----" -- pr list --repo g2crowd/ue --state open
grep -q "CRED_HELPER_INVOKED GITHUB_APP_ID=file-id" "${CALL_LOG}" && pass "file-sourced GITHUB_APP_ID forwarded to credential helper" \
  || fail "file fallback did not reach credential helper: $(cat "${CALL_LOG}")"
grep -q "REAL_GH_INVOKED GH_TOKEN=minted-token-for-file-id" "${CALL_LOG}" && pass "token minted from the file-sourced id reached gh" \
  || fail "expected token from file-sourced id: $(cat "${CALL_LOG}")"

echo "env GITHUB_APP_ID takes precedence over the file when both are present"
new_home
mkdir -p "${TEST_HOME}/.secrets"
printf '%s' "file-id" > "${TEST_HOME}/.secrets/GITHUB_APP_ID"
run GITHUB_APP_ID=env-id GITHUB_APP_PRIVATE_KEY="-----BEGIN FAKE-----" -- pr list
grep -q "CRED_HELPER_INVOKED GITHUB_APP_ID=env-id" "${CALL_LOG}" && pass "env value wins over the file" \
  || fail "expected env-id to win over file-id: $(cat "${CALL_LOG}")"

echo "no App creds at all falls through to the real gh unchanged"
new_home
run -- pr list --repo g2crowd/ue --state open
grep -q "CRED_HELPER_INVOKED" "${CALL_LOG}" && fail "credential helper invoked with no App creds present" \
  || pass "credential helper never invoked"
grep -q "REAL_GH_INVOKED GH_TOKEN=<unset>" "${CALL_LOG}" && pass "fell through to real gh with no GH_TOKEN" \
  || fail "did not fall through cleanly: $(cat "${CALL_LOG}")"

echo "GITHUB_APP_ID present (env or file) but GITHUB_APP_PRIVATE_KEY absent falls through unchanged"
new_home
mkdir -p "${TEST_HOME}/.secrets"
printf '%s' "file-id" > "${TEST_HOME}/.secrets/GITHUB_APP_ID"
run -- pr list
grep -q "CRED_HELPER_INVOKED" "${CALL_LOG}" && fail "credential helper invoked without a private key" \
  || pass "credential helper never invoked without a private key"
grep -q "REAL_GH_INVOKED GH_TOKEN=<unset>" "${CALL_LOG}" && pass "fell through to real gh with no GH_TOKEN" \
  || fail "did not fall through cleanly: $(cat "${CALL_LOG}")"

echo "credential helper returning no token falls through to the real gh unchanged"
new_home
mkdir -p "${TEST_HOME}/.secrets"
# No file and no env GITHUB_APP_ID -> fake helper prints nothing -> empty token.
run GITHUB_APP_PRIVATE_KEY="-----BEGIN FAKE-----" -- pr list
grep -q "REAL_GH_INVOKED GH_TOKEN=<unset>" "${CALL_LOG}" && pass "empty token from helper falls through cleanly" \
  || fail "expected clean fallthrough on empty token: $(cat "${CALL_LOG}")"

echo
if (( failures > 0 )); then
  echo "${failures} failure(s)" >&2
  exit 1
fi
echo "all gh-platform-bot-wrapper tests passed"
