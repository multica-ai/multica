---
document_type: requirements
project_id: SPEC-2026-05-13-001
version: 1.0.0
last_updated: 2026-05-13T21:50:00+05:30
status: in-review
---

# Forge: Slack + Archive + Tab Rebrand — Product Requirements

## Executive Summary

Forge (Asymbl's self-hosted fork of multica-ai/multica) just synced to upstream v0.2.32. After spec creation, a full jcodemunch + LSP audit found significantly more Multica brand seepage than the initial three features. The PR now covers:

1. **Slack webhook notifications** — workspace-admin-configurable, fires on issue status change to selected statuses.
2. **Archive relabel** — `cancelled` status displayed as "Archive" in UI (zero DB/schema change).
3. **Brand audit fixes** — 10 confirmed seepage points found across the codebase, most critically the co-authored-by git trailer that ALREADY writes the wrong identity (`multica-agent <github@multica.ai>`) into every agent-made commit. Full audit in §Rebrand Audit Findings.

### Rebrand Audit Findings

Full jcodemunch + manual audit of all `multica-agent`, `multica\.ai`, `Multica Agent`, `Multica daemon`, `@multica_hq` strings (excluding internal package path names `@multica/core` etc. and `@multica.ai` test fixture email addresses which are acceptable):

| # | File | Line | String | Severity |
|---|---|---|---|---|
| B1 | `server/internal/daemon/repocache/cache.go` | 802 | `TRAILER="Co-authored-by: multica-agent <github@multica.ai>"` — **actual shell script written to every agent repo** | **CRITICAL** |
| B2 | `server/internal/daemon/repocache/cache_test.go` | 1158,1190,1277,1315,1363 | Tests expect `multica-agent` — CI currently passes while the production bug exists | **CRITICAL** |
| B3 | `server/internal/daemon/repocache/cache.go` | 772 | `const multicaHookMarker = "# multica:prepare-commit-msg:co-authored-by"` — sentinel embedded in installed hooks | High |
| B4 | `server/internal/daemon/repocache/cache.go` | 788-792 | Hook comments: "Multica Agent", "Multica daemon", "Installed by the Multica daemon" | Medium |
| B5 | `apps/desktop/src/main/index.ts` | 243 | `app.setName("Multica")` — runtime app name; creates `WM_CLASS=Multica` which **conflicts with our `StartupWMClass: Forge`** in electron-builder.yml | High |
| B6 | `apps/desktop/src/main/index.ts` | 230-231 | `"Multica Canary"` — dev window title | Low |
| B7 | `apps/desktop/package.json` | 3,6,7,14,15 | `"productName": "Multica"`, description "Multica Desktop", homepage `multica.ai`, author `support@multica.ai` | High |
| B8 | `packages/views/runtimes/components/runtimes-page.tsx` | 226 | `href="https://multica.ai/docs/daemon-runtimes"` — broken external link for Forge users | Medium |
| B9 | `server/pkg/agent/{codex,hermes,kimi,kiro}.go` | ~152-181 | `"name": "multica-agent-sdk"` in ACP initialize handshake sent to external agent processes | Low |
| B10 | `apps/web/app/layout.tsx` | 76,94,95 | `https://www.multica.ai`, `@multica_hq` Twitter — tab title/OG metadata (originally scoped in spec, now fully enumerated) | High |

**Why B1+B2 are critical**: The setting page (Settings → Labs → Git → "Co-authored-by trailer") already shows the correct Forge branding in the description text (`forge-agent <github@asymbl.com>` — that text was fixed earlier). But the **actual shell script** that gets installed into agent git repos still writes the Multica identity. Every agent-authored commit since the v0.2.32 sync has been attributed to `multica-agent@multica.ai`. The tests pass because they were also taken from upstream unchanged — they expect the wrong value. This is a silent functional regression.

**Why B5+B7 interact**: `apps/desktop/package.json` `productName: "Multica"` is the ASAR-embedded default name. The upstream fix for the Linux WM_CLASS bug added `app.setName("Multica")` in `index.ts` as belt-and-suspenders. For Forge, both must say "Forge" or the window manager sees `WM_CLASS=Multica` which fails to associate with our `.desktop` entry (`StartupWMClass: Forge`).

## Problem Statement

### The Problem

**Slack**: Today, status changes only appear in the in-app inbox. Team leads watching a board want async notification when an issue moves to `in_review`, `done`, or `blocked` so they can stay in flow without poll-refreshing the board. Self-hosted teams want this without OAuth complexity.

**Archive**: The `cancelled` status semantically functions as archive in Forge today — it's already excluded from the board (`BOARD_STATUSES` in `packages/core/issues/config/status.ts:24`), already cancels running agent tasks (`server/internal/handler/issue.go:1593`), already dismisses task_failed inbox rows (`server/cmd/server/notification_listeners.go:152`). The "Cancelled" wording reads as failure; "Archive" reads as the intended completed-and-tucked-away meaning.

**Tab rebrand**: After the v0.2.32 upstream sync (PR #2), the browser tab still displays the Multica brand. The `verify-patches.sh` script did not check `apps/web/app/layout.tsx` metadata or `apps/web/public/favicon.svg`. The user spotted the leak.

### Impact

| Feature | Affected users | Severity |
|---|---|---|
| Slack notifications | All 5 Forge workspaces — anyone who lives in Slack and uses Forge issues | High (DX) |
| Archive relabel | Every workspace member who sees the `Cancelled` status | Medium (UX clarity) |
| Tab rebrand | Anyone using forge.asymbl.app in a browser | High (brand integrity, blocks public demo) |

### Current State

| Feature | Current behaviour |
|---|---|
| Slack notifications | Not implemented. Users get inbox-only notifications. |
| Archive relabel | UI shows "Cancelled" label everywhere. Backend status value is `cancelled`. |
| Tab rebrand | Tab shows Multica asterisk + "Multica — Project Management for Human + Agent Teams". |

## Goals and Success Criteria

### Primary Goal

Ship a workspace-admin-configurable Slack integration, rename the `cancelled` status to "Archive" in UI, and finish the v0.2.32 rebrand gaps — without changing the DB schema for status, without breaking upstream sync, and without introducing new untested RBAC surfaces.

### Success Metrics

| Metric | Target | Measurement |
|---|---|---|
| Slack webhook end-to-end latency (status change → message in Slack) | < 5 seconds p95 | Manual test with timestamps |
| Slack webhook failure isolation | Failed Slack POST does NOT block issue update HTTP response | Integration test: simulate 500 from Slack |
| Archive label visibility | 0 occurrences of "Cancelled" in user-facing strings | grep audit + manual screens |
| Browser tab brand integrity | Tab title contains "Forge", favicon is Asymbl mark | Manual test on forge.asymbl.app |
| Fork patch verification | `bash scripts/verify-patches.sh` exits 0, count = 28+ | Run script |
| Regression suite | Existing 26 patches + new checks all green | Run script |
| Upstream merge risk | Slack code in `server/internal/integrations/slack/` (new package, zero conflict) | `git log` against future upstream |

### Non-Goals (Explicit Exclusions)

- ❌ Slack DMs to specific users (would require Slack Bot Token + OAuth — different integration, not in scope)
- ❌ Multiple Slack channels per workspace (MVP is one webhook URL per workspace)
- ❌ Renaming the DB column value `cancelled` to `archive` (the 79-file blast radius makes this an upstream-sync nightmare)
- ❌ Per-user notification preferences for Slack (workspace-level only — Slack routing is integration config, not user preference, per zen architecture analysis)
- ❌ Custom workspace-defined statuses (separate feature, see RESEARCH_NOTES.md for why deferred)
- ❌ Notifications on issue create, assignee change, comment, or any event other than status change (MVP is status-change only)
- ❌ Replacing the inbox notification system (Slack is a parallel channel, not a replacement)
- ❌ Email or Microsoft Teams integration (Slack only)

## User Analysis

### Primary Users

**Workspace admin/owner** (configures Slack):
- **Who**: Asymbl team leads — Shiv, Ben, others setting up Forge for their squad
- **Needs**: Wire up a Slack channel once, never touch it again
- **Context**: Settings → Integrations panel, after the GitHub card

**Workspace member** (receives notifications):
- **Who**: Engineers, PMs, anyone watching the board
- **Needs**: Know when an issue moves to a status they care about, without watching the inbox
- **Context**: Slack channel (workspace #ops, #squad-x, etc.)

### User Stories

1. As a workspace **admin**, I want to paste a Slack webhook URL into Forge settings and choose which status transitions trigger a Slack message, so that my team gets channel notifications without me writing any integration code.
2. As a workspace **member**, I want to see when an issue moves to `In Review` or `Blocked` in our team Slack channel, so I know when to look at it.
3. As any user, I want the browser tab to say "Forge" not "Multica", so I can find the right tab among many.
4. As a user who closes/archives issues, I want the label to say "Archive" instead of "Cancelled", so the wording matches the intent (work is done and tucked away, not failed).

## Functional Requirements

### Must Have (P0)

| ID | Requirement | Rationale | Acceptance Criteria |
|---|---|---|---|
| FR-001 | Workspace admin can create one Slack webhook config per workspace via a new Settings → Integrations card | Single-config MVP keeps blast radius small | Admin sees Slack card next to GitHub card; can paste webhook URL, set status filters, save |
| FR-002 | Non-admin members see read-only "managed by workspace admin" hint, cannot edit | Matches existing RBAC pattern (`canManage` in `integrations-tab.tsx:33`) | Member visits Settings → Integrations: sees Slack card but inputs disabled |
| FR-003 | Status filter UI lets admin pick which target statuses fire a notification | Some teams care about Done, others about Blocked | Admin sees checkboxes for all 7 statuses; selections persist after save |
| FR-004 | When an issue's status changes to a selected status, a Slack message is POSTed to the webhook URL | Core function | Manual test: change issue status to a selected status, verify message appears in Slack channel within 5 seconds |
| FR-005 | Slack message format includes: issue identifier (e.g. MUL-42), title, old → new status, actor name, link to issue on forge.asymbl.app | Standard issue tracker Slack format | Visual inspection of message; contains all 5 fields |
| FR-006 | Slack POST is asynchronous and isolated — Slack failures do NOT block or fail the issue update HTTP response | The event bus dispatches sync within request goroutine; blocking would add latency to every issue update | Integration test: mock Slack 500, verify issue update returns 200; verify no error propagated to caller |
| FR-007 | Slack POST has a 5-second timeout; failures are logged but do not retry in v1 | Bounded latency, no queue infra in v1 | Inspect logs; verify timeout enforced |
| FR-008 | "Cancelled" label renamed to "Archive" in all user-visible strings | UX clarity | grep audit shows 0 "Cancelled" in user-facing files (excluding DB column values) |
| FR-009 | Browser tab title reads "Forge — …" not "Multica — …" (`apps/web/app/layout.tsx` B10) | Brand integrity | Open forge.asymbl.app, inspect `<title>`: contains "Forge" |
| FR-010 | Browser tab favicon is the Asymbl mark, not the Multica asterisk | Brand integrity | Hard reload forge.asymbl.app, inspect favicon: Asymbl bracket+dot mark visible |
| FR-011 | `verify-patches.sh` gains checks covering all B1-B10 audit findings | Regression prevention for future upstream syncs | Run script: 33+ checks pass; intentionally break each → script fails |
| **FR-012** | **Co-authored-by hook script writes `forge-agent <github@asymbl.com>` (B1 fix)** | **Every agent commit since v0.2.32 sync is attributed to `multica-agent@multica.ai`. Functional regression.** | **`repocache/cache.go:802` trailer string updated; `TestCreateWorktreeInstallsCoAuthoredByHook` and related tests updated to expect `forge-agent <github@asymbl.com>`; create a new agent task and make a git commit: verify `git log` shows `Co-authored-by: forge-agent <github@asymbl.com>`** |
| **FR-013** | **`app.setName("Forge")` in `apps/desktop/src/main/index.ts` (B5 fix)** | **WM_CLASS=Multica conflicts with `StartupWMClass: Forge` in electron-builder.yml, breaking Linux window association** | **Desktop app shows "Forge" in titlebar, About dialog, `xprop WM_CLASS` on Linux shows "Forge"** |
| **FR-014** | **`apps/desktop/package.json` `productName` and metadata updated to Forge (B7 fix)** | **ASAR-embedded name falls back to "Multica" if `app.setName` ever fails; installer metadata shows wrong name** | **`productName: "Forge"`, description/homepage updated; About dialog in desktop shows "Forge"** |
| **FR-015** | **Daemon runtime docs link updated from `multica.ai/docs/daemon-runtimes` to Forge docs or removed (B8 fix)** | **Broken external link in the runtimes page settings panel for Forge users** | **Clicking the link does not 404; opens Forge/Asymbl-relevant docs or is removed if no equivalent page exists** |

### Should Have (P1)

| ID | Requirement | Rationale | Acceptance Criteria |
|---|---|---|---|
| FR-101 | Admin can disable Slack integration without deleting the config (toggle) | Common pattern for muting without losing setup | Toggle disabled → no Slack messages fire; toggle re-enabled → messages resume |
| FR-102 | "Test message" button posts a synthetic message to verify webhook works | Self-service validation | Admin clicks Test, sees "Test message from Forge" in Slack |
| FR-103 | Webhook URL is masked in the UI after first save (shows last 6 chars only) | Secrets hygiene | After save and reload, input shows `••••••••••XYZ123` |

### Nice to Have (P2)

| ID | Requirement | Rationale | Acceptance Criteria |
|---|---|---|---|
| FR-201 | Slack message uses Slack Block Kit (rich card) instead of plain text | Better visual presentation | Message shows formatted card with title, status pills, link button |
| FR-202 | Display a "last successful send" timestamp on the integration card | Debugging aid | Card shows "Last sent: 2 minutes ago" |

## Non-Functional Requirements

### Performance

- Slack POST adds zero latency to issue update HTTP responses (async goroutine).
- Slack POST timeout: 5 seconds (bounded failure).
- Settings panel load time: < 100ms p95.

### Security

- Webhook URL stored in `workspace_slack_integrations.webhook_url` (DB column). Encryption-at-rest deferred to v2 (the URL itself is the secret; intercept requires DB access which already means full compromise).
- Webhook URL never returned in plain text in API responses after first save (masked).
- All write routes gated `RequireWorkspaceRole(queries, "owner", "admin")` matching existing pattern (`router.go:447`).
- Webhook URL validated to start with `https://hooks.slack.com/services/` to prevent SSRF abuse — config rejected otherwise.

### Scalability

- One webhook per workspace × N workspaces × M status changes per second. At Forge's scale (5 workspaces, <50 status changes/day total): trivial.
- Future: if multiple webhooks per workspace are needed, the table is already plural-friendly (no `UNIQUE workspace_id` on the table itself, only a partial index on `enabled=true`).

### Reliability

- Slack failure does not affect issue update success. The event bus listener uses a recover-from-panic wrapper.
- DB migration is additive only (new table). Rollback = drop table.

### Maintainability

- All Slack code lives in `server/internal/integrations/slack/` — one new package. Zero changes to existing handler files (besides ~5 lines in `notification_listeners.go`).
- Frontend addition is one new Card in the existing `integrations-tab.tsx`, mirroring the GitHub card pattern.

## Technical Constraints

- Must work on the existing `pgvector/pgvector:pg17` Postgres image (no new extensions).
- Must not change the `status` CHECK constraint (would clash with upstream's potential custom statuses feature).
- Must survive `verify-patches.sh` — new patches added to the script for: tab title, favicon, Archive label, Slack handler presence.
- Must use the existing event bus pattern (`server/internal/events/bus.go`), not a new pub/sub mechanism.
- Must use the existing settings tab structure (`integrations-tab.tsx`).

## Dependencies

### Internal Dependencies

- `server/internal/events/bus.go` — event bus already exists, no changes needed
- `server/cmd/server/notification_listeners.go` — hook point at line 582 (`EventIssueUpdated`)
- `packages/views/settings/components/integrations-tab.tsx` — UI extension point
- `packages/core/issues/config/status.ts` — status label location
- `packages/views/locales/en/issues.json` + `projects.json` — i18n label location

### External Dependencies

- Slack Incoming Webhooks (no API key, just URL) — https://api.slack.com/messaging/webhooks
- No new npm or Go dependencies required. Go `net/http` already in use.

## Risks and Mitigations

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| Webhook URL leaked via API response | Low | High | Mask URL on read; never return plain text after first save |
| Slack outage blocks issue updates | Medium | High | Async goroutine with 5s timeout; failures logged not returned to user |
| User pastes wrong URL (e.g. internal IP) | Medium | High | Validate `https://hooks.slack.com/services/` prefix server-side |
| Upstream adds Slack feature with different schema | Low | Medium | Code isolated in new package; one 5-line hook in `notification_listeners.go` is the only conflict surface |
| Archive relabel breaks downstream string match (search by status name) | Low | Low | Status filter UI uses status enum value `cancelled`, not the label string — only display text changes |
| Tab rebrand breaks Next.js OpenGraph share-card images | Low | Low | `metadataBase` URL change is the only Open Graph field touched; the rest of the OG block updates wholesale |
| `verify-patches.sh` new checks too strict, blocks valid future state | Low | Medium | Use grep patterns that allow flexibility (e.g. match "Forge" not exact strings) |

## Open Questions

- [x] Should Slack messages fire on **all** status changes, or only **inbound** transitions to specific statuses? **Resolved**: only on transitions where the *new* status is in the configured set.
- [x] Should non-admin members see the integration config? **Resolved**: yes, read-only (matches GitHub card pattern).
- [x] Multiple webhooks per workspace? **Resolved**: deferred to v2 — partial unique index allows adding more rows later without schema change.
- [x] Encrypt webhook URL? **Resolved**: deferred — URL secrecy is already gated by DB access which means full system compromise.
- [ ] Slack message language: English only in MVP, or use the workspace member's i18n locale? **Open** — MVP says English; flag for future.

## Glossary

| Term | Definition |
|---|---|
| Slack Incoming Webhook | A URL provided by Slack that accepts POST requests and posts the body to a specific channel. No OAuth needed. |
| Slack Bot Token | An OAuth-issued token that authenticates as a Slack app, can DM users and read history. Out of scope for v1. |
| Status change event | Fired on the Forge event bus when `prev.Status != current.Status` after an issue update. Already exists. |
| `notifTypeToGroup` | Existing map in `notification_listeners.go:80` mapping inbox notif types to user preference groups. Slack does NOT use this — workspace-level integration, not user preference. |

## References

- jcodemunch blast radius for `IssueStatus`: 79 files, risk 0.59 — see `RESEARCH_NOTES.md`
- jcodemunch blast radius for `STATUS_CONFIG`: contained — see `RESEARCH_NOTES.md`
- zen architectural analysis (gpt-5.4, high thinking mode): see `RESEARCH_NOTES.md`
- Slack docs: https://api.slack.com/messaging/webhooks
- Existing GitHub integration pattern: `packages/views/settings/components/integrations-tab.tsx`
- v0.2.32 sync PR: https://github.com/shivasymbl/forge/pull/2
