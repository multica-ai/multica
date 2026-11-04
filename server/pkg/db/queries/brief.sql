-- The Secretary's briefs. Every query filters by workspace_id; there are no
-- foreign keys.

-- name: CreateBrief :one
INSERT INTO brief (
    id, workspace_id, kind, period_start, period_end, title, body, proposal_ids,
    created_by_agent_id, created_by_member_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING *;

-- name: GetBriefInWorkspace :one
SELECT * FROM brief WHERE id = $1 AND workspace_id = $2;

-- name: ListBriefs :many
-- Newest first. kind is an optional filter. Only company briefs: a floor's
-- brief is read through ListFloorBriefsForFloor, so the boss's list is not
-- crowded by floor reports (spec 4.8).
SELECT * FROM brief
WHERE workspace_id = sqlc.arg('workspace_id')
  AND scope_type = 'company'
  AND (sqlc.narg('kind')::text IS NULL OR kind = sqlc.narg('kind')::text)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit')::int;

-- name: ListWorkspaceOwnerUserIDs :many
-- The boss: workspace owners only (admins can publish and decide, but the
-- brief announcement goes to owners).
SELECT user_id FROM member
WHERE workspace_id = $1 AND role = 'owner'
ORDER BY created_at ASC;

-- name: ListProposalIDsInWorkspace :many
-- Which of the given proposal ids exist in the workspace.
SELECT id FROM proposal
WHERE workspace_id = sqlc.arg('workspace_id') AND id = ANY(sqlc.arg('ids')::uuid[]);

-- name: DeleteWorkspaceBriefs :exec
-- Workspace teardown: briefs have no foreign keys to cascade.
DELETE FROM brief WHERE brief.workspace_id = $1;
