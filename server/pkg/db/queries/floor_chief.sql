-- Floor chiefs (spec 4.8): the agent that publishes a floor's daily brief. One
-- chief per floor. Every query filters by workspace_id.

-- name: ListFloorChiefs :many
SELECT * FROM floor_chief
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY floor_id;

-- name: GetFloorChief :one
SELECT * FROM floor_chief
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND floor_id = sqlc.arg('floor_id')::uuid;

-- name: ListFloorsChairedByAgent :many
SELECT floor_id FROM floor_chief
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND agent_id = sqlc.arg('agent_id')::uuid;

-- name: UpsertFloorChief :one
-- A new chief forgets the previous chief's routine: the caller pauses that one
-- and the next SetFloorChiefBriefAutopilot records the new chief's.
INSERT INTO floor_chief (workspace_id, floor_id, agent_id, created_by)
VALUES (sqlc.arg('workspace_id')::uuid, sqlc.arg('floor_id')::uuid, sqlc.arg('agent_id')::uuid, sqlc.narg('created_by')::uuid)
ON CONFLICT (workspace_id, floor_id)
DO UPDATE SET
    brief_autopilot_id = CASE WHEN floor_chief.agent_id = EXCLUDED.agent_id THEN floor_chief.brief_autopilot_id ELSE NULL END,
    agent_id = EXCLUDED.agent_id,
    updated_at = now()
RETURNING *;

-- name: SetFloorChiefBriefAutopilot :one
UPDATE floor_chief SET brief_autopilot_id = sqlc.arg('brief_autopilot_id')::uuid, updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND floor_id = sqlc.arg('floor_id')::uuid
RETURNING *;

-- name: DeleteFloorChief :exec
DELETE FROM floor_chief
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND floor_id = sqlc.arg('floor_id')::uuid;

-- name: DeleteWorkspaceFloorChiefs :exec
-- Workspace teardown: floor chiefs have no foreign keys.
DELETE FROM floor_chief WHERE workspace_id = $1;

-- name: CreateScopedBrief :one
-- A brief with a scope. The company brief keeps using CreateBrief.
INSERT INTO brief (
    id, workspace_id, kind, period_start, period_end, title, body, proposal_ids,
    created_by_agent_id, created_by_member_id, scope_type, scope_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
) RETURNING *;

-- name: ListLatestFloorBriefs :many
-- The newest brief of each floor published since the given time.
SELECT DISTINCT ON (scope_id) *
FROM brief
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND scope_type = 'floor'
  AND created_at >= sqlc.arg('since')::timestamptz
ORDER BY scope_id, created_at DESC, id DESC;

-- name: ListFloorBriefsForFloor :many
SELECT * FROM brief
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND scope_type = 'floor'
  AND scope_id = sqlc.arg('scope_id')::uuid
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit')::int;

-- name: CountLiveSquads :one
SELECT count(*)::int FROM squad
WHERE workspace_id = sqlc.arg('workspace_id')::uuid AND archived_at IS NULL;

-- name: GetWorkspaceDigestMode :one
SELECT digest_mode FROM workspace WHERE id = sqlc.arg('workspace_id')::uuid;

-- name: SetWorkspaceDigestMode :one
UPDATE workspace SET digest_mode = sqlc.arg('digest_mode')::text
WHERE id = sqlc.arg('workspace_id')::uuid
RETURNING digest_mode;

-- name: GetCompanyFloorByID :one
SELECT id, name FROM company_floor
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: ListSquadsOnFloor :many
-- Live squads on a floor, for the floor-scoped digest.
SELECT id FROM squad
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND floor_id = sqlc.arg('floor_id')::uuid
  AND archived_at IS NULL;
