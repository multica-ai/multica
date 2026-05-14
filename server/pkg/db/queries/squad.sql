-- name: CreateSquad :one
INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSquad :one
SELECT * FROM squad WHERE id = $1;

-- name: GetSquadInWorkspace :one
SELECT * FROM squad WHERE id = $1 AND workspace_id = $2;

-- name: ListSquads :many
SELECT * FROM squad WHERE workspace_id = $1 AND archived_at IS NULL ORDER BY created_at ASC;

-- name: ListAllSquads :many
SELECT * FROM squad WHERE workspace_id = $1 ORDER BY created_at ASC;

-- name: UpdateSquad :one
UPDATE squad SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    leader_id = COALESCE(sqlc.narg('leader_id'), leader_id),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    instructions = COALESCE(sqlc.narg('instructions'), instructions),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveSquad :one
UPDATE squad SET archived_at = now(), archived_by = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: AddSquadMember :one
INSERT INTO squad_member (squad_id, member_type, member_id, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: RemoveSquadMember :execrows
DELETE FROM squad_member
WHERE squad_id = $1 AND member_type = $2 AND member_id = $3;

-- name: ListSquadMembers :many
SELECT * FROM squad_member WHERE squad_id = $1 ORDER BY created_at ASC;

-- name: UpdateSquadMemberRole :one
UPDATE squad_member SET role = $4
WHERE squad_id = $1 AND member_type = $2 AND member_id = $3
RETURNING *;

-- name: IsSquadMember :one
SELECT EXISTS(
    SELECT 1 FROM squad_member
    WHERE squad_id = $1 AND member_type = $2 AND member_id = $3
) AS is_member;

-- name: CountSquadMembers :one
SELECT count(*) FROM squad_member WHERE squad_id = $1;

-- name: GetSquadByAssignee :one
-- Look up the squad when an issue is assigned to a squad.
SELECT s.* FROM squad s WHERE s.id = $1 AND s.workspace_id = $2;

-- name: ListSquadsByMember :many
-- Find all squads a given entity belongs to in a workspace.
SELECT s.* FROM squad s
JOIN squad_member sm ON sm.squad_id = s.id
WHERE s.workspace_id = $1 AND sm.member_type = $2 AND sm.member_id = $3
ORDER BY s.created_at ASC;

-- name: TransferSquadAssignees :exec
-- Transfer all issues assigned to a squad to the squad's leader agent.
UPDATE issue SET assignee_type = 'agent', assignee_id = $2, updated_at = now()
WHERE assignee_type = 'squad' AND assignee_id = $1;
