-- name: CreateRepoBinding :one
INSERT INTO workspace_repo_binding (
    workspace_id, repo_full_name, installation_id, cr_bot_username
) VALUES ($1, $2, $3, COALESCE(sqlc.narg('cr_bot_username')::text, 'coderabbitai[bot]'))
RETURNING *;

-- name: GetRepoBinding :one
SELECT * FROM workspace_repo_binding
WHERE id = $1;

-- name: GetRepoBindingByRepo :one
SELECT * FROM workspace_repo_binding
WHERE repo_full_name = $1 AND active = true;

-- name: ListRepoBindingsForWorkspace :many
SELECT * FROM workspace_repo_binding
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: UpdateRepoBindingInstallation :one
UPDATE workspace_repo_binding SET
    installation_id = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetRepoBindingActive :one
UPDATE workspace_repo_binding SET
    active = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteRepoBinding :exec
DELETE FROM workspace_repo_binding
WHERE id = $1 AND workspace_id = $2;
