# Mobile "More" Tab: Dropdown → Page — Design

Date: 2026-07-08
Status: Approved

## Context

`apps/mobile`'s bottom tab bar has four tabs: Inbox, My Issues, Chat, More.
The first three are real navigable screens. More is not — tapping it
intercepts `tabPress` in `app/(app)/[workspace]/(tabs)/_layout.tsx` and
imperatively opens a `DropdownMenu` popover
(`components/nav/more-tab-dropdown.tsx`) anchored above the tab bar,
instead of navigating anywhere. The popover contains:

- A user-identity row (avatar + name/email) → pushes `/[workspace]/more/settings`
- A current-workspace row → pushes `/[workspace]/switch-workspace`
  (disabled, no chevron, when the user only belongs to one workspace)
- Three nav rows: Pinned, Issues, Projects → push their existing
  `more/pins`, `more/issues`, `more/projects` routes

`app/(app)/[workspace]/(tabs)/more.tsx` currently exists only because
Expo Router requires a route file behind every `Tabs.Screen`; it's a
stub that redirects to Inbox if ever reached directly (it never is, in
normal use, since the tab press is always intercepted).

Desktop/web's sidebar (`packages/views/layout/app-sidebar.tsx`) exposes
substantially more top-level sections than mobile does today (Autopilots,
Agents, Squads, Usage, Runtimes, Skills, plus a much larger Settings tab
set: workspace admin, members, GitHub/Slack/Lark/Composio integrations,
API tokens, Labs). None of that is in scope here — see Non-goals.

## Goal

Replace the More tab's dropdown-popover interaction with a real,
navigable page, carrying over the exact same content and destinations
the dropdown has today. No new feature entry points.

## Non-goals

- Adding any desktop-only feature (Autopilots, Squads, Usage, Runtimes,
  Skills, Members, Integrations, Tokens, Labs, workspace admin settings)
  to the More page. That's deliberately deferred to a separate,
  later-prioritized effort per feature.
- Any visual redesign beyond adopting the existing `SectionGroup`/
  `NavRow` list pattern already used by `more/settings.tsx`.

## Changes

### 1. `app/(app)/[workspace]/(tabs)/more.tsx` becomes the real page

Replaces the current redirect-stub. Renders:

- A section with the user-identity row and the workspace row, same
  destinations and same single-workspace-disables-the-row behavior as
  today's `UserCard`/`WorkspaceCard` in the dropdown.
- A second section (title: `workspace.more_page.section_title`,
  mirroring the renamed key below) with three `NavRow`s: Pinned, Issues,
  Projects, pushing the same three existing routes as today.

Header title is `common.tabs.more` (already exists — reused, not new).
As a top-level tab destination it gets no back button, consistent with
Inbox/My Issues/Chat.

### 2. Remove the interception and the dropdown

In `app/(app)/[workspace]/(tabs)/_layout.tsx`:
- Delete the More `Tabs.Screen`'s `listeners` prop (the `tabPress` /
  `e.preventDefault()` / `moreTriggerRef.current?.open()` block) — the
  tab becomes a plain navigable screen like its siblings.
- Delete the `moreTriggerRef` declaration and the `<MoreTabDropdownAnchor
  triggerRef={moreTriggerRef} />` mount.

Delete `components/nav/more-tab-dropdown.tsx` entirely (`UserCard`,
`WorkspaceCard`, `MoreTabDropdownAnchor`, the `useCurrentWorkspace`
helper) — nothing else references it.

### 3. Extract `SectionGroup`/`NavRow` to a shared location

Both components are currently private to `more/settings.tsx`. The More
page becomes a second call site with an identical need, so they move to
`components/ui/section-group.tsx` (two named exports,
`SectionGroup`/`NavRow`, signatures unchanged) and `settings.tsx` imports
them from there instead of defining them locally. This is a reuse of an
already-proven pattern, not a new primitive — the mobile CLAUDE.md's
"three callers" threshold for new primitives doesn't gate moving an
existing one to its second real caller.

### 4. Locale keys

`apps/mobile/locales/{en,zh-Hans}/workspace.json`'s `more_dropdown.*`
section is renamed to `more_page.*` (same leaf keys, values unchanged —
`account_settings_a11y`, `workspace_fallback`, `switch_workspace_a11y`,
`nav.pinned`, `nav.issues`, `nav.projects`), plus one new key,
`more_page.section_title`, for the second section's header ("Workspace"
/ "工作区" — mirrors the existing `workspaces.title` wording precedent in
`settings.json`). Renamed additively-safe since both locale files change
together; the parity test catches any mismatch.

## Testing

No new test files — none of the touched files (`more.tsx`,
`_layout.tsx`, the deleted dropdown, `settings.tsx`) have existing
coverage, consistent with how prior mobile UI-only changes in this
project have been verified (typecheck + lint + manual pass, not new
unit tests for screen components).

## Verification

1. `pnpm --filter @multica/mobile typecheck`
2. `pnpm --filter @multica/mobile lint`
3. `pnpm --filter @multica/mobile test` (locale parity)
4. Manual: tap the More tab — lands on the new page (no popover).
   Tap the user row → Settings. Tap the workspace row → switch-workspace
   sheet (or confirm it's disabled/no-chevron on a single-workspace
   account). Tap Pinned/Issues/Projects → each existing screen. Confirm
   Settings still renders correctly after the `SectionGroup`/`NavRow`
   extraction (no visual regression). Confirm both languages render
   correctly on the new page.
