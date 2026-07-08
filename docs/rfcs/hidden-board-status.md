# RFC: Hidden / archivable board statuses

- Status: Draft
- Tracking issues: JON-115 (epic), JON-116 (this RFC), JON-117..JON-120

## Motivation

Users want to move completed issues out of the active board view without sending
them back to `backlog` or losing them. A concrete case: on a "Manejo Facturas"
board, issues that have been in `done` for more than 2h should disappear from the
board while remaining tracked and recoverable.

## Current state (as-is)

Issue status is a **fixed enum hardcoded across four layers**:

| Layer | File | Reference |
|-------|------|-----------|
| DB | `server/migrations/001_init.up.sql` | `CHECK (status IN (...))` (lines 57-58) |
| Backend (Go) | `server/internal/handler/issue.go` | `validIssueStatuses` (line 74), sort `CASE` (line ~512) |
| Core (TS) | `packages/core/types/issue.ts` (`IssueStatus`), `packages/core/issues/config/status.ts` (`STATUS_ORDER`, `ALL_STATUSES`, `BOARD_STATUSES`, `STATUS_CONFIG`) | — |
| Mobile | `apps/mobile/lib/issue-status.ts` | mirror of the enum |

Key finding: `BOARD_STATUSES` in `packages/core/issues/config/status.ts` **already
excludes `cancelled`**, so `cancelled` issues are already hidden from board columns.
This means the "hide from board" behavior already exists — it is just semantically
wrong to reuse `cancelled` for "completed and archived".

## Options

### Option A — Add a built-in `archived` terminal status (recommended first PR)

Introduce one new status, `archived`, that is a terminal state excluded from
`BOARD_STATUSES` (same treatment as `cancelled`). Semantically correct for
"done and hidden".

Changes (small, reviewable, single PR):
1. DB: add `archived` to the `CHECK` constraint (new migration `NNN_add_archived_status`).
2. Go: add `"archived"` to `validIssueStatuses`; give it a sort weight.
3. Core TS: add `"archived"` to `IssueStatus`, `STATUS_ORDER`, `ALL_STATUSES`, and
   `STATUS_CONFIG`; leave it OUT of `BOARD_STATUSES`.
4. Mobile: mirror in `apps/mobile/lib/issue-status.ts`.
5. i18n: add the label to `packages/views/locales/*/issues.json`.

Pros: ships quickly, low risk, immediately usable by the auto-transition rule.
Cons: still a fixed enum; not user-defined.

### Option B — User-defined custom statuses with a `hidden` flag (the JON-115 epic)

Model statuses as data (per workspace, optional per-project override) with fields:
`key`, `label`, `category` (todo/in_progress/done/terminal), `order`, `hidden`.

Changes (multi-PR): new `issue_status` table + migration off the enum, API contract,
list/board endpoints honoring `hidden` + order, CLI (`multica status ...`), board UI
for custom/hidden columns, mobile parity.

Pros: fully flexible. Cons: large surface, data migration off the enum, higher risk.

## Related: time-based auto-transition (JON-120)

A native rule "if an issue has been in status X for more than N hours, move it to
status Y" would replace the current autopilot workaround and measure time from the
actual status change (not the generic `updated_at`). Pairs naturally with an
`archived`/hidden status as the destination.

## Recommendation

Ship **Option A** first (`archived` status) as the immediate, low-risk PR that solves
the user's need with correct semantics, then pursue Option B (JON-115) and the
auto-transition rule (JON-120) as follow-ups.
