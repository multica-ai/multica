# Forge Fork Patches

Forge is Asymbl's self-hosted fork of [multica-ai/multica](https://github.com/multica-ai/multica).
This document is the authoritative record of every deliberate divergence from upstream.

**Purpose of this file:**
- Tell future engineers exactly what we changed and why
- Drive the upstream-sync checklist — every section = one thing to re-verify after a cherry-pick
- Power the CI guard (see `.depot/workflows/fork-check.yml`) which fails if a patch is silently lost

---

## How to use this document

### Syncing from upstream
```bash
git remote add upstream https://github.com/multica-ai/multica.git
git fetch upstream
git cherry-pick <upstream-commit-sha>  # or git merge upstream/main --no-ff
```
After every sync: run through **every section below** and verify the patch still holds.
The CI guard will catch regressions automatically but this doc tells you *what* to fix.

### Adding a new patch
1. Make the code change
2. Add an entry to this file under the right category
3. Add a corresponding grep check to `scripts/verify-patches.sh`
4. Commit both together

---

## Category 1 — Brand / Identity

All user-visible Multica references replaced with Forge/Asymbl.

### 1.1 Desktop production URLs
**File:** `apps/desktop/.env.production`
**Change:** `VITE_API_URL`, `VITE_WS_URL`, `VITE_APP_URL` all point to `https://forge.asymbl.app` (were `multica.ai`).
**Why:** Desktop app must connect to our self-hosted server, not upstream's cloud.
**Verify:** `grep -q "forge.asymbl.app" apps/desktop/.env.production`

### 1.2 Electron app identity
**File:** `apps/desktop/electron-builder.yml`
**Change:** `appId: com.asymbl.forge`, `productName: Forge`, `copyright: "Copyright © 2026 Asymbl"`.
**Why:** Mac bundle ID and app name must reflect our brand.
**Verify:** `grep -q "com.asymbl.forge" apps/desktop/electron-builder.yml`

### 1.3 Asymbl logo in desktop login
**File:** `packages/ui/components/common/multica-icon.tsx`
**Change:** `ASSET_SRC = "/brand/asymbl-mark.png"` — serves Asymbl brand mark instead of Multica asterisk glyph.
**File:** `apps/desktop/src/renderer/public/brand/asymbl-mark.png` — asset bundled into renderer.
**Why:** Electron uses `file://` protocol; absolute paths fail without the asset in renderer/public.
**Verify:** `test -f apps/desktop/src/renderer/public/brand/asymbl-mark.png`

### 1.4 Forge desktop Google OAuth redirect
**File:** `apps/desktop/src/renderer/src/pages/login.tsx`
**Change:** `WEB_URL = import.meta.env.VITE_APP_URL` — derives redirect from env rather than hardcoding. Together with 1.1 this sends Google OAuth to `forge.asymbl.app/login?platform=desktop`.
**Verify:** `grep -q "VITE_APP_URL" apps/desktop/src/renderer/src/pages/login.tsx`

### 1.5 Email domain restriction
**File:** `server/internal/handler/auth.go` (upstream patch — check on sync)
**Change:** `ALLOWED_EMAIL_DOMAINS` env var read as plural. Only `@asymbl.com` emails accepted.
**Critical:** The env var is `ALLOWED_EMAIL_DOMAINS` (with S). `ALLOWED_EMAIL_DOMAIN` (singular) is silently ignored.
**Doppler key:** `forge/prd → ALLOWED_EMAIL_DOMAINS=asymbl.com`
**Verify:** `grep -q "ALLOWED_EMAIL_DOMAINS" server/cmd/server/router.go`

### 1.6 Email sender / templates
**File:** `server/internal/service/email.go`
**Change:** Default FROM address `forge@asymbl.app`; fallback app URL → `forge.asymbl.app`.
**Verify:** `grep -q "forge@asymbl.app" server/internal/service/email.go`

---

## Category 2 — RBAC (Security hardening)

Upstream Multica has no role-based restrictions on agent/runtime management.
We added them because multiple agent providers (Hermes, OpenClaw) are infrastructure details
that should not be visible to workspace members.

### 2.1 POST /api/agents — admin/owner only (B1)
**File:** `server/cmd/server/router.go`
**Change:** `r.With(middleware.RequireWorkspaceRole(queries, "owner", "admin")).Post("/", h.CreateAgent)`
**Why:** Members should not create agents; only admins configure the AI fleet.
**Verify:** `grep -q 'RequireWorkspaceRole.*CreateAgent' server/cmd/server/router.go`

### 2.2 GET /api/runtimes — member-accessible with sensitive fields stripped (B2)
**File:** `server/cmd/server/router.go`
**Change:** `GET /` is now member-accessible (no role gate). All per-runtime sub-routes (`/usage`, `/activity`, `/update`, `DELETE /`, etc.) remain under `r.Use(middleware.RequireWorkspaceRole(...))` inside the `/{runtimeId}` sub-router.
**Why:** Members need runtime status for agent online/offline display (presence map) but must not manage runtimes.
**Verify:** Check that the `/{runtimeId}` group still has RequireWorkspaceRole but the root `r.Get("/", ...)` does not.

### 2.3 Runtime response field stripping for non-admins (B2 handler)
**File:** `server/internal/handler/runtime.go`
**Change:** `ListAgentRuntimes` strips for non-admins:
- `item.Provider = ""`
- `item.LaunchHeader = ""`
- `item.DeviceInfo = ""` (e.g. "Hermes Agent v0.11.0...", "OpenClaw 2026.4.26")
- `item.Metadata = map[string]any{}`
- `item.Name = stripProviderFromName(item.Name, rt.Provider)` — strips "Hermes (" prefix, e.g. "Hermes (ben-corpay)" → "ben-corpay"
**Why:** Daemon names runtimes as "Hermes (device-name)" — both name and device_info reveal provider type.
**Verify:** `grep -q "stripProviderFromName" server/internal/handler/runtime.go`
**Verify:** `grep -q 'DeviceInfo = ""' server/internal/handler/runtime.go`

### 2.4 Daemon PAT registration — admin/owner only (B3)
**File:** `server/internal/handler/daemon.go`
**Change:** PAT-authenticated `DaemonRegister` uses `h.requireWorkspaceRole(w, r, req.WorkspaceID, "workspace not found", "owner", "admin")` instead of `requireWorkspaceMember`.
**Why:** Only admins should be able to connect new remote machines to the workspace.
**Note:** `mdt_` daemon tokens bypass this check via `DaemonWorkspaceIDFromContext` — Ben droplets are unaffected.
**Verify:** `grep -q 'requireWorkspaceRole.*owner.*admin' server/internal/handler/daemon.go`

### 2.5 Frontend permission rules (F1)
**File:** `packages/core/permissions/rules.ts`
**Change:** Added `canCreateAgent`, `canViewRuntimes`, `canConnectRuntime` — all require `isAdminLike` role.
**Verify:** `grep -q "canCreateAgent" packages/core/permissions/rules.ts`

### 2.6 Agents page — create button gated, runtimeListOptions ordered (F2)
**File:** `packages/views/agents/components/agents-page.tsx`
**Changes:**
- `memberListOptions` query moved before `runtimeListOptions` (hook order change)
- `PageHeaderBar` receives `isAdmin` prop — button `disabled={!isAdmin}`
- `EmptyState` conditionally renders create button: `{isAdmin && <Button>}`
- All `PageHeaderBar` / `EmptyState` call sites pass `isAdmin={isWorkspaceAdmin}`
**Verify:** `grep -q "isAdmin={isWorkspaceAdmin}" packages/views/agents/components/agents-page.tsx`

### 2.7 Runtimes page — member redirect + connect button gate (F3)
**File:** `packages/views/runtimes/components/runtimes-page.tsx`
**Changes:**
- `memberListOptions` + `isAdmin` computed before `runtimeListOptions` (enabled: isAdmin)
- Skeleton condition: `isLoading || membersLoading || fetching`
- After loading: `if (!isAdmin) { navigation.push("/"); return null; }`
- `PageHeaderBar` / `EmptyState` hide connect button for non-admins
**Verify:** `grep -q "navigation.push" packages/views/runtimes/components/runtimes-page.tsx`

### 2.8 Runtime detail page — member redirect (F3 detail)
**File:** `packages/views/runtimes/components/runtime-detail-page.tsx`
**Change:** Fetches member role; if `!isAdmin && myRole !== null` redirects to `/`.
**Why:** Members can navigate directly to `/runtimes/{id}` via URL — must be blocked.
**Verify:** `grep -q "navigation.push" packages/views/runtimes/components/runtime-detail-page.tsx`

### 2.9 Sidebar — "Create workspace" hidden for members
**File:** `packages/views/layout/app-sidebar.tsx`
**Change:** `isWorkspaceAdmin` derived from `memberListOptions`; `Create workspace` menu item only renders when admin.
**Verify:** `grep -q "isWorkspaceAdmin" packages/views/layout/app-sidebar.tsx`

---

## Category 3 — Infrastructure / Deployment

### 3.1 Docker Compose — PostHog env vars
**File:** `docker-compose.selfhost.yml`
**Change:** Added `POSTHOG_API_KEY: ${POSTHOG_API_KEY:-}` and `POSTHOG_HOST: ${POSTHOG_HOST:-https://us.i.posthog.com}` to backend service environment.
**Why:** Upstream doesn't ship with PostHog wired; we track Forge platform usage.
**Verify:** `grep -q "POSTHOG_API_KEY" docker-compose.selfhost.yml`

### 3.2 GoReleaser — homebrew tap removed
**File:** `.goreleaser.yml`
**Change:** `brews:` section removed (was pointing to `multica-ai/homebrew-tap` with an upstream token we don't have).
**Why:** Prevents release failures on our fork.
**Verify:** `! grep -q "homebrew-tap" .goreleaser.yml`

### 3.3 CI/CD — Depot workflows
**Directory:** `.depot/workflows/`
**Files:** `ci.yml`, `deploy.yml`, `release.yml`
**Change:** All CI moved to Depot (org `p6gdqmvg63`, project `kx9jqgpx56`). GitHub Actions stubs in `.github/workflows/` redirect to Depot.
**deploy.yml note:** `DOPPLER_TOKEN` Depot CI secret refreshes `/root/.env` on every deploy. New Doppler secrets require a push to main to take effect.
**Verify:** `test -f .depot/workflows/deploy.yml`

### 3.4 Cloudflare Tunnel config
**Location:** `/etc/cloudflared/config.yml` on droplet `209.38.78.178`
**Change:** Ingress rules for `forge.asymbl.app` (backend port 8080, frontend port 3000) and `forge-kuma.asymbl.app` (Uptime Kuma port 3001).
**Not in git** — managed on the droplet directly.
**Verify (manual):** `ssh root@209.38.78.178 cat /etc/cloudflared/config.yml`

---

## Category 4 — Analytics / Observability

### 4.1 PostHog session recording and exception capture
**File:** `packages/core/analytics/index.ts`
**Change:**
- `capture_exceptions: true` (was `false`)
- `disable_session_recording: false` (was `true`)
- `session_recording: { maskAllInputs: true, maskTextSelector: "[data-ph-no-capture]" }`
**Why:** Upstream disables all capture by default (minimal SaaS funnel setup). Forge needs session replay and error tracking for an internal tool.
**PostHog project:** Asymbl-Forge (id: 406520, US Cloud)
**Verify:** `grep -q "capture_exceptions: true" packages/core/analytics/index.ts`

---

## CI Guard

The script `scripts/verify-patches.sh` checks every `Verify:` entry above.
It runs as the `fork-check` job in `.depot/workflows/fork-check.yml` on every PR to `main`.

To run locally:
```bash
bash scripts/verify-patches.sh
```

Exit code 0 = all patches intact. Non-zero = list of failed checks printed.

---

## Known upstream features we don't use

| Feature | Status | Reason |
|---------|--------|--------|
| Homebrew tap | Skipped | No `multica-ai/homebrew-tap` access |
| Google OAuth | Planned (Phase 2) | OTP-only for now |
| Apple code signing | Planned | DMG is unsigned, distributed via GitHub Releases |
| Horizontal scaling (Redis, multi-instance) | N/A | Single-droplet, in-memory hub |
| Sentry error tracking | Replaced by PostHog | Unified observability |

---

## Upstream sync procedure

```bash
# 1. Fetch upstream
git remote add upstream https://github.com/multica-ai/multica.git 2>/dev/null || true
git fetch upstream

# 2. Inspect what changed
git log upstream/main --oneline --since="last-sync-date"

# 3. Cherry-pick security/bug fixes selectively
git cherry-pick <sha>

# 4. Run patch verification
bash scripts/verify-patches.sh

# 5. Run full test suite
make check

# 6. If any verify fails, re-apply the patch from this doc
# 7. Commit with message: "chore: sync upstream <sha> — re-apply Forge patches"
```

**Never** `git merge upstream/main` without reviewing the diff first — upstream branding changes will overwrite ours.
