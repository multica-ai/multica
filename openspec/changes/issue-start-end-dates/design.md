## Context

Issues currently support a single optional `due_date`, which is stored in PostgreSQL, exposed through the REST API, propagated through sqlc-generated models, and edited in both frontend apps. The issue update flow already distinguishes omitted fields from explicit `null` values, which is important for date fields because clearing a value must be different from leaving it unchanged.

This change adds a second pair of scheduling fields to the same issue model: `start_date` and `end_date`. The implementation is cross-cutting because it touches the database schema, SQL queries, generated Go code, handler validation, realtime update payloads, activity/notification listeners, and mirrored issue flows in `apps/web` and `apps/workspace`.

Constraints that shape the design:

- The repo already uses `TIMESTAMPTZ` plus RFC3339 strings for `due_date`, so introducing a different storage contract for `start_date` and `end_date` would create inconsistent issue date behavior.
- The two frontend apps keep separate but parallel issue implementations, so the change must be applied symmetrically.
- The issue timeline and notifications already react to explicit change flags such as `due_date_changed`, so schedule date changes should follow the same event pattern.

## Goals / Non-Goals

**Goals:**

- Add nullable `start_date` and `end_date` fields to the issue model and persist them in the main issue table.
- Expose the new fields in issue create, read, update, and list flows through the backend API.
- Allow clients to set, change, and clear the fields independently while still validating an invalid range when both values are present.
- Surface the new fields in issue creation and issue detail workflows in both frontend apps.
- Publish explicit schedule-date change metadata so realtime consumers, activity logging, and notifications remain consistent with other issue field changes.

**Non-Goals:**

- Automatic duration calculations, time reporting, or roll-up analytics.
- New list filtering, sorting, or board-layout behavior based on `start_date` or `end_date`.
- A broader redesign of `due_date` timezone handling.
- Dedicated CLI authoring UX for the new fields in this change.

## Decisions

### 1. Store `start_date` and `end_date` as nullable `TIMESTAMPTZ` columns on `issue`

The backend will add two nullable columns directly to the existing `issue` table and extend the issue SQL queries to read and write them alongside `due_date`.

Why this approach:

- It matches the established representation for `due_date`, which keeps the issue date contract internally consistent.
- It avoids a second schedule table or JSON payload, which would add unnecessary joins or ad hoc parsing for a simple pair of fields.
- It keeps sqlc generation straightforward because the issue row shape remains the single source of truth.

Alternatives considered:

- Use `DATE` columns instead of `TIMESTAMPTZ`: rejected for now because it would introduce a second date contract next to `due_date`.
- Store schedule dates in a separate metadata table: rejected because the fields are core issue attributes, not optional extensions.

### 2. Mirror existing issue API semantics, with server-side range validation

`CreateIssueRequest` and `UpdateIssueRequest` will accept `start_date` and `end_date` as optional RFC3339 strings, with update requests also supporting explicit `null` to clear either field. Validation will happen on the server after merging the new values with the previous issue state, so the server can reject a schedule where both dates are present and `start_date` is later than `end_date`.

Why this approach:

- It preserves the current API contract style, which lowers migration risk for the handlers and frontend clients.
- It keeps clear semantics deterministic: omitted means unchanged, `null` means clear, and a string means set.
- It prevents clients from saving an inverted date window.

Alternatives considered:

- Allow any ordering and leave interpretation to clients: rejected because it weakens the usefulness of the new fields as a schedule window.
- Require both dates together: rejected because teams often know only one side of the window at a given time.

### 3. Reuse the existing date-picker pattern in both frontend apps

The current `DueDatePicker` behavior will be generalized so the same interaction model can update `due_date`, `start_date`, or `end_date`. Both `apps/web` and `apps/workspace` will add the new controls to issue creation and issue detail properties, while keeping the initial scope focused on create/detail flows instead of redesigning list and board surfaces.

Why this approach:

- It keeps interaction consistent with the date control users already have.
- It avoids copying nearly identical picker implementations for each field.
- It limits the UI scope to the flows where users actively set or inspect issue metadata.

Alternatives considered:

- Build separate dedicated pickers for start and end dates: rejected because the behavior is identical apart from label and target field.
- Expose the new fields only in issue detail: rejected because users also need to seed the values when creating a new issue.

### 4. Extend the existing `issue:updated` change metadata instead of adding a new event type

Issue update payloads will add `start_date_changed`, `end_date_changed`, `prev_start_date`, and `prev_end_date`, and downstream listeners will use those fields to create activity and notification entries that mirror the existing `due_date_changed` flow.

Why this approach:

- It preserves the current event topology and keeps consumers on one update stream.
- It lets listeners apply the same pattern they already use for `due_date` without introducing a generic diff parser.
- It keeps schedule date changes visible in activity history.

Alternatives considered:

- Add a new event just for schedule updates: rejected because it fragments issue update handling.
- Emit only the final issue object and let listeners infer diffs: rejected because current listeners already depend on explicit change flags.

## Risks / Trade-offs

- [Timezone drift remains aligned with `due_date`] -> Mitigation: keep the storage and API contract identical to `due_date` in this change, and treat date-normalization as a separate follow-up if needed.
- [Cross-app drift between `apps/web` and `apps/workspace`] -> Mitigation: implement and test both apps in the same change, using mirrored tasks.
- [Range validation could break partial edits if implemented before merge semantics] -> Mitigation: validate against the effective post-update values, not just the request payload.
- [Missing listener updates would make schedule changes invisible in activity history] -> Mitigation: include explicit change flags and add listener coverage for date-change events.

## Migration Plan

1. Add nullable `start_date` and `end_date` columns with an additive migration.
2. Extend SQL queries and regenerate sqlc artifacts.
3. Update handler request parsing, response serialization, and schedule-range validation.
4. Extend issue update events plus activity/notification listeners.
5. Update mirrored frontend types and create/detail UI in `apps/web` and `apps/workspace`.

Deployment can be backend-first because the new columns are nullable and older clients will ignore the additional response fields. If rollback is required after users have started populating the new columns, prefer rolling back application code and forward-fixing the schema because dropping the columns would discard captured schedule data.

## Open Questions

None for this change.