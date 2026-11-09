-- name: ListWorkspaceLinks :many
SELECT * FROM workspace_link
WHERE workspace_id = $1 AND project_id IS NOT DISTINCT FROM sqlc.narg('project_id')::uuid
ORDER BY position, created_at, id;

-- name: GetWorkspaceLink :one
SELECT * FROM workspace_link WHERE id = $1 AND workspace_id = $2;

-- name: GetWorkspaceLinkForUpdate :one
-- Lock the scoped row until the caller's transaction ends so partial updates
-- merge against the latest committed values instead of overwriting each other.
SELECT * FROM workspace_link
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: CountWorkspaceLinks :one
SELECT count(*) FROM workspace_link
WHERE workspace_id = $1 AND project_id IS NOT DISTINCT FROM sqlc.narg('project_id')::uuid;

-- name: CreateWorkspaceLink :one
INSERT INTO workspace_link (workspace_id, project_id, name, icon, url_template, open_mode, allow_same_origin, position, created_by, show_name)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: UpdateWorkspaceLink :one
UPDATE workspace_link SET name=$3, icon=$4, url_template=$5, open_mode=$6, allow_same_origin=$7, position=$8, show_name=$9, updated_at=now()
WHERE id=$1 AND workspace_id=$2 RETURNING *;

-- name: DeleteWorkspaceLink :one
DELETE FROM workspace_link WHERE id=$1 AND workspace_id=$2 RETURNING id;

-- name: DeleteProjectWorkspaceLinks :exec
DELETE FROM workspace_link WHERE workspace_id=$1 AND project_id=$2;
