-- name: ListUserInfisicalFolders :many
SELECT id, workspace_id, user_id, environment, secret_path, created_at, updated_at
FROM cerebro_user_infisical_folder
WHERE workspace_id = $1 AND user_id = $2
ORDER BY environment, secret_path;

-- name: DeleteUserInfisicalFolders :exec
DELETE FROM cerebro_user_infisical_folder
WHERE workspace_id = $1 AND user_id = $2;

-- name: InsertUserInfisicalFolder :one
INSERT INTO cerebro_user_infisical_folder (
    workspace_id,
    user_id,
    environment,
    secret_path
) VALUES (
    $1,
    $2,
    $3,
    $4
)
ON CONFLICT (workspace_id, user_id, environment, secret_path)
DO UPDATE SET updated_at = now()
RETURNING id, workspace_id, user_id, environment, secret_path, created_at, updated_at;
