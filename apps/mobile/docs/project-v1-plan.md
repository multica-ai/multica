# Project surface — v1 implementation plan

**Status**: Pre-implementation, awaiting approval.
**Pairs with**: `project-v1-gap-audit.md` (the audit identifying every gap).
**Date**: 2026-05-20

## Context

This PR brings mobile's Project surface to release-quality parity with web/desktop for the **operate / assign-work / collaborate** use cases. The deferred items (Pin, inline title edit, emoji picker, filters, Gantt, etc.) are documented in the gap audit and not addressed here.

## Scope

Six items, one PR:

1. **Tier B finish**: migrate `project-status-picker-sheet.tsx` + `project-priority-picker-sheet.tsx` → route-modal `*-body.tsx` + route file
2. **Progress section** on detail (horizontal bar + `done / total` text)
3. **View mode switcher** on detail's related-issues area (Board / List), with Board view as new
4. **Create-form draft persistence** (mirror `new-issue-draft-store`)
5. **Hardcoded hex sweep** (5 occurrences → tokens or `THEME[scheme].mutedForeground`)
6. **Hand-drawn SVG → existing primitives** (3 occurrences → `IconButton` / `Ionicons`)

## Out of scope

See `project-v1-gap-audit.md` "Out of scope" section. Don't touch.

## Component decisions (waterfall per item)

| # | Item | Component | Reason |
|---|---|---|---|
| 1a | Status picker shell | New file `app/(app)/[workspace]/project/[id]/picker/status.tsx` (route) | Mirrors existing `picker/lead.tsx` and the issue pickers — established mobile route-modal pattern |
| 1a | Status picker body | New file `components/project/pickers/project-status-picker-body.tsx` | Rename + extract from `project-status-picker-sheet.tsx` |
| 1b | Priority picker | Same pattern: `picker/priority.tsx` route + `project-priority-picker-body.tsx` body | Same |
| 2 | Progress bar | Inline composition in `project-header-card.tsx`: `<View className="h-1.5 bg-secondary rounded-full"><View className="h-full bg-brand" style={{ width: `${pct}%` }} /></View>` | Trivial primitive composition; no new component needed (below three-occurrence threshold) |
| 2 | Progress text | `<Text className="text-xs text-muted-foreground">` | Existing token |
| 3 | View mode switcher | iOS native `SegmentedControl` (`@react-native-segmented-control/segmented-control`) — already installed | Principle 3 native > RNR > discuss; same pattern as workspace Issues scope tabs |
| 3 | Board view | `ScrollView horizontal pagingEnabled={false}` + per-status column = `FlatList` of `IssueRow` | Reuses `IssueRow`; no new primitive |
| 3 | View mode state | Local `useState` in `project-related-issues.tsx`, NOT a Zustand store | No cross-component need; matches mobile/CLAUDE.md "Don't add state unless explicitly required" |
| 4 | Draft store | New `apps/mobile/data/stores/new-project-draft-store.ts`, mirroring `new-issue-draft-store.ts` exactly | Three-occurrence threshold: new-issue, new-project — close to threshold but pattern is small enough that the duplicated store is cheaper than a generic abstraction |
| 5 | Hex → token | `text-muted-foreground` class via `Ionicons color={THEME[scheme].mutedForeground}` | Same pattern as the my-issues `Chip` X icon |
| 6a | PlusButton → IconButton | `<IconButton name="add" />` | Existing primitive |
| 6b | Chevron → Ionicons | `<Ionicons name="chevron-forward" size={14} color={THEME[scheme].mutedForeground} />` | Existing |

## API / data layer

No new endpoints. All data already available:

- Progress bar: `Project.issue_count` + `Project.done_count` (already in schema, `packages/core/types/project.ts:17-18`)
- View mode switcher: same `issueListOptions` filtered by `project_id` already used in `project-related-issues.tsx`
- Tier B picker migration: existing `useUpdateProject` mutation (no API change)
- Draft persistence: pure client state, no backend

## State / store changes

| Store | Change |
|---|---|
| `new-project-draft-store.ts` (new) | `title`, `description`, `icon`, `status`, `priority`, in-memory zustand store; `clear()` action on submit success; no persist middleware in v1 (matches `new-issue-draft-store.ts`) |
| (no other store touched) | Status / Priority picker state stays in the existing `useUpdateProject` mutation flow; view mode is local component state |

## File-by-file

### New files

```
apps/mobile/app/(app)/[workspace]/project/[id]/picker/status.tsx
apps/mobile/app/(app)/[workspace]/project/[id]/picker/priority.tsx
apps/mobile/components/project/pickers/project-status-picker-body.tsx   (renamed/extracted from -sheet.tsx)
apps/mobile/components/project/pickers/project-priority-picker-body.tsx (renamed/extracted from -sheet.tsx)
apps/mobile/data/stores/new-project-draft-store.ts
```

### Deleted files

```
apps/mobile/components/project/pickers/project-status-picker-sheet.tsx
apps/mobile/components/project/pickers/project-priority-picker-sheet.tsx
```

### Modified files

```
apps/mobile/app/(app)/[workspace]/_layout.tsx
  - register two new Stack screens for status + priority picker routes
  - presentation: "formSheet" with detents matching the lead picker

apps/mobile/app/(app)/[workspace]/project/[id].tsx
  - swap inline Modal opens for `router.push({ pathname: "[workspace]/project/[id]/picker/status", ... })`
  - same for priority
  - remove the two Modal state variables and Modal JSX
  - update the headerRight platform-conditional icon color to use THEME[scheme] (line 156 hardcoded hex)

apps/mobile/app/(app)/[workspace]/project/new.tsx
  - import useNewProjectDraftStore
  - read draft into form on mount, write on every field change
  - call clearDraft on successful create

apps/mobile/components/project/project-header-card.tsx
  - add progress section: horizontal bar + "{done_count} / {issue_count}" text + percentage
  - reuse existing card padding

apps/mobile/components/project/project-related-issues.tsx
  - add view mode local state (`"list" | "board"`)
  - add SegmentedControl two-segment header
  - List mode: refactor current "Open / Done" two-bucket grouping into proper status grouping (BOARD_STATUSES — matches web)
  - Board mode: horizontal ScrollView, each column = status header + FlatList of IssueRow
  - replace hand-drawn chevron SVG (line 168-191, 184) with Ionicons
  - drop react-native-svg import if no other usage

apps/mobile/components/project/project-properties-section.tsx
  - replace hand-drawn chevron SVG (line 130-142, 135) with Ionicons
  - drop react-native-svg import if no other usage

apps/mobile/components/project/project-resources-section.tsx
  - replace hardcoded #71717a (line 112) with THEME[scheme].mutedForeground

apps/mobile/app/(app)/[workspace]/more/projects.tsx
  - replace hand-drawn PlusButton SVG (line 108-127) with <IconButton name="add">
  - drop react-native-svg import if no other usage
```

## Implementation order

Optimal order to keep typecheck green at each step:

1. **State store**: create `new-project-draft-store.ts` (no consumers yet, isolated)
2. **Picker bodies**: create `*-picker-body.tsx` files (rename of `-sheet.tsx`); existing imports still point to old, intermediate state OK
3. **Picker routes**: create `picker/status.tsx` and `picker/priority.tsx` route files that mount the bodies
4. **Register in `_layout.tsx`**: add new routes to Stack config
5. **`project/[id].tsx`**: switch from Modal opens to `router.push` for both pickers; delete old Modal state and JSX
6. **Delete old picker sheet files** (`-sheet.tsx`) — at this point nothing imports them
7. **Progress section in `project-header-card.tsx`**
8. **View mode switcher in `project-related-issues.tsx`** — Board view + status-grouped List, replacing Open/Done buckets
9. **Hex / SVG sweep** in `project-properties-section.tsx`, `project-resources-section.tsx`, `more/projects.tsx`
10. **`new.tsx` draft integration**: wire `useNewProjectDraftStore` into the create form
11. **Typecheck** (`pnpm --filter @multica/mobile exec tsc --noEmit`) — must exit 0

## Verification

### Required before PR open

1. `pnpm --filter @multica/mobile exec tsc --noEmit` → exit 0
2. Manual on simulator (cold start app, login, pick workspace):
   - **Tier B picker**: open a project → tap status chip → native iOS pageSheet drags down → pick new status → reflects in chip. Same for priority. Compare gesture to lead picker (already migrated) — should be identical.
   - **Progress bar**: detail screen shows horizontal bar + "X / Y" matching the project's actual `done_count` / `issue_count`. Bar fills proportionally; 0 issues shows empty bar (no divide-by-zero crash).
   - **View mode switcher**: tap "Board" → see horizontal scroll of status columns; tap "List" → see vertical SectionList grouped by `BOARD_STATUSES`. Verify "Cancelled" status issues do NOT render in either view (matches web).
   - **Draft persistence**: open new-project form → type title "Test" → background app via home button → re-foreground → title still "Test". Tap Cancel → re-open new-project → form is empty.
   - **Dark mode**: toggle Settings → Appearance → Dark. Open project list, project detail, properties section, resources section. All icons / chevrons / +Add button now grey-appropriately for dark mode (no stuck `#71717a` slate).
3. Cross-client parity: open the same project on web and mobile, verify progress numbers match exactly.

### Optional checks

- Test on iPad in split-screen (the Board horizontal scroll should still behave)
- Test with a project that has zero issues (progress bar empty, Board / List show empty state)
- Test with a project that has 100+ issues (Board horizontal scroll smooth)

## Risks & rollback

| Risk | Mitigation |
|---|---|
| Board view perf with many issues | Use `FlatList` inside each status column, not `ScrollView` of rows. RNGH respects scroll within scroll. |
| Status-grouped List rendering "Cancelled" issues by accident | Mirror `BOARD_STATUSES` filtering from `more/issues.tsx:97-102` — already the canonical pattern in mobile. |
| Draft store leak between projects | `useNewProjectDraftStore` is global; `clearDraft` is called on submit success AND on Cancel button. If user backs out via swipe-back, draft persists — intentional, mirroring `new-issue-draft-store`. |
| `project/[id].tsx` becomes too long with both Modal removal + progress wiring | Acceptable for one PR; split if it crosses ~600 lines. |

Rollback: revert the diff. No DB / API / cache key changes. WS subscribers unchanged.

## Estimated diff size

- ~80 lines (picker migration: 2 new bodies + 2 new routes + edits to `[id].tsx` + `_layout.tsx`)
- ~40 lines (progress bar in header card)
- ~120 lines (view mode + Board view in related-issues)
- ~50 lines (draft store + integration in `new.tsx`)
- ~30 lines (hex/SVG sweep across 4 files)
- ~10 lines (route registrations in `_layout.tsx`)

**Total**: ~330 lines net, distributed across ~10 files (5 new, 2 deleted, 6 modified).

## Open questions (none blocking — defaults captured below)

These are decisions already made by me as the engineering choices. Listed for transparency.

1. **Board view orientation**: horizontal column scroll (each column = one status), not vertical scroll of status sections. Why: Board's job is "see all statuses side-by-side"; vertical defeats the point. Linear iOS / Things use horizontal.
2. **List mode status grouping**: full `BOARD_STATUSES` (6 groups), replacing the current "Open / Done" rollup. Why: parity with web; the rollup was a mobile-only invention.
3. **Draft persistence storage**: in-memory zustand only, no `expo-secure-store` persist middleware. Why: matches existing `new-issue-draft-store` pattern; cold-kill resilience can come in v2 if user feedback demands.
4. **Status / Priority picker presentation**: iOS `formSheet` (medium detent), matching lead picker. Why: gesture consistency across the attribute row.

If any of these defaults need to change, flag before execution.
