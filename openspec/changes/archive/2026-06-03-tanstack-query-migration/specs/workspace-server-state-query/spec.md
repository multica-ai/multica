## ADDED Requirements

### Requirement: The workspace app uses TanStack Query as the source of truth for server-backed data
The workspace app SHALL load shared server-backed data through TanStack Query rather than long-lived Zustand stores or hook-local fetch state.

#### Scenario: Booting the app with a cached session
- **WHEN** a user opens `apps/workspace` with a cached auth token
- **THEN** the app fetches the current user and workspace list through query-backed bootstrap logic
- **AND** the active workspace is selected without rehydrating remote collections into permanent Zustand data stores

#### Scenario: Opening list-oriented product surfaces
- **WHEN** a user opens board, backlog, today, upcoming, notifications, my-work, or inbox in the workspace app
- **THEN** those surfaces read their server-backed collections from query-backed caches scoped to the active workspace
- **AND** refresh behavior comes from query invalidation or refetching rather than store `fetch()` methods

### Requirement: Issue detail resources use query-backed hooks and mutations
The workspace app SHALL manage issue detail resources such as timeline entries, issue reactions, subscribers, and task state through query-backed hooks and domain mutations.

#### Scenario: Opening an issue detail page
- **WHEN** a user opens an issue detail page
- **THEN** timeline, subscriber, reaction, and task-related data are loaded through query-backed hooks for that issue
- **AND** the app does not rely on standalone hook-local fetch state as the source of truth for those resources

#### Scenario: Mutating issue detail resources
- **WHEN** a user creates or edits a comment, toggles a reaction, changes subscribers, or acts on task-related controls from issue detail
- **THEN** the app uses domain mutation hooks that update or invalidate the relevant issue-scoped query cache entries
- **AND** optimistic UI behavior remains consistent with the current issue and task contracts

### Requirement: Client-only UI state stays separate from query-managed server state
The workspace app SHALL keep client-only UI state outside the query cache and SHALL not use TanStack Query as a replacement for local view, draft, or modal state.

#### Scenario: Changing local workspace UI preferences
- **WHEN** a user changes a board/list preference, filter, draft value, selection state, modal state, or similar client-only UI state
- **THEN** that state remains managed by Zustand or component-local state
- **AND** the change does not create a synthetic server-state query just to hold UI-only data

### Requirement: Auth and workspace lifecycle isolate query cache correctly
The workspace app SHALL isolate session-scoped and workspace-scoped query data across login, logout, and workspace switching.

#### Scenario: Switching workspaces
- **WHEN** a user switches from one workspace to another
- **THEN** workspace-scoped query cache entries for the old workspace are cleared or replaced before the new workspace data is shown
- **AND** subsequent list and detail reads use the newly selected workspace context

#### Scenario: Logging out
- **WHEN** a user logs out of the workspace app
- **THEN** session-scoped and workspace-scoped query data are cleared along with the stored auth/session context
- **AND** the next session starts from a clean query cache state

### Requirement: Realtime and reconnect keep query caches coherent
The workspace app SHALL keep query-managed server state coherent when websocket events arrive or when the websocket reconnects.

#### Scenario: Receiving a realtime issue update
- **WHEN** the workspace app receives an issue-related websocket event for the active workspace
- **THEN** the relevant query cache entries are updated directly when the payload is sufficient
- **AND** the relevant query cache entries are invalidated when a direct patch would be unsafe or incomplete

#### Scenario: Reconnecting after websocket interruption
- **WHEN** the workspace websocket reconnects after a disconnect
- **THEN** the app refetches the relevant active queries for the current workspace and issue scopes
- **AND** reconnect recovery does not depend on legacy store refresh methods