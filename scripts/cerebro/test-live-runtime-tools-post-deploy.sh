#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
check_script="$repo_root/scripts/cerebro/check-live-runtime-tools-post-deploy.sh"
deploy_script="$repo_root/.deploy/deploy.sh"
rollout_runbook="$repo_root/docs/agents/task-mandate-rollout.md"
deploy_runbook="$repo_root/DEPLOY.md"
ci_workflow="$repo_root/.github/workflows/ci.yml"
post_deploy_workflow="$repo_root/.github/workflows/cerebro-post-deploy-live-runtime-tools.yml"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fixtures="$tmp/fixtures"
mkdir -p "$fixtures"

cat >"$tmp/multica" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == "--profile local runtime list --output json" ]]; then
  cat "$FIXTURES/runtime-list.json"
  exit 0
fi
exit 22
STUB
chmod +x "$tmp/multica"

cat >"$tmp/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >"$FIXTURES/curl-args"
cat "$FIXTURES/version.json"
STUB
chmod +x "$tmp/curl"

run_check() {
  FIXTURES="$fixtures" \
    PATH="$tmp:$PATH" \
    MULTICA_POST_DEPLOY_CLI="$tmp/multica" \
    MULTICA_POST_DEPLOY_TIMEOUT=0 \
    MULTICA_POST_DEPLOY_INTERVAL=0 \
    MULTICA_POST_DEPLOY_VERSION_URL="https://live.example/version" \
    MULTICA_POST_DEPLOY_EXPECTED_COMMIT="expected-sha" \
    CEREBRO_CF_ACCESS_CLIENT_ID="test-client-id" \
    CEREBRO_CF_ACCESS_CLIENT_SECRET="test-client-secret" \
    bash "$check_script"
}

expect_pass() {
  local label="$1"
  if run_check >"$tmp/stdout" 2>"$tmp/stderr"; then
    printf 'PASS: %s\n' "$label"
  else
    printf 'FAIL: %s\n' "$label" >&2
    cat "$tmp/stderr" >&2
    exit 1
  fi
}

expect_fail() {
  local label="$1" expected="$2"
  if run_check >"$tmp/stdout" 2>"$tmp/stderr"; then
    printf 'FAIL: %s unexpectedly passed\n' "$label" >&2
    exit 1
  fi
  if ! grep -Fq "$expected" "$tmp/stderr"; then
    printf 'FAIL: %s did not report %q\n' "$label" "$expected" >&2
    cat "$tmp/stderr" >&2
    exit 1
  fi
  printf 'PASS: %s\n' "$label"
}

if FIXTURES="$fixtures" PATH="$tmp:$PATH" \
    MULTICA_POST_DEPLOY_CLI="$tmp/multica" \
    MULTICA_POST_DEPLOY_TIMEOUT=08 \
    bash "$check_script" >"$tmp/stdout" 2>"$tmp/stderr"; then
  printf 'FAIL: invalid numeric configuration unexpectedly passed\n' >&2
  exit 1
fi
grep -Fq "must be non-negative integers" "$tmp/stderr"
printf 'PASS: invalid numeric configuration fails before Bash arithmetic\n'

cat >"$fixtures/version.json" <<'JSON'
{"commit":"expected-sha","builtAt":"2026-08-03T06:00:00Z"}
JSON
cat >"$fixtures/runtime-list.json" <<'JSON'
[
  {
    "id":"online-provider",
    "name":"Provider Runtime",
    "status":"online",
    "capabilities":{"discovery_method":"probed","tools":["Read","Bash"]}
  },
  {"id":"offline-empty","name":"Offline Runtime","status":"offline","capabilities":{}}
]
JSON
expect_pass "an online Runtime with a capability report passes and offline empty Runtimes are ignored"
grep -Fq "CF-Access-Client-Id: test-client-id" "$fixtures/curl-args"
grep -Fq "CF-Access-Client-Secret: test-client-secret" "$fixtures/curl-args"
printf 'PASS: deployed commit verification sends both Cloudflare Access headers\n'

if FIXTURES="$fixtures" PATH="$tmp:$PATH" \
    MULTICA_POST_DEPLOY_CLI="$tmp/multica" \
    MULTICA_POST_DEPLOY_TIMEOUT=0 \
    MULTICA_POST_DEPLOY_VERSION_URL="https://live.example/version" \
    MULTICA_POST_DEPLOY_EXPECTED_COMMIT="expected-sha" \
    CEREBRO_CF_ACCESS_CLIENT_ID="half-pair" \
    bash "$check_script" >"$tmp/stdout" 2>"$tmp/stderr"; then
  printf 'FAIL: half-configured Cloudflare Access pair unexpectedly passed\n' >&2
  exit 1
fi
grep -Fq "must be configured together" "$tmp/stderr"
printf 'PASS: half-configured Cloudflare Access credentials fail closed\n'

cat >"$fixtures/version.json" <<'JSON'
{"commit":"old-sha","builtAt":"2026-08-03T05:59:00Z"}
JSON
expect_fail "the gate waits for the exact deployed commit" "deployed commit is old-sha; waiting for expected-sha"
cat >"$fixtures/version.json" <<'JSON'
{"commit":"expected-sha","builtAt":"2026-08-03T06:00:00Z"}
JSON

cat >"$fixtures/runtime-list.json" <<'JSON'
[{
  "id":"online-empty",
  "name":"Empty Runtime",
  "status":"online",
  "capabilities":{}
}]
JSON
expect_fail "an online zero-tool Runtime fails" "Empty Runtime (online-empty) offers zero tools"

cat >"$fixtures/runtime-list.json" <<'JSON'
[{
  "id":"online-missing",
  "name":"Missing Capability Report",
  "status":"online"
}]
JSON
expect_fail "a missing Runtime capability report fails closed" "offers zero tools while online"

cat >"$fixtures/runtime-list.json" <<'JSON'
[{"id":"offline-only","name":"Offline Runtime","status":"offline"}]
JSON
expect_fail "a deploy with no verifiable online Runtime fails closed" "no online Runtime was available"

python3 - "$deploy_script" <<'PY'
from pathlib import Path
import sys

text = Path(sys.argv[1]).read_text()
call = 'scripts/cerebro/check-live-runtime-tools-post-deploy.sh'
smoke = text.index('if ! smoke_test; then')
mark_success = text.index('echo "$NEW_SHA" > "$LAST_OK_FILE"')
assert call in text, "deploy must invoke the live Runtime tool gate"
assert smoke < text.index(call) < mark_success, "live Runtime tool gate must run after smoke and before success"
assert 'MULTICA_POST_DEPLOY_EXPECTED_COMMIT="$NEW_SHA"' in text
gate_start = text.index('if ! MULTICA_POST_DEPLOY_CLI=')
gate_end = text.index('# Smoke test passed.', gate_start)
gate = text[gate_start:gate_end]
assert "rollback_to_old" not in gate, "a live gate failure must not roll back only the frontend"
PY
printf 'PASS: every canonical deploy runs the live Runtime tool gate before success\n'

python3 - "$ci_workflow" <<'PY'
from pathlib import Path
import sys

text = Path(sys.argv[1]).read_text()
assert "bash scripts/cerebro/test-live-runtime-tools-post-deploy.sh" in text, (
    "CI must run the post-deploy Runtime tool regression contract"
)
PY
printf 'PASS: CI runs the post-deploy Runtime tool regression contract\n'

python3 - "$post_deploy_workflow" <<'PY'
from pathlib import Path
import sys

text = Path(sys.argv[1]).read_text()
job = "post-deploy-live-runtime-tools:"
assert "branches: [main, production]" in text, "every Sliplane deploy branch must trigger the gate"
assert "queue: max" in text, "all pending deployed commits must retain a live verdict"
assert "cancel-in-progress: false" in text, "a newer push must not cancel an in-flight deploy verdict"
assert job in text, "the Sliplane workflow must have a post-deploy live Runtime tool job"
section = text[text.index(job):]
assert "scripts/cerebro/check-live-runtime-tools-post-deploy.sh" in section, (
    "the Sliplane workflow must run the shared live Runtime tool checker"
)
assert "MULTICA_TOKEN: ${{ secrets.MULTICA_TOKEN }}" in section
assert "MULTICA_SERVER_URL: ${{ vars.MULTICA_SERVER_URL }}" in section
assert "MULTICA_WORKSPACE_ID: ${{ vars.MULTICA_WORKSPACE_ID }}" in section
assert "MULTICA_APP_URL: ${{ vars.MULTICA_APP_URL }}" in section
assert ': "${MULTICA_WORKSPACE_ID:?environment variable MULTICA_WORKSPACE_ID is required}"' in section
assert "MULTICA_POST_DEPLOY_GRACE: 120" in section
assert 'MULTICA_POST_DEPLOY_VERSION_URL=${MULTICA_APP_URL%/}/version' in section
assert "MULTICA_POST_DEPLOY_EXPECTED_COMMIT: ${{ github.sha }}" in section
PY
printf 'PASS: every Sliplane deploy branch runs the authenticated post-deploy gate\n'

python3 - "$deploy_runbook" <<'PY'
from pathlib import Path
import sys

text = Path(sys.argv[1]).read_text()
assert "cerebro-post-deploy-live-runtime-tools.yml" in text
assert "A failed live Runtime tool gate means the deployment is not accepted" in " ".join(text.split())
PY
printf 'PASS: the authoritative deploy runbook requires the live Runtime tool gate\n'

python3 - "$rollout_runbook" <<'PY'
from pathlib import Path
import sys

text = Path(sys.argv[1]).read_text()
required = [
    "## Post-deploy live Runtime tool gate",
    "every staging and production deployment",
    "check-live-runtime-tools-post-deploy.sh",
    "## `legacy` retirement gate",
    "seven consecutive 24-hour periods",
    "`cerebro_task_mandate_enforcement` enabled",
    "zero parity denials",
    "72-hour rollback window",
    "workspace IDs, Runtime IDs, agent IDs",
    "`reason_code`",
]
for item in required:
    assert item in text, f"retirement gate must document {item}"
PY
printf 'PASS: the legacy retirement gate has measurable cohort, parity, and rollback criteria\n'
