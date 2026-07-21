---
title: Agent List Access-Scope Management - Plan
type: feat
date: 2026-07-14
topic: agent-list-access-scope
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
product_contract_source: ce-brainstorm
execution: code
---

# Agent List Access-Scope Management - Plan

> **Product Contract preservation:** unchanged except R7 — bulk now offers all three access scopes (Workspace / Specific people / Owner-only) via the reused AccessPicker; user override of the original two-tier limit during planning. All other R/A/F/AE IDs and scope boundaries preserved.

## Goal Capsule

- **Objective:** Make an agent's access scope a first-class, visible, and manageable dimension on the agents list page, so any access-scope question or change is answerable without raw SQL or opening many per-agent detail pages.
- **Product authority:** Product Contract from `ce-brainstorm` (this file's prior requirements-only state). Planning enriches it in place; the only product change is the R7 override noted above.
- **Stop conditions:** All four implementation units land; `pnpm typecheck`, `pnpm test`, `pnpm lint` green; a browser smoke confirms the column, filter, and bulk action on real agents.
- **Execution profile:** Pure frontend wiring in `packages/core/agents/` and `packages/views/agents/`. No backend change.
- **Open blockers:** None blocking. The structural decisions (single confirmation dialog, owner-only gate, AccessPicker wrapper) are settled here; only final dialog wording is polished during implementation.
- **Tail ownership:** The implementing agent (`ce-work` or a human) owns execution; progress is derived from git, not stored here.

---

## Product Contract

### Summary

A generic, upstream-able agents-list-page feature with three connected mechanics: a three-state "effective access" column (Workspace / Specific people / Owner-only) visible by default, a matching filter dimension, and a "Set access scope" bulk action on the existing batch toolbar that offers all three scopes via the reused AccessPicker. Together they form a see → filter → change loop, computed entirely from existing permission fields with no backend change.

### Problem Frame

Operators cannot read or change agent access scope from the list page today. The list shows eight columns — agent, status, owner, runtime, last_active, runs, model, created — none of which says who can invoke an agent. The only access signal is a small lock marker derived from the `visibility` field, and `visibility` is itself a lossy two-state projection of the authoritative `permission_mode` + `invocation_targets`: a `public_to` agent scoped to specific people maps to `visibility: "private"`, indistinguishable from a truly owner-only agent.

The cost showed up directly. Inventorying which agents were private versus workspace-invocable required raw SQL because no column or filter answers it. Changing scope across many agents required a bulk `UPDATE` + `INSERT` in SQL — observed at the scale of 70 agents across 5 workspaces — because the UI edits one agent at a time. These are the workflows the list page should own, and it currently forces operators outside the product to do them.

### Requirements

**Effective-access column**

- R1. The list gains a column showing each agent's effective access scope as one of three states — Workspace, Specific people, or Owner-only — computed from `permission_mode` + `invocation_targets`, not from the derived `visibility` field.
- R2. The state mapping is deterministic: Workspace = `public_to` with a workspace target; Specific people = `public_to` scoped to member/team target(s) (or no targets); Owner-only = `private`.
- R3. The column is visible by default, not hidden behind the column toggle.
- R4. The column label is localized across all four locales (en, zh-Hans, ja, ko); Chinese copy follows the project conventions doc, keeping the product noun "agent" in English within zh-Hans.

**Access filter**

- R5. The list filter gains an `access` multi-select dimension with the same three values, isolating a class in one click independent of whether the column is visible.

**Bulk edit**

- R6. The batch toolbar offers a "Set access scope" action that applies a chosen scope to all selected agents. The action is gated by ownership (`isOwnedByMe`), not the broader manage permission, because the backend enforces owner-only writes for `permission_mode` / `invocation_targets` — agents the operator does not own are skipped (a stricter gate than archive/restore, which admit workspace admins).
- R7. Bulk offers all three access scopes — Workspace, Specific people, and Owner-only — by reusing the AccessPicker. The chosen scope, including any specific-people member targets, applies to all selected agents. (Overridden during planning from an earlier two-tier limit; the operator wants the flexibility and can reverse a wrong choice by re-running bulk edit.)
- R8. A bulk change requires a confirmation dialog that shows the target scope and the affected-agent count clearly enough that an erroneous change can be reversed by running bulk edit again.

**Uniform treatment**

- R9. All agents are treated uniformly; no built-in-agent special-casing.

### Key Decisions

- **Three-state effective access over raw `visibility`.** `visibility` is a derived two-state projection; a raw-visibility column would label a specific-people agent as "private" and mislead operators about who can run it.
- **Frontend-computed from the existing list payload.** No backend or API change. `permission_mode` and `invocation_targets` are both present in the list response (`SELECT *` carries `permission_mode`; the handler attaches `invocation_targets`).
- **Column visible by default.** At-a-glance access information is prioritized over horizontal density. Room is made by accepting horizontal scroll (mirroring the existing pattern), not by hiding another column.
- **Bulk offers all three scopes via the reused AccessPicker.** Specific-people is allowed in bulk (R7 override); the chosen member-target set applies uniformly to all selected agents.
- **Reuse existing infrastructure.** The batch toolbar, its sequential runner, the filter dimension model, the access picker, and the column system all exist; this is wiring, not new machinery.
- **Generic and upstream-able.** Uniform agent treatment and no self-host special-casing, so the change is mergeable as-is into the official repo.

### Key Flows

- F1. Bulk access-scope change
  - **Trigger:** Operator selects one or more agents and chooses "Set access scope" in the batch toolbar.
  - **Actors:** Owner of the selected agents (the backend rejects access-scope writes from non-owners, including workspace admins).
  - **Steps:** Choose target scope (Workspace, Specific people, or Owner-only) via AccessPicker → confirmation dialog shows the scope, the owned count, and the skipped count → confirm → applied to all owned selected agents; non-owned ones are skipped → list refreshes.
  - **Covered by:** R6, R7, R8.

### Acceptance Examples

- AE1.
  - **Given:** an agent with `permission_mode: public_to` and a member target.
  - **When:** the list renders.
  - **Then:** the effective-access column shows "Specific people", not "Owner-only", even though derived `visibility` is "private".
  - **Covers:** R1, R2.
- AE2.
  - **Given:** a `private` agent with no invocation targets.
  - **When:** the list renders.
  - **Then:** the effective-access column shows "Owner-only".
  - **Covers:** R1, R2.
- AE3.
  - **Given:** a selection mixing owned and non-owned agents.
  - **When:** the operator bulk-sets scope to Workspace and confirms.
  - **Then:** the confirmation dialog shows the affected count and the skip count; only owned selected agents change; non-owned ones are skipped.
  - **Covers:** R6, R8.
- AE4.
  - **Given:** a workspace with private and workspace-invocable agents.
  - **When:** the operator filters access = Owner-only.
  - **Then:** only private agents remain.
  - **Covers:** R5.
- AE5.
  - **Given:** three selected agents.
  - **When:** the operator bulk-sets scope to Specific people with a chosen member set and confirms.
  - **Then:** all three agents receive the same `public_to` + member-target configuration.
  - **Covers:** R7.

### Scope Boundaries

**Deferred for later**

- The #5230 "automation cannot trigger this agent" affordance on the Owner-only state. It is a separate upstream bug with its own fix path; keeping it out keeps this PR focused and mergeable.
- A workspace-level default access scope for newly created agents. It prevents the cleanup backlog from recurring but is a separate settings-surface concern.
- Inline (per-row popover) access editing. Per-agent inspector plus the new bulk action cover the same ground.

**Deferred to follow-up work**

- Refactoring `AccessPicker`'s internal scope logic to call the new shared effective-access helper (U1). The helper and the picker currently duplicate the same derivation; consolidating is a clean follow-up but is deliberately out of scope here to keep the PR focused.

**Outside this change**

- Backend API or schema changes. Effective access is frontend-computed from fields already in the list payload.
- Special handling of built-in agents. Uniform treatment is the generic product decision.

### Dependencies / Assumptions

- `permission_mode` and `invocation_targets` are present in the agent list payload. **Verified** — `ListAgents` is `SELECT *` (carries `permission_mode`) and the handler attaches `invocation_targets` per agent.
- The existing per-agent update path (`api.updateAgent`) can be driven per selected agent by the batch toolbar's sequential runner, mirroring archive/restore.

### Sources / Research

- Column system: `packages/core/agents/stores/view-store.ts` (`AgentColumnKey`, `AGENT_DEFAULT_HIDDEN_COLUMNS`, `toggleColumn`); `packages/views/agents/components/agents-page.tsx` (grid template, track vars, header cells, row cells, skeleton); `packages/views/agents/components/agent-list-toolbar.tsx` (column show/hide toggle).
- AccessPicker + persistence: `packages/views/agents/components/inspector/access-picker.tsx` (props, `AccessChange`, inline scope derivation); `packages/views/agents/components/agent-detail-page.tsx` → `packages/core/api/client.ts` (`api.updateAgent`).
- Filter model: `packages/core/agents/stores/view-store.ts` (`AgentListFilters`, `EMPTY_AGENT_FILTERS`, `toggleFilter`); `packages/views/agents/components/agent-list-toolbar.tsx`; row-filter predicate in `agents-page.tsx`.
- Batch toolbar: `packages/views/agents/components/agents-page.tsx` (`AgentBatchToolbar`, `runBatch`, `allManageable`, archive-with-confirm template, `selectedIds`).
- Permission semantics: `server/internal/handler/agent_access.go` (`canInvokeAgent`); derivation comment in `packages/core/types/agent.ts`.
- List payload: `server/internal/handler/agent.go` (`ListAgents` attaches `invocation_targets`); `server/pkg/db/queries/agent.sql` (`ListAgents`).
- i18n: `packages/views/locales/{en,zh-Hans,ja,ko}/agents.json`; `apps/docs/content/docs/developers/conventions.zh.mdx`.
- Tests: `packages/views/agents/components/inspector/access-picker.test.tsx`; `packages/core/agents/stores/view-store.test.ts`; `packages/views/agents/components/agent-detail-page.test.tsx`.
- Learnings: `docs/solutions/ui-bugs/skill-autocomplete-cold-cache.md`; `docs/solutions/design-patterns/adaptive-skill-import-dialog.md`; `docs/solutions/workflow-issues/run-comment-view-run-button-rollout.md`.
- Originating ideation: `docs/ideation/2026-07-14-agent-list-visibility-field-ideation.html`.

---

## Planning Contract

### Key Technical Decisions

- **KTD1 — Frontend pure function, no backend change.** Effective access is a pure helper over `permission_mode` + `invocation_targets`, both verified present in the list payload. The helper lives in `packages/core/agents/` and is shared by the column (U2) and filter (U3).
- **KTD2 — Three-state mapping mirrors the authoritative gate.** Owner-only = `private`; Workspace = `public_to` + a workspace target; Specific people = `public_to` without a workspace target. This matches `canInvokeAgent`.
- **KTD3 — Bulk reuses AccessPicker and persists via `api.updateAgent`, gated by ownership.** All three scopes are offered (R7 override). The bulk action calls `api.updateAgent(id, { permission_mode, invocation_targets })` per selected agent through `runBatch`, mirroring the archive/restore pattern; the existing invalidation of `workspaceKeys.agents(wsId)` refreshes the list. The action is gated by `isOwnedByMe` (not `canManage`), matching the backend's owner-only write gate for these fields — `canManage` admits workspace admins who would receive 403s and abort the sequential batch.
- **KTD4 — Column visible by default.** The key is added to `AgentColumnKey` but not to `AGENT_DEFAULT_HIDDEN_COLUMNS`; the show/hide toggle still works.
- **KTD5 — Derive in render, never mirror to Zustand.** The three-state value is a pure function of server fields; compute it with `useMemo` at the cell/predicate. The runtime may omit either field on legacy self-host backends or stale caches, so the helper fails safe to Owner-only when `permission_mode` is absent; a `public_to` agent with absent `invocation_targets` stays Specific people (per R2/KTD2).
- **KTD6 — AccessPicker reused via a thin wrapper.** A `<BulkAccessPicker>` wrapper renders `AccessPicker` with a new optional `hideFooter` prop that suppresses its internal Save button, so the bulk dialog's own confirm button is the sole apply trigger. Only the `hideFooter` opt-in prop is added to `AccessPicker`; the adjacent refactor (consolidating the picker's internal derivation into U1's helper) remains out of scope.

### Assumptions

- The list payload carries `permission_mode` and `invocation_targets` (verified).
- `AccessPicker` is rendered inside the bulk dialog via a `<BulkAccessPicker>` wrapper that adds a `hideFooter` prop (KTD6). The wrapper initializes the picker with no scope preselected and owns the confirm-disabled logic; `ownerId` is cosmetic for the Owner-only label and `members` is the workspace member list (uniform across selected agents).

### Sequencing

U1 (shared helper) lands first. U2 (column), U3 (filter), and U4 (bulk) all depend on U1 and can proceed in parallel after it. U2 and U3 both touch the toolbar and the row layer; coordinate those edits to avoid merge friction.

---

## Implementation Units

### U1. Shared effective-access helper

- **Goal:** A pure, tested function deriving an agent's effective access scope (three states) from `permission_mode` + `invocation_targets`, shared by the column and the filter.
- **Requirements:** R1, R2.
- **Dependencies:** none.
- **Files:** `packages/core/agents/effective-access.ts` (new); `packages/core/agents/effective-access.test.ts` (new).
- **Approach:** Export `AccessScope = "workspace" | "specific-people" | "owner-only"` and `effectiveAccessScope(permissionMode, invocationTargets)`. Mapping: `private` → owner-only; `public_to` with a workspace target → workspace; `public_to` without a workspace target → specific-people. Fail safe to owner-only when `permission_mode` is absent; a `public_to` agent with absent `invocation_targets` stays specific-people (the runtime may omit either field on legacy backends or stale caches). Reference the inline derivation in `packages/views/agents/components/inspector/access-picker.tsx` for the same logic; do not modify that file (KTD6).
- **Patterns to follow:** Pure helpers in `packages/core/agents/` such as `visibility-label.ts`. Respect the `packages/core` boundary (no `react-dom`, no `localStorage`, no `process.env`).
- **Test scenarios:** `private` → owner-only; `public_to` + workspace target → workspace; `public_to` + member target only → specific-people; `public_to` + team target only → specific-people; `public_to` + no targets → specific-people; missing `permission_mode` → owner-only (defensive); `public_to` with missing targets → specific-people.
- **Verification:** Unit tests pass; the helper is pure with no side effects.

### U2. Effective-access column (visible by default)

- **Goal:** A new default-visible column showing the three-state effective access label, derived via U1.
- **Requirements:** R1, R3, R4. (R2 is realized by U1.)
- **Dependencies:** U1.
- **Files:** `packages/core/agents/stores/view-store.ts` (`AgentColumnKey`; do not add to `AGENT_DEFAULT_HIDDEN_COLUMNS`); `packages/views/agents/components/agents-page.tsx` (`COLUMN_WIDTHS`, track vars, grid template, header cell, `AccessCell`, skeleton cell); `packages/views/agents/components/agent-list-toolbar.tsx` (`COLUMN_KEYS`, `COLUMN_LABELS`); `packages/views/locales/{en,zh-Hans,ja,ko}/agents.json` (new `columns.access`; reuse existing `access.*` state labels).
- **Approach:** Add `"access"` to `AgentColumnKey` and leave it out of `AGENT_DEFAULT_HIDDEN_COLUMNS` so it shows by default. Place the column immediately after `owner` (access scope is conceptually about who can use the agent, adjacent to ownership): add the width entry, the `--agc-access` track var, and the grid track in that position. Add a header cell and an `AccessCell` that renders the localized label from `effectiveAccessScope(row.agent.permission_mode, row.agent.invocation_targets)` via `useMemo`. Add the matching skeleton cell and the toolbar toggle entries. Add `columns.access` in all four locales, keeping "agent" in English within zh-Hans. Horizontal density is handled by accepting scroll, not by hiding another column. Accessibility: the `AccessCell` exposes the localized text label (not icon-only) so screen readers announce the scope.
- **Patterns to follow:** Existing cells (`OwnerCell`, `RuntimeCell`); the column show/hide toggle in `agent-list-toolbar.tsx`.
- **Test scenarios:** Renders "Workspace" for a `public_to` + workspace agent; renders "Specific people" for a `public_to` + member agent (Covers AE1); renders "Owner-only" for a `private` agent (Covers AE2); renders a safe label when fields are absent; column is visible on a fresh load; toggling hides and shows the column; the cell text is reachable to assistive tech (not icon-only).
- **Verification:** Render the page with `@multica/core/api` mocked and a callable store (per the repo test rules); assert the label per agent shape; `pnpm typecheck`.

### U3. Access filter dimension

- **Goal:** A new `access` multi-select filter that isolates a class in one click, independent of column visibility.
- **Requirements:** R5.
- **Dependencies:** U1.
- **Files:** `packages/core/agents/stores/view-store.ts` (`AgentListFilters.access`, `EMPTY_AGENT_FILTERS.access`); `packages/views/agents/components/agent-list-toolbar.tsx` (new sub-menu; bump `countActiveFilterDimensions`); `packages/views/agents/components/agents-page.tsx` (row-filter predicate); locale filter-option labels.
- **Approach:** Add `access: string[]` to `AgentListFilters` with an empty default; `toggleFilter` and rehydration are generic and need no per-key change. Add a predicate: drop a row unless `filters.access` includes its `effectiveAccessScope(...)`. Mirror the `availability` sub-menu in the toolbar and increment the active-dimension counter. Filter values share the column's three labels. Accessibility: the access sub-menu reuses the `availability` sub-menu's keyboard navigation and ARIA semantics.
- **Patterns to follow:** The `availability` dimension end-to-end (declaration, sub-menu, predicate).
- **Test scenarios:** Filtering `access = owner-only` leaves only private agents (Covers AE4); the filter combines with another dimension; clearing it restores the full list; the store persists the filter under the workspace view key; the predicate uses the same helper as the column; the sub-menu matches the `availability` sub-menu's keyboard/ARIA behavior.
- **Verification:** `view-store.test.ts` direct-manipulation pattern; a page predicate test; `pnpm typecheck`.

### U4. Bulk "Set access scope" action with confirmation

- **Goal:** A batch-toolbar action that sets access scope across selected agents via the reused AccessPicker, with a confirmation dialog showing scope and count.
- **Requirements:** R6, R7, R8, R9.
- **Dependencies:** U1.
- **Files:** `packages/views/agents/components/agents-page.tsx` (`AgentBatchToolbar` action, dialog state; `runBatch` wiring); `packages/views/agents/components/bulk-set-access-dialog.tsx` (new, embeds `AccessPicker` via the `<BulkAccessPicker>` wrapper); `packages/views/agents/components/inspector/access-picker.tsx` (adds optional `hideFooter` prop — KTD6).
- **Approach:** Add a "Set access scope…" button to `AgentBatchToolbar`, enabled when at least one selected row is `isOwnedByMe` and not busy (gated by ownership, not `canManage` — see KTD3). Use a **single dialog**: a `<BulkAccessPicker>` wrapper renders `AccessPicker` with the new `hideFooter` prop (KTD6) so the picker's internal Save is suppressed and the dialog's own confirm button is the sole apply trigger. The dialog opens with **no scope preselected** and confirm disabled until the operator picks one; when "Specific people" is chosen, confirm stays disabled until ≥1 member target is selected. A live summary line shows "Applies to N agents" (the owned subset) and updates as the selection or scope changes; non-owned selected agents are skipped and their count is surfaced in the summary and the post-action toast. On confirm, map the picker's `AccessChange` to `api.updateAgent(id, { permission_mode, invocation_targets })` and call `runBatch(fn, ownedSelectedRows)`. `runBatch` already invalidates `workspaceKeys.agents(wsId)`. Accessibility: the dialog traps focus, restores focus on close, and the affected-count text is part of the dialog's accessible name.
- **Patterns to follow:** The archive action with confirm dialog in `agents-page.tsx`; the `runBatch` usage for restore.
- **Test scenarios:** Select three owned agents, choose Workspace, confirm shows "Applies to 3", applies `updateAgent` per id, list invalidates and reflects Workspace (Covers AE3, F1); mixed owned/non-owned selection → button enabled (any-owned), summary shows owned + skipped counts, only owned agents change (Covers AE3); dialog opens with no scope preselected and confirm disabled until one is picked; choose Specific people with no member selected → confirm disabled, with ≥1 member → enabled, same member set applies to all owned selected (Covers AE5, R7); choose Owner-only → applies `private` and clears targets; cancel applies nothing; busy/disabled states; an error mid-batch toasts and invalidates; the dialog traps and restores focus and exposes the affected count in its accessible name.
- **Verification:** Page-level test mocking `@multica/core/api` (`api.updateAgent` spy) with a callable store; assert per-id calls and query invalidation; `pnpm typecheck`.

---

## Verification Contract

- `pnpm typecheck` — strict-mode types across `packages/core` and `packages/views`.
- `pnpm test` — Vitest: U1 helper unit tests; `view-store.test.ts` for the new filter and column; page-level tests for the column label, the filter predicate, and the bulk action (mock `@multica/core/api` and use the callable-store pattern per the repo test rules).
- `pnpm lint` — repo lint clean.
- Browser smoke (`pnpm dev:web`, per the repo UI rule): open the agents list and confirm the column shows the correct three states for real agents, the filter isolates each class, and the bulk dialog shows scope + count, applies, and refreshes the list.
- Accessibility: the column cell text is announced (not icon-only); the access filter sub-menu matches the `availability` sub-menu's keyboard/ARIA behavior; the bulk dialog traps and restores focus and exposes the affected count in its accessible name.

---

## Definition of Done

- **Global:** All four units implemented; `pnpm typecheck`, `pnpm test`, and `pnpm lint` green; browser smoke confirms column, filter, and bulk on real agents; no backend changes; `AccessPicker` reused via wrapper with only an opt-in `hideFooter` prop added (KTD6); "agent" kept in English within zh-Hans; any abandoned experimental code removed.
- **Per unit:**
  - U1: helper pure and unit-tested across all three states plus the defensive cases.
  - U2: column visible by default (placed after `owner`) and renders the correct state per agent.
  - U3: filter isolates each class, combines with other dimensions, and persists.
  - U4: bulk applies all three scopes, shows the confirmation, skips non-owned agents (surfacing the skip count), and invalidates the list.
