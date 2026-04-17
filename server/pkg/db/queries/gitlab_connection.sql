-- name: CreateWorkspaceGitlabConnection :one
INSERT INTO workspace_gitlab_connection (
    workspace_id,
    gitlab_project_id,
    gitlab_project_path,
    service_token_encrypted,
    service_token_user_id,
    connection_status
)
VALUES ($1, $2, $3, $4, $5, 'connected')
RETURNING *;

-- name: GetWorkspaceGitlabConnection :one
SELECT * FROM workspace_gitlab_connection
WHERE workspace_id = $1;

-- name: DeleteWorkspaceGitlabConnection :exec
DELETE FROM workspace_gitlab_connection
WHERE workspace_id = $1;

-- name: UpsertUserGitlabConnection :one
INSERT INTO user_gitlab_connection (
    user_id,
    workspace_id,
    gitlab_user_id,
    gitlab_username,
    pat_encrypted
)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, workspace_id) DO UPDATE SET
    gitlab_user_id = EXCLUDED.gitlab_user_id,
    gitlab_username = EXCLUDED.gitlab_username,
    pat_encrypted = EXCLUDED.pat_encrypted
RETURNING *;

-- name: GetUserGitlabConnection :one
SELECT * FROM user_gitlab_connection
WHERE user_id = $1 AND workspace_id = $2;

-- name: DeleteUserGitlabConnection :exec
DELETE FROM user_gitlab_connection
WHERE user_id = $1 AND workspace_id = $2;

-- name: UpdateWorkspaceGitlabConnectionStatus :exec
UPDATE workspace_gitlab_connection
SET connection_status = $2,
    status_message    = $3,
    updated_at        = now()
WHERE workspace_id = $1;
