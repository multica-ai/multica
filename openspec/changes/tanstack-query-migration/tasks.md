## 1. Query infrastructure

- [x] 1.1 Add `@tanstack/react-query` to `apps/workspace` and create the shared query client/provider plus query key conventions under `apps/workspace/src/shared/query`.
- [x] 1.2 Wrap the workspace app root with `QueryClientProvider` so feature code can use queries during auth/bootstrap and route rendering.
- [x] 1.3 Define cache clearing rules for login, logout, reconnect recovery, and workspace switching.

## 2. Auth and workspace bootstrap

- [x] 2.1 Move current-user and workspace-list loading from store-owned bootstrap logic to query-backed session/workspace flows.
- [x] 2.2 Split persisted workspace selection or session helpers from remote workspace/member/agent/skill collections so those collections no longer live in long-lived Zustand stores.
- [x] 2.3 Update auth initialization and workspace switching so they prime or refetch queries instead of hydrating issue, inbox, and runtime data through store fetch calls.

## 3. Shared workspace domains

- [x] 3.1 Migrate issues, inbox, and runtimes list state from Zustand stores to query-backed reads plus mutation hooks.
- [x] 3.2 Migrate members, agents, and skills to query-backed reads plus domain mutation hooks.
- [x] 3.3 Replace direct component-level `api.*` mutations in workspace features with query-aware mutation hooks that patch or invalidate the correct caches.

## 4. Issue detail and task domains

- [x] 4.1 Convert issue timeline, issue reactions, and subscribers from hook-local fetch state to query-backed hooks with optimistic mutation behavior.
- [x] 4.2 Convert active task, task messages, and task history reads in issue detail surfaces to query-backed hooks.
- [x] 4.3 Keep board, backlog, today, upcoming, notifications, my-work, and issue detail surfaces compatible with the current issue and task contracts during migration.

## 5. Realtime cache coherence

- [x] 5.1 Replace websocket-to-store synchronization with query cache updates or invalidation keyed by session, workspace, and issue scope.
- [x] 5.2 Rework reconnect recovery so it refetches the affected queries instead of calling legacy store `fetch()` methods.
- [x] 5.3 Preserve the current REST, realtime, auth, and routing contracts while changing only the client-side server-state layer.

## 6. Verification

- [x] 6.1 Add or update `apps/workspace` tests for auth/bootstrap, workspace switching, and query-backed list/detail rendering.
- [x] 6.2 Add or update workspace coverage for optimistic mutations and realtime-driven cache refresh behavior.
- [ ] 6.3 Run the relevant workspace, backend, and end-to-end checks before archiving the change.