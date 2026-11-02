#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Next.js and TestApiClient load dotenv files. Require a disposable checkout so
# local application credentials cannot leak into this isolated gate.
shopt -s nullglob
for file in .env .env.worktree apps/web/.env apps/web/.env.*; do
  [[ "$file" == *.example ]] && continue
  if [[ -f "$file" ]]; then
    printf 'Retained E2E requires a checkout without %s\n' "$file" >&2
    exit 1
  fi
done

# A renamed or removed spec must fail before building, rather than silently
# reducing Playwright's testMatch to the remaining file.
for spec in e2e/onboarding-smoke.spec.ts e2e/dag-task-lines.spec.ts e2e/issue-dependencies.spec.ts e2e/comments.spec.ts; do
  if [[ ! -f "$spec" ]]; then
    printf 'Required retained E2E spec is missing: %s\n' "$spec" >&2
    exit 1
  fi
done

# The CI job owns the PostgreSQL service at this endpoint. Local reproduction
# must provision the same disposable database; never forward a production DB.
export DATABASE_URL='postgres://labrastro_e2e:labrastro_e2e@127.0.0.1:15432/labrastro_e2e?sslmode=disable'
export DATABASE_REPLICA_URL=''
export PORT=18080 FRONTEND_PORT=13000
export FRONTEND_ORIGIN='http://127.0.0.1:13000'
export MULTICA_APP_URL="$FRONTEND_ORIGIN" PLAYWRIGHT_BASE_URL="$FRONTEND_ORIGIN"
export NEXT_PUBLIC_API_URL='http://127.0.0.1:18080'
export NEXT_PUBLIC_WS_URL='ws://127.0.0.1:18080/ws'
export REMOTE_API_URL="$NEXT_PUBLIC_API_URL" CORS_ALLOWED_ORIGINS="$FRONTEND_ORIGIN"
export APP_ENV=test JWT_SECRET='retained-e2e-disposable-test-secret'
export MULTICA_DEV_VERIFICATION_CODE='' RESEND_API_KEY='' SMTP_HOST='' REDIS_URL=''
export DOCS_URL='' DO_NOT_TRACK=1 NEXT_TELEMETRY_DISABLED=1 ANALYTICS_DISABLED=1

mkdir -p test-results/retained-e2e
(
  cd server &&
  go build -o bin/retained-e2e-server ./cmd/server &&
  go run ./cmd/migrate up
) > test-results/retained-e2e/backend-setup.log 2>&1 || {
  cat test-results/retained-e2e/backend-setup.log
  exit 1
}
pnpm --filter @multica/web build > test-results/retained-e2e/web-build.log 2>&1 || {
  cat test-results/retained-e2e/web-build.log
  exit 1
}
pnpm exec playwright test --config playwright.retained.config.ts
