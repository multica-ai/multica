## Why

Issues currently have no way for team members or agents to record how much time was spent working on them. Without worklog support, teams cannot track effort, estimate future work more accurately, or surface which issues are consuming disproportionate time. Adding a worklog model gives every issue a persistent, ordered history of time entries that both humans and AI agents can create and review.

## What Changes

- Add a `worklog` table as the canonical time-log record, and a `worklog_issue` join table to associate worklogs with issues. This two-table model allows worklog entries to be reused by future features (e.g., Pomodoro focus timer) without schema changes.
- Expose REST endpoints to create, list, update, and delete worklog entries under each issue (`/issues/:id/worklogs`).
- Surface a worklog section in the issue detail view in the workspace frontend: log-time form, entry list showing total logged time, and delete capability.
- Allow AI agents to create worklog entries on issues they work on, so time spent in automated tasks is visible alongside human time logs.
- Broadcast real-time events when worklog entries are created, updated, or deleted so all connected clients stay in sync.

## Capabilities

### New Capabilities

- `issue-worklog`: Create, read, update, and delete time log entries on issues, visible to all workspace members with access to the issue.

### Modified Capabilities

- `issue-detail`: The issue detail view gains a worklog section showing total logged time and individual entries.

## Impact

- New database tables and migration: `worklog` (canonical) + `worklog_issue` (join).
- New SQL queries and sqlc-generated Go code.
- New REST endpoints and handler in the Go backend.
- New WS event types for worklog create/update/delete.
- New frontend types, API calls, mutations, and UI components in `apps/workspace`.
- Tests covering worklog CRUD flows for both backend and frontend.
