-- Retrospective triggers (spec 4.7). Every query filters by workspace_id. The
-- trigger rows are append-only history of what fired; the evidence readers
-- below read the sources of truth (initiatives, proposals, task queue, the
-- governance log, the initiative timeline), never a copy of them.

-- name: LatestRetroTriggerForSubject :one
-- The newest firing of one subject. subject_id is NULL for the company.
SELECT * FROM retro_trigger
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND trigger_kind = sqlc.arg('trigger_kind')::text
  AND subject_type = sqlc.arg('subject_type')::text
  AND subject_id IS NOT DISTINCT FROM sqlc.narg('subject_id')::uuid
ORDER BY fired_at DESC
LIMIT 1;

-- name: InsertRetroTrigger :one
INSERT INTO retro_trigger (id, workspace_id, trigger_kind, subject_type, subject_id, issue_id, evidence, fired_at)
VALUES (
    sqlc.arg('id')::uuid,
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('trigger_kind')::text,
    sqlc.arg('subject_type')::text,
    sqlc.narg('subject_id')::uuid,
    sqlc.narg('issue_id')::uuid,
    sqlc.arg('evidence')::jsonb,
    sqlc.arg('fired_at')::timestamptz
)
RETURNING *;

-- name: ListRetroTriggersInWorkspace :many
-- Newest first. Used by the job's tests and by an admin's read of what fired.
SELECT * FROM retro_trigger
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY fired_at DESC, id DESC
LIMIT sqlc.arg('row_limit')::int;

-- name: ListInitiativesScaledSince :many
-- Initiatives that scaled a follow-on at an operate review at or after @since
-- (spec 3.6). Scaling leaves the initiative operating, so its retrospective
-- is not a closed one.
SELECT i.* FROM initiative i
WHERE i.workspace_id = sqlc.arg('workspace_id')::uuid
  AND EXISTS (
      SELECT 1 FROM initiative_stage_event e
      WHERE e.workspace_id = i.workspace_id
        AND e.initiative_id = i.id
        AND e.kind = 'scaled'
        AND e.created_at >= sqlc.arg('since')::timestamptz
  )
ORDER BY i.id;

-- name: ListInitiativesClosedSince :many
SELECT * FROM initiative
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND status = 'closed'
  AND closed_at >= sqlc.arg('since')::timestamptz
ORDER BY closed_at, id;

-- name: ListRejectedLaunchGates :many
-- Rejected launch gates of the workspace since the given time: stage_gate
-- proposals whose target stage is launch. The initiative is in the payload.
SELECT (p.payload->>'initiative_id')::text AS initiative_id, p.decided_at
FROM proposal p
WHERE p.workspace_id = sqlc.arg('workspace_id')::uuid
  AND p.kind = 'stage_gate'
  AND p.state = 'rejected'
  AND p.payload->>'to' = 'launch'
  AND p.decided_at >= sqlc.arg('since')::timestamptz
ORDER BY p.decided_at, p.id;

-- name: ListAgentFailureClusters :many
-- Agents with at least row_min failed runs of one failure category since the
-- given time. A runtime's failures are not the agent's (spec 4.6): every
-- failure_reason starting with runtime is left out.
SELECT t.agent_id, t.failure_reason::text AS failure_reason, count(*)::int AS failures
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')::uuid
  AND t.status = 'failed'
  AND t.completed_at >= sqlc.arg('since')::timestamptz
  AND t.failure_reason IS NOT NULL
  AND t.failure_reason <> ''
  AND t.failure_reason NOT LIKE 'runtime%'
GROUP BY t.agent_id, t.failure_reason
HAVING count(*) >= sqlc.arg('row_min')::int
ORDER BY t.agent_id, t.failure_reason;

-- name: ListSquadRecentDecisions :many
-- The three newest decided proposals of every squad. The caller checks that
-- they are all rejections (spec 4.7: three rejections in a row).
SELECT squad_id, state, decided_at
FROM (
    SELECT p.squad_id, p.state, p.decided_at,
           row_number() OVER (PARTITION BY p.squad_id ORDER BY p.decided_at DESC, p.id DESC) AS rn
    FROM proposal p
    WHERE p.workspace_id = sqlc.arg('workspace_id')::uuid
      AND p.state IN ('approved', 'rejected')
      AND p.decided_at IS NOT NULL
) recent
WHERE rn <= 3
ORDER BY squad_id, decided_at DESC;

-- name: ListBudgetHoldsSince :many
-- Budget holds recorded since the given time, read from the budget_hold table
-- (migration 800) and joined to the budget for its scope, which is the subject a
-- retrospective is about. A hold is one row per held task.
SELECT b.scope_type AS subject_type, b.scope_id AS subject_id, h.created_at
FROM budget_hold h
JOIN budget b ON b.id = h.budget_id AND b.workspace_id = h.workspace_id
WHERE h.workspace_id = sqlc.arg('workspace_id')::uuid
  AND h.created_at >= sqlc.arg('since')::timestamptz
ORDER BY h.created_at, h.id;

-- name: ListInitiativeStageEntriesSince :many
-- The stage entries of the workspace's initiatives since the given time, oldest
-- first per initiative. The SLA check walks each initiative's entries in order.
SELECT initiative_id, to_stage, created_at
FROM initiative_stage_event
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND to_stage IS NOT NULL
  AND kind IN ('created', 'advanced', 'sent_back', 'forced')
  AND created_at >= sqlc.arg('since')::timestamptz
ORDER BY initiative_id, created_at, id;

-- name: GetSquadLeaderForRetro :one
-- The leader of a live squad, and the squad's name for the issue title.
SELECT id, name, leader_id FROM squad
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND archived_at IS NULL;

-- name: DeleteWorkspaceRetroTriggers :exec
-- Workspace teardown: the trigger history has no foreign keys.
DELETE FROM retro_trigger WHERE workspace_id = $1;

-- name: ListRetroWorkspaceIDs :many
-- Every workspace, for the trigger pass.
SELECT id FROM workspace ORDER BY created_at ASC, id ASC;

-- name: InsertRetroRequest :exec
-- The durable follow-up a close writes in its own transaction (spec 4.7). The
-- retrospective run consumes it, so a close outside the run's window still fires.
INSERT INTO retro_request (id, workspace_id, trigger_kind, subject_id)
VALUES (sqlc.arg('id')::uuid, sqlc.arg('workspace_id')::uuid, sqlc.arg('trigger_kind')::text, sqlc.arg('subject_id')::uuid);

-- name: ListPendingRetroRequests :many
SELECT id, subject_id, trigger_kind FROM retro_request
WHERE workspace_id = sqlc.arg('workspace_id')::uuid AND processed_at IS NULL
ORDER BY requested_at, id;

-- name: MarkRetroRequestProcessed :exec
UPDATE retro_request SET processed_at = now()
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: DeleteWorkspaceRetroRequests :exec
-- Workspace teardown only: a request has no foreign key, so teardown removes it.
DELETE FROM retro_request WHERE workspace_id = $1;
