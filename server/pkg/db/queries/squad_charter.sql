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

-- name: ListRoutineComplianceFacts :many
-- The facts the compliance derivation reads, one row per routine, optionally for
-- one squad (a NULL @squad_id means the whole workspace).
--
-- The decider is the routine's latest run that finished: completed, failed, or
-- skipped for a reason other than a policy hold (quota_exceeded, already_active).
-- Runs still in flight and policy skips do not decide the state. The evidence
-- flags look at the decider's window, which starts when it was triggered; they
-- are false when there is no decider, because the comparison with a NULL start
-- is never true.
--
-- Columns the derivation reads only when present are COALESCEd to a non-NULL
-- value, because sqlc types lateral-join columns as NOT NULL.
SELECT
    r.autopilot_id,
    r.squad_id,
    r.purpose,
    r.expected_output,
    r.required,
    r.grace_hours,
    a.title AS autopilot_title,
    a.status AS autopilot_status,
    (s.id IS NOT NULL)::boolean AS has_schedule,
    COALESCE(s.cron_expression, '')::text AS schedule_cron_expression,
    COALESCE(s.timezone, 'UTC')::text AS schedule_timezone,
    -- Overdue fires are counted from the last planned fire, the same point the
    -- scheduler scans from; a schedule that has never fired counts from its creation.
    COALESCE(
        (SELECT max(p.planned_at) FROM autopilot_run p WHERE p.autopilot_id = r.autopilot_id),
        s.created_at,
        a.created_at
    )::timestamptz AS schedule_baseline,
    (d.id IS NOT NULL)::boolean AS has_decider,
    COALESCE(d.status, '')::text AS decider_status,
    COALESCE(d.triggered_at, a.created_at)::timestamptz AS decider_triggered_at,
    (d.issue_id IS NOT NULL)::boolean AS decider_has_issue,
    EXISTS (
        SELECT 1 FROM proposal p
        WHERE p.workspace_id = r.workspace_id
          AND p.squad_id = r.squad_id
          AND p.created_at >= d.triggered_at
    ) AS has_proposal,
    EXISTS (
        SELECT 1 FROM knowledge_link kl
        WHERE kl.workspace_id = r.workspace_id
          AND kl.created_by_type = 'agent'
          AND kl.created_by_id IN (
              SELECT sq.leader_id FROM squad sq WHERE sq.id = r.squad_id
              UNION
              SELECT sm.member_id FROM squad_member sm
              WHERE sm.squad_id = r.squad_id AND sm.member_type = 'agent'
          )
          AND kl.created_at >= d.triggered_at
          AND kl.state <> 'failed'
    ) AS has_kb_document,
    EXISTS (
        SELECT 1 FROM comment c
        WHERE c.issue_id = d.issue_id
          AND c.type = 'comment'
          AND c.author_type = 'agent'
          AND c.author_id IN (
              SELECT sq.leader_id FROM squad sq WHERE sq.id = r.squad_id
              UNION
              SELECT sm.member_id FROM squad_member sm
              WHERE sm.squad_id = r.squad_id AND sm.member_type = 'agent'
          )
          AND c.created_at >= d.triggered_at
    ) AS has_report,
    -- A squad leader's recorded no_action with a reason, on the run's issue. The
    -- reason is required: a bare no_action is padding, not an honest decision.
    EXISTS (
        SELECT 1 FROM activity_log al
        WHERE al.workspace_id = r.workspace_id
          AND al.issue_id = d.issue_id
          AND al.action = 'squad_leader_evaluated'
          AND al.details->>'outcome' = 'no_action'
          AND btrim(COALESCE(al.details->>'reason', '')) <> ''
          AND al.created_at >= d.triggered_at
    ) AS has_no_action
FROM squad_routine r
JOIN autopilot a ON a.id = r.autopilot_id
LEFT JOIN LATERAL (
    SELECT t.id, t.cron_expression, t.timezone, t.created_at
    FROM autopilot_trigger t
    WHERE t.autopilot_id = r.autopilot_id AND t.kind = 'schedule' AND t.enabled
    ORDER BY t.created_at ASC, t.id ASC
    LIMIT 1
) s ON true
LEFT JOIN LATERAL (
    SELECT run.id, run.status, run.triggered_at, run.issue_id
    FROM autopilot_run run
    WHERE run.autopilot_id = r.autopilot_id
      AND (
          run.status IN ('completed', 'failed')
          OR (run.status = 'skipped'
              AND COALESCE(run.reason_code, '') NOT IN ('quota_exceeded', 'already_active'))
      )
    ORDER BY run.created_at DESC, run.id DESC
    LIMIT 1
) d ON true
WHERE r.workspace_id = sqlc.arg('workspace_id')::uuid
  AND (sqlc.narg('squad_id')::uuid IS NULL OR r.squad_id = sqlc.narg('squad_id')::uuid)
ORDER BY r.created_at ASC, r.autopilot_id ASC;
