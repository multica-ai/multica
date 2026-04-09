## Why

Issues currently support a single `due_date`, which is not enough for teams that want to record when work is planned to start and when it actually ends. Adding `start_date` and `end_date` gives issues a simple time window that can be used for planning, tracking, and later automation without overloading the existing due date field.

## What Changes

- Add `start_date` and `end_date` as optional issue fields alongside `due_date`.
- Allow clients to create, update, clear, and read both fields through the issue API.
- Show and edit the new fields in issue creation and issue detail flows in both frontend apps.
- Persist the new fields in the database and expose them through generated models, realtime payloads, and related issue views.
- Keep the change scoped to date-window support for issues, without introducing duration calculations or reporting in this change.

## Capabilities

### New Capabilities
- `issue-schedule-dates`: Manage optional start and end dates on issues across storage, API, and user-facing issue workflows.

### Modified Capabilities

## Impact

- Database schema and SQL queries for issues.
- Backend issue request/response types, update events, and generated sqlc artifacts.
- Frontend issue types, create/edit forms, detail views, and list/card presentation in `apps/web` and `apps/workspace`.
- Tests covering issue CRUD flows and date field behavior.