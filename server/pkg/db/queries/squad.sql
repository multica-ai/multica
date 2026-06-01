-- name: CreateSquad :one
INSERT INTO multica_squad (workspace_id, name, description, leader_id, creator_id, avatar_url)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetSquad :one
SELECT * FROM multica_squad WHERE id = $1;

-- name: GetSquadInWorkspace :one
SELECT * FROM multica_squad WHERE id = $1 AND workspace_id = $2;

-- name: ListSquads :many
SELECT * FROM multica_squad WHERE workspace_id = $1 AND archived_at IS NULL ORDER BY created_at ASC;

-- name: ListAllSquads :many
SELECT * FROM multica_squad WHERE workspace_id = $1 ORDER BY created_at ASC;

-- name: UpdateSquad :one
UPDATE multica_squad SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    leader_id = COALESCE(sqlc.narg('leader_id'), leader_id),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    instructions = COALESCE(sqlc.narg('instructions'), instructions),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveSquad :one
UPDATE multica_squad SET archived_at = now(), archived_by = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: AddSquadMember :one
INSERT INTO multica_squad_member (squad_id, member_type, member_id, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: RemoveSquadMember :execrows
DELETE FROM multica_squad_member
WHERE squad_id = $1 AND member_type = $2 AND member_id = $3;

-- name: ListSquadMembers :many
SELECT * FROM multica_squad_member WHERE squad_id = $1 ORDER BY created_at ASC;

-- name: UpdateSquadMemberRole :one
UPDATE multica_squad_member SET role = $4
WHERE squad_id = $1 AND member_type = $2 AND member_id = $3
RETURNING *;

-- name: IsSquadMember :one
SELECT EXISTS(
    SELECT 1 FROM multica_squad_member
    WHERE squad_id = $1 AND member_type = $2 AND member_id = $3
) AS is_member;

-- name: CountSquadMembers :one
SELECT count(*) FROM multica_squad_member WHERE squad_id = $1;

-- name: GetSquadByAssignee :one
-- Look up the multica_squad when an multica_issue is assigned to a multica_squad.
SELECT s.* FROM multica_squad s WHERE s.id = $1 AND s.workspace_id = $2;

-- name: ListSquadsByMember :many
-- Find all squads a given entity belongs to in a multica_workspace.
SELECT s.* FROM multica_squad s
JOIN multica_squad_member sm ON sm.squad_id = s.id
WHERE s.workspace_id = $1 AND sm.member_type = $2 AND sm.member_id = $3
ORDER BY s.created_at ASC;

-- name: TransferSquadAssignees :exec
-- Transfer all issues assigned to a multica_squad to the multica_squad's leader multica_agent.
UPDATE multica_issue SET assignee_type = 'agent', assignee_id = $2, updated_at = now()
WHERE assignee_type = 'squad' AND assignee_id = $1;

-- name: TransferSquadAutopilotsToLeader :exec
-- Mirrors TransferSquadAssignees for multica_autopilot rows: when a multica_squad is archived,
-- any multica_autopilot still pointing at the multica_squad would otherwise dangle and the
-- admission gate would skip every subsequent dispatch with "assignee multica_squad
-- cannot be resolved". Rewrite the assignee in place to the leader multica_agent so
-- the multica_autopilot keeps firing under the same leader-only execution semantics
-- it had a moment before the archive (Path A from MUL-2429).
UPDATE multica_autopilot
SET assignee_type = 'agent',
    assignee_id = $2,
    updated_at = now()
WHERE assignee_type = 'squad' AND assignee_id = $1;

-- name: ListSquadMemberStatusRows :many
-- Per-row join used to build the multica_squad-members status view. One row per
-- (multica_squad_member × active_task); members with no active task return a
-- single row with NULL task_* columns. Human members and multica_agent members
-- with no multica_agent row also return one row with NULL agent_/runtime_ columns.
-- The handler aggregates rows by member_id.
SELECT
    sm.id              AS squad_member_id,
    sm.member_type     AS member_type,
    sm.member_id       AS member_id,
    a.archived_at      AS agent_archived_at,
    ar.status          AS runtime_status,
    ar.last_seen_at    AS runtime_last_seen_at,
    atq.id             AS task_id,
    atq.status         AS task_status,
    atq.issue_id       AS task_issue_id,
    atq.dispatched_at  AS task_dispatched_at,
    i.number           AS issue_number,
    i.title            AS issue_title,
    i.status           AS issue_status
FROM multica_squad_member sm
LEFT JOIN multica_agent a
       ON sm.member_type = 'agent' AND a.id = sm.member_id
LEFT JOIN multica_agent_runtime ar
       ON ar.id = a.runtime_id
LEFT JOIN multica_agent_task_queue atq
       ON sm.member_type = 'agent'
      AND atq.agent_id = sm.member_id
      AND atq.status IN ('dispatched', 'running')
LEFT JOIN multica_issue i
       ON i.id = atq.issue_id
WHERE sm.squad_id = $1
ORDER BY sm.created_at ASC, atq.dispatched_at DESC NULLS LAST;
