-- A squad's charter and its routines. Every query filters by workspace_id;
-- there are no foreign keys, so squad and autopilot links are validated by the
-- handlers, and deleting an autopilot unlinks its routine in the same transaction.

-- name: GetSquadCharter :one
SELECT * FROM squad_charter
WHERE squad_id = $1 AND workspace_id = $2;

-- name: InsertSquadCharter :one
-- The first save. A row that already exists is left alone (no rows returned),
-- so a concurrent first save surfaces as a revision conflict instead of an
-- overwrite.
INSERT INTO squad_charter (
    squad_id, workspace_id, mission, responsibilities, outputs, boundaries,
    escalation, intake, response_hours, operating_review_enabled,
    updated_by_type, updated_by_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
ON CONFLICT (squad_id) DO NOTHING
RETURNING *;

-- name: UpdateSquadCharter :one
-- Replaces the charter only while the stored revision is the one the client read.
-- No rows back means the revision moved on, and the handler reports a conflict.
UPDATE squad_charter SET
    mission = $3,
    responsibilities = $4,
    outputs = $5,
    boundaries = $6,
    escalation = $7,
    intake = $8,
    response_hours = $9,
    -- Optional: an omitted flag keeps its stored value, since the operating review
    -- is switched on and off apart from the charter's text.
    operating_review_enabled = COALESCE(sqlc.narg('operating_review_enabled')::boolean, operating_review_enabled),
    revision = revision + 1,
    updated_by_type = $10,
    updated_by_id = $11,
    updated_at = now()
WHERE squad_id = $1 AND workspace_id = $2 AND revision = $12
RETURNING *;

-- name: DeleteWorkspaceSquadCharters :exec
-- Workspace teardown: charters have no foreign keys to cascade.
DELETE FROM squad_charter WHERE workspace_id = $1;

-- name: ListSquadRoutines :many
-- A squad's routines with the linked autopilot's schedule and its most recent
-- run. Compliance state is derived elsewhere; this only reports the facts.
SELECT
    r.autopilot_id,
    r.workspace_id,
    r.squad_id,
    r.purpose,
    r.expected_output,
    r.required,
    r.grace_hours,
    r.created_at,
    a.title AS autopilot_title,
    a.status AS autopilot_status,
    s.cron_expression AS schedule_cron_expression,
    s.timezone AS schedule_timezone,
    s.next_run_at AS schedule_next_run_at,
    lr.id AS last_run_id,
    -- sqlc types a lateral-join column as NOT NULL, so an autopilot with no run
    -- yet is reported as '' here; last_run_id is the signal that a run exists.
    COALESCE(lr.status, '')::text AS last_run_status,
    COALESCE(lr.source, '')::text AS last_run_source,
    lr.triggered_at AS last_run_triggered_at,
    lr.completed_at AS last_run_completed_at
FROM squad_routine r
JOIN autopilot a ON a.id = r.autopilot_id
LEFT JOIN LATERAL (
    SELECT t.cron_expression, t.timezone, t.next_run_at
    FROM autopilot_trigger t
    WHERE t.autopilot_id = r.autopilot_id AND t.kind = 'schedule' AND t.enabled
    ORDER BY t.created_at ASC, t.id ASC
    LIMIT 1
) s ON true
LEFT JOIN LATERAL (
    SELECT run.id, run.status, run.source, run.triggered_at, run.completed_at
    FROM autopilot_run run
    WHERE run.autopilot_id = r.autopilot_id
    ORDER BY run.created_at DESC, run.id DESC
    LIMIT 1
) lr ON true
WHERE r.workspace_id = $1 AND r.squad_id = $2
ORDER BY r.created_at ASC, r.autopilot_id ASC;

-- name: GetSquadRoutine :one
SELECT * FROM squad_routine
WHERE autopilot_id = $1 AND squad_id = $2 AND workspace_id = $3;

-- name: InsertSquadRoutine :one
-- A routine links an autopilot at most once. A conflict returns no rows.
INSERT INTO squad_routine (
    autopilot_id, workspace_id, squad_id, purpose, expected_output, required, grace_hours
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (autopilot_id) DO NOTHING
RETURNING *;

-- name: DeleteSquadRoutine :execrows
DELETE FROM squad_routine
WHERE autopilot_id = $1 AND squad_id = $2 AND workspace_id = $3;

-- name: DeleteSquadRoutinesForAutopilot :execrows
-- Run in the same transaction as an autopilot's archive, so a routine never
-- outlives the autopilot it points at.
DELETE FROM squad_routine WHERE autopilot_id = $1 AND workspace_id = $2;

-- name: DeleteWorkspaceSquadRoutines :exec
-- Workspace teardown: routines have no foreign keys to cascade.
DELETE FROM squad_routine WHERE workspace_id = $1;
