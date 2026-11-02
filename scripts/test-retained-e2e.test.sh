#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/checkout/scripts" "$fixture/checkout/server" "$fixture/checkout/apps/web" "$fixture/bin"
mkdir -p "$fixture/checkout/e2e"
touch "$fixture/checkout/e2e/onboarding-smoke.spec.ts" "$fixture/checkout/e2e/dag-task-lines.spec.ts" "$fixture/checkout/e2e/issue-dependencies.spec.ts" "$fixture/checkout/e2e/comments.spec.ts"
cp "$repo_root/scripts/test-retained-e2e.sh" "$fixture/checkout/scripts/"
export RETAINED_TEST_CALLS="$fixture/calls"

# Fake build tools exercise sequencing and exported endpoints without opening
# a socket, creating a database or launching a browser.
cat > "$fixture/bin/go" <<'STUB'
#!/usr/bin/env bash
set -eu
printf '%s %s\n' "$(basename "$0")" "$*" >> "$RETAINED_TEST_CALLS"
[[ "$DATABASE_URL" == 'postgres://labrastro_e2e:labrastro_e2e@127.0.0.1:15432/labrastro_e2e?sslmode=disable' ]]
[[ "$NEXT_PUBLIC_API_URL" == 'http://127.0.0.1:18080' ]]
[[ "$NEXT_PUBLIC_WS_URL" == 'ws://127.0.0.1:18080/ws' ]]
[[ "$FRONTEND_ORIGIN" == 'http://127.0.0.1:13000' ]]
[[ -z "$DATABASE_REPLICA_URL$SMTP_HOST$RESEND_API_KEY$REDIS_URL" ]]
[[ "$DO_NOT_TRACK" == 1 && "$NEXT_TELEMETRY_DISABLED" == 1 && "$ANALYTICS_DISABLED" == 1 ]]
[[ -z "${RETAINED_TEST_FAIL:-}" || "$*" != *"$RETAINED_TEST_FAIL"* ]]
STUB
cp "$fixture/bin/go" "$fixture/bin/pnpm"
chmod +x "$fixture/bin/go" "$fixture/bin/pnpm"
export PATH="$fixture/bin:$PATH"
export DATABASE_URL='postgres://must-not-be-used.invalid/app'
export NEXT_PUBLIC_API_URL='https://must-not-be-used.invalid'
export DATABASE_REPLICA_URL=invalid SMTP_HOST=invalid RESEND_API_KEY=invalid REDIS_URL=invalid
export DO_NOT_TRACK=0 NEXT_TELEMETRY_DISABLED=0 ANALYTICS_DISABLED=0 POSTHOG_API_KEY=invalid

runner="$fixture/checkout/scripts/test-retained-e2e.sh"
bash "$runner"
[[ "$(wc -l < "$RETAINED_TEST_CALLS")" -eq 4 ]]
grep -Fxq 'pnpm exec playwright test --config playwright.retained.config.ts' "$RETAINED_TEST_CALLS"

# Any build/migration/browser failure must stop the gate, never report success
# or proceed into later phases (especially migration after a failed build).
for failure in 'go build' 'go run' 'web build' 'playwright test'; do
  : > "$RETAINED_TEST_CALLS"
  export RETAINED_TEST_FAIL="${failure#go }"
  if bash "$runner" > "$fixture/result" 2>&1; then
    echo "Expected failure to propagate: $failure" >&2
    exit 1
  fi
  case "$failure" in
    'go build') expected=1 ;;
    'go run') expected=2 ;;
    'web build') expected=3 ;;
    'playwright test') expected=4 ;;
  esac
  [[ "$(wc -l < "$RETAINED_TEST_CALLS")" -eq "$expected" ]]
done
unset RETAINED_TEST_FAIL

for spec in onboarding-smoke.spec.ts dag-task-lines.spec.ts issue-dependencies.spec.ts comments.spec.ts; do
  : > "$RETAINED_TEST_CALLS"
  rm "$fixture/checkout/e2e/$spec"
  if bash "$runner" > "$fixture/result" 2>&1; then
    echo "Expected missing required spec to be rejected: $spec" >&2
    exit 1
  fi
  [[ ! -s "$RETAINED_TEST_CALLS" ]]
  grep -Fq "Required retained E2E spec is missing: e2e/$spec" "$fixture/result"
  touch "$fixture/checkout/e2e/$spec"
done

for dotenv in .env .env.worktree apps/web/.env apps/web/.env.production.local; do
  : > "$RETAINED_TEST_CALLS"
  touch "$fixture/checkout/$dotenv"
  if bash "$runner" > "$fixture/result" 2>&1; then
    echo "Expected dotenv override to be rejected: $dotenv" >&2
    exit 1
  fi
  [[ ! -s "$RETAINED_TEST_CALLS" ]]
  grep -Fq "$dotenv" "$fixture/result"
  rm "$fixture/checkout/$dotenv"
done
echo "Retained E2E enforces specs, endpoints and disabled telemetry; setup/build/browser failures stop the gate."
