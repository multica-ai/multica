# Issue Hierarchy Views Plan

## Goal

Make parent/child issue relationships visible in the main issue workspace without changing the backend schema:

- `tree` list view: child issues render under their visible parent where possible, and otherwise keep a parent badge.
- `swimlane` board view: parent issues become lanes, while child issues remain organized by Kanban status columns.

## Constraints

- Keep React Query as the owner of server state.
- Keep Zustand limited to persisted view preferences.
- Reuse existing `parent_issue_id`, issue list caches, and child-progress query.
- Preserve the existing flat `list` and grouped `board` views.

## Implementation Steps

1. Add a pure hierarchy utility for tree rows and swimlane lanes.
2. Add unit tests for same-status nesting, cross-status parent badges, and swimlane lane buckets.
3. Update `ListView` so it can render either flat rows or hierarchy rows.
4. Add `TreeView` as a thin wrapper around hierarchy-aware `ListView`.
5. Add `SwimlaneBoardView` for parent lanes with status columns and DnD status/parent moves.
6. Extend issue view mode state and display controls with `tree` and `swimlane`.
7. Add focused view tests and run typecheck.

## Acceptance

- A child issue is no longer visually indistinguishable from a top-level issue in tree mode.
- A child whose parent is outside the current status section still shows the parent identifier.
- Swimlane mode shows each parent as a row and distributes children into their current status columns.
- Standalone top-level issues remain visible in an unparented lane.
