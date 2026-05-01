## Context

Issues in Multica support comments, reactions, subscribers, and scheduling dates, but there is no mechanism to record how long was spent on a piece of work. Worklog entries are a lightweight model: each entry carries an author (member or agent), a duration in minutes, an optional description, and a `logged_at` timestamp that represents when the work happened (which may differ from when the entry was created).

The backend uses the same polymorphic actor pattern (`author_type` + `author_id`) that already exists on comments, so agents can log time for automated work the same way members do for manual work. The API follows the same REST + WebSocket pattern used for comments and reactions: REST for mutations, WebSocket events for real-time propagation.

The frontend adds a worklog panel inside the existing issue detail layout, reusing the same actor avatar, time-formatting, and mutation hook patterns already present in the issue detail and comment features.

A key design driver is **future reusability**: a Pomodoro focus timer feature (planned) will need to produce time-log records in the same canonical model. Rather than coupling worklogs to issues via a direct foreign key on the `worklog` row, this change uses a `worklog` table as the authoritative time-log entity and a `worklog_issue` join table that associates worklogs with issues. This allows Pomodoro sessions and other future sources to write into `worklog` without schema changes, and enables a worklog entry to eventually be associated with more than one issue or other entity types.

Constraints:

- All queries must filter by `workspace_id` for multi-tenancy correctness.
- `duration_minutes` must be a positive integer; the API must reject zero or negative values.
- Entries can only be deleted by their author or a workspace owner/admin.
- The same `issue_access` path used by comments must gate access to worklog endpoints.
- Do not hand-edit generated sqlc files; run `make sqlc` after modifying queries.

## Goals / Non-Goals

**Goals:**

- Persist worklog entries (duration, description, logged_at, actor, type) in a standalone `worklog` table.
- Link worklogs to issues via a `worklog_issue` join table, keeping the canonical worklog record independent of the issue domain.
- Expose CRUD endpoints at `POST /issues/:id/worklogs`, `GET /issues/:id/worklogs`, `PATCH /worklogs/:id`, `DELETE /worklogs/:id`.
- Broadcast `worklog:created`, `worklog:updated`, `worklog:deleted` WebSocket events so all connected clients stay in sync.
- Show a worklog section in the issue detail view with total logged time, individual entries, a log-time form, and delete actions.
- Let AI agents create worklog entries on issues they are assigned to.

**Non-Goals:**

- Pomodoro timer UI or session state in this change — but the schema must not block it.
- Automatic worklog creation from agent task duration.
- Worklog-based reporting, burn-down charts, or aggregated analytics across issues.
- Billing or invoicing integration.
- Editing the `logged_at` field after an entry is created.
- Many-to-many worklog-to-issue links in the API surface for this change (one worklog → one issue is the only exposed path now; the schema supports more).

## Decisions

### 1. Two-table model: `worklog` + `worklog_issue` join table

Rather than a single `worklog_issue` table with `issue_id` baked in, the schema splits into two tables.

**`worklog`** — canonical time-log record:

| Column | Type | Notes |
|---|---|---|
| `id` | `UUID` | PK, `gen_random_uuid()` |
| `workspace_id` | `UUID` | FK → `workspace`, multi-tenancy filter |
| `author_type` | `TEXT` | `'member'` or `'agent'` |
| `author_id` | `UUID` | Polymorphic author |
| `duration_minutes` | `INT` | Positive integer, required |
| `description` | `TEXT` | Nullable |
| `type` | `TEXT` | `'manual'` (default) — `'pomodoro'` reserved for the timer feature |
| `logged_at` | `TIMESTAMPTZ` | Defaults to `now()` at insert time |
| `created_at` | `TIMESTAMPTZ` | `now()` |
| `updated_at` | `TIMESTAMPTZ` | `now()` |

**`worklog_issue`** — join table linking a worklog to an issue:

| Column | Type | Notes |
|---|---|---|
| `id` | `UUID` | PK |
| `worklog_id` | `UUID` | FK → `worklog` ON DELETE CASCADE |
| `issue_id` | `UUID` | FK → `issue` ON DELETE CASCADE |
| `workspace_id` | `UUID` | FK → `workspace`, redundant but avoids joins for multi-tenancy filters |
| `created_at` | `TIMESTAMPTZ` | `now()` |

A `UNIQUE(worklog_id, issue_id)` constraint prevents duplicate links.

Why this approach:

- Decouples the canonical time record from the issue domain. When the Pomodoro feature arrives, sessions can write `worklog` rows of `type = 'pomodoro'` and link them to issues through the same `worklog_issue` table — no schema migration needed.
- Keeps the `worklog` row clean for potential future links to projects, sprints, or personal time without a `project_worklog`, `sprint_worklog` proliferation — or a messy polymorphic nullable column pattern.
- `workspace_id` on both tables ensures every query stays within the workspace boundary without extra joins.

Alternatives considered:

- Single `worklog_issue` table with `issue_id` directly: rejected because it hard-codes the issue binding on the canonical record, forcing a schema change to reuse worklogs for Pomodoro or other sources.
- Polymorphic `entity_type` + `entity_id` on `worklog`: rejected because it breaks referential integrity and complicates sqlc query generation.
- Store worklogs as a JSONB array on the issue: rejected because entries cannot be queried, paginated, or audited cleanly.

### 2. `duration_minutes` as the canonical time unit

Minutes are the smallest practical unit for manual time logging and match the Jira convention. The API accepts and returns `duration_minutes` as an integer. The frontend converts between human-readable strings (e.g. "1h 30m") and the integer representation purely in UI code; the canonical backend value is always minutes.

Why this approach:

- A single integer column is trivially summed for totals and does not require parsing.
- Minutes are granular enough for most teams without introducing fractional hours or complex duration types.

Alternatives considered:

- Use `duration_seconds`: unnecessarily precise for manual log entries.
- Accept a human-readable string like `"1h30m"` on the API: requires parser and validation complexity on the server with no added value.

### 3. REST + WebSocket pattern matching comments

Four REST endpoints:

- `POST /issues/:id/worklogs` — create entry
- `GET /issues/:id/worklogs` — list all entries for the issue (no pagination for now; issues rarely have more than a few dozen log entries)
- `PATCH /worklogs/:id` — update `duration_minutes` and/or `description`
- `DELETE /worklogs/:id` — delete entry (author or workspace owner/admin only)

Each mutation broadcasts a WebSocket event (`worklog:created`, `worklog:updated`, `worklog:deleted`) using the existing Hub broadcast pattern.

Why this approach:

- Consistent with comments, reactions, and subscribers, minimizing learning overhead.
- Separate create/list vs. individual update/delete routes match the existing route grouping in `cmd/server/router.go`.

Alternatives considered:

- A single generic PATCH that upserts: rejected because separate create and update semantics are clearer and easier to test.
- No real-time events for worklogs: rejected because the issue detail view shows a live timeline and users expect changes to propagate.

### 4. Frontend: worklog section inside the issue detail timeline panel

The issue detail view already has a timeline/comment panel on the right. The worklog section will be added as a collapsible block in that panel, below the comment list. It contains:

- Total logged time (e.g., "3h 45m") as a summary badge.
- A list of individual entries with actor avatar, duration, description, and a delete icon (shown only to the author and workspace admins).
- A "Log time" form (duration input as a text field accepting "Xh Ym" format, optional description, and a Submit button).

The duration text field parses input on blur and converts it to `duration_minutes` before calling the mutation. Parsing is handled by a small utility function colocated with the worklog components.

Why this approach:

- Keeps all time-related data visible in one place in the issue detail.
- Reuses existing actor avatar, mutation hook, and toast notification patterns.
- Avoids a dedicated route or modal, keeping the interaction lightweight.

Alternatives considered:

- Dedicated `/issues/:id/worklog` sub-route: unnecessary complexity for what is typically a secondary view on the issue.
- Inline editing of duration on each entry: deferred to a future iteration since the primary use case is adding new entries, not editing old ones.

## Risks / Trade-offs

- [Large worklog lists could slow the issue detail] → Mitigation: display the 50 most recent entries; add pagination if needed in a follow-up.
- [Agent worklog creation requires no user action] → Mitigation: entries show actor type visually (robot icon for agents), consistent with assignee rendering.
- [Duration parsing edge cases] → Mitigation: unit-test the parser for common formats and reject invalid input with a clear validation message.
- [Extra join for issue worklog queries] → The `worklog_issue` join table adds one join to every list query, but the workloads are small (typically < 100 entries per issue) and the architectural flexibility outweighs the negligible query cost.
- [Pomodoro type added speculatively] → The `type` column is additive; the default `'manual'` covers all current use cases and the column is ignored by UI until the Pomodoro feature lands.

## Future: Pomodoro Integration

When the Pomodoro focus-timer feature is introduced, it will:

1. Track a `pomodoro_session` (start_time, end_time, status, linked issue_id) separately.
2. On session completion, create a `worklog` row with `type = 'pomodoro'` and `duration_minutes = 25` (or the actual elapsed time).
3. Insert a matching `worklog_issue` row to associate it with the active issue.
4. Pomodoro-created entries render identically in the worklog section with a tomato icon as the source indicator.

No schema changes to `worklog` or `worklog_issue` are needed for this path.

## Migration Plan

1. Add migration `035_issue_worklog` creating the `worklog` table and the `worklog_issue` join table.
2. Write SQL queries: `CreateWorklog` + `CreateWorklogIssue` (paired insert), `ListWorklogsByIssue`, `GetWorklogByID`, `UpdateWorklog`, `DeleteWorklog` (cascade removes `worklog_issue` row).
3. Run `make sqlc` to regenerate Go models.
4. Add handler, WS events, and routes to the Go backend.
5. Add frontend types, API methods, mutations, and issue detail UI.
6. Add backend and frontend tests.
