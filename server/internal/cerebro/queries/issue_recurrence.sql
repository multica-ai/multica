-- TECH-3064 / FIR-334: recurring issues. All queries live in the cerebro
-- queries package so they generate into cerebrodb alongside the other
-- cerebro-only tables. A few queries read/write the upstream issue,
-- attachment and issue_to_label tables (same pattern as the sprint sweeper)
-- — that is intentional and stays out of the upstream issue schema.

-- ===========================================================================
-- cerebro_issue_recurrence — CRUD
-- ===========================================================================

-- name: CreateCerebroIssueRecurrence :one
INSERT INTO cerebro_issue_recurrence (
    workspace_id, project_id, source_issue_id,
    frequency, interval_count, weekdays, days_after,
    trigger_status, anchor,
    create_new_issue, new_status,
    recur_forever, end_date, max_occurrences,
    armed, enabled,
    trigger_type, stale_handling, stale_status, date_format, next_run_at,
    created_by_type, created_by_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
        $17, $18, $19, $20, $21, $22, $23)
RETURNING id, workspace_id, project_id, source_issue_id,
          frequency, interval_count, weekdays, days_after,
          trigger_status, anchor,
          create_new_issue, new_status,
          recur_forever, end_date, max_occurrences, occurrence_count,
          armed, enabled,
          created_by_type, created_by_id,
          created_at, updated_at,
          trigger_type, stale_handling, stale_status, date_format, next_run_at;

-- name: GetCerebroIssueRecurrence :one
SELECT id, workspace_id, project_id, source_issue_id,
       frequency, interval_count, weekdays, days_after,
       trigger_status, anchor,
       create_new_issue, new_status,
       recur_forever, end_date, max_occurrences, occurrence_count,
       armed, enabled,
       created_by_type, created_by_id,
       created_at, updated_at,
       trigger_type, stale_handling, stale_status, date_format, next_run_at
FROM cerebro_issue_recurrence
WHERE id = $1;

-- name: GetCerebroIssueRecurrenceBySourceIssue :one
SELECT id, workspace_id, project_id, source_issue_id,
       frequency, interval_count, weekdays, days_after,
       trigger_status, anchor,
       create_new_issue, new_status,
       recur_forever, end_date, max_occurrences, occurrence_count,
       armed, enabled,
       created_by_type, created_by_id,
       created_at, updated_at,
       trigger_type, stale_handling, stale_status, date_format, next_run_at
FROM cerebro_issue_recurrence
WHERE source_issue_id = $1;

-- name: ListEnabledCerebroIssueRecurrences :many
-- Sweeper hot path: every enabled rule across all workspaces. The sweeper
-- filters by feature flag (per workspace) and inspects the source issue in
-- Go, keeping this query trivially indexable.
SELECT id, workspace_id, project_id, source_issue_id,
       frequency, interval_count, weekdays, days_after,
       trigger_status, anchor,
       create_new_issue, new_status,
       recur_forever, end_date, max_occurrences, occurrence_count,
       armed, enabled,
       created_by_type, created_by_id,
       created_at, updated_at,
       trigger_type, stale_handling, stale_status, date_format, next_run_at
FROM cerebro_issue_recurrence
WHERE enabled = TRUE;

-- name: UpdateCerebroIssueRecurrence :one
-- Full config update from the issue-side panel.
UPDATE cerebro_issue_recurrence
SET frequency        = $2,
    interval_count   = $3,
    weekdays         = $4,
    days_after       = $5,
    trigger_status   = $6,
    anchor           = $7,
    create_new_issue = $8,
    new_status       = $9,
    recur_forever    = $10,
    end_date         = $11,
    max_occurrences  = $12,
    enabled          = $13,
    trigger_type     = $14,
    stale_handling   = $15,
    stale_status     = $16,
    date_format      = $17,
    next_run_at      = $18,
    updated_at       = now()
WHERE id = $1
RETURNING id, workspace_id, project_id, source_issue_id,
          frequency, interval_count, weekdays, days_after,
          trigger_status, anchor,
          create_new_issue, new_status,
          recur_forever, end_date, max_occurrences, occurrence_count,
          armed, enabled,
          created_by_type, created_by_id,
          created_at, updated_at,
          trigger_type, stale_handling, stale_status, date_format, next_run_at;

-- name: SetCerebroIssueRecurrenceArmed :exec
UPDATE cerebro_issue_recurrence
SET armed = $2, updated_at = now()
WHERE id = $1;

-- name: AdvanceCerebroIssueRecurrenceAfterFire :exec
-- Applied after a successful spawn: repoint to the (possibly new) source
-- issue, disarm until it re-opens, bump the occurrence counter, carry the
-- enabled flag (set FALSE when a stop condition was reached), and advance the
-- schedule clock (NULL for completion rules, which leave next_run_at unused).
UPDATE cerebro_issue_recurrence
SET source_issue_id  = $2,
    occurrence_count = $3,
    armed            = $4,
    enabled          = $5,
    next_run_at      = $6,
    updated_at       = now()
WHERE id = $1;

-- name: GetLatestSpawnedIssueForRecurrence :one
-- Most recently spawned occurrence for a rule, used by schedule-based
-- auto_close to find the previous issue to close. Skips reopen rows
-- (spawned_issue_id IS NULL).
SELECT spawned_issue_id
FROM cerebro_issue_recurrence_log
WHERE recurrence_id = $1 AND spawned_issue_id IS NOT NULL
ORDER BY occurrence_number DESC
LIMIT 1;

-- name: DeleteCerebroIssueRecurrence :exec
DELETE FROM cerebro_issue_recurrence WHERE id = $1;

-- name: InsertCerebroIssueRecurrenceLog :exec
INSERT INTO cerebro_issue_recurrence_log (
    recurrence_id, trigger_issue_id, spawned_issue_id, occurrence_number
)
VALUES ($1, $2, $3, $4);

-- ===========================================================================
-- feature flag + issue-copy helpers (read upstream tables)
-- ===========================================================================

-- name: GetCerebroRecurringIssuesFlagForWorkspace :one
-- Workspace-level value of the cerebro_recurring_issues flag. ErrNoRows
-- means never set → default OFF → the sweeper skips the workspace.
SELECT enabled FROM cerebro_feature_flags
WHERE workspace_id = $1
  AND user_id = '00000000-0000-0000-0000-000000000000'
  AND flag_key = 'cerebro_recurring_issues';

-- name: ReopenCerebroRecurringIssue :exec
-- Reopen path (create_new_issue=FALSE): move the same issue into its new
-- status with a bumped due date instead of spawning a fresh issue.
UPDATE issue
SET status     = $2,
    due_date   = $3,
    updated_at = now()
WHERE id = $1;

-- name: CloseCerebroRecurringIssueStatus :exec
-- Schedule-based auto_close: move a still-open previous occurrence into the
-- configured stale_status (e.g. 'cancelled') without touching its due date.
UPDATE issue
SET status     = $2,
    updated_at = now()
WHERE id = $1;

-- name: CopyCerebroIssueLabels :exec
-- Copy every label from the source issue onto the target issue.
INSERT INTO issue_to_label (issue_id, label_id)
SELECT $1, src.label_id FROM issue_to_label AS src WHERE src.issue_id = $2
ON CONFLICT (issue_id, label_id) DO NOTHING;

-- name: CopyCerebroIssueAttachments :exec
-- Clone the source issue's attachment rows onto the target issue. They
-- reference the same uploaded files (same url) — we only duplicate the row.
INSERT INTO attachment (
    workspace_id, issue_id, uploader_type, uploader_id,
    filename, url, content_type, size_bytes
)
SELECT src.workspace_id, $1, src.uploader_type, src.uploader_id,
       src.filename, src.url, src.content_type, src.size_bytes
FROM attachment AS src
WHERE src.issue_id = $2;
