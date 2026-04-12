## Context

The primary product client now lives in `apps/workspace`. That app already has a clear feature-based structure, but server-backed data is still managed through a mix of patterns:

- `useAuthStore` owns `user` and auth bootstrap state.
- `useWorkspaceStore` owns the current workspace plus remote collections such as workspaces, members, agents, and skills.
- `useIssueStore`, `useInboxStore`, and `useRuntimeStore` own remote list data.
- `useIssueTimeline`, `useIssueSubscribers`, and `useIssueReactions` each fetch data in local hook state and reimplement optimistic mutation and reconnect logic.
- Many components still call `api.*` directly for mutations and then manually patch local state or refetch.
- `useRealtimeSync` currently patches Zustand stores directly and calls store fetch methods on reconnect.

This means the app has no single source of truth for server state, no shared query key system, and no uniform lifecycle for invalidation, optimistic updates, or reconnect recovery.

At the same time, the repository has changed since the original plan document was written:

- `apps/workspace` is the main product surface.
- `apps/web` is the marketing/public site.
- There is no current `packages/core` or `apps/web/core/` structure to migrate toward.
- The active product changes depend on preserving the current `issue`, workspace, auth, and realtime contracts.

So this change should focus on the workspace app's caching architecture, not on premature monorepo extraction.

## Goals / Non-Goals

**Goals:**

- Make TanStack Query the source of truth for server-backed client data in `apps/workspace`.
- Separate server state from client-only UI state so Zustand stores stop acting as remote caches.
- Standardize query keys, mutation invalidation, optimistic updates, reconnect behavior, and workspace-switch handling.
- Migrate list-oriented domains and issue detail/task domains to query-backed hooks and mutation APIs.
- Preserve current REST, websocket, routing, and domain contracts while changing the internal client architecture.

**Non-Goals:**

- Migrating the marketing site in `apps/web`.
- Introducing `packages/core`, `packages/views`, or a new monorepo extraction layer.
- Redesigning backend APIs, issue semantics, or realtime message contracts.
- Removing Zustand entirely from the workspace app.
- Rewriting unrelated UI components that are not affected by the server-state boundary.

## Decisions

### 1. Scope TanStack Query to `apps/workspace` and keep the current transport layer

This change will add TanStack Query only to `apps/workspace`. The existing `ApiClient` in `apps/workspace/src/shared/api/` remains the transport layer and source of request semantics. Query hooks wrap that client; they do not replace it.

Why this approach:

- It aligns with the current repository architecture.
- It avoids mixing the migration with speculative monorepo work.
- It keeps the change focused on caching, invalidation, and client state boundaries.

Alternatives considered:

- Recreate the original `apps/web/core/` extraction plan now: rejected because the product app no longer lives there.
- Replace the API client abstraction entirely: rejected because transport is not the problem this change is solving.

### 2. Use feature-local query/mutation modules plus a shared query client

The workspace app will introduce a shared query client and key conventions under `apps/workspace/src/shared/query/`, while individual feature domains define their own `queries.ts`, `mutations.ts`, and related hooks.

Representative layout:

- `apps/workspace/src/shared/query/query-client.ts`
- `apps/workspace/src/shared/query/provider.tsx`
- `apps/workspace/src/features/auth/queries.ts`
- `apps/workspace/src/features/workspace/queries.ts`
- `apps/workspace/src/features/issues/{queries.ts,mutations.ts}`
- `apps/workspace/src/features/inbox/{queries.ts,mutations.ts}`
- `apps/workspace/src/features/runtimes/{queries.ts,mutations.ts}`

Why this approach:

- It matches the existing feature architecture instead of introducing a parallel `core/` tree.
- It keeps query logic close to the owning domain.
- It makes future extraction possible without treating it as part of this change.

### 3. Keep only client-only state in Zustand stores

Remote collections and server-derived records move to Query. Zustand remains responsible for UI/session state that is local to the client, such as:

- modal registry state
- view mode, filters, and persisted issue/workbench preferences
- issue draft state and selection state
- active issue ID or selected runtime ID
- persisted session helpers such as token presence or current workspace ID selection

Remote data that should move out of Zustand includes:

- current user profile (`getMe`)
- workspace list and current workspace-backed resources
- members, agents, skills
- issues, inbox items, runtimes
- issue timeline, subscribers, reactions
- active task, task messages, and task history data used in issue detail surfaces

Why this approach:

- It gives the app a clean server-state boundary.
- It keeps persisted UI preferences without forcing them into query cache.
- It avoids continuing the current pattern where stores act as both preference storage and remote cache.

### 4. Query keys must be workspace-aware and auth-aware

All workspace-scoped queries will include the active workspace ID in their keys. Session-scoped resources such as `me` remain separate from workspace-scoped resources.

Representative keys:

- `['session', 'me']`
- `['workspaces']`
- `['workspace', workspaceId, 'members']`
- `['workspace', workspaceId, 'agents']`
- `['issues', workspaceId, filters]`
- `['issue', issueId]`
- `['issue', issueId, 'timeline']`
- `['issue', issueId, 'subscribers']`
- `['issue', issueId, 'reactions']`
- `['issue', issueId, 'active-task']`

On workspace switch, the app will update the API client's workspace ID, clear or remove workspace-scoped cache entries for the old workspace, and then let the new workspace queries refetch. On logout, the app clears session and workspace-scoped queries together.

Why this approach:

- It prevents cross-workspace cache bleed.
- It preserves the repo's multi-tenant model.
- It makes reconnect and invalidation behavior predictable.

### 5. Realtime updates target the query cache, not domain stores

`useRealtimeSync` will move from store patching to query cache maintenance. When an incoming payload fully describes the affected record, the app updates cached query data directly. When the payload is partial or ambiguous, it invalidates the relevant query keys.

Examples:

- `issue:created`, `issue:updated`, `issue:deleted` update or invalidate issue list/detail queries.
- `inbox:new` updates inbox queries.
- workspace, member, agent, or skill events invalidate the corresponding workspace-scoped queries when the payload is not sufficient for a safe direct patch.
- reconnect recovery refetches the active workspace's relevant queries instead of calling legacy store `fetch()` methods.

Why this approach:

- It centralizes cache coherence in one layer.
- It reduces coupling between websocket handlers and store shape.
- It matches TanStack Query's strengths without changing the server event protocol.

### 6. Migrate issue detail hooks and direct mutation call sites into query-backed domain APIs

The migration is not complete if only list stores move. The issue detail surface currently owns a large amount of bespoke server state through hook-local `useState` and direct `api.*` calls. This change will move those detail domains behind query-backed hooks and domain mutation APIs.

Affected areas include:

- timeline loading and comment/reaction mutations
- subscriber loading and subscribe/unsubscribe mutations
- issue reactions
- active task lookup, task message loading, and task history loading
- direct issue, inbox, workspace, settings, skills, and agent mutations currently triggered straight from components

Why this approach:

- It avoids ending up with two competing server-state patterns after the migration.
- It brings optimistic updates and invalidation into a consistent domain layer.
- It makes later product work less likely to reintroduce manual fetch logic.

## Risks / Trade-offs

- [The migration touches many call sites and selectors] -> Mitigation: stage the work by domain, define shared query keys early, and migrate one remote domain at a time.
- [Workspace switching could expose stale data from the old workspace] -> Mitigation: include workspace ID in query keys and remove workspace-scoped cache entries during switch.
- [Some websocket payloads are too partial for safe direct cache updates] -> Mitigation: invalidate those queries instead of forcing brittle patch logic.
- [Current-user data is used widely through `useAuthStore`] -> Mitigation: introduce a dedicated current-user query/hook early in the migration and update callers in a focused pass.
- [Tests may rely on current store-driven timing] -> Mitigation: update workspace tests to assert UI outcomes rather than implementation-specific store behavior.

## Migration Plan

1. Add TanStack Query infrastructure to `apps/workspace` and wire a root provider.
2. Move auth/bootstrap and workspace switching to query-backed session/workspace flows.
3. Migrate shared list domains: issues, inbox, runtimes, members, agents, and skills.
4. Migrate issue detail/task domains and direct component mutation call sites.
5. Rework realtime sync and reconnect recovery around query cache behavior.
6. Verify route, auth, workspace, issue, and realtime behavior through targeted tests.

## Open Questions

None for this change.