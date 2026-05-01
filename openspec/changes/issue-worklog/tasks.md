## 1. Database and backend contract

- [x] 1.1 Add migration `035_issue_worklog` that creates two tables: (a) `worklog` — the canonical time-log record with columns `id`, `workspace_id`, `author_type`, `author_id`, `duration_minutes` (positive integer CHECK), `description` (nullable text), `type` (TEXT default `'manual'`), `logged_at` (TIMESTAMPTZ, default `now()`), `created_at`, `updated_at`; (b) `worklog_issue` — join table with `id`, `worklog_id` (FK → `worklog` ON DELETE CASCADE), `issue_id` (FK → `issue` ON DELETE CASCADE), `workspace_id`, `created_at`, and a `UNIQUE(worklog_id, issue_id)` constraint.
- [x] 1.2 Write SQL queries in `server/pkg/db/queries/`: `CreateWorklog` (inserts into `worklog`), `CreateWorklogIssue` (inserts into `worklog_issue`), `ListWorklogsByIssue` (JOIN query filtered by `issue_id` + `workspace_id`), `GetWorklogByID`, `UpdateWorklog` (duration + description), `DeleteWorklog` (deletes the `worklog` row; `worklog_issue` is cleaned up by cascade). Run `make sqlc` to regenerate Go models.
- [x] 1.3 Add a `WorklogResponse` struct and a `worklog.go` handler with `CreateWorklog`, `ListWorklogs`, `UpdateWorklog`, and `DeleteWorklog` methods that validate `duration_minutes > 0`, perform the paired `worklog` + `worklog_issue` insert for create, enforce authorship for delete/update, and filter all queries by `workspace_id`.

## 2. Backend routing, events, and tests

- [x] 2.1 Register routes in `cmd/server/router.go`: `POST /issues/{id}/worklogs`, `GET /issues/{id}/worklogs` (protected), `PATCH /worklogs/{id}`, `DELETE /worklogs/{id}` (protected).
- [x] 2.2 Add `worklog:created`, `worklog:updated`, `worklog:deleted` event types to `server/pkg/protocol/events.go` (or the equivalent event constants file) and broadcast them from the handler mutations.
- [x] 2.3 Add Go tests in `server/internal/handler/` covering: create with valid and invalid duration, list returns entries for the correct issue/workspace, update changes duration or description, delete enforces author ownership, and non-author members cannot delete.

## 3. Frontend types, API, and mutations

- [ ] 3.1 Add `Worklog` interface and `WorklogCreatedPayload`, `WorklogUpdatedPayload`, `WorklogDeletedPayload` event payload types to `apps/workspace/src/shared/types/` and register the new event strings in `WSEventType`.
- [ ] 3.2 Add `listWorklogs`, `createWorklog`, `updateWorklog`, and `deleteWorklog` methods to the API client in `apps/workspace/src/shared/api/`.
- [ ] 3.3 Add `useWorklogMutations` hook (or extend issue mutations) in `apps/workspace/src/features/issues/` with create, update, and delete mutations that refresh the worklog list on success and show toast notifications on error.

## 4. Frontend UI: issue detail worklog section

- [ ] 4.1 Create a `parseDuration(input: string): number | null` utility in `apps/workspace/src/features/issues/utils/` that converts strings like `"1h 30m"`, `"90m"`, `"1h"`, `"2"` (bare minutes) to `duration_minutes` integers, and a complementary `formatDuration(minutes: number): string` for display.
- [ ] 4.2 Create a `WorklogSection` component in `apps/workspace/src/features/issues/components/` that renders: total logged time summary, a list of worklog entries with actor avatar, formatted duration, description, logged-at date, and a delete button (shown to the author and workspace admins), and a "Log time" form with a duration text input and optional description field.
- [ ] 4.3 Mount `WorklogSection` in the issue detail panel, below the comment input, and wire it to `useWSEvent` so real-time `worklog:created`, `worklog:updated`, and `worklog:deleted` events update the displayed list.

## 5. Frontend tests and end-to-end verification

- [ ] 5.1 Add Vitest unit tests for `parseDuration` and `formatDuration` covering valid inputs, edge cases (zero, negative, malformed strings), and round-trip accuracy.
- [ ] 5.2 Add a Vitest component test for `WorklogSection` covering: empty state renders log-time form, entries list shows correct total, delete button calls the mutation, and submitting the form with a valid duration calls `createWorklog`.
- [ ] 5.3 Run `make check` (typecheck + unit tests + Go tests + E2E) and fix any failures before marking the change complete.
