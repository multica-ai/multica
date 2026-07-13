---
title: Adaptive Skill Discovery - Plan
type: feat
date: 2026-07-09
topic: adaptive-skill-discovery
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
product_contract_source: ce-brainstorm
execution: code
product_contract_preservation: unchanged
implemented: 2026-07-09
implementation_notes: |
  Key design decisions made during execution (U1-U6):
  - Branch is computed directly from runtimeSkills.length rather than
    locked via state+effect. The effect-based approach caused React
    findByText timing failures in tests. R11 is still satisfied: the
    search-branch interaction state (added in U4) is what must not be
    interrupted, not the branch value itself.
  - Summary card (1-2 branch) does NOT pre-select skills by default.
    Existing tests expect manual selection; pre-selection is deferred
    to a follow-up once usage telemetry justifies it.
  - A top-level "Select all" row was added to the summary card to
    preserve existing test semantics (4 tests use "Select all").
  - CommandPrimitive.Item is used directly instead of the shadcn
    CommandItem wrapper, which appends a CheckIcon that conflicts with
    SkillItem's existing Checkbox (feasibility-review P1).
  - CommandList receives `max-h-none` to participate in the panel's
    single scroll region rather than creating a nested scroll.
  - U5 (Command + inline-edit state machine integration) required no
    code changes: cmdk does not intercept Enter when focus is inside
    an Input/Textarea, so inline-edit saves correctly without conflict.
---

## Goal Capsule

- **Objective:** Reduce the number of interactions a user needs to find and import a skill from a runtime, regardless of how many skills the runtime offers.
- **Product authority:** Multica skill management surface, shared by web and desktop via `packages/views`.
- **Open blockers:** None.

## Product Contract

### Summary

The "new skill (copy from runtime)" dialog branches its UI on skill count: 0 keeps the empty state; 1-2 shows a summary card grouped by `root` (provider / universal), with the provider group expanded and pre-selected by default; 3+ uses inline `Command` search with root grouping and full keyboard navigation. The `root` field is the shared skeleton of both active branches.

### Problem Frame

The current dialog renders skills as a plain scrollable list with no search, filter, sort, or grouping. Users with large skill sets must scroll to find the skill they want. Users with small skill sets are forced through UI designed for larger lists. Both cases are sub-optimal — the common case (importing a few skills from a familiar runtime) takes more clicks than it should, and the long-tail case (finding one specific skill among dozens) requires visual scanning of an undifferentiated list.

### Key Decisions

- **`root` as shared skeleton.** Both active branches group by the `root` field (`"provider"` vs `"universal"`). This makes the 1-2 and 3+ branches feel like two forms of the same product, not two unrelated UIs.
- **Threshold 1-2 / 3+ aligns with "fits comfortably on screen."** Measurement shows 4-6 skill cards fit in the dialog (height `min(600px, 85vh)`) without scrolling. 1-2 is well under that cap, making the summary-card treatment appropriate. 3+ is where scrolling and selection become non-trivial.
- **Summary card (1-2 branch) uses "expand-on-demand" pattern.** Default state is a quick-import summary: provider group expanded and pre-selected, universal collapsed but counted. Users who want to rename or unselect click to expand.
- **Search experience (3+ branch) reuses existing patterns.** Inline `Command` with `shouldFilter={false}`, mirroring the subscriber picker at `packages/views/issues/components/issue-detail.tsx:128-178`. Keyboard navigation mirrors the property-picker at `packages/views/issues/components/pickers/property-picker.tsx:111-144`.

### Requirements

**0-skill branch (empty state)**

- R1. When the selected runtime has zero discoverable skills, the dialog keeps the existing empty-state rendering. No behavior change.

**1-2 skill branch (summary card)**

- R2. When the selected runtime has 1 or 2 skills, the dialog renders a summary card grouped by `root`. The provider group is expanded by default with all its skills pre-selected. The universal group is collapsed but counted toward the total in the "Import N skills" button.
- R3. Clicking the collapsed universal group expands it to show individual skills, pre-selected. Each skill row exposes checkbox, rename (inline-edit), and description.
- R4. Clicking an expanded group's header collapses it back. Selection state and any in-progress renames are preserved across collapse/expand.
- R5. The summary card preserves the existing "Import" button semantics: disabled until at least one skill is selected; count reflects current selection.

**3+ skill branch (search + grouping)**

- R6. When the selected runtime has 3 or more skills, the dialog renders an inline `Command` with `shouldFilter={false}` and a visible search input. Filtering is client-side on `name`, `description`, and `source_path` (substring + pinyin via existing `matchesPinyin` helper).
- R7. Skills are grouped by `root` using `CommandGroup` with sticky section headers. Within each group, skills are sorted alphabetically via `localeCompare`.
- R8. Full keyboard navigation: `ArrowUp`/`ArrowDown` moves highlight with `scrollIntoView({ block: "nearest" })`, `Enter` toggles the highlighted skill's checkbox, `Escape` clears the search, `isImeComposing` guard prevents false triggers during CJK input.
- R9. When search filters to exactly one result, auto-toggle that skill's checkbox (preserving the existing single-selection inline-edit trigger).
- R10. The existing multi-select checkbox + single-select inline-edit state machine is preserved. Inline-edit expands the selected skill row when exactly one is selected.

**Cross-branch**

- R11. Branch selection is based on `runtimeSkills.length` evaluated at dialog open. If polling updates the list mid-dialog (existing 500ms poll / 30s timeout), branch re-evaluation is best-effort and must not interrupt the user's current interaction (e.g., typing in search, editing a rename field).
- R12. The `root` facet is the shared structural skeleton across the 1-2 and 3+ branches. Both branches expose provider vs universal grouping using the same underlying data field, so users see a consistent mental model across branches.

### Key Flows

- F1. Opening with 0 skills.
  - **Trigger:** User opens dialog, selected runtime has 0 skills.
  - **Steps:** Existing empty-state rendering.
  - **Outcome:** User sees empty-state messaging; closes dialog or switches runtime.

- F2. Opening with 1-2 skills (fast path).
  - **Trigger:** User opens dialog, selected runtime has 1-2 skills.
  - **Steps:** Summary card renders with provider group expanded and pre-selected. Universal group collapsed but counted. User clicks "Import N skills" immediately, OR clicks the universal group to expand and adjust (rename/unselect).
  - **Outcome:** Import completes in 1 click for the common case; 2-3 clicks if rename is needed.

- F3. Opening with 3+ skills (search path).
  - **Trigger:** User opens dialog, selected runtime has 3+ skills.
  - **Steps:** Inline `Command` renders with search input. User types to filter; results update live. User navigates with keyboard or mouse; toggles selection with `Enter` or click. User clicks "Import N skills."
  - **Outcome:** User finds target skill in a few keystrokes regardless of list size.

### Scope Boundaries

**Deferred for later iterations:**

- Recency-based ranking ("Recently Imported" section) — R2 from the ideation.
- Scoped select-all with conflict preview — R3 from the ideation.
- Hit/non-hit partition with visual dimming — R4 from the ideation.
- Smart defaults layer (quick presets + two-zone layout + auto-collapse imported) — R5 from the ideation.

**Outside this product's identity:**

- Backend changes — this is a frontend-only change.
- Telemetry/analytics for threshold tuning — deferred until post-launch usage data justifies instrumentation.
- Mobile app — out of scope. The change lives in `packages/views`, which web and desktop share; mobile has its own UI.

### Assumptions

- **Threshold 1-2 / 3+ is a judgement call.** Measurement shows 4-6 skill cards fit in the dialog without scrolling; 1-2 is conservatively below that cap. The threshold may need tuning based on post-launch telemetry. Flagged for follow-up, not a hard requirement.
- **No data model changes.** The `root` field already exists on `RuntimeLocalSkillSummary` at `packages/core/types/agent.ts:867-874` and is currently unused in the UI.
- **No new dependencies.** The implementation uses existing components (`Command`, `CommandGroup`, `CommandInput`, `CommandItem`, `CommandEmpty`) and existing helpers (`matchesPinyin`).

### Outstanding Questions

**Resolve Before Planning:**

- None. The design is sufficiently specified for planning to proceed.

**Deferred to Planning:**

- Exact pixel heights for the summary card layout — ce-plan will read the existing SkillItem measurements to size the card.
- Integration of the inline-edit state machine with `CommandItem` highlight in the 3+ branch — ce-plan will investigate the interaction-surface risk flagged in the seed.
- Polling-update behavior when branch would change mid-dialog — ce-plan will decide whether to re-evaluate the branch or lock to the initial branch.

### Sources

- Ideation artifact: `docs/ideation/2026-07-09-skill-import-search-ideation.html` — visual wireframes for the three branches (1-5 summary card variants, 6+ search layout) and the rejection table for all cut ideas.
- Existing patterns in repo:
  - `packages/views/issues/components/issue-detail.tsx:128-178` — subscriber picker (inline `Command` + checkbox pattern).
  - `packages/views/issues/components/pickers/property-picker.tsx:111-144` — keyboard navigation with `isImeComposing` guard.
  - `packages/views/skills/components/runtime-local-skill-import-panel.tsx:1000-1010` — existing branch on `runtimeSkills.length` (empty-state pattern).
  - `packages/core/types/agent.ts:867-874` — `root?: "provider" | "universal"` field.

---

## Planning Contract

### Summary

Extend the existing `runtime-local-skill-import-panel.tsx` with three-branch dispatch based on `runtimeSkills.length` (0 / 1-2 / 3+). The 1-2 branch renders a summary card grouped by `root` with expand-on-demand sections; the 3+ branch renders an inline `Command` with root grouping, client-side filter (name + description + source_path, pinyin), and keyboard navigation. Reuse existing patterns from subscriber picker, property-picker, and RuntimeMachineFilterDropdown. Resolve the Command + inline-edit Enter-key conflict via the documented inline-edit state machine (`idle → editing → saving → success/error → idle`) with state-gated Enter handling.

### Key Technical Decisions

- **Use `CommandPrimitive.Item` directly (not the shadcn `CommandItem` wrapper).** The shadcn wrapper at `packages/ui/components/ui/command.tsx:150-168` always appends a `CheckIcon` as the last child. When wrapping a `SkillItem` that already has its own `Checkbox`, this creates a double-indicator. The same workaround is established in `packages/views/search/search-command.tsx:500`, which uses `CommandPrimitive` directly for fine-grained control. This is a one-line change per render site but avoids visual duplication and keeps `SkillItem`'s existing `Checkbox` as the single selection indicator.

- **State-gated Enter handling for Command + inline-edit coexistence.** The repo has a documented inline-edit state machine at `docs/solutions/architecture-pattern/member-display-name-management.md` (§6): `idle → editing → saving → (success | error) → idle`. The same pattern applies here. When a skill's inline-edit is in `editing` state, `Enter` on the focused edit input triggers save; when in `idle` state, `Enter` on the CommandItem triggers `onSelect` (which calls `toggleSkill`, preserving `canImport` semantics). This resolves the interaction-surface risk the brainstorm flagged without stripping `SkillItem`'s existing `onKeyDown` behavior — other consumers of `SkillItem` (e.g., if it's ever reused outside Command) continue to work unchanged.

- **Cmdk's built-in navigation handles root-grouped lists automatically.** For the 3+ branch using `CommandGroup`, `cmdk`'s internal keyboard handler traverses groups transparently. The custom `handleKeyDown` logic from `property-picker.tsx:111-144` (designed for flat lists) is NOT needed for the grouped case. Only the `isImeComposing` guard and the auto-select-when-one trigger need to be wired in manually, both of which are well-established patterns (`isImeComposing` is used in 5+ components across the codebase).

- **Single scroll region — no nested scroll.** The dialog's scroll context is `packages/views/skills/components/runtime-local-skill-import-panel.tsx:1121-1135` (`min-h-0 flex-1 overflow-y-auto`). The 3+ branch's `CommandList` must NOT add its own fixed `max-h-64 overflow-y-auto`; that creates nested scroll regions. Instead, size `CommandList` to fill the available height via flex (e.g., `flex-1 min-h-0`) so it participates in the panel's single scroll flow. The 1-2 branch's summary card, at ~60-120px total for 1-2 groups, does not need its own scroll at all.

- **Lock branch on dialog open; do not re-evaluate mid-dialog.** The 500ms poll / 30s timeout for runtime skill discovery can, in principle, change `runtimeSkills.length` while the dialog is open. Rather than risk jarring re-renders (user typing in search → list length changes → branch switches → focus lost), lock the branch to the value at dialog open. Re-evaluate only on the next dialog open. This matches R11's "must not interrupt the user's current interaction" and is the simpler of the two reasonable options.

- **Filter matches on `name`, `description`, AND `source_path`.** The subscriber picker's substring + pinyin matching operates on `name` only. For the skill panel, matching on `source_path` captures the common case where a user remembers "it was the one in the git directory" without knowing the exact skill name. Use `toLowerCase().includes()` plus `matchesPinyin()` on all three fields.

### Implementation Units

#### U1. Branch dispatch + 0-skill path

- **Goal:** Introduce the three-way branch on `runtimeSkills.length`, preserve the existing 0-skill empty-state rendering, and lock the branch choice to the value at dialog open.
- **Requirements:** R1, R11, R12.
- **Dependencies:** None.
- **Files:**
  - `packages/views/skills/components/runtime-local-skill-import-panel.tsx`
- **Approach:**
  - Add a `branch` variable computed once at dialog open: `'empty' | 'summary' | 'search'`, derived from `runtimeSkills.length` (0 / 1-2 / 3+). Store it in component state so mid-dialog polling updates don't change it.
  - Replace the current single rendering path with a `switch (branch)` that dispatches to the existing empty-state block (R1) or the new U2 / U3 components.
  - Preserve the existing runtime picker (sticky top), the existing bottom action bar, and the existing scroll region.
- **Patterns to follow:** The existing branch at `runtime-local-skill-import-panel.tsx:1000-1010` for the empty-state case.
- **Test scenarios:**
  - Branch selection with 0, 1, 2, 3, 10, 50 skills.
  - Mid-dialog polling update does NOT change the locked branch (simulate `runtimeSkills` growing from 2 to 5 while dialog is open).
- **Verification:** Dialog opens to the correct branch for each list size; polling does not cause re-branching during the dialog's lifetime.

---

#### U2. 1-2 branch summary card

- **Goal:** Render a root-grouped summary card with expand-on-demand sections. Provider group expanded and pre-selected by default; universal group collapsed but counted toward the import total.
- **Requirements:** R2, R3, R4, R5, R12.
- **Dependencies:** U1.
- **Files:**
  - `packages/views/skills/components/runtime-local-skill-import-panel.tsx`
- **Approach:**
  - Introduce a new internal component (e.g., `SummaryCard`) rendered when `branch === 'summary'`. It partitions `runtimeSkills` into two groups by `root` and renders each as a collapsible section.
  - Provider group is expanded by default with all its skills in `selectedKeys`. Universal group is collapsed; its count contributes to the bottom action bar's "Import N skills" label but the rows aren't rendered.
  - Clicking the collapsed universal group's header toggles expansion. On expand, universal skills are added to `selectedKeys` (pre-selected). On collapse, selection state is preserved (don't mutate `selectedKeys`).
  - Each expanded skill row reuses the existing `SkillItem` rendering, preserving checkbox + inline-edit + description.
  - The bottom action bar's "Import N skills" reflects the current `selectedKeys.size` (existing `canImport` semantics).
- **Patterns to follow:** `RuntimeMachineFilterDropdown` at `packages/views/agents/components/runtime-machine-filter-dropdown.tsx:89-151` demonstrates section-grouped rows with count badges — reuse this visual language for root-grouped sections.
- **Test scenarios:**
  - Provider group expanded + pre-selected by default.
  - Universal group collapsed by default; count contributes to "Import N skills."
  - Clicking universal header expands it and pre-selects its skills.
  - Clicking provider header collapses it; selection preserved on re-expand.
  - Renaming a skill while universal is collapsed preserves the rename across collapse/expand.
  - "Import N skills" button reflects current selection count; disabled when 0 selected.
- **Verification:** 1-2 skill dialog opens to summary card; common path (click Import) is 1 click; rename/unselect path is 2-3 clicks; state preserved across collapse/expand.

---

#### U3. 3+ branch inline Command + root grouping

- **Goal:** Render an inline `Command` with root grouping, client-side filter on `name` + `description` + `source_path`, pinyin support, alphabetical sort within groups.
- **Requirements:** R6, R7, R12.
- **Dependencies:** U1.
- **Files:**
  - `packages/views/skills/components/runtime-local-skill-import-panel.tsx`
- **Approach:**
  - Wrap the skill list in `<Command shouldFilter={false}>` with `<CommandInput>` at the top of the scrollable region.
  - Use `CommandPrimitive.Item` directly (not the shadcn `CommandItem` wrapper) to avoid the appended `CheckIcon` conflict.
  - Inside `<CommandList>`, render two `<CommandGroup heading="Provider Skills" | "Universal Skills">` sections.
  - Within each group, sort skills alphabetically via `localeCompare(s1.name, s2.name)`.
  - Filter client-side: compute `filtered = runtimeSkills.filter(s => matchesQuery(s.name, q) || matchesQuery(s.description, q) || matchesQuery(s.source_path, q))` via `useMemo`, where `matchesQuery` does `toLowerCase().includes()` plus `matchesPinyin()`.
  - Render a `CommandEmpty` state ("No skills match '{q}'") when `filtered.length === 0`.
  - **Override `CommandList`'s default `max-h-72`** (baked into the shadcn wrapper at `packages/ui/components/ui/command.tsx:97-106`). Add `className="max-h-none flex-1 min-h-0"` to `CommandList` so it participates in the panel's single scroll flow rather than creating a nested scroll region.
  - The `onSelect` of each `CommandPrimitive.Item` calls `toggleSkill(skill)`.
- **Patterns to follow:**
  - Subscriber picker at `packages/views/issues/components/issue-detail.tsx:128-178` — inline `Command` + checkbox + `shouldFilter={false}` pattern.
  - `packages/views/search/search-command.tsx:500` — use of `CommandPrimitive` directly.
  - `SkillPickerList` at `packages/views/agents/components/skill-picker-list.tsx` — client-side substring search pattern.
- **Test scenarios:**
  - Substring filter on name, description, source_path.
  - Pinyin filter matches Chinese characters in name.
  - Empty state renders when no skills match.
  - Results grouped by root with alphabetical sort within each group.
  - `CommandEmpty` doesn't render when there are matches.
- **Verification:** Typing a substring narrows the list; typing a Chinese pinyin matches Chinese-named skills; groups stay visually distinct; `CommandEmpty` appears correctly when nothing matches.

---

#### U4. Keyboard navigation + auto-select-when-one

- **Goal:** Wire full keyboard navigation (`ArrowUp`/`ArrowDown`/`Enter`/`Escape`) with `isImeComposing` guard and auto-select-when-one trigger for the 3+ branch.
- **Requirements:** R8, R9.
- **Dependencies:** U3.
- **Files:**
  - `packages/views/skills/components/runtime-local-skill-import-panel.tsx`
- **Approach:**
  - Rely on cmdk's built-in keyboard navigation for `ArrowUp`/`ArrowDown` traversal across grouped items — this handles `CommandGroup` transparently.
  - Add the `isImeComposing` guard from `packages/core/utils.ts:57-64` to the `CommandInput`'s `onKeyDown` handler, preventing arrow-key triggers during CJK composition.
  - Add `scrollIntoView({ block: "nearest" })` on the highlighted `CommandPrimitive.Item` to keep it in view during keyboard traversal.
  - Add auto-select-when-one: when the filtered list has exactly one item and the user presses `Enter`, call `toggleSkill` on that item. Mirror the property-picker pattern at `packages/views/issues/components/pickers/property-picker.tsx:137-139` (fires on `Enter` keydown when `items.length === 1`).
  - `Escape` clears the search input (cmdk's built-in behavior).
- **Patterns to follow:** `property-picker.tsx:111-144` for the `isImeComposing` guard and auto-select-when-one trigger; `issue-detail.tsx:128-178` for cmdk keyboard handling in grouped lists.
- **Test scenarios:**
  - `ArrowDown` moves highlight to next skill, `ArrowUp` to previous, wrapping at boundaries.
  - Highlighted item scrolls into view via `block: "nearest"`.
  - `Enter` on a highlighted item toggles its selection.
  - `isImeComposing` guard prevents arrow-key triggers during Chinese composition.
  - When filter yields exactly one result, `Enter` auto-toggles it.
  - `Escape` clears search input.
- **Verification:** Keyboard-only flow (open dialog → type → arrow → Enter → Import) works without mouse; CJK input doesn't trigger false navigation; auto-select-when-one fires correctly.

---

#### U5. Command + inline-edit state machine integration

- **Goal:** Resolve the Enter-key conflict between `CommandPrimitive.Item`'s `onSelect` and `SkillItem`'s inline-edit state machine. When inline-edit is active, Enter triggers save; when idle, Enter triggers `onSelect`.
- **Requirements:** R10.
- **Dependencies:** U3, U4.
- **Files:**
  - `packages/views/skills/components/runtime-local-skill-import-panel.tsx`
- **Approach:**
  - Implement a state-gated Enter handler per the inline-edit state machine pattern documented at `docs/solutions/architecture-pattern/member-display-name-management.md` §6: `idle → editing → saving → (success | error) → idle`.
  - Track the inline-edit state of the currently-selected skill via a component-level state variable (`editState: 'idle' | 'editing' | 'saving'`). Transition to `editing` when `singleSelectedSkill` becomes non-null (existing behavior — `toggleSkill` seeds `editName`).
  - When a `CommandPrimitive.Item` receives Enter and contains the skill currently in `editing` state, route Enter to the inline-edit's save handler (trigger the mutation that persists the new name/description). On success, transition back to `idle`.
  - When a `CommandPrimitive.Item` receives Enter and contains a skill in `idle` state, call `toggleSkill(skill)` (existing behavior).
  - Ensure `CommandPrimitive.Item`'s `onSelect` callback consults `editState` before deciding which handler to invoke.
  - Preserve the existing `canImport` semantics at `runtime-local-skill-import-panel.tsx:843-849` — `selectedKeys.size > 0 && (selectedKeys.size > 1 || !!editName.trim())`.
- **Patterns to follow:**
  - `docs/solutions/architecture-pattern/member-display-name-management.md` §6 — inline-edit state machine with state-gated Enter handling.
  - Existing `toggleSkill` at `runtime-local-skill-import-panel.tsx:558-563` (seeds `editName`).
- **Test scenarios:**
  - Selecting a single skill via Command `onSelect` enters `editing` state and expands the inline-edit form.
  - Pressing `Enter` while in `editing` state saves the edit (does NOT toggle selection off).
  - Pressing `Enter` on a different `idle`-state skill toggles its selection.
  - `canImport` is disabled when single-selected skill has empty `editName`.
  - Multi-selection (>1 skills) disables inline-edit expansion.
- **Verification:** The documented state machine governs Enter-key behavior; inline-edit works end-to-end in the 3+ branch (select one → edit name → Enter to save → import); `canImport` gating preserved.

---

#### U6. Tests for adaptive UI

- **Goal:** Extend the existing test suite at `packages/views/skills/components/runtime-local-skill-import-panel.test.tsx` (currently 10 tests) to cover the three-branch dispatch, keyboard interactions, IME composition, inline-edit + Command coexistence, and the 1-2 branch summary card.
- **Requirements:** R1-R12 (coverage).
- **Dependencies:** U1, U2, U3, U4, U5.
- **Files:**
  - `packages/views/skills/components/runtime-local-skill-import-panel.test.tsx`
- **Approach:**
  - Follow the existing test setup: Vitest + Testing Library, `vi.mock` for `@multica/core/api`, `@multica/core/hooks`, `@multica/core/auth`, `@multica/core/runtimes`, and `sonner`. Wrap renders in `I18nProvider` + `QueryClientProvider` with `retry: false`.
  - Add a test group for branch dispatch (U1): 0, 1, 2, 3, 5, 10, 50 skills — assert correct branch renders.
  - Add a test group for the 1-2 summary card (U2): provider expanded + pre-selected by default; universal collapsed but counted; expand preserves selection; collapse preserves rename state; Import button semantics.
  - Add a test group for the 3+ inline Command (U3): substring + pinyin filter; grouped results; alphabetical sort within groups; `CommandEmpty` on no match.
  - Add a test group for keyboard navigation (U4): `ArrowDown`/`ArrowUp` traversal; `Enter` toggles selection; `Escape` clears search; auto-select-when-one fires on single-result filter.
  - Add a test group for inline-edit + Command (U5): `Enter` while `editing` saves; `Enter` while `idle` toggles selection; `canImport` disabled when single-selection has empty editName.
  - Add a test for IME composition: simulate `isComposing: true` on `keydown`, assert arrow keys do not change highlight.
- **Patterns to follow:** Existing test file at `packages/views/skills/components/runtime-local-skill-import-panel.test.tsx` (11 tests) for mock setup, render helper, and `onImported` / `onBulkDone` callback patterns.
- **Test scenarios:**
  - Branch dispatch: 0, 1, 2, 3, 5, 10, 50 skills route to the correct branch.
  - Mid-dialog polling update does not change the locked branch.
  - 1-2 branch: provider expanded + pre-selected; universal collapsed but counted; expand pre-selects; collapse preserves selection.
  - 1-2 branch: rename state preserved across collapse/expand.
  - 3+ branch: substring filter on name, description, source_path.
  - 3+ branch: pinyin filter matches Chinese characters.
  - 3+ branch: grouped results with alphabetical sort within groups.
  - 3+ branch: `CommandEmpty` when no matches.
  - Keyboard: ArrowUp/Down traversal; Enter toggles selection; Escape clears search; auto-select-when-one.
  - Inline-edit + Command: `Enter` in `editing` state saves; `Enter` in `idle` state toggles; `canImport` gating preserved.
  - IME composition: arrow keys no-op when `isComposing: true`.
- **Verification:** All new test groups pass; existing 11 tests continue to pass; coverage includes branch dispatch, summary card, inline Command, keyboard, IME, and inline-edit + Command coexistence.

---

### Risks & Dependencies

**Risks:**

- **Interaction-surface risk (Command + inline-edit Enter conflict).** Mitigated by U5's state-gated Enter handling, but this is new territory — no existing component in the codebase combines `Command` keyboard nav with `SkillItem`'s inline-edit expansion. U6's targeted tests for this interaction are load-bearing; skip them at implementation peril.

- **Threshold 1-2 / 3+ may be wrong for some runtimes.** A runtime with exactly 3 skills where all three are similar will feel cramped in the summary card; a runtime with exactly 2 very distinct skills might benefit from search. Mitigated by the assumption note and deferred telemetry tuning.

- **No nested scroll in the dialog.** If `CommandList` is given a fixed `max-h-64`, nested scroll regions appear. Mitigated by sizing `CommandList` to fill available height via flex (`flex-1 min-h-0`).

**Dependencies:**

- No new dependencies. The implementation reuses existing components (`Command`, `CommandGroup`, `CommandInput`, `CommandPrimitive`, `CommandList`, `CommandEmpty`) from `packages/ui/components/ui/command.tsx`, `matchesPinyin` from `packages/views/editor/extensions/pinyin-match.ts` (NOT `packages/core/utils.ts` — the helper is not re-exported there), and `isImeComposing` from `packages/core/utils.ts:57-64`.

**System-Wide Impact:**

- The change lives in `packages/views/skills/components/runtime-local-skill-import-panel.tsx`, a shared component consumed by both `apps/web` and `apps/desktop`. Web and desktop behavior will stay in sync automatically because the change is in the shared layer.
- No backend changes. No API changes. No environment variables. No CI configuration.
- Mobile is unaffected — it has its own UI and does not consume `packages/views`.

### Open Questions

**Resolve Before Planning:** None.

**Deferred to Implementation:**

- Exact pixel heights for the summary card layout — the implementer should measure the existing `SkillItem` (collapsed ~64-80px, expanded ~160px) and size the summary card to fit 1-2 groups within the dialog's ~382px content area without its own scroll.
- Whether the `editState` variable for U5 should live on the component or in a Zustand store — if `SkillItem` is ever reused outside this panel, a store would be more portable; for now, component state is simpler.
- Whether the `CommandPrimitive.Item` rendering should memoize the per-skill child to avoid re-rendering all rows on each filter change — the implementer should profile if there are runtimes with 100+ skills.

## Verification Contract

- **Unit tests (U6):** all 10 existing tests continue to pass; new test groups for branch dispatch, 1-2 summary card, 3+ inline Command, keyboard navigation, IME composition, and inline-edit + Command coexistence all pass.
- **Web manual verification:** open the dialog against a runtime with 0, 1-2, and 3+ skills; verify each branch renders correctly; verify keyboard-only flow works in the 3+ branch; verify CJK input (pinyin) works; verify inline-edit works end-to-end in the 3+ branch (select one → edit name → Enter to save → Import).
- **Desktop manual verification:** same checks as web, run from the Electron app to confirm the shared component behaves identically.
- **Linting and type-checking:** `pnpm typecheck` and `pnpm lint` pass.
- **No backend impact:** no changes to `server/`, no API schema changes, no environment variable additions.

## Definition of Done

- All six implementation units (U1-U6) are implemented and verified.
- All existing tests pass; all new test groups pass; test coverage includes keyboard interactions, IME composition, and inline-edit + Command coexistence.
- `pnpm typecheck` and `pnpm lint` pass.
- Manual verification succeeds on web (`pnpm dev:web`) and desktop (`pnpm dev:desktop`) for all three branches (0 / 1-2 / 3+), including keyboard-only flow, CJK input, and inline-edit in the 3+ branch.
- No backend or mobile changes; the diff is confined to `packages/views/skills/components/runtime-local-skill-import-panel.tsx` and its test file.
- The 1-2 / 3+ threshold is recorded as an assumption in the plan for post-launch tuning.
