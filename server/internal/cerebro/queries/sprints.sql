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
       move_incomplete_enabled, move_incomplete_target_status, timezone,
       created_at, updated_at,
       accepts_external_issues
FROM cerebro_sprint_settings
WHERE project_id = $1;

-- name: ListCerebroSprintSettingsByWorkspace :many
SELECT project_id, workspace_id, enabled,
       duration_unit, duration_count, start_weekday,
       name_template,
       auto_create_enabled, auto_create_lead_days,
       move_incomplete_enabled, move_incomplete_target_status, timezone,
       created_at, updated_at,
       accepts_external_issues
FROM cerebro_sprint_settings
WHERE workspace_id = $1;

-- name: UpsertCerebroSprintSettings :one
INSERT INTO cerebro_sprint_settings (
    project_id, workspace_id, enabled,
    duration_unit, duration_count, start_weekday,
    name_template,
    auto_create_enabled, auto_create_lead_days,
    move_incomplete_enabled, move_incomplete_target_status, timezone,
    accepts_external_issues
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (project_id) DO UPDATE
SET enabled                 = EXCLUDED.enabled,
    duration_unit           = EXCLUDED.duration_unit,
    duration_count          = EXCLUDED.duration_count,
    start_weekday           = EXCLUDED.start_weekday,
    name_template           = EXCLUDED.name_template,
    auto_create_enabled     = EXCLUDED.auto_create_enabled,
    auto_create_lead_days   = EXCLUDED.auto_create_lead_days,
    move_incomplete_enabled = EXCLUDED.move_incomplete_enabled,
    move_incomplete_target_status = EXCLUDED.move_incomplete_target_status,
    timezone                = EXCLUDED.timezone,
    accepts_external_issues = EXCLUDED.accepts_external_issues,
    updated_at              = now()
RETURNING project_id, workspace_id, enabled,
          duration_unit, duration_count, start_weekday,
          name_template,
          auto_create_enabled, auto_create_lead_days,
          move_incomplete_enabled, move_incomplete_target_status, timezone,
          created_at, updated_at,
          accepts_external_issues;

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
       move_incomplete_enabled, move_incomplete_target_status, timezone
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

-- name: ListCerebroSprintsByWorkspace :many
-- FIR-2500: workspace-wide sprint listing so the CLI can find sprints (for
-- example the active one) without knowing the owning project up front.
-- $1 = workspace_id; sqlc.narg('status') optionally filters by sprint status.
SELECT s.id, s.workspace_id, s.project_id, s.name, s.sequence_no, s.status,
       s.start_date, s.end_date, s.goal, s.created_at, s.updated_at,
       p.title AS project_title
FROM cerebro_sprint s
JOIN project p ON p.id = s.project_id
WHERE s.workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR s.status = sqlc.narg('status')::text)
ORDER BY (s.status = 'active') DESC, p.title ASC, s.sequence_no DESC;

-- name: ListSelectableCerebroSprintsForIssue :many
-- FIR-1657: the sprints an issue may be assigned to. Always the issue's own
-- project sprints, PLUS sprints of any other project in the same workspace
-- whose settings opted in (enabled AND accepts_external_issues). Each row
-- carries the owning project's title so the picker can group by project, and
-- a flag marking the issue's home project. $1 = workspace_id, $2 = the
-- issue's project_id (may be NULL for an issue with no project).
SELECT s.id, s.workspace_id, s.project_id, s.name, s.sequence_no, s.status,
       s.start_date, s.end_date, s.goal, s.created_at, s.updated_at,
       p.title AS project_title,
       (s.project_id = $2) AS is_own_project
FROM cerebro_sprint s
JOIN project p ON p.id = s.project_id
LEFT JOIN cerebro_sprint_settings st ON st.project_id = s.project_id
WHERE s.workspace_id = $1
  AND (
        s.project_id = $2
        OR (st.enabled = TRUE AND st.accepts_external_issues = TRUE)
      )
ORDER BY (s.project_id = $2) DESC, p.title ASC, s.sequence_no DESC;

-- name: GetCerebroIssueProjectID :one
-- The project a given issue belongs to. Used to scope sprint selection and
-- validate cross-project assignment. project_id is nullable upstream.
SELECT project_id FROM issue WHERE id = $1;

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

-- name: ListCerebroSprintIssueDetailsBySprint :many
-- FIR-2500: sprint overview for the CLI — each issue in the sprint joined
-- with its upstream title/status so an agent gets the full picture in one
-- call instead of N follow-up issue lookups.
SELECT csi.issue_id, csi.sprint_id, csi.added_at,
       i.number, i.title, i.status, i.priority,
       i.assignee_type, i.assignee_id
FROM cerebro_sprint_issue csi
JOIN issue i ON i.id = csi.issue_id
WHERE csi.sprint_id = $1
ORDER BY i.number ASC;

-- name: ListIncompleteIssuesInCerebroSprint :many
-- Sweeper helper. Returns the upstream issue rows still in a non-terminal
-- status that belong to the given sprint, so the sweeper can move them to
-- the next sprint. Terminal statuses (done, cancelled) are excluded.
SELECT i.id
FROM cerebro_sprint_issue csi
JOIN issue i ON i.id = csi.issue_id
WHERE csi.sprint_id = $1
  AND i.status NOT IN ('done', 'cancelled');

-- name: MoveIncompleteCerebroSprintIssuesToStatus :exec
UPDATE issue
SET status = $2,
    updated_at = now()
WHERE id = ANY($1::uuid[]);

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
    sprint_day_offset,
    enabled
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id, workspace_id, project_id,
          cadence_unit, cadence_count,
          title, description, priority,
          assignee_type, assignee_id,
          sprint_day_offset,
          enabled, created_at, updated_at;

-- name: GetCerebroSprintRecurringTask :one
SELECT id, workspace_id, project_id,
       cadence_unit, cadence_count,
       title, description, priority,
       assignee_type, assignee_id,
       sprint_day_offset,
       enabled, created_at, updated_at
FROM cerebro_sprint_recurring_task
WHERE id = $1;

-- name: ListCerebroSprintRecurringTasksByProject :many
SELECT id, workspace_id, project_id,
       cadence_unit, cadence_count,
       title, description, priority,
       assignee_type, assignee_id,
       sprint_day_offset,
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
       sprint_day_offset,
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
    sprint_day_offset = $9,
    enabled       = $10,
    updated_at    = now()
WHERE id = $1
RETURNING id, workspace_id, project_id,
          cadence_unit, cadence_count,
          title, description, priority,
          assignee_type, assignee_id,
          sprint_day_offset,
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
