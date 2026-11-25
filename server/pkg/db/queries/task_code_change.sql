-- name: UpsertTaskCodeChange :exec
-- One snapshot per task, written when the run ends. A re-report for the same
-- task replaces the previous one.
INSERT INTO task_code_change (task_id, workspace_id, issue_id, repos, repos_truncated)
VALUES (@task_id, @workspace_id, sqlc.narg(issue_id), @repos, @repos_truncated)
ON CONFLICT (task_id) DO UPDATE
SET repos = EXCLUDED.repos,
    repos_truncated = EXCLUDED.repos_truncated,
    updated_at = now();

-- name: GetTaskCodeChange :one
SELECT task_id, repos, repos_truncated, updated_at
FROM task_code_change
WHERE task_id = $1;
