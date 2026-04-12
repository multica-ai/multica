## Why

The workspace app currently uses a mix of Zustand stores, component-local `useState` hooks, and ad hoc refetch logic as both UI state and server cache. That makes the client harder to reason about in a few concrete ways:

- Shared remote data such as the current user, workspaces, members, agents, skills, issues, inbox items, and runtimes lives inside long-lived Zustand stores.
- Issue detail resources such as timeline entries, subscribers, reactions, and task state are fetched separately in custom hooks with their own reconnect recovery and optimistic update logic.
- Many mutations still call `api.*` directly from components and then manually patch state or trigger follow-up refetches.
- Realtime sync is coupled directly to store shape, so cache behavior is scattered across stores, hooks, and websocket handlers.

The new project-management views in `apps/workspace` make these problems more visible because board, backlog, today, upcoming, notifications, and issue detail all depend on coherent list/detail cache behavior.

This work does not conflict with `gradual-project-management-transition`, `issue-start-end-dates`, or `upload-attachment-fixes`. It keeps the existing `issue`, auth, workspace, and realtime contracts intact while changing only the client-side server-state layer in `apps/workspace`.

## What Changes

- Add TanStack Query to `apps/workspace` and introduce a shared workspace query client/provider.
- Migrate server-backed client data from long-lived Zustand stores and bespoke fetch hooks to query and mutation modules aligned with the current feature structure.
- Keep Zustand only for client-only state such as modal visibility, navigation/view preferences, drafts, selection state, active IDs, and other persisted UI state.
- Replace websocket-to-store synchronization with websocket-driven query cache updates or invalidation, plus query-based reconnect recovery.
- Keep `apps/web`, monorepo package extraction, and any future `packages/core` work out of scope for this change.

## Capabilities

### New Capabilities

- `workspace-server-state-query`: Use TanStack Query as the source of truth for server-backed state in `apps/workspace` while keeping client-only UI state separate.

## Impact

- `apps/workspace` dependencies and root providers.
- `apps/workspace/src/shared/api/` remains the transport layer, but no longer acts as the implicit cache boundary.
- Auth/bootstrap flows, workspace switching, and reconnect recovery.
- Query and mutation behavior for issues, inbox, workspace resources, runtimes, settings, agents, skills, and issue detail/task data.
- Realtime synchronization currently implemented in the workspace app.