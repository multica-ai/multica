# Mobile: Nav Rearrangement + Collapsible Status Groups

**Date:** 2026-07-01
**Branch:** feature/shared-runtimes-agents

---

## Overview

Two independent mobile changes:

1. **Collapsible status groups** on the project detail screen — tap a status section header to collapse/expand its issue list. State persists within the session (Zustand, in-memory).
2. **Bottom nav rearrangement** — Projects, Issues, Pins become first-class tabs; Inbox and My Issues move into the More dropdown.

---

## Feature 1: Collapsible Status Groups

### Context

`ProjectRelatedIssues` (`apps/mobile/components/project/project-related-issues.tsx`) renders issues grouped into up to 6 status sections (`BOARD_STATUSES`). With many issues each section can be long; users need a way to minimize sections they don't care about.

### Store

New file: `apps/mobile/data/stores/project-collapse-store.ts`

```ts
// Shape: projectId → array of currently-collapsed IssueStatus values
Record<string, IssueStatus[]>
```

- `toggle(projectId, status)` — add if absent, remove if present
- `isCollapsed(projectId, status) → boolean`
- In-memory Zustand only (UI layout preference, not persisted to disk)

### Component changes

**`SectionHeader`** (inside `project-related-issues.tsx`):
- Becomes a `Pressable`
- Shows `sf:chevron.right` icon, rotated 90° when expanded (0° when collapsed) — standard iOS disclosure pattern
- Calls `onToggle()` on press

**`ProjectRelatedIssues`**:
- Reads `isCollapsed(projectId, status)` and `toggle` from the store
- When a section is collapsed: renders `SectionHeader` only, skips the issue rows
- When expanded: renders `SectionHeader` + issue rows (current behaviour)

### What's not changing

- The `more/issues.tsx` workspace-wide issues screen is **not** getting collapsible sections in this PR — the user only requested it on the project detail screen. Add later if needed.
- No default-collapsed state for any status — all sections start expanded on first visit to a project.

---

## Feature 2: Bottom Nav Rearrangement

### New tab order

| Position | Tab | SF Symbol (inactive / active) |
|---|---|---|
| 1 | Projects | `sf:square.stack` / `sf:square.stack.fill` |
| 2 | Issues | `sf:list.bullet` (no fill variant) |
| 3 | Pins | `sf:pin` / `sf:pin.fill` |
| 4 | Chat | `sf:bubble.left` / `sf:bubble.left.fill` (unchanged) |
| 5 | More | `sf:ellipsis` (unchanged) |

Inbox unread badge **moves to the More tab** — the count was on the Inbox tab; since Inbox moves into the More dropdown, the badge surfaces on More so users still see pending notifications.

Chat unread badge stays on Chat (unchanged).

### File moves

| Old path | New path | Header change |
|---|---|---|
| `app/(app)/[workspace]/(tabs)/inbox.tsx` | `app/(app)/[workspace]/more/inbox.tsx` | Add `Stack.Screen options={{ title: "Inbox" }}` (currently uses in-body `<Header>`) |
| `app/(app)/[workspace]/(tabs)/my-issues.tsx` | `app/(app)/[workspace]/more/my-issues.tsx` | Add `Stack.Screen options={{ title: "My Issues" }}` |
| `app/(app)/[workspace]/more/projects.tsx` | `app/(app)/[workspace]/(tabs)/projects.tsx` | Replace `Stack.Screen` header with in-body `<Header>` + `<HeaderActions>` (matches existing tab pattern) |
| `app/(app)/[workspace]/more/issues.tsx` | `app/(app)/[workspace]/(tabs)/issues.tsx` | Add in-body `<Header>` (currently gets title from workspace `_layout.tsx` Stack) |
| `app/(app)/[workspace]/more/pins.tsx` | `app/(app)/[workspace]/(tabs)/pins.tsx` | Replace Stack header with in-body `<Header>` (same as projects + issues) |

### `(tabs)/_layout.tsx`

- Remove `inbox` and `my-issues` `Tabs.Screen` entries
- Add `projects`, `issues`, `pins` entries in the order above
- Move `inboxBadge` to the `more` tab's `tabBarBadge` prop (remove from inbox screen definition)
- `chatBadge` unchanged on `chat`

### `more-tab-dropdown.tsx`

`NAV_ITEMS` replaces the current `[Pinned, Issues, Projects]` with `[Inbox, My Issues]`:

```ts
const NAV_ITEMS: NavItem[] = [
  { label: "Inbox",     icon: "tray",      path: "/more/inbox" },
  { label: "My Issues", icon: "checklist", path: "/more/my-issues" },
];
```

Active-state highlight logic (`isActive`) is unchanged — it still checks `pathname.startsWith(target)`.

### `(app)/[workspace]/_layout.tsx`

The workspace Stack declares routes for all `more/*` pages. The two new pages (`more/inbox`, `more/my-issues`) need Stack.Screen entries with titles. The three moved-out pages (`more/projects`, `more/issues`, `more/pins`) have their Stack entries removed.

---

## Out of scope

- Collapsible sections on the workspace-wide Issues screen (`(tabs)/issues.tsx`)
- Badge count for Inbox inside the More dropdown item row (the tab-level badge is sufficient)
- Android-specific styling (iOS is primary target per `apps/mobile/CLAUDE.md`)
