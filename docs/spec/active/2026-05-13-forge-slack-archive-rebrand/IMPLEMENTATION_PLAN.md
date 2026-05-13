---
document_type: implementation_plan
project_id: SPEC-2026-05-13-001
version: 1.0.0
last_updated: 2026-05-13T21:50:00+05:30
status: in-review
estimated_effort: "3.5 dev-days (28 hours)"
---

# Forge: Slack + Archive + Tab Rebrand — Implementation Plan

## Overview

Phased rollout. Tab rebrand + Archive relabel are mechanical and ship first (Phase 1). Slack integration is the bulk of the work (Phases 2-4). Phase 5 verifies and ships.

## Phase Summary

| Phase | Duration | Key Deliverables |
|---|---|---|
| **Phase 1: Quick wins** | 1 hour | Tab rebrand + Archive relabel + `verify-patches.sh` updates |
| **Phase 1b: Brand audit fixes** | 2 hours | Fix B1–B10 findings from jcodemunch+LSP audit; co-authored-by CRITICAL bug |
| **Phase 2: Slack backend** | 8 hours | DB migration, sqlc queries, Go integration package, handler, router |
| **Phase 3: Slack notification hook** | 2 hours | 5-line wire-up in `notification_listeners.go` + recover-from-panic test |
| **Phase 4: Slack frontend** | 6 hours | Settings card, API client, hooks, tests |
| **Phase 5: Tests + Verify + Ship** | 4 hours | Full test suite, verify-patches, manual smoke, deploy |
| **Phase 6: Post-deploy verification** | 1 hour | Production smoke + Ben fleet check |

**Total: ~24 hours focused dev time (3 working days with buffer)**

---

## Phase 1: Quick wins — Tab rebrand + Archive relabel

**Duration**: 1 hour
**Goal**: Ship the easy renames so the brand leak in the tab is gone and "Archive" replaces "Cancelled".
**Prerequisites**: None.

### Task 1.1: Fix browser tab metadata

- **Description**: Replace Multica metadata in Next.js layout with Forge/Asymbl branding.
- **File**: `apps/web/app/layout.tsx`
- **Effort**: 15 min
- **Acceptance**:
  - [ ] `metadataBase` is `https://forge.asymbl.app`
  - [ ] `title.default` contains "Forge" (e.g. `"Forge — Asymbl's AI-native project workspace"`)
  - [ ] `title.template` is `"%s | Forge"`
  - [ ] `description` updated to Asymbl Forge wording
  - [ ] `openGraph.siteName` is `"Forge"`
  - [ ] `twitter.site`/`creator` updated or removed if no handle yet
- **Notes**: Mirror changes in `apps/web/app/(landing)/layout.tsx` lines 24, 30

### Task 1.2: Replace favicon

- **Description**: The current `apps/web/public/favicon.svg` is the Multica asterisk. Two options:
  - A) Replace SVG contents with Asymbl bracket+dot mark
  - B) Delete `favicon.svg` so browsers fall through to `/favicon.ico` which redirects to `/brand/favicon.png` (already Asymbl)
- **Recommendation**: Option B — simpler, uses existing Asymbl asset
- **File**: `apps/web/public/favicon.svg` (delete) + update layout.tsx icons metadata to point at `/brand/favicon.png`
- **Effort**: 15 min
- **Acceptance**:
  - [ ] Hard reload forge.asymbl.app
  - [ ] Tab favicon shows Asymbl bracket+dot mark (orange/blue), NOT the black asterisk
  - [ ] Tab title contains "Forge"

### Task 1.3: Relabel `cancelled` → "Archive" in UI

- **Description**: 5 locations updated, no DB change, no backend logic change.
- **Files**:
  - `packages/core/issues/config/status.ts:49` — `label: "Cancelled"` → `"Archive"`
  - `packages/views/locales/en/issues.json:16` — `"cancelled": "Cancelled"` → `"Archive"`
  - `packages/views/locales/en/issues.json:240` — `"status_cancelled": "Cancelled"` → `"Archive"`
  - `packages/views/locales/en/projects.json:21` — `"cancelled": "Cancelled"` → `"Archive"`
  - `server/cmd/server/notification_listeners.go:31` — `"cancelled": "Cancelled"` → `"Archive"`
  - `packages/views/locales/zh-Hans/issues.json` — search and update if upstream zh has a translation
  - `packages/views/locales/zh-Hans/projects.json` — same
- **Effort**: 20 min
- **Acceptance**:
  - [ ] `grep -r '"Cancelled"' packages/` returns only DB/enum value strings, not display labels
  - [ ] Status filter dropdown in the issues page shows "Archive" not "Cancelled"
  - [ ] Inbox notification for a `cancelled` status change says "moved to Archive"

### Task 1.4: Add tab rebrand + Archive checks to `verify-patches.sh`

- **Description**: Prevent regression at next upstream sync.
- **File**: `scripts/verify-patches.sh`
- **Effort**: 10 min
- **Acceptance** — these new checks added under a new "Brand polish" section:
  - [ ] Check 1.7: `grep -q '"Forge — \|forge.asymbl.app' apps/web/app/layout.tsx` (tab title)
  - [ ] Check 1.8: `! grep -q '"Multica' apps/web/app/layout.tsx` (no Multica brand strings)
  - [ ] Check 1.9: `grep -q '"Archive"' packages/core/issues/config/status.ts` (archive label present)
- **Run**: `bash scripts/verify-patches.sh` — must show 29/29 passed (26 existing + 3 new)

### Phase 1 deliverables

- [ ] forge.asymbl.app tab shows Asymbl favicon + "Forge" in title
- [ ] All "Cancelled" labels in UI now say "Archive"
- [ ] `verify-patches.sh` has 3 new checks, total ≥ 29

### Phase 1 exit criteria

- [ ] Local `pnpm typecheck` passes
- [ ] Local `bash scripts/verify-patches.sh` passes
- [ ] Manual visual check: load forge.asymbl.app, verify tab + Archive label
- [ ] Ready to commit

---

## Phase 1b: Brand audit fixes (B1–B10)

**Duration**: 2 hours
**Goal**: Fix every Multica brand string found by the jcodemunch + LSP audit. B1 is a silent functional regression — fix it first.
**Prerequisites**: None (independent of Phase 1, can ship same commit).

> **Audit source**: jcodemunch `search_text` + manual `grep` on 2026-05-13.
> Full list in REQUIREMENTS.md §Rebrand Audit Findings.

---

### Task 1b.1: Fix co-authored-by hook script — B1 CRITICAL

**Files**:
- `server/internal/daemon/repocache/cache.go`
- `server/internal/daemon/repocache/cache_test.go`

**Effort**: 30 min

**Change in `cache.go`**:

```go
// Line 772: rename the sentinel constant (keep old value in signatures list)
const forgeHookMarker = "# forge:prepare-commit-msg:co-authored-by"

// Line 782-785: add forge marker to recognition list, keep legacy multica entry
var daemonInstalledHookSignatures = []string{
    forgeHookMarker,
    multicaHookMarker,                   // keep for legacy hook removal
    "# Installed by the Multica daemon.", // keep for legacy hook removal
}

// Line 789-811: update the hook script
const prepareCommitMsgHook = `#!/bin/sh
# forge:prepare-commit-msg:co-authored-by
# Forge: add Co-authored-by trailer for the Forge Agent.
# Installed by the Forge daemon. Do not edit — it will be overwritten.

COMMIT_MSG_FILE="$1"
COMMIT_SOURCE="$2"

case "$COMMIT_SOURCE" in
  merge|squash) exit 0 ;;
esac

TRAILER="Co-authored-by: forge-agent <github@asymbl.com>"

if grep -qF "$TRAILER" "$COMMIT_MSG_FILE"; then
  exit 0
fi

git interpret-trailers --in-place --trailer "$TRAILER" "$COMMIT_MSG_FILE"
`
```

**Why keep the legacy `multica` markers in `daemonInstalledHookSignatures`**: The comment is explicit — `"Add to this list — never remove from it"`. Existing Ben droplets that have the old `multica-agent` hook installed need `removeCoAuthoredByHook` to recognize and remove it when the user disables the setting. If we remove the marker, old hooks on running daemons become orphans. Keep both.

**Change in `cache_test.go`** (5 locations):

```go
// All 5 occurrences: replace multica-agent with forge-agent
// Line 1158: expectedTrailer := "Co-authored-by: forge-agent <github@asymbl.com>"
// Line 1190: same
// Line 1277: if strings.Contains(commitMsg, "Co-authored-by: forge-agent") {
// Line 1315: TRAILER="Co-authored-by: forge-agent <github@asymbl.com>"
// Line 1363: if commitMsg := string(out); strings.Contains(commitMsg, "Co-authored-by: forge-agent") {
```

**Acceptance**:
- [ ] `go test ./server/internal/daemon/repocache/...` passes (was passing with wrong expected value before)
- [ ] Create a test commit in an agent worktree: `git log` shows `Co-authored-by: forge-agent <github@asymbl.com>`
- [ ] Grep: `grep -r 'multica-agent' server/internal/daemon/repocache/*.go` returns 0 results (excluding the legacy signatures list)

---

### Task 1b.2: Fix desktop app name — B5 + B7

**Files**:
- `apps/desktop/src/main/index.ts`
- `apps/desktop/package.json`

**Effort**: 20 min

**Change in `index.ts`**:

```typescript
// Line 230-231: rename dev window title
const DEV_APP_NAME =
  process.env.DESKTOP_APP_SUFFIX
  ? `Forge Canary ${process.env.DESKTOP_APP_SUFFIX}`
  : "Forge Canary";

// Line 243: fix runtime app name (WM_CLASS source)
app.setName("Forge");
```

**Change in `apps/desktop/package.json`**:

```json
{
  "productName": "Forge",
  "description": "Forge Desktop — Asymbl's AI-native project workspace.",
  "homepage": "https://forge.asymbl.app",
  "author": {
    "name": "Asymbl",
    "email": "support@asymbl.com"
  }
}
```

**Acceptance**:
- [ ] Desktop titlebar shows "Forge" not "Multica"
- [ ] On Linux: `xprop WM_CLASS` on running Forge window shows `"Forge", "Forge"` (matches `StartupWMClass: Forge` in electron-builder.yml)
- [ ] `verify-patches.sh` new check passes (see Task 1b.5)

---

### Task 1b.3: Fix daemon runtime docs link — B8

**File**: `packages/views/runtimes/components/runtimes-page.tsx`

**Effort**: 10 min

**Change**: Replace `https://multica.ai/docs/daemon-runtimes` with Forge-relevant target.

Options (decide at implementation time):
- A) Point to Asymbl docs when they exist: `https://forge.asymbl.app/docs/daemon-runtimes`
- B) Remove the link entirely (safest — no dead link) and remove the surrounding anchor element

**Recommendation**: Option B until Forge has its own docs site.

**Acceptance**:
- [ ] No `multica.ai` link visible in the runtimes settings panel
- [ ] No 404 when clicking what used to be the docs link

---

### Task 1b.4: Fix ACP client name in agent packages — B9 (Low priority, last)

**Files**: `server/pkg/agent/codex.go`, `hermes.go`, `kimi.go`, `kiro.go`

**Effort**: 10 min

**Change**: In each file, replace `"name": "multica-agent-sdk"` with `"name": "forge-agent-sdk"` in the ACP `initialize` handshake.

**Note**: This name is sent to external AI agent processes (Codex, Hermes, Kimi, Kiro). The agents themselves may or may not display it. Not user-visible in Forge UI but technically wrong.

**Acceptance**:
- [ ] `grep -r 'multica-agent-sdk' server/pkg/agent/` returns 0 results
- [ ] `go build ./server/...` still passes

---

### Task 1b.5: Add B1-B10 checks to `verify-patches.sh`

**File**: `scripts/verify-patches.sh`

**Effort**: 20 min

**New section — "Brand audit" (Section 7)**:

```bash
echo ""
echo "[ 7 ] Brand Audit (B1-B10)"

check "7.1 co-authored-by hook: forge-agent not multica-agent" \
  "grep -q 'forge-agent <github@asymbl.com>' server/internal/daemon/repocache/cache.go"

check "7.2 co-authored-by hook: TRAILER uses forge identity" \
  "! grep -q 'multica-agent' server/internal/daemon/repocache/cache.go"

check "7.3 desktop app.setName is Forge not Multica" \
  "grep -q 'app.setName(\"Forge\")' apps/desktop/src/main/index.ts"

check "7.4 desktop package.json productName is Forge" \
  "node -e \"const p=require('./apps/desktop/package.json'); process.exit(p.productName==='Forge'?0:1)\""

check "7.5 no multica.ai homepage in desktop package.json" \
  "! grep -q 'multica.ai' apps/desktop/package.json"

check "7.6 runtimes-page has no multica.ai external link" \
  "! grep -q 'multica.ai' packages/views/runtimes/components/runtimes-page.tsx"

check "7.7 ACP client name is forge-agent-sdk" \
  "grep -q 'forge-agent-sdk' server/pkg/agent/codex.go && \
   grep -q 'forge-agent-sdk' server/pkg/agent/hermes.go"

check "7.8 web layout has no multica.ai metadataBase" \
  "! grep -q 'multica.ai' apps/web/app/layout.tsx"

check "7.9 web layout twitter handle is not multica_hq" \
  "! grep -q 'multica_hq' apps/web/app/layout.tsx"
```

Expected result after Phase 1 + 1b: **26 existing + 3 Phase 1 brand + 9 Phase 1b brand audit = 38 total checks passing**.

---

### Phase 1b exit criteria

- [ ] `go test ./server/internal/daemon/repocache/...` green (B1 fix validates)
- [ ] `grep -r 'multica-agent' server/internal/daemon/repocache/*.go` → 0 results outside legacy signatures list
- [ ] `bash scripts/verify-patches.sh` → 38/38
- [ ] Desktop builds (`pnpm --filter @multica/desktop build`) still compile

---

## Phase 2: Slack backend (DB + Go package + handler + router)

**Duration**: 8 hours
**Goal**: All backend pieces for Slack — DB schema, queries, integration package, HTTP handler, RBAC-gated routes.
**Prerequisites**: Phase 1 merged (so the branch is clean).

### Task 2.1: Create DB migration

- **Description**: Add `workspace_slack_integrations` table per `ARCHITECTURE.md` Data Models.
- **Files**:
  - `server/migrations/089_workspace_slack_integrations.up.sql` (new)
  - `server/migrations/089_workspace_slack_integrations.down.sql` (new)
- **Effort**: 30 min
- **Note**: Check upstream migration sequence — if upstream has added 089 between when this spec was written and implementation, bump to next available number (090, 091, …). The `verify-patches.sh` add for this should match the chosen number.
- **Acceptance**:
  - [ ] `make migrate-up` succeeds locally
  - [ ] `make migrate-down` rolls it back cleanly
  - [ ] Partial unique index allows exactly one `enabled=true` row per workspace, multiple `enabled=false` allowed

### Task 2.2: sqlc queries

- **Description**: CRUD queries for the new table.
- **File**: `server/pkg/db/queries/slack.sql` (new)
- **Queries needed**:
  - `GetSlackIntegrationForWorkspace(workspace_id)` — used by the listener
  - `UpsertSlackIntegration(...)` — for PUT
  - `DeleteSlackIntegration(workspace_id)` — for DELETE
  - `UpdateSlackIntegrationLastSent(id, last_sent_at, last_error)` — listener writes status
- **Effort**: 1 hour
- **Acceptance**:
  - [ ] `make sqlc` regenerates without error
  - [ ] Generated Go code compiles (`go build ./server/...`)
  - [ ] No new sqlc generation warnings

### Task 2.3: Slack integration package

- **Description**: New Go package per ARCHITECTURE.md Component 1.
- **Files**:
  - `server/internal/integrations/slack/notify.go` (new) — `NotifyStatusChange(...)` entrypoint
  - `server/internal/integrations/slack/format.go` (new) — `BuildMessage(...)` returns Slack webhook JSON body
  - `server/internal/integrations/slack/client.go` (new) — `Post(ctx, url, body)` with 5s timeout + SSRF validation
- **Effort**: 2 hours
- **Acceptance**:
  - [ ] `NotifyStatusChange` is exactly one public function — other code is internal
  - [ ] `BuildMessage` produces valid Slack JSON (verified via Slack's payload spec)
  - [ ] `Post` rejects any URL not starting with `https://hooks.slack.com/services/`
  - [ ] All three files have associated `_test.go` (see Phase 5)

### Task 2.4: HTTP handler

- **Description**: 4 routes per ARCHITECTURE.md Component 3.
- **File**: `server/internal/handler/slack_integration.go` (new)
- **Methods**:
  - `GetSlackIntegration(w, r)` — returns masked URL
  - `PutSlackIntegration(w, r)` — validate URL prefix + status array, upsert
  - `DeleteSlackIntegration(w, r)` — disable + clear
  - `TestSlackIntegration(w, r)` — fire a synthetic message synchronously, return 200 or 502
- **Effort**: 2 hours
- **Acceptance**:
  - [ ] Webhook URL never returned plain text on GET — always masked
  - [ ] PUT validates `https://hooks.slack.com/services/` prefix → 400 on bad URL
  - [ ] PUT validates `trigger_statuses` array values → 400 on unknown status
  - [ ] All 4 handlers use `parseUUIDOrBadRequest` per project handler convention (CLAUDE.md)

### Task 2.5: Router wiring

- **Description**: Wire the 4 routes admin-only.
- **File**: `server/cmd/server/router.go`
- **Effort**: 30 min
- **Change**:
  ```go
  r.Route("/api/workspaces/{wsId}/integrations/slack", func(r chi.Router) {
      r.Use(middleware.RequireWorkspaceRole(queries, "owner", "admin"))
      r.Get("/", h.GetSlackIntegration)
      r.Put("/", h.PutSlackIntegration)
      r.Delete("/", h.DeleteSlackIntegration)
      r.Post("/test", h.TestSlackIntegration)
  })
  ```
- **Acceptance**:
  - [ ] All 4 routes return 403 for member role (manual test)
  - [ ] All 4 routes return 200/204 for owner/admin
  - [ ] `verify-patches.sh` updated with check that Slack admin gate exists

### Phase 2 exit criteria

- [ ] `go build ./server/...` passes
- [ ] `make test` passes (existing tests still green)
- [ ] Postman/curl test: PUT a webhook URL as admin, GET returns it masked, DELETE clears it

---

## Phase 3: Slack notification hook

**Duration**: 2 hours
**Goal**: Wire the new Slack package into the existing event bus listener.

### Task 3.1: Hook into notification_listeners.go

- **Description**: 5-line addition inside the existing `if statusChanged {` block.
- **File**: `server/cmd/server/notification_listeners.go` (~line 654)
- **Effort**: 30 min
- **Change**:
  ```go
  if statusChanged {
      // ... existing inbox notification logic unchanged ...

      // NEW: forward to Slack integration (async, isolated)
      slack.NotifyStatusChange(
          ctx,
          queries,
          issue.WorkspaceID,
          issue,
          prevStatus,
          actorDisplayName(ctx, queries, e.ActorType, e.ActorID),
      )
  }
  ```
- **Acceptance**:
  - [ ] Import added: `"github.com/multica-ai/multica/server/internal/integrations/slack"`
  - [ ] Single function call, no business logic inline
  - [ ] Function returns immediately (verified: the package internally spawns goroutine)

### Task 3.2: Verify panic isolation

- **Description**: Ensure a panic inside Slack code does NOT take down the listener.
- **Effort**: 30 min
- **Test approach**: The existing event bus already has `recover()` in `bus.go`. Confirm by adding a test in `notify_test.go` that forces a panic.
- **Acceptance**:
  - [ ] Manual: deliberately panic inside `NotifyStatusChange`, run a status change, verify other listeners still fire
  - [ ] Unit test: `TestNotifyPanicIsolated` — `bus.Publish` returns normally even if Slack panics

### Task 3.3: Update `verify-patches.sh`

- **Description**: Add check for Slack hook presence.
- **Effort**: 5 min
- **Change**: add check:
  ```bash
  check "6.1 Slack integration hooked in notification_listeners" \
    "grep -q 'slack.NotifyStatusChange' server/cmd/server/notification_listeners.go"
  ```

### Phase 3 exit criteria

- [ ] Status change in dev DB → log line "slack: posting to webhook" appears
- [ ] Status change with no webhook configured → silent (no log line)
- [ ] `make test` still passes

---

## Phase 4: Slack frontend (settings card + API client)

**Duration**: 6 hours
**Goal**: User-facing settings UI to configure Slack integration.

### Task 4.1: Add API client methods

- **Description**: 4 methods on `ApiClient` for the 4 routes.
- **File**: `packages/core/api/client.ts`
- **Effort**: 1 hour
- **Methods**:
  - `getSlackIntegration(wsId)` → `Promise<SlackIntegration>`
  - `putSlackIntegration(wsId, body)` → `Promise<SlackIntegration>`
  - `deleteSlackIntegration(wsId)` → `Promise<void>`
  - `testSlackIntegration(wsId)` → `Promise<{ok: boolean}>`
- **Acceptance**:
  - [ ] Types defined in `packages/core/types/slack.ts` (new)
  - [ ] All 4 methods exported from `packages/core/api/index.ts`

### Task 4.2: TanStack Query hooks

- **Description**: Following the pattern of `notification-preferences/queries.ts` + `mutations.ts`.
- **Files**:
  - `packages/core/slack-integration/queries.ts` (new)
  - `packages/core/slack-integration/mutations.ts` (new)
  - `packages/core/slack-integration/index.ts` (new)
- **Effort**: 1 hour
- **Exports**:
  - `slackIntegrationOptions(wsId)` — query
  - `useUpdateSlackIntegration()` — mutation
  - `useDeleteSlackIntegration()` — mutation
  - `useTestSlackIntegration()` — mutation
- **Acceptance**:
  - [ ] Workspace-scoped query keys (`["slack-integration", wsId]`)
  - [ ] Mutations invalidate the query on success

### Task 4.3: Slack card in integrations-tab

- **Description**: Add a second `<Card>` to `integrations-tab.tsx` per ARCHITECTURE.md Component 4.
- **File**: `packages/views/settings/components/integrations-tab.tsx`
- **Effort**: 3 hours
- **Components needed**:
  - `SlackMark` SVG (inline, matching `GitHubMark` pattern)
  - Webhook URL input (`<Input type="password">`)
  - Status checkbox grid (`ALL_STATUSES.map(...)`)
  - Enable/disable Switch
  - "Send test" button
  - Save button (admin-only)
- **Acceptance**:
  - [ ] Card renders for admins with editable fields
  - [ ] Card renders for members with read-only hint
  - [ ] Save calls `useUpdateSlackIntegration` and shows toast on success/error
  - [ ] Test button shows success/failure toast
  - [ ] After save, webhook input shows masked value

### Task 4.4: i18n strings

- **Description**: Add Slack-related keys to `packages/views/locales/en/settings.json`.
- **Effort**: 30 min
- **Keys**:
  - `integrations.slack_title`: "Slack"
  - `integrations.slack_description`: "Post to a Slack channel when issue status changes."
  - `integrations.slack_webhook_label`: "Webhook URL"
  - `integrations.slack_webhook_placeholder`: "https://hooks.slack.com/services/..."
  - `integrations.slack_triggers_label`: "Notify when status changes to:"
  - `integrations.slack_test_button`: "Send test"
  - `integrations.slack_toast_test_success`: "Test message sent to Slack"
  - `integrations.slack_toast_test_failed`: "Test failed. Check the webhook URL."
  - `integrations.slack_toast_saved`: "Slack integration saved"
- **Acceptance**:
  - [ ] No hardcoded English in `integrations-tab.tsx` Slack card section
  - [ ] zh-Hans gets pass-through copies (English filler is acceptable for v1)

### Phase 4 exit criteria

- [ ] `pnpm typecheck` passes
- [ ] `pnpm test` passes (existing frontend tests still green)
- [ ] Manual: settings page renders Slack card without errors

---

## Phase 5: Tests + Verify + Ship

**Duration**: 4 hours
**Goal**: Comprehensive test coverage + green CI + deploy.

### Test Classes

#### Backend (Go)

**`server/internal/integrations/slack/notify_test.go`** (new)

```go
package slack

func TestNotifyStatusChange_NoIntegrationConfigured(t *testing.T)
//   Setup: workspace has no slack row
//   Action: call NotifyStatusChange
//   Assert: no HTTP request made, no error logged

func TestNotifyStatusChange_DisabledIntegration(t *testing.T)
//   Setup: row exists, enabled=false
//   Action: call NotifyStatusChange
//   Assert: no HTTP request made

func TestNotifyStatusChange_StatusNotInTriggerSet(t *testing.T)
//   Setup: row exists, enabled=true, trigger_statuses=["done"]
//   Action: call NotifyStatusChange with status="in_progress"
//   Assert: no HTTP request made

func TestNotifyStatusChange_FiresOnMatch(t *testing.T)
//   Setup: row exists, enabled, triggers=["done"]
//   Action: status changed to "done"
//   Assert: POST made to mock Slack server with correct payload

func TestNotifyStatusChange_EmptyTriggersFiresOnAll(t *testing.T)
//   Setup: row exists, enabled, triggers=[]
//   Action: any status change
//   Assert: POST made

func TestNotifyStatusChange_TimeoutBoundedAt5s(t *testing.T)
//   Setup: Slack server that sleeps 10s
//   Action: NotifyStatusChange
//   Assert: returns within ~5.5s, last_error populated

func TestNotifyStatusChange_PanicIsolated(t *testing.T)
//   Setup: force panic inside the goroutine
//   Action: NotifyStatusChange
//   Assert: caller goroutine unaffected, no crash
```

**`server/internal/integrations/slack/format_test.go`** (new)

```go
func TestBuildMessage_ContainsIssueKey(t *testing.T)
func TestBuildMessage_ContainsTitle(t *testing.T)
func TestBuildMessage_ContainsStatusTransition(t *testing.T)
func TestBuildMessage_ContainsActor(t *testing.T)
func TestBuildMessage_ContainsForgeLink(t *testing.T)
func TestBuildMessage_EscapesSpecialChars(t *testing.T)
//   Test that markdown special chars in issue title don't break Slack rendering
```

**`server/internal/integrations/slack/client_test.go`** (new)

```go
func TestPost_RejectsNonSlackURL(t *testing.T)
//   URLs to test: http://, https://example.com, https://slack.com/, https://hooks.slack.com/
//   Only the last should be accepted

func TestPost_RejectsHTTPNotHTTPS(t *testing.T)
func TestPost_SuccessOn200(t *testing.T)
func TestPost_ErrorOn4xx(t *testing.T)
func TestPost_ErrorOn5xx(t *testing.T)
func TestPost_TimeoutEnforced(t *testing.T)
```

**`server/internal/handler/slack_integration_test.go`** (new)

```go
func TestGetSlackIntegration_MemberRejected(t *testing.T)
//   Setup: member role, integration exists
//   Action: GET
//   Assert: 403

func TestGetSlackIntegration_AdminMaskedURL(t *testing.T)
//   Setup: admin role, webhook_url stored
//   Action: GET
//   Assert: 200, response.webhook_url_masked ends with last 6 chars

func TestPutSlackIntegration_AdminCreates(t *testing.T)
func TestPutSlackIntegration_RejectsNonSlackURL(t *testing.T)
//   Body: { webhook_url: "https://evil.com/webhook" }
//   Assert: 400

func TestPutSlackIntegration_RejectsUnknownStatus(t *testing.T)
//   Body: { trigger_statuses: ["fake_status"] }
//   Assert: 400

func TestDeleteSlackIntegration_AdminClears(t *testing.T)
func TestTestSlackIntegration_FiresSynthetic(t *testing.T)
//   Use mock Slack server, verify POST received
```

**`server/internal/handler/slack_integration_e2e_test.go`** (new)

```go
func TestE2E_StatusChangeFiresWebhook(t *testing.T)
//   Setup: configure Slack integration via PUT, change issue status via UpdateIssue
//   Assert: mock Slack server received POST within 5s

func TestE2E_SlackFailureDoesNotBlockIssueUpdate(t *testing.T)
//   Setup: configure Slack integration with mock server that 500s
//   Action: PATCH /api/issues/{id} { status: "done" }
//   Assert: 200 returned, last_error populated, no error propagated to caller
```

#### Frontend (TypeScript / Vitest)

**`packages/views/settings/components/integrations-tab.test.tsx`** (extend existing)

```tsx
describe("Slack card", () => {
  it("renders for admin with editable fields", ...)
  it("renders for member with read-only hint", ...)
  it("saves webhook URL and shows toast", ...)
  it("shows masked URL after save", ...)
  it("test button fires and shows success toast", ...)
  it("test button shows failure toast on Slack error", ...)
  it("status checkboxes toggle trigger_statuses", ...)
})
```

**`packages/core/slack-integration/mutations.test.ts`** (new)

```ts
describe("useUpdateSlackIntegration", () => {
  it("invalidates query on success", ...)
  it("rolls back on error", ...)
})
```

#### Verify patches script

**`scripts/verify-patches.sh`** (extend existing)

New checks added:

```bash
# Section 1 — Brand identity (extension)
check "1.7 Web layout page title is Forge, not Multica" \
  "grep -q '\"Forge — \\|forge.asymbl.app' apps/web/app/layout.tsx && ! grep -q '\"Multica' apps/web/app/layout.tsx"

check "1.8 Web favicon does not reference multica asterisk SVG" \
  "! grep -q 'aria-label=\"Multica\"' apps/web/public/favicon.svg 2>/dev/null || ! test -f apps/web/public/favicon.svg"

check "1.9 Archive label replaces Cancelled in status config" \
  "grep -q '\"Archive\"' packages/core/issues/config/status.ts"

# Section 6 — Slack integration (new section)
check "6.1 Slack integration package exists" \
  "test -f server/internal/integrations/slack/notify.go"

check "6.2 Slack hook wired in notification_listeners" \
  "grep -q 'slack.NotifyStatusChange' server/cmd/server/notification_listeners.go"

check "6.3 Slack admin RBAC gate" \
  "grep -q 'RequireWorkspaceRole.*owner.*admin' server/cmd/server/router.go && grep -q 'slack' server/cmd/server/router.go"

check "6.4 Slack migration exists" \
  "ls server/migrations/*_workspace_slack_integrations.up.sql > /dev/null 2>&1"
```

Expected result: 26 + 3 (brand polish) + 4 (Slack) = **33 patches pass**.

### Task 5.1: Run all tests locally

```bash
cd /Users/sdevinarayanan/Asymbl/Multica
make test           # Go unit + integration (includes repocache tests for B1 fix)
pnpm typecheck      # TS types
pnpm test           # Vitest
bash scripts/verify-patches.sh   # Should show 38/38 (26 original + 3 Phase 1 + 9 Phase 1b)
```

- **Effort**: 1 hour to fix anything that fails

### Task 5.2: Push branch + PR + CI

```bash
git checkout -b feat/slack-archive-rebrand
git add -A
git commit -m "feat: Slack webhook notifications + Archive relabel + tab rebrand"
git push origin feat/slack-archive-rebrand
gh pr create --base main --title "feat: Slack notifications + Archive relabel + tab rebrand" --body "..."
```

- **Effort**: 30 min for PR description + CI fix iteration

### Task 5.3: Merge + Deploy

- **Effort**: 1 hour wait for Depot CI build + deploy
- **Acceptance**:
  - [ ] Depot CI green (both backend + frontend jobs)
  - [ ] Migration 089 applied in production logs
  - [ ] forge.asymbl.app/health returns 200

### Task 5.4: Production smoke test

- **Effort**: 30 min
- **Steps**:
  1. Open forge.asymbl.app — verify Forge tab title + Asymbl favicon
  2. Settings → Integrations — verify Slack card visible
  3. As admin: paste test Slack webhook URL, select "Done" trigger, save
  4. Send Test → verify Slack message appears
  5. Create issue, change status to Done → verify Slack message appears in real channel
  6. As member: verify Slack card is read-only
  7. Inspect issue with status `cancelled` → verify label says "Archive"

### Phase 5 exit criteria

- [ ] All tests green locally and in CI
- [ ] PR merged to main
- [ ] Production deployed and smoke-tested

---

## Phase 6: Post-deploy verification

**Duration**: 1 hour
**Goal**: Confirm production state, update memory, no follow-up needed.

### Task 6.1: Production patch verification

```bash
bash scripts/verify-patches.sh   # 33/33 on production checkout
```

### Task 6.2: Ben fleet check

- **Description**: Daemons don't need to change for this feature (it's server-side). But verify they're still healthy.
- **Effort**: 5 min
- **Steps**:
  ```bash
  for IP in 147.182.244.89 146.190.49.230 137.184.40.183 147.182.194.102; do
    ssh root@$IP "forge daemon status | head -3"
  done
  ```
- **Acceptance**: All 4 Bens report `running`, 4 workspaces

### Task 6.3: Update memory

- **Description**: Add observations to claude-mem memory.
- **Files**:
  - `~/.claude/projects/.../memory/project_forge_infra.md` — add: "Slack integration v1 shipped 2026-05-XX"
  - `~/.claude/projects/.../memory/project_forge_gotchas.md` — add gotcha if any surfaced during implementation

### Task 6.4: Move spec from active → completed

```bash
mv docs/spec/active/2026-05-13-forge-slack-archive-rebrand docs/spec/completed/
```

Update README.md `status: completed`, `completed: 2026-05-XX`.

---

## Dependency Graph

```
Phase 1 (tab + Archive)
   ├──> standalone, mergeable independently
   │
Phase 2 (Slack backend)
   │   Task 2.1 (migration) ──> Task 2.2 (sqlc) ──> Task 2.3 (package) ─┐
   │                                              ──> Task 2.4 (handler)┤
   │                                                                    ▼
   │                                                              Task 2.5 (router)
   │
Phase 3 (hook)
   │   needs Phase 2 ──> Task 3.1 ──> Task 3.2 ──> Task 3.3
   │
Phase 4 (frontend)
   │   needs Phase 2 (API) ──> Task 4.1 (client) ──> Task 4.2 (hooks) ──> Task 4.3 (UI)
   │                                                  Task 4.4 (i18n) — parallel
   │
Phase 5 (test+ship)
   │   needs all above ──> Task 5.1 ──> Task 5.2 ──> Task 5.3 ──> Task 5.4
   │
Phase 6 (post)
       needs deploy success
```

## Risk Mitigation Tasks

| Risk from REQUIREMENTS.md | Mitigation task | Phase |
|---|---|---|
| Webhook URL leaked via API | Task 2.4 masks on GET; verified in handler test | Phase 5 |
| Slack outage blocks issue updates | Task 3.2 panic isolation test + Task 5.1 e2e failure test | Phase 5 |
| SSRF abuse via webhook URL | Task 2.3 client validates `hooks.slack.com/services/` prefix + Task 5.1 test | Phase 5 |
| Upstream conflict on `notification_listeners.go` | Task 3.1 keeps hook to 5 lines | Phase 3 |

## Testing Checklist

**Brand audit (Phase 1b)**
- [ ] `go test ./server/internal/daemon/repocache/...` — verifies B1 fix (`forge-agent` trailer)
- [ ] `grep -r 'multica-agent' server/internal/daemon/repocache/*.go` → 0 results (outside legacy list)
- [ ] Manual: create agent task, make commit, verify `git log` shows `forge-agent <github@asymbl.com>`
- [ ] Manual: desktop app title shows "Forge" not "Multica"

**Slack integration**
- [ ] Unit: `slack/notify_test.go` (7 tests)
- [ ] Unit: `slack/format_test.go` (6 tests)
- [ ] Unit: `slack/client_test.go` (5 tests)
- [ ] Unit: `handler/slack_integration_test.go` (7 tests)
- [ ] E2E: `handler/slack_integration_e2e_test.go` (2 tests)
- [ ] Frontend: `integrations-tab.test.tsx` Slack section (7 tests)
- [ ] Frontend: `slack-integration/mutations.test.ts` (2 tests)
- [ ] Manual: production smoke test checklist (7 steps)
- [ ] `bash scripts/verify-patches.sh` shows 38/38 (26 existing + 3 brand + 9 audit)

## Documentation Tasks

- [ ] Update `docs/fork-patches.md` with the 7 new patch checks
- [ ] Update `CLAUDE.md` if Slack workflow has gotchas
- [ ] Add `docs/integrations/slack-setup.md` — user-facing how-to for getting a Slack webhook URL and configuring it in Forge

## Launch Checklist

- [ ] All tests passing (CI green)
- [ ] `verify-patches.sh` 33/33
- [ ] Production smoke test 7/7
- [ ] At least one workspace has Slack configured and verified working
- [ ] `docs/fork-patches.md` updated
- [ ] Spec moved to `completed/`

## Post-Launch (Day 1)

- [ ] Watch backend logs for Slack 4xx/5xx errors (24 hours)
- [ ] Watch issue update latency dashboard (should be flat)
- [ ] Gather feedback from team on message format

## Upgrade Plan When Upstream Adds Similar Features

### Scenario A: Upstream adds Slack integration in v0.2.36+

1. Compare upstream data model to our `workspace_slack_integrations`
2. If schemas match: replace our `slack/` package with upstream's, drop our migration into a no-op
3. If schemas differ: write a data migration to reshape rows, then switch
4. Conflict surface: 5 lines in `notification_listeners.go` — easy resolve
5. UI: replace our `<Card>` with upstream's settings component

### Scenario B: Upstream adds custom statuses (changes `IssueStatus` semantics)

- Archive relabel is unaffected — we only changed display strings, not enum values
- Slack integration's `trigger_statuses` array is unaffected — stores raw values

### Scenario C: Upstream rebrands or adds favicon flexibility

- Re-apply our tab rebrand on top of upstream's metadata
- `verify-patches.sh` catches regression

### Upgrade automation script

Add to `scripts/post-upstream-sync.sh` (new helper script invoked after merge):

```bash
#!/usr/bin/env bash
# Run after every `git merge upstream/main` to re-apply Forge brand polish
# that upstream tends to overwrite.

set -e

echo "Re-applying Forge brand polish..."

# Verify tab title
if grep -q '"Multica' apps/web/app/layout.tsx; then
  echo "WARN: layout.tsx has Multica branding back. Re-apply manually."
  exit 1
fi

# Verify Archive label
if ! grep -q '"Archive"' packages/core/issues/config/status.ts; then
  echo "WARN: status.ts lost Archive relabel. Re-apply manually."
  exit 1
fi

bash scripts/verify-patches.sh
echo "Post-upstream-sync verification PASS"
```
