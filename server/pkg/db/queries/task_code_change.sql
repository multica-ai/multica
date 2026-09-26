-- name: CreateTaskCodeChange :one
-- A run's code change (MUL-7651). First write wins: a daemon that retries an upload whose response it lost
-- sends the same diff again, and the conflict leaves the stored row alone.
-- Returns no row on conflict, so the caller can drop the patch it just stored.
INSERT INTO task_code_change (
    id, workspace_id, issue_id, task_id, agent_id, scope, source,
    repo_key, repo_label, repo_url, branch, base_ref, base_commit, head_commit,
    file_count, additions, deletions, files, files_truncated,
    patch_url, patch_size, patch_omitted
) VALUES (
    sqlc.arg(id), sqlc.arg(workspace_id), sqlc.arg(issue_id), sqlc.arg(task_id), sqlc.arg(agent_id),
    sqlc.arg(scope), sqlc.arg(source),
    sqlc.arg(repo_key), sqlc.arg(repo_label), sqlc.arg(repo_url), sqlc.arg(branch), sqlc.arg(base_ref),
    sqlc.arg(base_commit), sqlc.arg(head_commit),
    sqlc.arg(file_count), sqlc.arg(additions), sqlc.arg(deletions), sqlc.arg(files), sqlc.arg(files_truncated),
    sqlc.narg(patch_url), sqlc.arg(patch_size), sqlc.narg(patch_omitted)
)
ON CONFLICT (task_id, scope, repo_key) DO NOTHING
RETURNING *;

-- name: ListTaskCodeChangesByIssue :many
-- The issue page's summary: everything but the file list, which can run to
-- thousands of entries per row and is only needed once a change is opened.
SELECT id, workspace_id, issue_id, task_id, agent_id, scope, source,
       repo_key, repo_label, repo_url, branch, base_ref, base_commit, head_commit,
       file_count, additions, deletions, files_truncated,
       (patch_url IS NOT NULL)::boolean AS patch_available, patch_size, patch_omitted,
       created_at
FROM task_code_change
WHERE issue_id = sqlc.arg(issue_id) AND workspace_id = sqlc.arg(workspace_id)
ORDER BY created_at ASC, id ASC;

-- name: GetTaskCodeChange :one
SELECT * FROM task_code_change
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);

-- name: DeleteTaskCodeChangesByIssue :many
-- Part of the issue delete transaction; returns the stored patches so they can
-- be removed from object storage after commit.
DELETE FROM task_code_change
WHERE issue_id = sqlc.arg(issue_id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING patch_url;

-- name: ListTaskCodeChangePatchURLsByWorkspace :many
SELECT patch_url::text FROM task_code_change
WHERE workspace_id = sqlc.arg(workspace_id) AND patch_url IS NOT NULL
ORDER BY id;
