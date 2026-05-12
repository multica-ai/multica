---
date: 2026-05-12
topic: issue-identifier-badge
---

# Issue Identifier Badge on Detail Page

## Summary

Render the issue identifier (`MUL-123`) as a visually distinct badge in the PageHeader breadcrumb on the issue detail page, with click-to-copy behavior and toast confirmation.

## Problem Frame

On the issue detail page (`packages/views/issues/components/issue-detail.tsx`), the issue identifier is currently displayed as muted text (`text-muted-foreground`) inside the PageHeader breadcrumb trail. Users cannot quickly identify which issue they are viewing because the identifier blends into the navigation context. There is also no quick way to copy the identifier for pasting into Slack, commit messages, or branch names.

## Requirements

**Visual prominence**
- Replace the plain-text identifier in the PageHeader breadcrumb with a distinct badge/pill element
- Badge uses a subtle background (`bg-muted` or `bg-accent`) to separate it from navigation text
- Badge includes the status icon preceding the identifier (consistent with `IssueChip` pattern)
- Badge styling is readable but does not compete with the issue title for primary attention

**Copy interaction**
- Clicking the badge copies the identifier (`MUL-123`) to the clipboard
- Copy action shows a toast confirmation (e.g., "Identifier copied")
- Copy failure shows an error toast
- The `useIssueActions` hook exposes a `copyIdentifier` handler alongside existing `copyLink`

**i18n**
- Toast messages use the existing translation system (`useT("issues")`)

## Success Criteria

- A user can identify the current issue number within 1 second of opening the detail page
- A user can copy the issue identifier in a single click without opening dropdown menus
- The change does not increase header height or break existing layout on mobile/desktop

## Scope Boundaries

- Does not add keyboard shortcuts (separate improvement)
- Does not change the document/tab title
- Does not modify list view, board view, or other issue surfaces
- Does not extract a shared component (can be done as follow-up refactoring)
- Does not change the TitleEditor or fuse identifier into the title

## Key Decisions

- **Badge placement**: Keep in the current breadcrumb position, styled as a badge. This minimizes layout disruption while making the identifier immediately scannable.
- **Click-to-copy on the badge itself**: More discoverable than a hover-reveal button or menu item. The identifier is the natural copy target.
- **Status icon included**: Consistent with `IssueChip` and reinforces the issue's current state at a glance.
