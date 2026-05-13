#!/usr/bin/env bash
# verify-patches.sh — verify all Forge fork patches are still in place.
# Run locally: bash scripts/verify-patches.sh
# Run in CI:   .depot/workflows/fork-check.yml
# Exit 0 = all patches intact. Non-zero = list failed checks.

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PASS=0
FAIL=0
FAILURES=()

check() {
  local desc="$1"
  local cmd="$2"
  if eval "$cmd" &>/dev/null; then
    echo "  ✓  $desc"
    PASS=$((PASS + 1))
  else
    echo "  ✗  $desc"
    FAILURES+=("FAIL: $desc")
    FAIL=$((FAIL + 1))
  fi
}

echo ""
echo "=== Forge Fork Patch Verification ==="
echo ""

# ── 1. Brand / Identity ──────────────────────────────────────────────────────
echo "[ 1 ] Brand / Identity"
check "1.1 Desktop .env.production points to forge.asymbl.app" \
  "grep -q 'forge.asymbl.app' apps/desktop/.env.production"

check "1.2 Electron appId is com.asymbl.forge" \
  "grep -q 'com.asymbl.forge' apps/desktop/electron-builder.yml"

check "1.3 Asymbl mark PNG bundled in renderer/public" \
  "test -f apps/desktop/src/renderer/public/brand/asymbl-mark.png"

check "1.4 Desktop login page reads app URL from self-host runtime config" \
  "grep -q 'requireRuntimeAppUrl\|runtimeConfig' apps/desktop/src/renderer/src/pages/login.tsx"

check "1.5 router.go reads ALLOWED_EMAIL_DOMAINS env var (plural, not singular)" \
  "grep -q 'ALLOWED_EMAIL_DOMAINS' server/cmd/server/router.go"

check "1.6 Email service defaults to forge@asymbl.app" \
  "grep -q 'forge@asymbl.app' server/internal/service/email.go"

echo ""

# ── 2. RBAC ──────────────────────────────────────────────────────────────────
echo "[ 2 ] RBAC"
check "2.1 router.go: POST /api/agents gated to admin/owner" \
  "grep -q 'RequireWorkspaceRole.*CreateAgent\|CreateAgent.*RequireWorkspaceRole' server/cmd/server/router.go"

check "2.3 runtime.go: provider stripped for non-admins" \
  "grep -q 'Provider = \"\"' server/internal/handler/runtime.go"

check "2.3 runtime.go: device_info stripped for non-admins" \
  "grep -q 'DeviceInfo = \"\"' server/internal/handler/runtime.go"

check "2.3 runtime.go: stripProviderFromName helper exists" \
  "grep -q 'stripProviderFromName' server/internal/handler/runtime.go"

check "2.4 daemon.go: PAT path uses requireWorkspaceRole" \
  "grep -q 'requireWorkspaceRole.*owner.*admin' server/internal/handler/daemon.go"

check "2.5 permissions/rules.ts: canCreateAgent defined" \
  "grep -q 'canCreateAgent' packages/core/permissions/rules.ts"

check "2.6 agents-page: isAdmin prop threaded to PageHeaderBar" \
  "grep -q 'isAdmin={isWorkspaceAdmin}' packages/views/agents/components/agents-page.tsx"

check "2.7 runtimes-page: non-admin redirect" \
  "grep -q 'navigation.push' packages/views/runtimes/components/runtimes-page.tsx"

check "2.8 runtime-detail-page: non-admin redirect" \
  "grep -q 'navigation.push' packages/views/runtimes/components/runtime-detail-page.tsx"

check "2.9 app-sidebar: Create workspace gated by isWorkspaceAdmin" \
  "grep -q 'isWorkspaceAdmin' packages/views/layout/app-sidebar.tsx"

echo ""

# ── 3. Infrastructure ─────────────────────────────────────────────────────────
echo "[ 3 ] Infrastructure"
check "3.1 docker-compose.selfhost.yml: POSTHOG_API_KEY wired" \
  "grep -q 'POSTHOG_API_KEY' docker-compose.selfhost.yml"

check "3.2 .goreleaser.yml: homebrew-tap removed" \
  "! grep -q 'homebrew-tap' .goreleaser.yml"

check "3.3 Depot CI deploy workflow exists" \
  "test -f .depot/workflows/deploy.yml"

echo ""

# ── 4. Analytics ──────────────────────────────────────────────────────────────
echo "[ 4 ] Analytics"
check "4.1 PostHog session recording enabled" \
  "grep -q 'disable_session_recording: false' packages/core/analytics/index.ts"

check "4.1 PostHog exception capture enabled" \
  "grep -q 'capture_exceptions: true' packages/core/analytics/index.ts"

echo ""

# ── 5. Design System ──────────────────────────────────────────────────────────
echo "[ 5 ] Design System"
check "5.1 tokens.css: warm paper background (#fafaf7)" \
  "grep -q 'fafaf7' packages/ui/styles/tokens.css"

check "5.1 tokens.css: forest green success token (#1f6d3a)" \
  "grep -q '1f6d3a' packages/ui/styles/tokens.css"

check "5.1 tokens.css: warm beige border (#e8e6de)" \
  "grep -q 'e8e6de' packages/ui/styles/tokens.css"

check "5.2 web layout: Fraunces is the brand serif (not Source Serif 4)" \
  "grep -q 'Fraunces' apps/web/app/layout.tsx"

check "5.2 desktop: Fraunces fontsource import" \
  "grep -q 'fraunces' apps/desktop/src/renderer/src/main.tsx"

echo ""

# ── Summary ───────────────────────────────────────────────────────────────────
echo "=== Results: $PASS passed, $FAIL failed ==="
echo ""

if [ ${#FAILURES[@]} -gt 0 ]; then
  echo "Failed checks:"
  for f in "${FAILURES[@]}"; do
    echo "  $f"
  done
  echo ""
  echo "See docs/fork-patches.md for how to restore each patch."
  exit 1
fi

echo "All Forge patches intact."
exit 0
