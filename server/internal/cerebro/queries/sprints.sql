-- FIR-2666: cerebro project sprint feature. All queries live in the cerebro
-- queries package so they generate into cerebrodb alongside the other
-- cerebro-only tables.

-- ===========================================================================
-- cerebro_sprint_settings
-- ===========================================================================

-- name: GetCerebroSprintSettings :one
SELECT project_id, workspace_id, enabled,
       duration_unit, duration_count, start_weekday,
       name_template,
       auto_create_enabled, auto_create_lead_days,
       move_incomplete_enabled, timezone,
       created_at, updated_at
FROM cerebro_sprint_settings
WHERE project_id = $1;

-- name: ListCerebroSprintSettingsByWorkspace :many
SELECT project_id, workspace_id, enabled,
       duration_unit, duration_count, start_weekday,
       name_template,
       auto_create_enabled, auto_create_lead_days,
       move_incomplete_enabled, timezone,
       created_at, updated_at
FROM cerebro_sprint_settings
WHERE workspace_id = $1;

-- name: UpsertCerebroSprintSettings :one
INSERT INTO cerebro_sprint_settings (
    project_id, workspace_id, enabled,
    duration_unit, duration_count, start_weekday,
    name_template,
    auto_create_enabled, auto_create_lead_days,
    move_incomplete_enabled, timezone
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (project_id) DO UPDATE
SET enabled                 = EXCLUDED.enabled,
    duration_unit           = EXCLUDED.duration_unit,
    duration_count          = EXCLUDED.duration_count,
    start_weekday           = EXCLUDED.start_weekday,
    name_template           = EXCLUDED.name_template,
    auto_create_enabled     = EXCLUDED.auto_create_enabled,
    auto_create_lead_days   = EXCLUDED.auto_create_lead_days,
    move_incomplete_enabled = EXCLUDED.move_incomplete_enabled,
    timezone                = EXCLUDED.timezone,
    updated_at              = now()
RETURNING project_id, workspace_id, enabled,
          duration_unit, duration_count, start_weekday,
          name_template,
          auto_create_enabled, auto_create_lead_days,
          move_incomplete_enabled, timezone,
          created_at, updated_at;

-- name: DeleteCerebroSprintSettings :exec
DELETE FROM cerebro_sprint_settings WHERE project_id = $1;

-- name: ListCerebroSprintSettingsForAutoCreate :many
-- Sweeper hot path: every settings row that opted into auto-create AND
-- whose project still has the feature enabled. The sweeper filters further
-- in Go (active-sprint lookup, lead-days window) — keeping that out of SQL
-- keeps the query trivially indexable.
SELECT project_id, workspace_id, enabled,
       duration_unit, duration_count, start_weekday,
       name_template,
       auto_create_enabled, auto_create_lead_days,
       move_incomplete_enabled, timezone
FROM cerebro_sprint_settings
WHERE enabled = TRUE
  AND auto_create_enabled = TRUE;

-- ===========================================================================
-- cerebro_sprint
-- ===========================================================================

-- name: CreateCerebroSprint :one
INSERT INTO cerebro_sprint (
    workspace_id, project_id, name, sequence_no, status,
    start_date, end_date, goal
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, workspace_id, project_id, name, sequence_no, status,
          start_date, end_date, goal, created_at, updated_at;

-- name: GetCerebroSprint :one
SELECT id, workspace_id, project_id, name, sequence_no, status,
       start_date, end_date, goal, created_at, updated_at
FROM cerebro_sprint
WHERE id = $1;

-- name: ListCerebroSprintsByProject :many
SELECT id, workspace_id, project_id, name, sequence_no, status,
       start_date, end_date, goal, created_at, updated_at
FROM cerebro_sprint
WHERE project_id = $1
ORDER BY sequence_no DESC;

-- name: GetActiveCerebroSprintByProject :one
SELECT id, workspace_id, project_id, name, sequence_no, status,
       start_date, end_date, goal, created_at, updated_at
FROM cerebro_sprint
WHERE project_id = $1 AND status = 'active'
ORDER BY start_date DESC
LIMIT 1;

-- name: GetLatestCerebroSprintByProject :one
-- Highest sequence_no — used by the sweeper to decide whether a "next"
-- sprint already exists and to seed the new sprint's sequence_no.
SELECT id, workspace_id, project_id, name, sequence_no, status,
       start_date, end_date, goal, created_at, updated_at
FROM cerebro_sprint
WHERE project_id = $1
ORDER BY sequence_no DESC
LIMIT 1;

-- name: UpdateCerebroSprint :one
UPDATE cerebro_sprint
SET name       = $2,
    status     = $3,
    start_date = $4,
    end_date   = $5,
    goal       = $6,
    updated_at = now()
WHERE id = $1
RETURNING id, workspace_id, project_id, name, sequence_no, status,
          start_date, end_date, goal, created_at, updated_at;

-- name: SetCerebroSprintStatus :exec
UPDATE cerebro_sprint
SET status     = $2,
    updated_at = now()
WHERE id = $1;

-- name: DeleteCerebroSprint :exec
DELETE FROM cerebro_sprint WHERE id = $1;

-- name: ListExpiredActiveCerebroSprints :many
-- Sweeper helper: every active sprint whose end_date is on or before the
-- given cutoff. The sweeper marks them done.
SELECT id, workspace_id, project_id, name, sequence_no, status,
       start_date, end_date, goal, created_at, updated_at
FROM cerebro_sprint
WHERE status = 'active' AND end_date <= $1;

-- ===========================================================================
-- cerebro_sprint_issue
-- ===========================================================================

-- name: AssignIssueToCerebroSprint :exec
INSERT INTO cerebro_sprint_issue (issue_id, sprint_id)
VALUES ($1, $2)
ON CONFLICT (issue_id) DO UPDATE
SET sprint_id = EXCLUDED.sprint_id,
    added_at  = now();

-- name: RemoveIssueFromCerebroSprint :exec
DELETE FROM cerebro_sprint_issue WHERE issue_id = $1;

-- name: GetCerebroSprintForIssue :one
SELECT issue_id, sprint_id, added_at
FROM cerebro_sprint_issue
WHERE issue_id = $1;

-- name: ListCerebroSprintIssuesBySprint :many
SELECT issue_id, sprint_id, added_at
FROM cerebro_sprint_issue
WHERE sprint_id = $1
ORDER BY added_at ASC;

-- name: ListIncompleteIssuesInCerebroSprint :many
-- Sweeper helper. Returns the upstream issue rows still in a non-terminal
-- status that belong to the given sprint, so the sweeper can move them to
-- the next sprint. Terminal statuses (done, cancelled) are excluded.
SELECT i.id
FROM cerebro_sprint_issue csi
JOIN issue i ON i.id = csi.issue_id
WHERE csi.sprint_id = $1
  AND i.status NOT IN ('done', 'cancelled');

-- name: MoveCerebroSprintIssuesBatch :exec
-- Sweeper helper: bulk reassign the given issue IDs to the new sprint.
UPDATE cerebro_sprint_issue
SET sprint_id = $1,
    added_at  = now()
WHERE issue_id = ANY($2::uuid[]);

-- ===========================================================================
-- cerebro_sprint_recurring_task
-- ===========================================================================

-- name: CreateCerebroSprintRecurringTask :one
INSERT INTO cerebro_sprint_recurring_task (
    workspace_id, project_id,
    cadence_unit, cadence_count,
    title, description, priority,
    assignee_type, assignee_id,
    enabled
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, workspace_id, project_id,
          cadence_unit, cadence_count,
          title, description, priority,
          assignee_type, assignee_id,
          enabled, created_at, updated_at;

-- name: GetCerebroSprintRecurringTask :one
SELECT id, workspace_id, project_id,
       cadence_unit, cadence_count,
       title, description, priority,
       assignee_type, assignee_id,
       enabled, created_at, updated_at
FROM cerebro_sprint_recurring_task
WHERE id = $1;

-- name: ListCerebroSprintRecurringTasksByProject :many
SELECT id, workspace_id, project_id,
       cadence_unit, cadence_count,
       title, description, priority,
       assignee_type, assignee_id,
       enabled, created_at, updated_at
FROM cerebro_sprint_recurring_task
WHERE project_id = $1
ORDER BY created_at ASC;

-- name: ListCerebroSprintRecurringTasksForCadence :many
-- Sweeper helper: enabled templates whose cadence matches the new sprint's
-- (duration_unit, duration_count). These get cloned into the new sprint.
SELECT id, workspace_id, project_id,
       cadence_unit, cadence_count,
       title, description, priority,
       assignee_type, assignee_id,
       enabled, created_at, updated_at
FROM cerebro_sprint_recurring_task
WHERE project_id = $1
  AND enabled = TRUE
  AND cadence_unit = $2
  AND cadence_count = $3;

-- name: UpdateCerebroSprintRecurringTask :one
UPDATE cerebro_sprint_recurring_task
SET cadence_unit  = $2,
    cadence_count = $3,
    title         = $4,
    description   = $5,
    priority      = $6,
    assignee_type = $7,
    assignee_id   = $8,
    enabled       = $9,
    updated_at    = now()
WHERE id = $1
RETURNING id, workspace_id, project_id,
          cadence_unit, cadence_count,
          title, description, priority,
          assignee_type, assignee_id,
          enabled, created_at, updated_at;

-- name: DeleteCerebroSprintRecurringTask :exec
DELETE FROM cerebro_sprint_recurring_task WHERE id = $1;

-- ===========================================================================
-- sweeper helpers
-- ===========================================================================

-- name: GetCerebroSprintsFlagForWorkspace :one
-- Returns the workspace-level value of the cerebro_sprints feature flag.
-- pgx.ErrNoRows means the flag has never been set and the default (OFF)
-- applies — the sweeper treats that as "skip this workspace".
SELECT enabled FROM cerebro_feature_flags
WHERE workspace_id = $1
  AND user_id = '00000000-0000-0000-0000-000000000000'
  AND flag_key = 'cerebro_sprints';

-- name: GetCerebroSprintRecurringIssueCreator :one
-- Returns a stable user_id to use as creator on issues the sweeper clones
-- from a recurring task. The sweeper runs without a user session, so we
-- attribute creation to the oldest workspace owner. Falls back to any
-- admin if no owner exists.
SELECT user_id FROM member
WHERE workspace_id = $1
  AND role IN ('owner', 'admin')
ORDER BY (role = 'owner') DESC, created_at ASC
LIMIT 1;
