-- name: CreateGitLabConnection :one
INSERT INTO gitlab_connection (
    workspace_id, namespace, namespace_type, avatar_url, access_token,
    token_expires_at, connected_by_id
) VALUES (
    $1, $2, $3, sqlc.narg('avatar_url'), $4,
    sqlc.narg('token_expires_at'), sqlc.narg('connected_by_id')
)
ON CONFLICT (workspace_id, namespace) DO UPDATE SET
    namespace_type   = EXCLUDED.namespace_type,
    avatar_url       = EXCLUDED.avatar_url,
    access_token     = EXCLUDED.access_token,
    token_expires_at = EXCLUDED.token_expires_at,
    connected_by_id  = EXCLUDED.connected_by_id,
    updated_at       = now()
RETURNING *;

-- name: ListGitLabConnectionsByWorkspace :many
SELECT * FROM gitlab_connection
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetGitLabConnectionByID :one
SELECT * FROM gitlab_connection WHERE id = $1;

-- name: GetGitLabConnectionByNamespace :one
SELECT * FROM gitlab_connection
WHERE workspace_id = $1 AND namespace = $2;

-- name: GetGitLabConnectionByNamespaceGlobal :one
-- Used by the webhook handler to resolve workspace from project namespace
-- without knowing the workspace ID upfront.
-- ponytail: full-table scan on namespace; add index if connection count grows large.
SELECT * FROM gitlab_connection WHERE namespace = $1 LIMIT 1;

-- name: DeleteGitLabConnection :exec
DELETE FROM gitlab_connection WHERE id = $1 AND workspace_id = $2;

-- name: GetFirstGitLabConnectionByWorkspace :one
SELECT * FROM gitlab_connection WHERE workspace_id = $1 LIMIT 1;

-- name: UpsertGitLabMergeRequest :one
INSERT INTO gitlab_merge_request (
    workspace_id, connection_id, project_path, mr_iid,
    title, state, html_url, source_branch,
    author_username, author_avatar_url,
    merged_at, closed_at, mr_created_at, mr_updated_at
) VALUES (
    $1, $2, $3, $4,
    $5, $6, $7, sqlc.narg('source_branch'),
    sqlc.narg('author_username'), sqlc.narg('author_avatar_url'),
    sqlc.narg('merged_at'), sqlc.narg('closed_at'), $8, $9
)
ON CONFLICT (workspace_id, project_path, mr_iid) DO UPDATE SET
    connection_id    = EXCLUDED.connection_id,
    title            = EXCLUDED.title,
    state            = EXCLUDED.state,
    html_url         = EXCLUDED.html_url,
    source_branch    = EXCLUDED.source_branch,
    author_username  = EXCLUDED.author_username,
    author_avatar_url = EXCLUDED.author_avatar_url,
    merged_at        = EXCLUDED.merged_at,
    closed_at        = EXCLUDED.closed_at,
    mr_updated_at    = EXCLUDED.mr_updated_at,
    updated_at       = now()
RETURNING *;

-- name: GetGitLabMergeRequest :one
SELECT * FROM gitlab_merge_request
WHERE workspace_id = $1 AND project_path = $2 AND mr_iid = $3;

-- name: ListMergeRequestsByIssue :many
SELECT mr.*
FROM gitlab_merge_request mr
JOIN issue_merge_request imr ON imr.merge_request_id = mr.id
WHERE imr.issue_id = $1
ORDER BY mr.mr_created_at DESC;

-- name: LinkIssueToMergeRequest :exec
INSERT INTO issue_merge_request (issue_id, merge_request_id, close_intent)
VALUES ($1, $2, $3)
ON CONFLICT (issue_id, merge_request_id) DO UPDATE SET
    close_intent = EXCLUDED.close_intent;

-- name: ListIssueIDsForMergeRequest :many
SELECT issue_id FROM issue_merge_request WHERE merge_request_id = $1;

-- name: GetIssueMergeRequestCloseAggregate :one
SELECT
    COALESCE(SUM(CASE WHEN mr.state IN ('open') THEN 1 ELSE 0 END), 0)::bigint AS open_count,
    COALESCE(SUM(CASE WHEN mr.state = 'merged' AND imr.close_intent THEN 1 ELSE 0 END), 0)::bigint AS merged_with_close_intent_count
FROM gitlab_merge_request mr
JOIN issue_merge_request imr ON imr.merge_request_id = mr.id
WHERE imr.issue_id = $1;
